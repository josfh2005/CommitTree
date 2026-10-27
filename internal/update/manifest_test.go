package update

import (
	"errors"
	"strings"
	"testing"
)

const sha = "ABCDEF0123456789abcdef0123456789ABCDEF0123456789abcdef0123456789"

func manifestJSON(version, pageURL, name, hash string) []byte {
	return []byte(`{"version":"` + version + `","url":"` + pageURL + `","assets":{` +
		`"darwin-arm64":{"name":"` + name + `","sha256":"` + hash + `"}}}`)
}

func TestParseManifest(t *testing.T) {
	page := "https://github.com/josfh2005/CommitTree/releases/tag/v0.10.0"
	data := manifestJSON("0.10.0", page, "CommitTree-macos-arm64.zip", sha)
	r, err := ParseManifest(data, "darwin-arm64", DefaultManifestURL, false)
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != "0.10.0" {
		t.Errorf("Version = %q", r.Version)
	}
	if r.PageURL != page {
		t.Errorf("PageURL = %q", r.PageURL)
	}
	if r.AssetName != "CommitTree-macos-arm64.zip" {
		t.Errorf("AssetName = %q", r.AssetName)
	}
	want := "https://github.com/josfh2005/CommitTree/releases/download/v0.10.0/CommitTree-macos-arm64.zip"
	if r.AssetURL != want {
		t.Errorf("AssetURL = %q; want %q", r.AssetURL, want)
	}
	if r.AssetSHA256 != strings.ToLower(sha) {
		t.Errorf("AssetSHA256 = %q", r.AssetSHA256)
	}
}

func TestParseManifestPageURLReplaced(t *testing.T) {
	want := "https://github.com/josfh2005/CommitTree/releases/tag/v0.10.0"
	for _, bad := range []string{
		"http://evil.example/x",
		"https://evil.example/x",
		"http://github.com/x",
		"https://github.com.evil.io/x",
		"",
		"not a url",
	} {
		data := manifestJSON("0.10.0", bad, "CommitTree-macos-arm64.zip", sha)
		r, err := ParseManifest(data, "darwin-arm64", DefaultManifestURL, false)
		if err != nil {
			t.Fatalf("%q: %v", bad, err)
		}
		if r.PageURL != want {
			t.Errorf("url %q: PageURL = %q; want %q", bad, r.PageURL, want)
		}
	}
}

func TestParseManifestOverride(t *testing.T) {
	data := manifestJSON("0.10.0", "", "CommitTree-macos-arm64.zip", sha)
	cases := map[string]string{
		"http://127.0.0.1:9/dir/latest.json": "http://127.0.0.1:9/dir/CommitTree-macos-arm64.zip",
		"file:///tmp/x/latest.json":          "file:///tmp/x/CommitTree-macos-arm64.zip",
	}
	for manifestURL, want := range cases {
		r, err := ParseManifest(data, "darwin-arm64", manifestURL, true)
		if err != nil {
			t.Fatal(err)
		}
		if r.AssetURL != want {
			t.Errorf("%s: AssetURL = %q; want %q", manifestURL, r.AssetURL, want)
		}
	}
}

func TestParseManifestMissingAsset(t *testing.T) {
	data := manifestJSON("0.10.0", "", "CommitTree-macos-arm64.zip", sha)
	r, err := ParseManifest(data, "linux-arm64", DefaultManifestURL, false)
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != "0.10.0" {
		t.Errorf("Version = %q", r.Version)
	}
	if r.AssetName != "" || r.AssetURL != "" || r.AssetSHA256 != "" {
		t.Errorf("asset fields should be empty: %+v", r)
	}
}

func TestParseManifestInvalid(t *testing.T) {
	good := sha
	cases := map[string][]byte{
		"bad json":      []byte(`{`),
		"empty":         []byte(``),
		"bad version":   manifestJSON("v0.10.0", "", "a.zip", good),
		"short version": manifestJSON("0.10", "", "a.zip", good),
		"short sha":     manifestJSON("0.10.0", "", "a.zip", "abc123"),
		"non-hex sha":   manifestJSON("0.10.0", "", "a.zip", strings.Repeat("g", 64)),
		"slash name":    manifestJSON("0.10.0", "", "dir/a.zip", good),
		"dotdot name":   manifestJSON("0.10.0", "", "..a.zip", good),
		"parent name":   manifestJSON("0.10.0", "", "../a.zip", good),
		"backslash":     manifestJSON("0.10.0", "", `d\\a.zip`, good),
		"empty name":    manifestJSON("0.10.0", "", "", good),
	}
	for name, data := range cases {
		_, err := ParseManifest(data, "darwin-arm64", DefaultManifestURL, false)
		if !errors.Is(err, ErrInvalidManifest) {
			t.Errorf("%s: err = %v; want ErrInvalidManifest", name, err)
		}
	}
}
