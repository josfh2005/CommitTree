package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAllowedHost(t *testing.T) {
	for _, s := range []string{
		"https://github.com/x",
		"https://objects.githubusercontent.com/x",
		"https://release-assets.githubusercontent.com/x",
	} {
		u, _ := url.Parse(s)
		if !AllowedHost(u) {
			t.Errorf("%s should be allowed", s)
		}
	}
	for _, s := range []string{
		"http://github.com/x",
		"https://github.com.evil.io/x",
		"https://evilgithubusercontent.com/x",
		"https://githubusercontent.com/x",
		"https://github.com:8443/x",
		"file:///tmp/x",
	} {
		u, _ := url.Parse(s)
		if AllowedHost(u) {
			t.Errorf("%s should not be allowed", s)
		}
	}
}

func validManifest() []byte {
	return manifestJSON("0.10.0", "", "CommitTree-macos-arm64.zip", sha)
}

func TestLatestHTTP(t *testing.T) {
	var body []byte
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		w.Write(body)
	}))
	defer srv.Close()
	c := &Checker{ManifestURL: srv.URL + "/dir/latest.json", Override: true, Platform: "darwin-arm64"}

	body = validManifest()
	r, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != "0.10.0" || r.AssetURL != srv.URL+"/dir/CommitTree-macos-arm64.zip" {
		t.Errorf("release = %+v", r)
	}

	status = http.StatusNotFound
	if _, err := c.Latest(context.Background()); err == nil {
		t.Error("404 should fail")
	}

	status = http.StatusOK
	body = append(validManifest(), []byte(strings.Repeat(" ", 64*1024))...)
	if _, err := c.Latest(context.Background()); err == nil {
		t.Error("oversize body should fail")
	}
}

func TestLatestFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "latest.json")
	if err := os.WriteFile(p, validManifest(), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Checker{ManifestURL: "file://" + p, Override: true, Platform: "darwin-arm64"}
	r, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.AssetURL != "file://"+filepath.Join(dir, "CommitTree-macos-arm64.zip") {
		t.Errorf("AssetURL = %q", r.AssetURL)
	}

	big := filepath.Join(dir, "big.json")
	os.WriteFile(big, append(validManifest(), []byte(strings.Repeat(" ", 64*1024))...), 0o644)
	c.ManifestURL = "file://" + big
	if _, err := c.Latest(context.Background()); err == nil {
		t.Error("oversize file should fail")
	}

	// A file:// manifest is only honoured with the override.
	c.ManifestURL = "file://" + p
	c.Override = false
	if _, err := c.Latest(context.Background()); err == nil {
		t.Error("file:// without override should fail")
	}
}

func TestLatestManifestHostRefused(t *testing.T) {
	c := &Checker{ManifestURL: "https://evil.example/latest.json", Platform: "darwin-arm64"}
	if _, err := c.Latest(context.Background()); err == nil {
		t.Error("non-allowed manifest URL should fail")
	}
}

// routeTo sends every request, whatever its URL, to the test server.
type routeTo struct{ addr string }

func (rt routeTo) RoundTrip(req *http.Request) (*http.Response, error) {
	r2 := req.Clone(req.Context())
	r2.URL.Scheme = "http"
	r2.URL.Host = rt.addr
	return http.DefaultTransport.RoundTrip(r2)
}

func TestLatestRedirectBlocked(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Redirect(w, r, "https://evil.example/latest.json", http.StatusFound)
	}))
	defer srv.Close()
	c := &Checker{
		ManifestURL: DefaultManifestURL,
		Platform:    "darwin-arm64",
		HTTP:        &http.Client{Transport: routeTo{addr: srv.Listener.Addr().String()}},
	}
	_, err := c.Latest(context.Background())
	if err == nil || !strings.Contains(err.Error(), "refusing redirect") {
		t.Fatalf("err = %v; want a redirect refusal", err)
	}
	if hits.Load() != 1 {
		t.Errorf("server hits = %d; the redirect must not be followed", hits.Load())
	}
	if c.HTTP.CheckRedirect != nil {
		t.Error("the caller's client must not be modified")
	}
}

func TestLatestTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	c := &Checker{ManifestURL: srv.URL + "/latest.json", Override: true, Platform: "darwin-arm64"}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.Latest(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v; want DeadlineExceeded", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("took %v", time.Since(start))
	}
}

func TestNewChecker(t *testing.T) {
	c := NewChecker(func(string) string { return "" })
	if c.ManifestURL != DefaultManifestURL || c.Override || c.Platform == "" {
		t.Errorf("default checker = %+v", c)
	}
	c = NewChecker(func(k string) string {
		if k == ManifestEnv {
			return "file:///tmp/x/latest.json"
		}
		return ""
	})
	if c.ManifestURL != "file:///tmp/x/latest.json" || !c.Override {
		t.Errorf("override checker = %+v", c)
	}
}

func TestLatestRedirectAllowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/josfh2005/CommitTree/releases/latest/download/latest.json" {
			http.Redirect(w, r, "https://objects.githubusercontent.com/blob/latest.json", http.StatusFound)
			return
		}
		w.Write(validManifest())
	}))
	defer srv.Close()
	c := &Checker{
		ManifestURL: DefaultManifestURL,
		Platform:    "darwin-arm64",
		HTTP:        &http.Client{Transport: routeTo{addr: srv.Listener.Addr().String()}},
	}
	r, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != "0.10.0" || r.AssetName != "CommitTree-macos-arm64.zip" {
		t.Errorf("release = %+v", r)
	}
}
