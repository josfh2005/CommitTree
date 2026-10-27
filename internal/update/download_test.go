package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func sumHex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func newRelease(url string, data []byte) Release {
	return Release{AssetName: "asset.bin", AssetURL: url, AssetSHA256: sumHex(data)}
}

func overrideChecker() *Checker { return &Checker{Override: true, Platform: "linux-amd64"} }

func assertEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("dir should be empty, has %d entries (first: %s)", len(entries), entries[0].Name())
	}
}

func serve(t *testing.T, data []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDownloadOK(t *testing.T) {
	data := bytes.Repeat([]byte("commit"), 50000)
	srv := serve(t, data)
	dir := t.TempDir()
	var lastDone, lastTotal int64
	p, err := overrideChecker().Download(context.Background(), newRelease(srv.URL+"/asset.bin", data), dir,
		func(done, total int64) { lastDone, lastTotal = done, total })
	if err != nil {
		t.Fatal(err)
	}
	if p != filepath.Join(dir, "asset.bin") {
		t.Errorf("path = %q", p)
	}
	got, _ := os.ReadFile(p)
	if !bytes.Equal(got, data) {
		t.Error("file content differs")
	}
	if lastDone != int64(len(data)) || lastTotal != int64(len(data)) {
		t.Errorf("final progress = %d/%d; want %d/%d", lastDone, lastTotal, len(data), len(data))
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("dir has %d entries; want only the asset", len(entries))
	}
}

func TestDownloadChecksumCaseInsensitive(t *testing.T) {
	data := []byte("hello")
	srv := serve(t, data)
	r := newRelease(srv.URL+"/asset.bin", data)
	r.AssetSHA256 = strings.ToUpper(r.AssetSHA256)
	if _, err := overrideChecker().Download(context.Background(), r, t.TempDir(), nil); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadChecksumMismatch(t *testing.T) {
	srv := serve(t, []byte("tampered"))
	dir := t.TempDir()
	_, err := overrideChecker().Download(context.Background(), newRelease(srv.URL+"/asset.bin", []byte("original")), dir, nil)
	if !errors.Is(err, ErrChecksum) {
		t.Fatalf("err = %v; want ErrChecksum", err)
	}
	assertEmpty(t, dir)
}

func TestDownloadTooLarge(t *testing.T) {
	old := maxDownload
	maxDownload = 1 << 10
	defer func() { maxDownload = old }()
	data := make([]byte, 2<<10)

	// Content-Length known: refused before reading the body.
	srv := serve(t, data)
	dir := t.TempDir()
	_, err := overrideChecker().Download(context.Background(), newRelease(srv.URL+"/asset.bin", data), dir, nil)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v; want ErrTooLarge", err)
	}
	assertEmpty(t, dir)

	// Length unknown (chunked): refused while reading.
	chunked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(data[:1500])
		w.(http.Flusher).Flush()
		w.Write(data[1500:])
	}))
	defer chunked.Close()
	_, err = overrideChecker().Download(context.Background(), newRelease(chunked.URL+"/asset.bin", data), dir, nil)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("chunked: err = %v; want ErrTooLarge", err)
	}
	assertEmpty(t, dir)
}

func TestDownloadStalled(t *testing.T) {
	old := stallTimeout
	stallTimeout = 100 * time.Millisecond
	defer func() { stallTimeout = old }()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("abc"))
		w.(http.Flusher).Flush()
		select {
		case <-time.After(time.Second):
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	start := time.Now()
	_, err := overrideChecker().Download(context.Background(), newRelease(srv.URL+"/asset.bin", []byte("abcdef")), dir, nil)
	if !errors.Is(err, ErrStalled) {
		t.Fatalf("err = %v; want ErrStalled", err)
	}
	if time.Since(start) > 900*time.Millisecond {
		t.Errorf("took %v; the stall guard should fire at ~100 ms", time.Since(start))
	}
	assertEmpty(t, dir)
}

func TestDownloadStalledBeforeHeaders(t *testing.T) {
	old := stallTimeout
	stallTimeout = 100 * time.Millisecond
	defer func() { stallTimeout = old }()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(time.Second):
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	_, err := overrideChecker().Download(context.Background(), newRelease(srv.URL+"/asset.bin", []byte("x")), t.TempDir(), nil)
	if !errors.Is(err, ErrStalled) {
		t.Fatalf("err = %v; want ErrStalled", err)
	}
}

func TestDownloadCancelCleansUp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("abc"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := overrideChecker().Download(ctx, newRelease(srv.URL+"/asset.bin", []byte("abcdef")), dir,
		func(done, total int64) { cancel() })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v; want context.Canceled", err)
	}
	assertEmpty(t, dir)
}

func TestDownloadNoAsset(t *testing.T) {
	_, err := overrideChecker().Download(context.Background(), Release{Version: "0.10.0"}, t.TempDir(), nil)
	if !errors.Is(err, ErrNoAsset) {
		t.Fatalf("err = %v; want ErrNoAsset", err)
	}
}

func TestDownloadBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	dir := t.TempDir()
	if _, err := overrideChecker().Download(context.Background(), newRelease(srv.URL+"/asset.bin", nil), dir, nil); err == nil {
		t.Error("404 should fail")
	}
	assertEmpty(t, dir)
}

func TestDownloadFile(t *testing.T) {
	data := []byte("from a file")
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "asset.bin"), data, 0o644)
	dir := t.TempDir()
	p, err := overrideChecker().Download(context.Background(), newRelease("file://"+filepath.Join(src, "asset.bin"), data), dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); !bytes.Equal(got, data) {
		t.Error("content differs")
	}
}

func TestDownloadHostRules(t *testing.T) {
	c := &Checker{Platform: "linux-amd64"} // no override
	for _, u := range []string{
		"https://evil.example/asset.bin",
		"http://github.com/asset.bin",
		"file:///etc/passwd",
	} {
		dir := t.TempDir()
		if _, err := c.Download(context.Background(), newRelease(u, nil), dir, nil); err == nil {
			t.Errorf("%s should be refused without the override", u)
		}
		assertEmpty(t, dir)
	}
}

func TestDownloadRedirectBlocked(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Redirect(w, r, "https://evil.example/asset.bin", http.StatusFound)
	}))
	defer srv.Close()
	c := &Checker{Platform: "linux-amd64", HTTP: &http.Client{Transport: routeTo{addr: srv.Listener.Addr().String()}}}
	dir := t.TempDir()
	_, err := c.Download(context.Background(),
		newRelease("https://github.com/josfh2005/CommitTree/releases/download/v1.0.0/asset.bin", nil), dir, nil)
	if err == nil || !strings.Contains(err.Error(), "refusing redirect") {
		t.Fatalf("err = %v; want a redirect refusal", err)
	}
	if hits != 1 {
		t.Errorf("hits = %d; the redirect must not be followed", hits)
	}
	assertEmpty(t, dir)
}

func TestDownloadRedirectAllowed(t *testing.T) {
	data := []byte("release bytes")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/asset.bin" {
			http.Redirect(w, r, "https://release-assets.githubusercontent.com/blob", http.StatusFound)
			return
		}
		w.Write(data)
	}))
	defer srv.Close()
	c := &Checker{Platform: "linux-amd64", HTTP: &http.Client{
		Timeout:   time.Millisecond, // must be ignored: no total timeout on a download
		Transport: routeTo{addr: srv.Listener.Addr().String()},
	}}
	p, err := c.Download(context.Background(),
		newRelease("https://github.com/josfh2005/CommitTree/releases/download/v1.0.0/asset.bin", data), t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); !bytes.Equal(got, data) {
		t.Error("content differs")
	}
	if c.HTTP.CheckRedirect != nil || c.HTTP.Timeout != time.Millisecond {
		t.Error("the caller's client must not be modified")
	}
}
