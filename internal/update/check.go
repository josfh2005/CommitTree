package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"
)

const manifestTimeout = 15 * time.Second

// Checker reads the manifest and says which release is the latest.
type Checker struct {
	ManifestURL string
	// Override is set by ManifestEnv: any http(s) or file URL is accepted
	// and redirects are not restricted.
	Override bool
	// Platform is the manifest asset key, GOOS-GOARCH.
	Platform string
	// HTTP is the client used for http(s) manifests; nil uses a package
	// client. Its CheckRedirect is replaced for the call unless Override.
	HTTP *http.Client
}

// NewChecker builds a Checker from the environment (getenv is os.Getenv in
// production).
func NewChecker(getenv func(string) string) *Checker {
	c := &Checker{
		ManifestURL: DefaultManifestURL,
		Platform:    runtime.GOOS + "-" + runtime.GOARCH,
	}
	if v := strings.TrimSpace(getenv(ManifestEnv)); v != "" {
		c.ManifestURL = v
		c.Override = true
	}
	return c
}

// AllowedHost reports whether u may be fetched without the override: https on
// github.com or a subdomain of githubusercontent.com.
func AllowedHost(u *url.URL) bool {
	if u == nil || u.Scheme != "https" || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return h == "github.com" || strings.HasSuffix(h, ".githubusercontent.com")
}

var defaultClient = &http.Client{Timeout: manifestTimeout}

// Latest fetches the manifest and returns the release it describes. The
// request carries no token and nothing that identifies the user.
func (c *Checker) Latest(ctx context.Context) (Release, error) {
	ctx, cancel := context.WithTimeout(ctx, manifestTimeout)
	defer cancel()
	u, err := url.Parse(c.ManifestURL)
	if err != nil {
		return Release{}, fmt.Errorf("manifest URL: %w", err)
	}
	var data []byte
	switch {
	case u.Scheme == "file" && c.Override:
		data, err = readFileCapped(u.Path)
	case u.Scheme == "http" && c.Override, u.Scheme == "https" && (c.Override || AllowedHost(u)):
		data, err = c.fetch(ctx, u)
	default:
		return Release{}, fmt.Errorf("manifest URL %q is not allowed", c.ManifestURL)
	}
	if err != nil {
		return Release{}, err
	}
	return ParseManifest(data, c.Platform, c.ManifestURL, c.Override)
}

func (c *Checker) fetch(ctx context.Context, u *url.URL) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	client := c.HTTP
	if client == nil {
		client = defaultClient
	}
	if !c.Override {
		client = guardRedirects(client)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch manifest: %s", resp.Status)
	}
	return readCapped(resp.Body)
}

// guardRedirects returns a copy of client that follows at most 10 redirects
// and only to AllowedHost addresses. The caller's client is left as it was.
func guardRedirects(client *http.Client) *http.Client {
	cp := *client
	cp.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		if !AllowedHost(req.URL) {
			return fmt.Errorf("refusing redirect to %s", req.URL.Host)
		}
		return nil
	}
	return &cp
}

func readFileCapped(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	defer f.Close()
	return readCapped(f)
}

// readCapped reads at most maxManifest bytes and fails when there is more.
func readCapped(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxManifest+1))
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	if len(data) > maxManifest {
		return nil, fmt.Errorf("manifest is larger than %d KiB", maxManifest>>10)
	}
	return data, nil
}
