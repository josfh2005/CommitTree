package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

const (
	// Repo is the GitHub repository releases are published in.
	Repo = "josfh2005/CommitTree"
	// DefaultManifestURL always redirects to latest.json of the newest
	// non-draft, non-prerelease release.
	DefaultManifestURL = "https://github.com/" + Repo + "/releases/latest/download/latest.json"
	// ManifestEnv, when set to an http(s):// or file:// URL, replaces the
	// manifest URL and turns the host allow-list off (for testing).
	ManifestEnv = "COMMITTREE_UPDATE_MANIFEST"

	// maxManifest is the largest manifest accepted.
	maxManifest = 64 << 10
)

// ErrInvalidManifest means the manifest could not be used.
var ErrInvalidManifest = errors.New("invalid update manifest")

// Asset is one downloadable file of a release.
type Asset struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

// Manifest is the latest.json attached to every release. Assets is keyed by
// GOOS-GOARCH.
type Manifest struct {
	Version string           `json:"version"`
	URL     string           `json:"url"`
	Assets  map[string]Asset `json:"assets"`
}

// Release is the newest release as this platform sees it. The asset fields
// are empty when the manifest has no asset for the platform.
type Release struct {
	Version     string
	PageURL     string
	AssetName   string
	AssetURL    string
	AssetSHA256 string
}

// ParseManifest reads a manifest and resolves the asset for platform. The
// download URL is built here, never read from the manifest: on GitHub's
// release downloads, or, when override is set, next to manifestURL.
func ParseManifest(data []byte, platform, manifestURL string, override bool) (Release, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Release{}, fmt.Errorf("%w: %v", ErrInvalidManifest, err)
	}
	v, err := ParseVersion(m.Version)
	if err != nil {
		return Release{}, fmt.Errorf("%w: %v", ErrInvalidManifest, err)
	}
	rel := Release{Version: v.String(), PageURL: m.URL}
	if u, err := url.Parse(m.URL); err != nil || u.Scheme != "https" || u.Hostname() != "github.com" || u.Port() != "" {
		rel.PageURL = "https://github.com/" + Repo + "/releases/tag/v" + rel.Version
	}

	a, ok := m.Assets[platform]
	if !ok {
		return rel, nil
	}
	if a.Name == "" || strings.ContainsAny(a.Name, `/\`) || strings.Contains(a.Name, "..") {
		return Release{}, fmt.Errorf("%w: asset name %q", ErrInvalidManifest, a.Name)
	}
	if !validSHA256(a.SHA256) {
		return Release{}, fmt.Errorf("%w: sha256 of %q", ErrInvalidManifest, a.Name)
	}
	rel.AssetName = a.Name
	rel.AssetSHA256 = strings.ToLower(a.SHA256)
	if override {
		base, err := url.Parse(manifestURL)
		if err != nil {
			return Release{}, fmt.Errorf("manifest URL: %w", err)
		}
		rel.AssetURL = base.ResolveReference(&url.URL{Path: a.Name}).String()
	} else {
		rel.AssetURL = "https://github.com/" + Repo + "/releases/download/v" + rel.Version + "/" + url.PathEscape(a.Name)
	}
	return rel, nil
}

func validSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}
