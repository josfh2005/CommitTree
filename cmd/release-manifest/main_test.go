package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

// setup writes the three assets (content = file name) and a wails.json with
// the given version into a temp dir.
func setup(t *testing.T, version string, skip string) (dir, wails string) {
	t.Helper()
	dir = t.TempDir()
	for _, name := range assetFiles {
		if name == skip {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte("content of "+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	wails = filepath.Join(t.TempDir(), "wails.json")
	body := `{"name":"CommitTree","info":{"productVersion":"` + version + `"}}`
	if err := os.WriteFile(wails, []byte(body), 0o644); err != nil {
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
		if want := sum([]byte("content of " + name)); a.SHA256 != want {
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
