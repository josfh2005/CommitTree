package hosting

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *fakeClock { return &fakeClock{t: time.Unix(1_800_000_000, 0)} }
func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

const testToken = "Bearer s3cret-token"

func authOK(context.Context) (string, error) { return testToken, nil }

func newDoer(host Host, base string, clk *fakeClock) *Doer {
	return &Doer{Host: host, BaseURL: base, Auth: authOK, Limits: NewLimits(clk.Now)}
}

func TestDoerSendsAuthOnlyToBase(t *testing.T) {
	var got string
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		w.Write([]byte(`{"ok":true}`))
	}))
	defer a.Close()
	var hitsB int32
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hitsB, 1)
	}))
	defer b.Close()

	d := newDoer(GitHub, a.URL, newClock())
	var out struct{ OK bool }
	if _, err := d.JSON(context.Background(), "GET", "/user", nil, &out); err != nil {
		t.Fatal(err)
	}
	if got != testToken || !out.OK {
		t.Fatalf("auth %q ok %v", got, out.OK)
	}
	// an absolute URL on the base host (pagination links) is fine
	if _, err := d.JSON(context.Background(), "GET", a.URL+"/user?page=2", nil, nil); err != nil {
		t.Fatal(err)
	}
	// another host, even on the same IP with another port, is refused
	for _, target := range []string{b.URL + "/x?secret=1", "//" + strings.TrimPrefix(b.URL, "http://") + "/x"} {
		_, err := d.JSON(context.Background(), "GET", target, nil, nil)
		if err == nil {
			t.Fatalf("%s: want error", target)
		}
		if strings.Contains(err.Error(), "secret=1") || strings.Contains(err.Error(), "s3cret") {
			t.Fatalf("error leaks: %v", err)
		}
	}
	if _, err := d.Raw(context.Background(), b.URL+"/x", 10); err == nil {
		t.Fatal("Raw: want error")
	}
	if n := atomic.LoadInt32(&hitsB); n != 0 {
		t.Fatalf("other host got %d requests", n)
	}
}

func TestDoerRefusesCrossHostRedirect(t *testing.T) {
	var hitsB int32
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hitsB, 1)
	}))
	defer b.Close()
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, b.URL+"/steal?secret=1", http.StatusFound)
	}))
	defer a.Close()

	// also when the caller brings a client with its own, permissive policy
	own := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return nil }}
	for _, c := range []*http.Client{nil, own} {
		d := newDoer(GitHub, a.URL, newClock())
		d.HTTP = c
		_, err := d.JSON(context.Background(), "GET", "/x", nil, nil)
		if err == nil {
			t.Fatal("want error")
		}
		if strings.Contains(err.Error(), "secret=1") || strings.Contains(err.Error(), "s3cret") {
			t.Fatalf("error leaks: %v", err)
		}
	}
	if n := atomic.LoadInt32(&hitsB); n != 0 {
		t.Fatalf("redirect target got %d requests", n)
	}
}

func TestDoerFollowsSameHostRedirect(t *testing.T) {
	var auth string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a":
			http.Redirect(w, r, "/b", http.StatusFound)
		case "/b":
			auth = r.Header.Get("Authorization")
			w.Write([]byte("diff"))
		}
	}))
	defer s.Close()
	d := newDoer(Bitbucket, s.URL, newClock())
	b, err := d.Raw(context.Background(), "/a", 100)
	if err != nil || string(b) != "diff" {
		t.Fatalf("%q %v", b, err)
	}
	if auth != testToken {
		t.Fatalf("auth after redirect %q", auth)
	}
}

func TestDoer401(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer s.Close()
	d := newDoer(Bitbucket, s.URL, newClock())
	calls := 0
	d.OnReject = func() { calls++ }
	_, err := d.JSON(context.Background(), "GET", "/user", nil, nil)
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("got %v", err)
	}
	if calls != 1 {
		t.Fatalf("OnReject %d times", calls)
	}
	if err.Error() != "Bitbucket rejected the token" {
		t.Fatalf("copy: %q", err)
	}
}

func TestDoer403WithScopeDetail(t *testing.T) {
	for _, tc := range []struct {
		host Host
		body string
		want string
	}{
		{Bitbucket, `{"error":{"message":"Your credentials lack one or more required privilege scopes."}}`, "Your credentials lack one or more required privilege scopes."},
		{GitHub, `{"message":"Resource not accessible by personal access token"}`, "Resource not accessible by personal access token"},
	} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(tc.body))
		}))
		d := newDoer(tc.host, s.URL, newClock())
		_, err := d.JSON(context.Background(), "GET", "/r", nil, nil)
		s.Close()
		var na *NoAccessError
		if !errors.As(err, &na) || na.Detail != tc.want {
			t.Fatalf("%s: got %#v", tc.host, err)
		}
		if _, blocked := d.Limits.Blocked(tc.host); blocked {
			t.Fatalf("%s: a plain 403 must not block", tc.host)
		}
	}
}

func TestDoer404(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer s.Close()
	d := newDoer(GitHub, s.URL, newClock())
	_, err := d.JSON(context.Background(), "GET", "/r", nil, nil)
	var na *NoAccessError
	if !errors.As(err, &na) || na.Detail != "" {
		t.Fatalf("got %#v", err)
	}
}

func TestGitHubRateLimitHeaders(t *testing.T) {
	clk := newClock()
	reset := clk.Now().Add(10 * time.Minute).Unix()
	var hits int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset, 10))
		w.Write([]byte(`{}`))
	}))
	defer s.Close()
	d := newDoer(GitHub, s.URL, clk)
	if _, err := d.JSON(context.Background(), "GET", "/user", nil, nil); err != nil {
		t.Fatal(err)
	}
	rl, blocked := d.Limits.Blocked(GitHub)
	if !blocked || !rl.Known || !rl.Until.Equal(time.Unix(reset, 0)) {
		t.Fatalf("blocked %v %+v", blocked, rl)
	}
	_, err := d.JSON(context.Background(), "GET", "/user", nil, nil)
	var re *RateLimitError
	if !errors.As(err, &re) || !re.Known {
		t.Fatalf("got %v", err)
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("server hit %d times", hits)
	}
	// the limit lapses with the clock
	clk.Advance(11 * time.Minute)
	if _, blocked := d.Limits.Blocked(GitHub); blocked {
		t.Fatal("still blocked after reset")
	}
	// the other host is unaffected
	if _, blocked := d.Limits.Blocked(Bitbucket); blocked {
		t.Fatal("bitbucket blocked")
	}
}

func TestGitHubSecondaryRateLimitOn403(t *testing.T) {
	clk := newClock()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"You have exceeded a secondary rate limit."}`))
	}))
	defer s.Close()
	d := newDoer(GitHub, s.URL, clk)
	_, err := d.JSON(context.Background(), "GET", "/x", nil, nil)
	var re *RateLimitError
	if !errors.As(err, &re) || re.Known {
		t.Fatalf("got %v", err)
	}
	if rl, ok := d.Limits.Blocked(GitHub); !ok || !rl.Until.Equal(clk.Now().Add(60*time.Second)) {
		t.Fatalf("%+v %v", rl, ok)
	}
}

func TestBitbucketBackoff(t *testing.T) {
	clk := newClock()
	var mode atomic.Value
	mode.Store("429")
	var retryAfter atomic.Value
	retryAfter.Store("")
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mode.Load() == "ok" {
			w.Write([]byte(`{}`))
			return
		}
		if ra := retryAfter.Load().(string); ra != "" {
			w.Header().Set("Retry-After", ra)
		}
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer s.Close()
	d := newDoer(Bitbucket, s.URL, clk)
	ctx := context.Background()

	call := func() error { _, err := d.JSON(ctx, "GET", "/x", nil, nil); return err }
	expect := func(want time.Duration, known bool) {
		t.Helper()
		var re *RateLimitError
		if err := call(); !errors.As(err, &re) {
			t.Fatalf("got %v", err)
		}
		rl, ok := d.Limits.Blocked(Bitbucket)
		if !ok || rl.Known != known || !rl.Until.Equal(clk.Now().Add(want)) {
			t.Fatalf("want %v known=%v, got %+v ok=%v (now %v)", want, known, rl, ok, clk.Now())
		}
	}

	expect(60*time.Second, false)
	if err := call(); err == nil { // blocked: no request, a second 429 must not double
		t.Fatal("want blocked")
	}
	clk.Advance(61 * time.Second)
	expect(120*time.Second, false)
	for _, want := range []time.Duration{240 * time.Second, 480 * time.Second, 900 * time.Second, 900 * time.Second} {
		clk.Advance(d.mustUntil(t) + time.Second)
		expect(want, false)
	}

	// a successful answer starts the doubling over
	clk.Advance(901 * time.Second)
	mode.Store("ok")
	if err := call(); err != nil {
		t.Fatal(err)
	}
	mode.Store("429")
	expect(60*time.Second, false)

	// Retry-After is honoured and known
	clk.Advance(61 * time.Second)
	retryAfter.Store("30")
	expect(30*time.Second, true)
}

// mustUntil is how long the Bitbucket block still has to run.
func (d *Doer) mustUntil(t *testing.T) time.Duration {
	t.Helper()
	rl, ok := d.Limits.Blocked(Bitbucket)
	if !ok {
		t.Fatal("not blocked")
	}
	return rl.Until.Sub(d.Limits.now())
}

func TestRawCap(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strings.Repeat("x", 100)))
	}))
	defer s.Close()
	d := newDoer(Bitbucket, s.URL, newClock())
	if b, err := d.Raw(context.Background(), "/d", 100); err != nil || len(b) != 100 {
		t.Fatalf("at the cap: %d %v", len(b), err)
	}
	_, err := d.Raw(context.Background(), "/d", 99)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("got %v", err)
	}
	// chunked body with no Content-Length
	s2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < 10; i++ {
			w.Write([]byte(strings.Repeat("x", 10)))
			w.(http.Flusher).Flush()
		}
	}))
	defer s2.Close()
	d2 := newDoer(Bitbucket, s2.URL, newClock())
	if _, err := d2.Raw(context.Background(), "/d", 50); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("chunked: got %v", err)
	}
}

func TestDoerErrorsNeverCarryTheTokenOrQuery(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	url := s.URL
	s.Close() // connection refused
	d := newDoer(GitHub, url, newClock())
	_, err := d.JSON(context.Background(), "GET", "/x?q=hidden", nil, nil)
	if err == nil || strings.Contains(err.Error(), "hidden") || strings.Contains(err.Error(), "s3cret") {
		t.Fatalf("got %v", err)
	}
	s2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer s2.Close()
	d2 := newDoer(GitHub, s2.URL, newClock())
	_, err = d2.JSON(context.Background(), "GET", "/x?q=hidden", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "500") || strings.Contains(err.Error(), "hidden") {
		t.Fatalf("got %v", err)
	}
}

func TestDoerPostsJSONBody(t *testing.T) {
	var ct, body string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct = r.Header.Get("Content-Type")
		b := make([]byte, 100)
		n, _ := r.Body.Read(b)
		body = string(b[:n])
		w.Write([]byte(`{"data":{"n":2}}`))
	}))
	defer s.Close()
	d := newDoer(GitHub, s.URL, newClock())
	var out struct{ Data struct{ N int } }
	if _, err := d.JSON(context.Background(), "POST", "/graphql", map[string]string{"query": "q"}, &out); err != nil {
		t.Fatal(err)
	}
	if ct != "application/json" || body != `{"query":"q"}` || out.Data.N != 2 {
		t.Fatalf("%q %q %+v", ct, body, out)
	}
}

func TestLimitsConcurrent(t *testing.T) {
	l := NewLimits(nil)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l.backoff()
			l.Blocked(Bitbucket)
			l.blockUntil(GitHub, time.Now().Add(time.Second), true)
			l.resetBackoff()
		}()
	}
	wg.Wait()
}
