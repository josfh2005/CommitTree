// Command release-manifest writes latest.json, the manifest the application's
// update check reads, next to the release assets. See docs/spec/15-updates.md.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"git-ui/internal/update"
)

// assets are the files a release carries, keyed by GOOS-GOARCH.
var assets = map[string]string{
	"darwin-arm64": "CommitTree-macos-arm64.zip",
	"darwin-amd64": "CommitTree-macos-amd64.zip",
	"linux-amd64":  "CommitTree-linux-amd64.tar.gz",
}

func main() {
	dir := flag.String("dir", "build/bin", "folder holding the release assets; latest.json is written here")
	wails := flag.String("wails", "wails.json", "wails.json to read info.productVersion from")
	flag.Parse()
	m, err := build(*dir, *wails)
	if err != nil {
		fmt.Fprintln(os.Stderr, "release-manifest:", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s for v%s\n", filepath.Join(*dir, "latest.json"), m.Version)
}

// build reads the version from wailsPath, hashes the three assets in dir and
// writes dir/latest.json. Nothing is written if anything is missing or wrong.
func build(dir, wailsPath string) (update.Manifest, error) {
	raw, err := os.ReadFile(wailsPath)
	if err != nil {
		return update.Manifest{}, err
	}
	var w struct {
		Info struct {
			ProductVersion string `json:"productVersion"`
		} `json:"info"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		return update.Manifest{}, fmt.Errorf("%s: %w", wailsPath, err)
	}
	v, err := update.ParseVersion(w.Info.ProductVersion)
	if err != nil {
		return update.Manifest{}, fmt.Errorf("%s: info.productVersion: %w", wailsPath, err)
	}
	m := update.Manifest{
		Version: v.String(),
		URL:     "https://github.com/" + update.Repo + "/releases/tag/v" + v.String(),
		Assets:  map[string]update.Asset{},
	}
	for key, name := range assets {
		sum, err := fileSHA256(filepath.Join(dir, name))
		if err != nil {
			return update.Manifest{}, fmt.Errorf("asset %s: %w", name, err)
		}
		m.Assets[key] = update.Asset{Name: name, SHA256: sum}
	}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return update.Manifest{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "latest.json"), append(out, '\n'), 0o644); err != nil {
		return update.Manifest{}, err
	}
	return m, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
