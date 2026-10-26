package hosting

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrTooLarge means a response body was bigger than the caller allowed.
var ErrTooLarge = errors.New("response too large")

const (
	requestTimeout   = 20 * time.Second
	maxErrorBody     = 64 << 10 // error bodies are read up to this size
	maxDetailLen     = 300
	bitbucketBackoff = 60 * time.Second
	maxBackoff       = 15 * time.Minute
	unknownLimitWait = 60 * time.Second
)

// sharedHTTP is used when a Doer has no client of its own.
var sharedHTTP = &http.Client{Timeout: requestTimeout}

func (h Host) label() string {
	switch h {
	case Bitbucket:
		return "Bitbucket"
	case GitHub:
		return "GitHub"
	}
	return string(h)
}

// Doer sends authenticated requests to one host's API. Everything it sends
// stays on BaseURL's scheme and host, redirects included, so the Authorization
// value never reaches any other server. Error text never carries the
// Authorization value or a request URL.
type Doer struct {
	Host     Host
	BaseURL  string                                    // e.g. https://api.bitbucket.org/2.0
	Auth     func(ctx context.Context) (string, error) // full Authorization header value
	HTTP     *http.Client                              // nil: shared client with a 20 s timeout
	Limits   *Limits
	OnReject func() // called on a 401
}

// JSON sends a request with an optional JSON body and decodes a JSON answer
// into out (nil to ignore it). It returns the response headers.
func (d *Doer) JSON(ctx context.Context, method, pathOrURL string, body, out any) (http.Header, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode request body: %w", err)
		}
		rd = bytes.NewReader(b)
	}
	resp, err := d.do(ctx, method, pathOrURL, rd, "application/json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if out != nil {
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, d.transportErr(err)
		}
		if len(bytes.TrimSpace(raw)) > 0 {
			if err := json.Unmarshal(raw, out); err != nil {
				return nil, fmt.Errorf("%s: unreadable response: %w", d.Host.label(), err)
			}
		}
	}
	return resp.Header, nil
}

// Raw fetches a body (a diff, say) with a GET, failing with ErrTooLarge when
// it is longer than max bytes.
func (d *Doer) Raw(ctx context.Context, pathOrURL string, max int64) ([]byte, error) {
	resp, err := d.do(ctx, http.MethodGet, pathOrURL, nil, "*/*")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	tooLarge := fmt.Errorf("%s: %w (over %d bytes)", d.Host.label(), ErrTooLarge, max)
	if resp.ContentLength > max {
		return nil, tooLarge
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, d.transportErr(err)
	}
	if int64(len(b)) > max {
		return nil, tooLarge
	}
	return b, nil
}

// do sends one request and returns the response for a 2xx status. Any other
// status is mapped to an error and the body is closed.
func (d *Doer) do(ctx context.Context, method, pathOrURL string, body io.Reader, accept string) (*http.Response, error) {
	u, err := d.resolve(pathOrURL)
	if err != nil {
		return nil, err
	}
	if d.Limits != nil {
		if rl, blocked := d.Limits.Blocked(d.Host); blocked {
			return nil, &rl
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, fmt.Errorf("%s: invalid request", d.Host.label())
	}
	req.Header.Set("Accept", accept)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if d.Auth != nil {
		v, err := d.Auth(ctx)
		if err != nil {
			return nil, err
		}
		if v != "" {
			req.Header.Set("Authorization", v)
		}
	}
	resp, err := d.client().Do(req)
	if err != nil {
		return nil, d.transportErr(err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		d.observe(resp, nil)
		return resp, nil
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	msg := errorMessage(raw)
	d.observe(resp, &msg)
	return nil, d.statusErr(resp, msg)
}

// client returns a copy of the configured client whose redirect policy always
// keeps requests on the API host, whatever the caller's client does.
func (d *Doer) client() *http.Client {
	base, _ := url.Parse(d.BaseURL)
	c := *sharedHTTP
	if d.HTTP != nil {
		c = *d.HTTP
	}
	inner := c.CheckRedirect
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if base == nil || originOf(req.URL) != originOf(base) {
			return fmt.Errorf("refused a redirect to %s: the token stays on %s", req.URL.Host, d.Host.label())
		}
		if inner != nil {
			return inner(req, via)
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
	return &c
}

// resolve turns a path (under BaseURL) or an absolute URL into the URL to
// call, refusing anything outside BaseURL's scheme and host.
func (d *Doer) resolve(p string) (*url.URL, error) {
	base, err := url.Parse(d.BaseURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("%s: invalid API address", d.Host.label())
	}
	var u *url.URL
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") {
		if u, err = url.Parse(p); err != nil {
			return nil, fmt.Errorf("%s: invalid request path", d.Host.label())
		}
		if !u.IsAbs() && u.Host == "" {
			u = nil
		}
	}
	if u == nil {
		joined := strings.TrimRight(d.BaseURL, "/") + "/" + strings.TrimLeft(p, "/")
		if u, err = url.Parse(joined); err != nil {
			return nil, fmt.Errorf("%s: invalid request path", d.Host.label())
		}
	}
	if u.User != nil || originOf(u) != originOf(base) {
		return nil, fmt.Errorf("refused to send the token to %s: it only goes to %s", u.Host, base.Host)
	}
	return u, nil
}

// originOf is scheme://host[:port] in lower case, without a default port.
func originOf(u *url.URL) string {
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if port == "" || (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		return scheme + "://" + host
	}
	return scheme + "://" + net.JoinHostPort(host, port)
}

// transportErr drops the *url.Error wrapper, which carries the full URL
// (query included), and keeps the cause so errors.Is still sees it.
func (d *Doer) transportErr(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return fmt.Errorf("%s: request failed: %w", d.Host.label(), err)
}

// errorMessage reads the host's own error text: {"error":{"message":..}}
// (Bitbucket) or {"message":..} (GitHub).
func errorMessage(raw []byte) string {
	var v struct {
		Message string `json:"message"`
		Error   *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	m := v.Message
	if v.Error != nil && v.Error.Message != "" {
		m = v.Error.Message
	}
	m = strings.TrimSpace(m)
	if len(m) > maxDetailLen {
		m = m[:maxDetailLen]
	}
	return m
}

// observe feeds the rate-limit state from a response. msg is the host's error
// text for a non-2xx answer, nil for a 2xx one.
func (d *Doer) observe(resp *http.Response, msg *string) {
	l := d.Limits
	if l == nil {
		return
	}
	if d.Host == GitHub && resp.Header.Get("X-RateLimit-Remaining") == "0" {
		if secs, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			l.blockUntil(GitHub, time.Unix(secs, 0), true)
			return
		}
	}
	if msg == nil {
		if d.Host == Bitbucket {
			l.resetBackoff()
		}
		return
	}
	if !d.isRateLimited(resp, *msg) {
		return
	}
	if wait, ok := retryAfter(resp.Header, l.now()); ok {
		l.blockUntil(d.Host, l.now().Add(wait), true)
		return
	}
	if d.Host == Bitbucket {
		l.backoff()
		return
	}
	l.blockUntil(d.Host, l.now().Add(unknownLimitWait), false)
}

func (d *Doer) isRateLimited(resp *http.Response, msg string) bool {
	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		return true
	case http.StatusForbidden:
		return d.Host == GitHub && strings.Contains(strings.ToLower(msg), "rate limit")
	}
	return false
}

// retryAfter reads a Retry-After header: seconds or an HTTP date.
func retryAfter(h http.Header, now time.Time) (time.Duration, bool) {
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		if w := t.Sub(now); w > 0 {
			return w, true
		}
		return 0, true
	}
	return 0, false
}

// statusErr maps a non-2xx answer to the errors of the integration. The
// NoAccessError's Repo is left for the host client, which knows it.
func (d *Doer) statusErr(resp *http.Response, msg string) error {
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		if d.OnReject != nil {
			d.OnReject()
		}
		return fmt.Errorf("%s %w", d.Host.label(), ErrRejected)
	case d.isRateLimited(resp, msg):
		if d.Limits != nil {
			if rl, ok := d.Limits.Blocked(d.Host); ok {
				return &rl
			}
		}
		return &RateLimitError{Host: d.Host}
	case resp.StatusCode == http.StatusForbidden:
		return &NoAccessError{Detail: msg}
	case resp.StatusCode == http.StatusNotFound:
		return &NoAccessError{}
	}
	if msg != "" {
		return fmt.Errorf("%s: HTTP %d: %s", d.Host.label(), resp.StatusCode, msg)
	}
	return fmt.Errorf("%s: HTTP %d", d.Host.label(), resp.StatusCode)
}

// Limits is the per-host rate-limit state, safe for concurrent use.
type Limits struct {
	mu      sync.Mutex
	nowFn   func() time.Time
	blocked map[Host]limit
	wait    time.Duration // Bitbucket's current backoff step; 0 before the first 429
}

type limit struct {
	until time.Time
	known bool
}

// NewLimits makes an empty state. now is the clock (nil: time.Now).
func NewLimits(now func() time.Time) *Limits {
	if now == nil {
		now = time.Now
	}
	return &Limits{nowFn: now, blocked: map[Host]limit{}}
}

func (l *Limits) now() time.Time { return l.nowFn() }

// Blocked says whether the host's limit is still in force, and until when.
func (l *Limits) Blocked(h Host) (RateLimitError, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.blocked[h]
	if !ok || !l.now().Before(b.until) {
		return RateLimitError{}, false
	}
	return RateLimitError{Host: h, Until: b.until, Known: b.known}, true
}

func (l *Limits) blockUntil(h Host, until time.Time, known bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.blocked[h] = limit{until: until, known: known}
}

// backoff blocks Bitbucket for 60 s, doubling up to 15 min while 429s go on.
// A 429 that arrives while a block is already in force (another request that
// was in flight) does not double it again.
func (l *Limits) backoff() {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if b, ok := l.blocked[Bitbucket]; ok && now.Before(b.until) {
		return
	}
	switch {
	case l.wait == 0:
		l.wait = bitbucketBackoff
	case l.wait*2 > maxBackoff:
		l.wait = maxBackoff
	default:
		l.wait *= 2
	}
	l.blocked[Bitbucket] = limit{until: now.Add(l.wait)}
}

func (l *Limits) resetBackoff() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.wait = 0
}
