package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

var (
	// maxDownload is the largest archive accepted (and the largest file
	// extracted from one). A variable so tests can lower it.
	maxDownload int64 = 200 << 20
	// stallTimeout is how long a download may go without receiving a byte.
	stallTimeout = 30 * time.Second
)

var (
	// ErrNoAsset means the release has no file for this platform.
	ErrNoAsset = errors.New("release has no download for this platform")
	// ErrChecksum means the downloaded file does not match the manifest.
	ErrChecksum = errors.New("downloaded file does not match its checksum")
	// ErrTooLarge means the download is bigger than maxDownload.
	ErrTooLarge = errors.New("download is too large")
	// ErrStalled means no byte arrived for stallTimeout.
	ErrStalled = errors.New("download stalled")
)

// Download fetches the release asset into dir and returns dir/<AssetName>.
// progress, if not nil, is called as bytes arrive; total is -1 when unknown.
// There is no total time limit, only the stall guard. The file appears under
// its final name only once its size and SHA-256 are verified; any error
// leaves nothing behind.
func (c *Checker) Download(ctx context.Context, r Release, dir string, progress func(done, total int64)) (string, error) {
	if r.AssetURL == "" || r.AssetName == "" {
		return "", ErrNoAsset
	}
	if r.AssetName != filepath.Base(r.AssetName) || strings.ContainsAny(r.AssetName, `/\`) || r.AssetName == ".." {
		return "", fmt.Errorf("unsafe asset name %q", r.AssetName)
	}
	u, err := url.Parse(r.AssetURL)
	if err != nil {
		return "", fmt.Errorf("asset URL: %w", err)
	}
	if !c.assetAllowed(u) {
		return "", fmt.Errorf("asset URL %q is not allowed", r.AssetURL)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	// ctx is cancelled by the caller or by the stall timer; stalled tells
	// the two apart.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var stalled atomic.Bool
	timer := time.AfterFunc(stallTimeout, func() {
		stalled.Store(true)
		cancel()
	})
	defer timer.Stop()

	body, total, err := c.open(ctx, u)
	if err != nil {
		return "", downloadErr(ctx, &stalled, err)
	}
	defer body.Close()
	if total > maxDownload {
		return "", ErrTooLarge
	}

	part, err := os.CreateTemp(dir, r.AssetName+".*.part")
	if err != nil {
		return "", err
	}
	ok := false
	defer func() {
		part.Close()
		if !ok {
			os.Remove(part.Name())
		}
	}()

	h := sha256.New()
	var done int64
	buf := make([]byte, 64<<10)
	for {
		n, rerr := body.Read(buf)
		timer.Reset(stallTimeout)
		if n > 0 {
			done += int64(n)
			if done > maxDownload {
				return "", ErrTooLarge
			}
			if _, werr := io.MultiWriter(part, h).Write(buf[:n]); werr != nil {
				return "", werr
			}
			if progress != nil {
				progress(done, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return "", downloadErr(ctx, &stalled, rerr)
		}
	}
	if total >= 0 && done != total {
		return "", fmt.Errorf("download ended after %d of %d bytes", done, total)
	}
	if !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), r.AssetSHA256) {
		return "", ErrChecksum
	}
	if err := part.Close(); err != nil {
		return "", err
	}
	final := filepath.Join(dir, r.AssetName)
	if err := os.Rename(part.Name(), final); err != nil {
		return "", err
	}
	ok = true
	return final, nil
}

// assetAllowed applies the same rule as the manifest: with the override any
// http(s) or file URL, otherwise https on an allowed host.
func (c *Checker) assetAllowed(u *url.URL) bool {
	if c.Override {
		return u.Scheme == "file" || u.Scheme == "http" || u.Scheme == "https"
	}
	return AllowedHost(u)
}

// open starts reading the asset and returns its body and size (-1 unknown).
func (c *Checker) open(ctx context.Context, u *url.URL) (io.ReadCloser, int64, error) {
	if u.Scheme == "file" {
		f, err := os.Open(u.Path)
		if err != nil {
			return nil, 0, err
		}
		fi, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, 0, err
		}
		return f, fi.Size(), nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, 0, err
	}
	// Never the 15 s manifest client: a download has no total timeout.
	client := &http.Client{}
	if c.HTTP != nil {
		cp := *c.HTTP
		cp.Timeout = 0
		client = &cp
	}
	if !c.Override {
		client = guardRedirects(client)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, 0, fmt.Errorf("download: %s", resp.Status)
	}
	return resp.Body, resp.ContentLength, nil
}

// downloadErr maps a failure to the sentinel that explains it: a stall, or
// the caller's own cancellation, which is reported as ctx's error.
func downloadErr(ctx context.Context, stalled *atomic.Bool, err error) error {
	if stalled.Load() {
		return ErrStalled
	}
	if cerr := ctx.Err(); cerr != nil {
		return cerr
	}
	return fmt.Errorf("download: %w", err)
}
