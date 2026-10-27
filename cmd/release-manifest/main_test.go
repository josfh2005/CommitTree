package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git-ui/internal/update"
)

var assetFiles = map[string]string{
	"darwin-arm64": "CommitTree-macos-arm64.zip",
	"darwin-amd64": "CommitTree-macos-amd64.zip",
	"linux-amd64":  "CommitTree-linux-amd64.tar.gz",
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// assetBody is what an asset holds: for a mac zip a real zip with an Info.plist
// of the given version, for the rest "content of <name>".
func assetBody(t *testing.T, name, version string) []byte {
	t.Helper()
	if !strings.HasSuffix(name, ".zip") {
		return []byte("content of " + name)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("CommitTree.app/Contents/Info.plist")
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte(`<?xml version="1.0"?><plist><dict><key>CFBundleShortVersionString</key><string>` + version + `</string></dict></plist>`))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// setup writes the three assets (fresh, mac zips holding an Info.plist of the
// given version) and a wails.json with that version an hour older, into temp
// dirs.
func setup(t *testing.T, version string, skip string) (dir, wails string) {
	t.Helper()
	dir = t.TempDir()
	for _, name := range assetFiles {
		if name == skip {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), assetBody(t, name, version), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	wails = filepath.Join(t.TempDir(), "wails.json")
	body := `{"name":"CommitTree","info":{"productVersion":"` + version + `"}}`
	if err := os.WriteFile(wails, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(wails, old, old); err != nil {
		t.Fatal(err)
	}
	return dir, wails
}

func TestBuildWritesManifest(t *testing.T) {
	dir, wails := setup(t, "0.10.0", "")
	m, err := build(dir, wails)
	if err != nil {
		t.Fatal(err)
	}
	if m.Version != "0.10.0" {
		t.Errorf("version = %q", m.Version)
	}
	if want := "https://github.com/josfh2005/CommitTree/releases/tag/v0.10.0"; m.URL != want {
		t.Errorf("url = %q, want %q", m.URL, want)
	}
	if len(m.Assets) != 3 {
		t.Fatalf("assets = %v", m.Assets)
	}
	for key, name := range assetFiles {
		a, ok := m.Assets[key]
		if !ok {
			t.Fatalf("missing %s", key)
		}
		if a.Name != name {
			t.Errorf("%s name = %q, want %q", key, a.Name, name)
		}
		if want := sum(assetBody(t, name, "0.10.0")); a.SHA256 != want {
			t.Errorf("%s sha256 = %q, want %q", key, a.SHA256, want)
		}
	}
}

func TestBuildWritesLatestJSON(t *testing.T) {
	dir, wails := setup(t, "0.10.0", "")
	if _, err := build(dir, wails); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "latest.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.HasSuffix(s, "}\n") {
		t.Errorf("no trailing newline: %q", s)
	}
	if !strings.Contains(s, "\n  \"version\": \"0.10.0\"") {
		t.Errorf("not indented: %q", s)
	}
	for key, name := range assetFiles {
		rel, err := update.ParseManifest(data, key, "", false)
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		if rel.AssetName != name || rel.Version != "0.10.0" || rel.AssetSHA256 == "" {
			t.Errorf("%s: release = %+v", key, rel)
		}
	}
}

func TestBuildMissingAsset(t *testing.T) {
	dir, wails := setup(t, "0.10.0", "CommitTree-macos-amd64.zip")
	_, err := build(dir, wails)
	if err == nil || !strings.Contains(err.Error(), "CommitTree-macos-amd64.zip") {
		t.Fatalf("err = %v, want it to name the missing file", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "latest.json")); statErr == nil {
		t.Error("latest.json written despite the error")
	}
}

func TestBuildBadVersion(t *testing.T) {
	for _, v := range []string{"0.10", "v0.10.0", "0.10.0-beta", ""} {
		dir, wails := setup(t, v, "")
		if _, err := build(dir, wails); err == nil {
			t.Errorf("version %q: want an error", v)
		}
	}
}

func TestBuildMissingWails(t *testing.T) {
	dir, _ := setup(t, "0.10.0", "")
	if _, err := build(dir, filepath.Join(dir, "nope.json")); err == nil {
		t.Error("want an error")
	}
}

func TestBuildRefusesAssetOlderThanWails(t *testing.T) {
	for _, stale := range []string{"CommitTree-macos-arm64.zip", "CommitTree-linux-amd64.tar.gz"} {
		dir, wails := setup(t, "0.10.0", "")
		old := time.Now().Add(-2 * time.Hour)
		if err := os.Chtimes(filepath.Join(dir, stale), old, old); err != nil {
			t.Fatal(err)
		}
		_, err := build(dir, wails)
		if err == nil || !strings.Contains(err.Error(), stale) || !strings.Contains(err.Error(), "older") {
			t.Fatalf("%s: err = %v, want it to name the stale file", stale, err)
		}
		if _, statErr := os.Stat(filepath.Join(dir, "latest.json")); statErr == nil {
			t.Error("latest.json written despite the error")
		}
	}
}

func TestBuildRefusesMacZipOfAnotherVersion(t *testing.T) {
	dir, wails := setup(t, "0.10.0", "")
	name := "CommitTree-macos-amd64.zip"
	if err := os.WriteFile(filepath.Join(dir, name), assetBody(t, name, "0.9.0"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := build(dir, wails)
	if err == nil || !strings.Contains(err.Error(), name) || !strings.Contains(err.Error(), "0.9.0") {
		t.Fatalf("err = %v, want it to name the file and its version", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "latest.json")); statErr == nil {
		t.Error("latest.json written despite the error")
	}
}

func TestBuildRefusesMacZipWithoutInfoPlist(t *testing.T) {
	dir, wails := setup(t, "0.10.0", "")
	name := "CommitTree-macos-arm64.zip"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := build(dir, wails); err == nil || !strings.Contains(err.Error(), name) {
		t.Fatalf("err = %v, want it to name %s", err, name)
	}
}
