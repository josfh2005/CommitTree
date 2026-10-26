package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"git-ui/internal/hosting"
)

var repo = hosting.Repo{Host: hosting.GitHub, Owner: "acme", Name: "api", Remote: "origin"}

var me = hosting.Account{ID: "ana-dev", Login: "ana-dev", Name: "Ana Dev"}

var (
	hashA = strings.Repeat("a", 40)
	hashB = strings.Repeat("b", 40)
	hashC = strings.Repeat("c", 40)
	hashD = strings.Repeat("d", 40)
	hashE = strings.Repeat("e", 40)
)

// req is one recorded request.
type req struct {
	Method string
	URL    *url.URL
	Header http.Header
	Body   string
}

// fake is an httptest server that records every request and answers through
// a handler.
type fake struct {
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []req
}

func newFake(t *testing.T, h func(f *fake, w http.ResponseWriter, r *http.Request, body string)) (*fake, hosting.Client) {
	t.Helper()
	f := &fake{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.reqs = append(f.reqs, req{r.Method, r.URL, r.Header.Clone(), string(b)})
		f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "no auth", http.StatusUnauthorized)
			return
		}
		h(f, w, r, string(b))
	}))
	t.Cleanup(f.srv.Close)
	d := &hosting.Doer{Host: hosting.GitHub, BaseURL: f.srv.URL,
		Auth: func(context.Context) (string, error) { return "Bearer tok", nil }}
	return f, New(d)
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func send(t *testing.T, w http.ResponseWriter, name string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(fixture(t, name))
}

func (f *fake) all() []req {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]req(nil), f.reqs...)
}

func (f *fake) count() int { return len(f.all()) }

func (f *fake) paths() []string {
	var out []string
	for _, r := range f.all() {
		out = append(out, r.Method+" "+r.URL.Path)
	}
	return out
}

// gqlOnly answers every GraphQL POST with a fixture.
func gqlOnly(t *testing.T, name string) func(*fake, http.ResponseWriter, *http.Request, string) {
	return func(f *fake, w http.ResponseWriter, r *http.Request, body string) {
		if r.Method != "POST" || r.URL.Path != "/graphql" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		send(t, w, name)
	}
}

// gqlBody decodes a recorded GraphQL request.
func gqlBody(t *testing.T, body string) (query string, vars map[string]string) {
	t.Helper()
	var v struct {
		Query     string            `json:"query"`
		Variables map[string]string `json:"variables"`
	}
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatalf("graphql body %q: %v", body, err)
	}
	return v.Query, v.Variables
}

func TestUser(t *testing.T) {
	_, c := newFake(t, func(f *fake, w http.ResponseWriter, r *http.Request, _ string) {
		if r.Method != "GET" || r.URL.Path != "/user" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		send(t, w, "user.json")
	})
	got, err := c.User(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != (hosting.Account{ID: "ana-dev", Login: "ana-dev", Name: "Ana Dev"}) {
		t.Fatalf("account = %+v", got)
	}

	_, c = newFake(t, func(f *fake, w http.ResponseWriter, r *http.Request, _ string) {
		_, _ = w.Write([]byte(`{"login":"nameless","name":null}`))
	})
	got, err = c.User(context.Background())
	if err != nil || got.Name != "nameless" || got.ID != "nameless" {
		t.Fatalf("no display name: %+v, %v", got, err)
	}
}

func TestUserForbiddenIsAnAccountMessage(t *testing.T) {
	_, c := newFake(t, func(f *fake, w http.ResponseWriter, r *http.Request, body string) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Resource not accessible by personal access token"}`))
	})
	_, err := c.User(context.Background())
	if err == nil || strings.Contains(err.Error(), "No access to") ||
		!strings.HasPrefix(err.Error(), "GitHub token can't read your account: Resource not accessible") {
		t.Fatalf("err = %v", err)
	}
}

func TestHeaders(t *testing.T) {
	f, c := newFake(t, func(f *fake, w http.ResponseWriter, r *http.Request, _ string) {
		switch {
		case r.URL.Path == "/graphql":
			_, _ = w.Write([]byte(`{"data":{"repository":{"pullRequests":{"totalCount":0,"nodes":[]}}}}`))
		case strings.HasSuffix(r.URL.Path, "/files"), strings.Contains(r.URL.Path, "/comments"),
			strings.HasSuffix(r.URL.Path, "/reviews"), strings.HasSuffix(r.URL.Path, "/commits"):
			_, _ = w.Write([]byte(`[]`))
		case strings.Contains(r.URL.Path, "/pulls/"):
			send(t, w, "pr_detail.json")
		default:
			send(t, w, "user.json")
		}
	})
	ctx := context.Background()
	if _, err := c.User(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.PullRequests(ctx, repo, hosting.FilterOpen, me); err != nil {
		t.Fatal(err)
	}
	if _, err := c.PullRequest(ctx, repo, 12); err != nil {
		t.Fatal(err)
	}
	if _, err := c.PullRequestFiles(ctx, repo, 12); err != nil {
		t.Fatal(err)
	}
	reqs := f.all()
	if len(reqs) < 8 {
		t.Fatalf("only %d requests: %v", len(reqs), f.paths())
	}
	for _, r := range reqs {
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("%s %s: Accept = %q", r.Method, r.URL.Path, got)
		}
		if got := r.Header.Get("X-GitHub-Api-Version"); got != "2022-11-28" {
			t.Errorf("%s %s: X-GitHub-Api-Version = %q", r.Method, r.URL.Path, got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("%s %s: Authorization = %q", r.Method, r.URL.Path, got)
		}
	}
}

func TestNewDoesNotChangeTheDoer(t *testing.T) {
	d := &hosting.Doer{Host: hosting.GitHub, BaseURL: "https://api.github.com"}
	New(d)
	if d.HTTP != nil {
		t.Fatal("New changed the caller's Doer")
	}
}

func TestStatusesBatchesOf50(t *testing.T) {
	hashes := make([]string, 120)
	for i := range hashes {
		hashes[i] = fmt.Sprintf("%040x", i+1)
	}
	f, c := newFake(t, func(f *fake, w http.ResponseWriter, r *http.Request, body string) {
		_, _ = w.Write([]byte(`{"data":{"repository":{}}}`))
	})
	got, err := c.Statuses(context.Background(), repo, hashes)
	if err != nil {
		t.Fatal(err)
	}
	reqs := f.all()
	if len(reqs) != 3 {
		t.Fatalf("requests = %d (%v), want 3", len(reqs), f.paths())
	}
	for i, r := range reqs {
		if r.Method != "POST" || r.URL.Path != "/graphql" {
			t.Errorf("request %d = %s %s", i, r.Method, r.URL.Path)
		}
	}
	q1, _ := gqlBody(t, reqs[0].Body)
	q2, v2 := gqlBody(t, reqs[1].Body)
	q3, _ := gqlBody(t, reqs[2].Body)
	if !strings.Contains(q2, "c49:") || strings.Contains(q2, "c50:") {
		t.Errorf("second query must hold c0..c49 only: %.200s", q2)
	}
	if !strings.Contains(q1, fmt.Sprintf("%q", hashes[0])) || !strings.Contains(q2, fmt.Sprintf("%q", hashes[50])) ||
		!strings.Contains(q3, fmt.Sprintf("%q", hashes[119])) || strings.Contains(q3, "c20:") {
		t.Errorf("hashes are not spread 50/50/20 over the queries")
	}
	if v2["owner"] != "acme" || v2["name"] != "api" {
		t.Errorf("variables = %v", v2)
	}
	if len(got) != 120 || got[hashes[0]].State != hosting.None || got[hashes[119]].Hash != hashes[119] {
		t.Errorf("got %d entries, first %+v", len(got), got[hashes[0]])
	}
}

func TestStatusesMapping(t *testing.T) {
	f, c := newFake(t, gqlOnly(t, "statuses_mapping.json"))
	got, err := c.Statuses(context.Background(), repo, []string{hashA, hashB, hashC, hashD, hashE})
	if err != nil {
		t.Fatal(err)
	}
	if f.count() != 1 {
		t.Fatalf("requests = %d, want 1", f.count())
	}
	want := map[string]hosting.State{
		hashA: hosting.Stopped, // check run cancelled
		hashB: hosting.Failed,  // status context ERROR beats a green run
		hashC: hosting.None,    // unknown commit (null object)
		hashD: hosting.None,    // no rollup
		// test failed then re-run green (counts green), docs skipped (green),
		// e2e in progress and deploy EXPECTED (running): no failure, so running.
		hashE: hosting.Running,
	}
	for h, st := range want {
		if got[h].State != st || got[h].Hash != h {
			t.Errorf("%s: got %+v, want state %s", h[:1], got[h], st)
		}
	}
	a := got[hashA].Checks
	if len(a) != 1 || a[0].Key != "build" || a[0].Name != "build" || a[0].URL != "https://github.com/acme/api/actions/runs/1" ||
		a[0].DurationSec != 95 || a[0].Started.IsZero() || a[0].Created.IsZero() {
		t.Errorf("A checks = %+v", a)
	}
	var legacy *hosting.Check
	for i, ck := range got[hashB].Checks {
		if ck.Key == "ci/legacy" {
			legacy = &got[hashB].Checks[i]
		}
	}
	if legacy == nil || legacy.State != hosting.Failed || legacy.URL != "https://ci.example.com/7" {
		t.Errorf("status context = %+v", legacy)
	}
	if got[hashC].Checks != nil || got[hashD].Checks != nil {
		t.Errorf("none statuses must carry no checks")
	}
	byKey := map[string]hosting.Check{}
	for _, ck := range got[hashE].Checks {
		byKey[ck.Key] = ck
	}
	if len(byKey) != 4 || byKey["test"].State != hosting.Success || byKey["docs"].State != hosting.Success ||
		byKey["e2e"].State != hosting.Running || byKey["deploy"].State != hosting.Running {
		t.Errorf("E checks = %+v", got[hashE].Checks)
	}
}

func TestCheckStateMapping(t *testing.T) {
	cases := []struct {
		n    gqlContext
		want hosting.State
		ok   bool
	}{
		{gqlContext{Typename: "CheckRun", Status: "QUEUED"}, hosting.Running, true},
		{gqlContext{Typename: "CheckRun", Status: "IN_PROGRESS", Conclusion: "SUCCESS"}, hosting.Running, true},
		{gqlContext{Typename: "CheckRun", Status: "COMPLETED", Conclusion: "SUCCESS"}, hosting.Success, true},
		{gqlContext{Typename: "CheckRun", Status: "COMPLETED", Conclusion: "NEUTRAL"}, hosting.Success, true},
		{gqlContext{Typename: "CheckRun", Status: "COMPLETED", Conclusion: "SKIPPED"}, hosting.Success, true},
		{gqlContext{Typename: "CheckRun", Status: "COMPLETED", Conclusion: "FAILURE"}, hosting.Failed, true},
		{gqlContext{Typename: "CheckRun", Status: "COMPLETED", Conclusion: "TIMED_OUT"}, hosting.Failed, true},
		{gqlContext{Typename: "CheckRun", Status: "COMPLETED", Conclusion: "STARTUP_FAILURE"}, hosting.Failed, true},
		{gqlContext{Typename: "CheckRun", Status: "COMPLETED", Conclusion: "ACTION_REQUIRED"}, hosting.Failed, true},
		{gqlContext{Typename: "CheckRun", Status: "COMPLETED", Conclusion: "CANCELLED"}, hosting.Stopped, true},
		{gqlContext{Typename: "CheckRun", Status: "COMPLETED", Conclusion: "STALE"}, hosting.Stopped, true},
		{gqlContext{Typename: "CheckRun", Status: "COMPLETED", Conclusion: "SOMETHING_NEW"}, "", false},
		{gqlContext{Typename: "StatusContext", State: "SUCCESS"}, hosting.Success, true},
		{gqlContext{Typename: "StatusContext", State: "FAILURE"}, hosting.Failed, true},
		{gqlContext{Typename: "StatusContext", State: "ERROR"}, hosting.Failed, true},
		{gqlContext{Typename: "StatusContext", State: "PENDING"}, hosting.Running, true},
		{gqlContext{Typename: "StatusContext", State: "EXPECTED"}, hosting.Running, true},
		{gqlContext{Typename: "StatusContext", State: "WHATEVER"}, "", false},
		{gqlContext{Typename: "Other"}, "", false},
	}
	for _, tc := range cases {
		ck, ok := tc.n.toCheck()
		if ok != tc.ok || ck.State != tc.want {
			t.Errorf("%+v: got (%s, %v), want (%s, %v)", tc.n, ck.State, ok, tc.want, tc.ok)
		}
	}
}

func TestQueuedCheckRunHasNoStartTime(t *testing.T) {
	// startedAt is null until a run starts: no invented start, and a queued
	// re-run beats the older finished run of the same check.
	var n gqlContext
	if err := json.Unmarshal([]byte(`{"__typename":"CheckRun","name":"build","status":"QUEUED","conclusion":null,"detailsUrl":"","startedAt":null,"completedAt":null}`), &n); err != nil {
		t.Fatal(err)
	}
	queued, ok := n.toCheck()
	if !ok || queued.State != hosting.Running || !queued.Started.IsZero() || !queued.Created.IsZero() {
		t.Fatalf("queued = %+v ok=%v", queued, ok)
	}
	old := hosting.Check{Key: "build", Name: "build", State: hosting.Failed, Created: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)}
	if st := hosting.Aggregate([]hosting.Check{old, queued}); st != hosting.Running {
		t.Fatalf("aggregate = %s, want running", st)
	}
}

func TestStatusesSkipsInvalidHashes(t *testing.T) {
	bad := `abc"){id}}} mutation{x`
	f, c := newFake(t, gqlOnly(t, "statuses_mapping.json"))
	hashes := []string{bad, "abc123", strings.Repeat("g", 40), hashA, strings.ToUpper(hashA), ""}
	got, err := c.Statuses(context.Background(), repo, hashes)
	if err != nil {
		t.Fatal(err)
	}
	reqs := f.all()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d, want 1 (the valid hash)", len(reqs))
	}
	q, _ := gqlBody(t, reqs[0].Body)
	if strings.Contains(q, "mutation") || strings.Contains(q, "abc123") || strings.Contains(q, "gggg") {
		t.Errorf("an invalid hash reached the query: %s", q)
	}
	if strings.Count(q, "object(oid:") != 1 {
		t.Errorf("the same commit in two cases must be asked once: %s", q)
	}
	if got[bad].State != hosting.None || got["abc123"].State != hosting.None {
		t.Errorf("invalid hashes must map to none: %+v", got)
	}
	if got[hashA].State != hosting.Stopped || got[strings.ToUpper(hashA)].State != hosting.Stopped {
		t.Errorf("valid hash: %+v / %+v", got[hashA], got[strings.ToUpper(hashA)])
	}
	if _, ok := got[""]; ok {
		t.Errorf("empty hash must be dropped")
	}

	f2, c2 := newFake(t, gqlOnly(t, "statuses_mapping.json"))
	got, err = c2.Statuses(context.Background(), repo, []string{"zz"})
	if err != nil || f2.count() != 0 || got["zz"].State != hosting.None {
		t.Errorf("only invalid hashes must cost no request: %v, %d, %+v", err, f2.count(), got)
	}
}

func TestOwnerAndNameAreVariables(t *testing.T) {
	evil := hosting.Repo{Host: hosting.GitHub, Owner: `o"}){x`, Name: `n$ame`}
	f, c := newFake(t, gqlOnly(t, "prs.json"))
	if _, _, err := c.PullRequests(context.Background(), evil, hosting.FilterOpen, me); err != nil {
		t.Fatal(err)
	}
	if _, err := c.PullRequestCount(context.Background(), evil); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Statuses(context.Background(), evil, []string{hashA}); err != nil {
		t.Fatal(err)
	}
	for _, r := range f.all() {
		q, v := gqlBody(t, r.Body)
		if strings.Contains(q, evil.Owner) || strings.Contains(q, evil.Name) {
			t.Errorf("owner or name interpolated into the query: %s", q)
		}
		if v["owner"] != evil.Owner || v["name"] != evil.Name {
			t.Errorf("variables = %v", v)
		}
		if !strings.Contains(q, "repository(owner: $owner, name: $name)") {
			t.Errorf("query = %s", q)
		}
	}
}

func notFoundAnswer(w http.ResponseWriter) {
	_, _ = w.Write([]byte(`{"data":{"repository":null},"errors":[{"type":"NOT_FOUND","path":["repository"],` +
		`"message":"Could not resolve to a Repository with the name 'acme/api'."}]}`))
}

func TestStatusesRepoNotFound(t *testing.T) {
	_, c := newFake(t, func(f *fake, w http.ResponseWriter, r *http.Request, _ string) { notFoundAnswer(w) })
	_, err := c.Statuses(context.Background(), repo, []string{hashA})
	var na *hosting.NoAccessError
	if !errors.As(err, &na) || na.Repo != "acme/api" {
		t.Fatalf("err = %v, want *NoAccessError for acme/api", err)
	}
	if err.Error() != "No access to acme/api with this account" {
		t.Errorf("text = %q", err.Error())
	}
	if _, err = c.Checks(context.Background(), repo, hashA); !errors.As(err, &na) {
		t.Errorf("Checks err = %v", err)
	}
}

func TestGraphQLErrorsInA200Answer(t *testing.T) {
	answer := ""
	_, c := newFake(t, func(f *fake, w http.ResponseWriter, r *http.Request, _ string) {
		_, _ = w.Write([]byte(answer))
	})
	ctx := context.Background()

	answer = `{"data":null,"errors":[{"type":"RATE_LIMITED","message":"API rate limit exceeded for user ID 1."}]}`
	_, _, err := c.PullRequests(ctx, repo, hosting.FilterOpen, me)
	var rl *hosting.RateLimitError
	if !errors.As(err, &rl) || rl.Host != hosting.GitHub {
		t.Errorf("rate limit: err = %v", err)
	}

	answer = `{"data":null,"errors":[{"type":"MAX_NODE_LIMIT_EXCEEDED","message":"Node limit of 500000 exceeded."},{"type":"X","message":"second"}]}`
	_, err = c.PullRequestCount(ctx, repo)
	var na *hosting.NoAccessError
	if err == nil || errors.As(err, &rl) || errors.As(err, &na) || err.Error() != "GitHub: Node limit of 500000 exceeded." {
		t.Errorf("other: err = %v", err)
	}

	// NOT_FOUND somewhere else than the repository is not a missing repository.
	answer = `{"data":{"repository":{}},"errors":[{"type":"NOT_FOUND","path":["repository","pullRequest"],"message":"no such pull request"}]}`
	_, err = c.PullRequestCount(ctx, repo)
	if err == nil || errors.As(err, &na) {
		t.Errorf("nested NOT_FOUND: err = %v", err)
	}

	// A null repository without an error is no access as well.
	answer = `{"data":{"repository":null}}`
	if _, err = c.PullRequestCount(ctx, repo); !errors.As(err, &na) {
		t.Errorf("null repository: err = %v", err)
	}
}

func TestRateLimitedGraphQLUsesTheKnownReset(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	limits := hosting.NewLimits(func() time.Time { return now })
	reset := now.Add(10 * time.Minute)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
		_, _ = w.Write([]byte(`{"data":null,"errors":[{"type":"RATE_LIMITED","message":"limit"}]}`))
	}))
	defer srv.Close()
	c := New(&hosting.Doer{Host: hosting.GitHub, BaseURL: srv.URL, Limits: limits})
	_, err := c.PullRequestCount(context.Background(), repo)
	var rl *hosting.RateLimitError
	if !errors.As(err, &rl) || !rl.Known || !rl.Until.Equal(reset) {
		t.Fatalf("err = %#v", err)
	}
}

func TestPullRequestsFiltersMineAndReview(t *testing.T) {
	_, c := newFake(t, gqlOnly(t, "prs.json"))
	ctx := context.Background()
	nums := func(prs []hosting.PullRequest) []int {
		var out []int
		for _, p := range prs {
			out = append(out, p.Number)
		}
		return out
	}

	open, more, err := c.PullRequests(ctx, repo, hosting.FilterOpen, me)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(nums(open)) != "[12 11 10 9]" || !more {
		t.Errorf("open = %v more = %v (totalCount is 130)", nums(open), more)
	}
	mine, more, err := c.PullRequests(ctx, repo, hosting.FilterMine, me)
	if err != nil || fmt.Sprint(nums(mine)) != "[12 9]" || more {
		t.Errorf("mine = %v more = %v err = %v", nums(mine), more, err)
	}
	review, more, err := c.PullRequests(ctx, repo, hosting.FilterReview, me)
	if err != nil || fmt.Sprint(nums(review)) != "[11]" || more {
		t.Errorf("review = %v more = %v err = %v (login case must not matter)", nums(review), more, err)
	}

	p := open[0]
	want := hosting.PullRequest{
		Number: 12, Title: "Add retries", Author: "ana-dev", AuthorID: "ana-dev", Source: "feature/retries", Target: "main",
		HeadHash: hashA, URL: "https://github.com/acme/api/pull/12", Approvals: 1, Comments: 3,
		Updated: time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC), State: hosting.Success,
	}
	if p != want {
		t.Errorf("PR 12 = %+v\nwant   %+v", p, want)
	}
	if !open[1].Draft || open[1].State != hosting.Running {
		t.Errorf("PR 11 = %+v", open[1])
	}
	if open[2].State != "" || open[2].Author != "ghost" {
		t.Errorf("PR 10 (no rollup, no author) = %+v", open[2])
	}
	if open[3].State != hosting.Failed {
		t.Errorf("PR 9 state = %q", open[3].State)
	}
}

func TestPullRequestsForkWhenHeadRepoNull(t *testing.T) {
	_, c := newFake(t, gqlOnly(t, "prs.json"))
	prs, _, err := c.PullRequests(context.Background(), repo, hosting.FilterOpen, me)
	if err != nil {
		t.Fatal(err)
	}
	fork := map[int]bool{}
	for _, p := range prs {
		fork[p.Number] = p.Fork
	}
	// 12 and 11: same repository. 10: head repository deleted (null). 9: cross repository.
	if fork[12] || fork[11] || !fork[10] || !fork[9] {
		t.Errorf("fork flags = %v", fork)
	}
}

func TestPullRequestsQueryShape(t *testing.T) {
	f, c := newFake(t, gqlOnly(t, "prs.json"))
	if _, _, err := c.PullRequests(context.Background(), repo, hosting.FilterOpen, me); err != nil {
		t.Fatal(err)
	}
	q, _ := gqlBody(t, f.all()[0].Body)
	for _, part := range []string{
		"pullRequests(states: OPEN, first: 100, orderBy: {field: UPDATED_AT, direction: DESC})", "totalCount",
		"headRefOid", "isCrossRepository", "headRepository { nameWithOwner }", "comments { totalCount }",
		"latestReviews(first: 50)", "reviewRequests(first: 50)", "commits(last: 1)", "statusCheckRollup { state }",
	} {
		if !strings.Contains(q, part) {
			t.Errorf("query lacks %q", part)
		}
	}
}

func TestPullRequestCount(t *testing.T) {
	f, c := newFake(t, func(f *fake, w http.ResponseWriter, r *http.Request, _ string) {
		_, _ = w.Write([]byte(`{"data":{"repository":{"pullRequests":{"totalCount":130}}}}`))
	})
	got, err := c.PullRequestCount(context.Background(), repo)
	if err != nil || got != (hosting.Count{N: 130, More: false}) {
		t.Fatalf("count = %+v, %v", got, err)
	}
	q, _ := gqlBody(t, f.all()[0].Body)
	if !strings.Contains(q, "pullRequests(states: OPEN) { totalCount }") {
		t.Errorf("query = %s", q)
	}
}

// detailHandler serves a pull request with the standard fixtures.
func detailHandler(t *testing.T, checks func(w http.ResponseWriter)) func(*fake, http.ResponseWriter, *http.Request, string) {
	return func(f *fake, w http.ResponseWriter, r *http.Request, _ string) {
		const pr = "/repos/acme/api/pulls/12"
		switch r.URL.Path {
		case "/graphql":
			checks(w)
		case pr:
			send(t, w, "pr_detail.json")
		case pr + "/reviews":
			send(t, w, "pr_reviews.json")
		case pr + "/commits":
			send(t, w, "pr_commits.json")
		case pr + "/comments":
			send(t, w, "pr_review_comments.json")
		case "/repos/acme/api/issues/12/comments":
			send(t, w, "pr_issue_comments.json")
		default:
			t.Errorf("unexpected request %s", r.URL)
			http.NotFound(w, r)
		}
	}
}

func okChecks(w http.ResponseWriter) {
	_, _ = w.Write([]byte(`{"data":{"repository":{"c0":{"statusCheckRollup":{"contexts":{"nodes":[` +
		`{"__typename":"CheckRun","name":"build","status":"COMPLETED","conclusion":"SUCCESS","detailsUrl":"https://x/1",` +
		`"startedAt":"2026-10-22T09:00:00Z","completedAt":"2026-10-22T09:02:00Z"}]}}}}}}`))
}

func TestPullRequestDetailMergesCommentsInOrder(t *testing.T) {
	f, c := newFake(t, detailHandler(t, okChecks))
	d, err := c.PullRequest(context.Background(), repo, 12)
	if err != nil {
		t.Fatal(err)
	}

	// Review bodies, issue comments and review comments, oldest first: the
	// COMMENTED review (T1), the change request (T2), the inline question (T3),
	// the approval (T4), the issue comment (T5), the reply (T6). Empty review
	// bodies, a dismissal and pending reviews add no row.
	var ids []string
	for _, cm := range d.Comments {
		ids = append(ids, cm.ID)
	}
	wantIDs := []string{"review-501", "review-503", "review-comment-8001", "review-504", "issue-7001", "review-comment-8002"}
	if fmt.Sprint(ids) != fmt.Sprint(wantIDs) {
		t.Fatalf("order = %v, want %v", ids, wantIDs)
	}
	byID := map[string]hosting.Comment{}
	for _, cm := range d.Comments {
		byID[cm.ID] = cm
	}
	if cm := byID["review-501"]; cm.Verdict != "commented" || cm.Author != "bob" || cm.Body != "Looks fine so far." {
		t.Errorf("review 501 = %+v", cm)
	}
	if byID["review-503"].Verdict != "requested changes" || byID["review-504"].Verdict != "approved" {
		t.Errorf("verdicts: %+v / %+v", byID["review-503"], byID["review-504"])
	}
	first, reply := byID["review-comment-8001"], byID["review-comment-8002"]
	if first.Parent != "" || first.Path != "retry.go" || first.Line != 14 {
		t.Errorf("first inline comment = %+v (line is original_line when line is null)", first)
	}
	if reply.Parent != first.ID || reply.Line != 15 || reply.Path != "retry.go" || reply.Author != "ana-dev" {
		t.Errorf("reply = %+v", reply)
	}
	if cm := byID["issue-7001"]; cm.Parent != "" || cm.Path != "" || cm.Line != 0 || cm.Author != "dana" {
		t.Errorf("issue comment = %+v", cm)
	}
	if d.CommentsMore {
		t.Errorf("CommentsMore set")
	}

	if d.Number != 12 || d.Title != "Add retries" || d.Description != "Retries the **call**." || d.Author != "ana-dev" ||
		d.Source != "feature/retries" || d.Target != "main" || d.HeadHash != hashA || d.Fork ||
		d.URL != "https://github.com/acme/api/pull/12" || d.PullRequest.Comments != 1 ||
		!d.Created.Equal(time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)) {
		t.Errorf("detail = %+v", d)
	}
	// bob approved (his later COMMENTED review does not matter), cy asked for
	// changes, eve's approval was dismissed, fay's review is pending (draft),
	// dana was requested and bob is requested again but keeps his verdict.
	wantRev := []hosting.Reviewer{{Name: "bob", Verdict: "approved"}, {Name: "cy", Verdict: "changes"}, {Name: "dana", Verdict: "pending"}}
	if fmt.Sprint(d.Reviewers) != fmt.Sprint(wantRev) {
		t.Errorf("reviewers = %+v, want %+v", d.Reviewers, wantRev)
	}
	if d.Approvals != 1 {
		t.Errorf("approvals = %d", d.Approvals)
	}
	if len(d.Commits) != 2 || d.Commits[0] != (hosting.PRCommit{Hash: strings.Repeat("1", 40), Short: "1111111", Subject: "Add retry loop", Author: "ana-dev"}) ||
		d.Commits[1].Author != "Ana D." {
		t.Errorf("commits = %+v", d.Commits)
	}
	if d.Checks.State != hosting.Success || d.Checks.Hash != hashA || len(d.Checks.Checks) != 1 {
		t.Errorf("checks = %+v", d.Checks)
	}

	for _, r := range f.all() {
		if strings.Contains(r.URL.Path, "/reviews") || strings.Contains(r.URL.Path, "/commits") || strings.Contains(r.URL.Path, "/comments") {
			if r.URL.Query().Get("per_page") != "100" || r.URL.Query().Get("page") != "1" {
				t.Errorf("%s query = %q", r.URL.Path, r.URL.RawQuery)
			}
		}
	}
}

func TestPullRequestFork(t *testing.T) {
	_, c := newFake(t, func(f *fake, w http.ResponseWriter, r *http.Request, _ string) {
		switch {
		case r.URL.Path == "/graphql":
			okChecks(w)
		case r.URL.Path == "/repos/acme/api/pulls/5":
			_, _ = w.Write([]byte(`{"number":5,"title":"t","user":null,"head":{"ref":"x","sha":"` + hashB + `","repo":null},"base":{"ref":"main","repo":{"full_name":"acme/api"}}}`))
		default:
			_, _ = w.Write([]byte(`[]`))
		}
	})
	d, err := c.PullRequest(context.Background(), repo, 5)
	if err != nil || !d.Fork || d.Author != "ghost" || len(d.Comments) != 0 {
		t.Fatalf("detail = %+v, %v", d, err)
	}
}

func TestPullRequestChecksFailure(t *testing.T) {
	t.Run("other errors leave none", func(t *testing.T) {
		for name, h := range map[string]func(http.ResponseWriter){
			"graphql error": func(w http.ResponseWriter) {
				_, _ = w.Write([]byte(`{"data":null,"errors":[{"type":"FORBIDDEN","message":"nope"}]}`))
			},
			"rate limit": func(w http.ResponseWriter) {
				_, _ = w.Write([]byte(`{"data":null,"errors":[{"type":"RATE_LIMITED","message":"slow"}]}`))
			},
			"no access": notFoundAnswer,
			"http error": func(w http.ResponseWriter) {
				http.Error(w, `{"message":"boom"}`, http.StatusBadGateway)
			},
		} {
			_, c := newFake(t, detailHandler(t, h))
			d, err := c.PullRequest(context.Background(), repo, 12)
			if err != nil {
				t.Errorf("%s: the detail must still load: %v", name, err)
				continue
			}
			if d.Checks.State != hosting.None || d.Checks.Hash != hashA || d.Checks.Checks != nil {
				t.Errorf("%s: checks = %+v", name, d.Checks)
			}
			if d.Title != "Add retries" || len(d.Comments) == 0 {
				t.Errorf("%s: detail = %+v", name, d)
			}
		}
	})
	t.Run("a rejected token propagates", func(t *testing.T) {
		_, c := newFake(t, detailHandler(t, func(w http.ResponseWriter) {
			http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
		}))
		_, err := c.PullRequest(context.Background(), repo, 12)
		if !errors.Is(err, hosting.ErrRejected) {
			t.Fatalf("err = %v, want ErrRejected", err)
		}
	})
}

func TestNoAccessNamesTheRepository(t *testing.T) {
	_, c := newFake(t, func(f *fake, w http.ResponseWriter, r *http.Request, _ string) {
		if r.URL.Path == "/graphql" {
			notFoundAnswer(w)
			return
		}
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	})
	ctx := context.Background()
	calls := map[string]func() error{
		"Statuses":         func() error { _, err := c.Statuses(ctx, repo, []string{hashA}); return err },
		"Checks":           func() error { _, err := c.Checks(ctx, repo, hashA); return err },
		"PullRequests":     func() error { _, _, err := c.PullRequests(ctx, repo, hosting.FilterOpen, me); return err },
		"PullRequestCount": func() error { _, err := c.PullRequestCount(ctx, repo); return err },
		"PullRequest":      func() error { _, err := c.PullRequest(ctx, repo, 12); return err },
		"PullRequestFiles": func() error { _, err := c.PullRequestFiles(ctx, repo, 12); return err },
	}
	for name, call := range calls {
		var na *hosting.NoAccessError
		err := call()
		if !errors.As(err, &na) || na.Repo != "acme/api" {
			t.Errorf("%s: err = %v, want *NoAccessError{Repo: acme/api}", name, err)
		}
	}
}

func TestNoAccessInLaterRESTCalls(t *testing.T) {
	// The pull request loads but one of the lists is forbidden.
	for _, denied := range []string{"/reviews", "/commits", "/comments"} {
		_, c := newFake(t, func(f *fake, w http.ResponseWriter, r *http.Request, b string) {
			if strings.HasSuffix(r.URL.Path, denied) {
				http.Error(w, `{"message":"Resource not accessible by personal access token"}`, http.StatusForbidden)
				return
			}
			detailHandler(t, okChecks)(f, w, r, b)
		})
		_, err := c.PullRequest(context.Background(), repo, 12)
		var na *hosting.NoAccessError
		if !errors.As(err, &na) || na.Repo != "acme/api" {
			t.Errorf("%s: err = %v", denied, err)
		}
	}
}

func TestPullRequestFilesNoPatch(t *testing.T) {
	f, c := newFake(t, func(f *fake, w http.ResponseWriter, r *http.Request, _ string) {
		if r.URL.Path != "/repos/acme/api/pulls/12/files" || r.URL.Query().Get("per_page") != "100" || r.URL.Query().Get("page") != "1" {
			t.Errorf("request = %s", r.URL)
		}
		send(t, w, "files.json")
	})
	files, err := c.PullRequestFiles(context.Background(), repo, 12)
	if err != nil {
		t.Fatal(err)
	}
	if f.count() != 1 || len(files) != 3 {
		t.Fatalf("requests %d, files %+v", f.count(), files)
	}
	if files[0].Path != "retry.go" || files[0].Status != "modified" || files[0].NoDiff || !strings.HasPrefix(files[0].Patch, "@@ -1,2") {
		t.Errorf("file 0 = %+v", files[0])
	}
	if files[1].Path != "logo.png" || files[1].Patch != "" || !files[1].NoDiff || files[1].Status != "added" {
		t.Errorf("file 1 (no patch) = %+v", files[1])
	}
	if files[2].Path != "new_name.go" || files[2].OldPath != "old_name.go" || files[2].Status != "renamed" {
		t.Errorf("file 2 = %+v", files[2])
	}
}

func TestPullRequestFilesPagination(t *testing.T) {
	filesPage := func(n int) string {
		var items []string
		for i := 0; i < 100; i++ {
			items = append(items, fmt.Sprintf(`{"filename":"f%d_%d","status":"added","patch":"@@"}`, n, i))
		}
		return "[" + strings.Join(items, ",") + "]"
	}
	t.Run("short page ends it", func(t *testing.T) {
		f, c := newFake(t, func(fk *fake, w http.ResponseWriter, r *http.Request, _ string) {
			switch r.URL.Query().Get("page") {
			case "1", "2":
				_, _ = w.Write([]byte(filesPage(1)))
			default:
				_, _ = w.Write([]byte(`[{"filename":"last","status":"added"}]`))
			}
		})
		files, err := c.PullRequestFiles(context.Background(), repo, 1)
		if err != nil || len(files) != 201 || f.count() != 3 {
			t.Fatalf("files %d, requests %d, err %v", len(files), f.count(), err)
		}
	})
	t.Run("30 pages at most", func(t *testing.T) {
		f, c := newFake(t, func(fk *fake, w http.ResponseWriter, r *http.Request, _ string) {
			_, _ = w.Write([]byte(filesPage(1)))
		})
		files, err := c.PullRequestFiles(context.Background(), repo, 1)
		if err != nil || len(files) != 3000 || f.count() != 30 {
			t.Fatalf("files %d, requests %d, err %v", len(files), f.count(), err)
		}
	})
}

func TestCommentCap(t *testing.T) {
	// 600 issue comments: the list stops at 500 and says there are more, and
	// no more pages than needed are read.
	item := func(id int) string {
		return fmt.Sprintf(`{"id":%d,"user":{"login":"u"},"body":"b","created_at":"%s"}`, id,
			time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(id)*time.Minute).Format(time.RFC3339))
	}
	f, c := newFake(t, func(fk *fake, w http.ResponseWriter, r *http.Request, b string) {
		switch {
		case r.URL.Path == "/graphql":
			okChecks(w)
		case r.URL.Path == "/repos/acme/api/pulls/12":
			send(t, w, "pr_detail.json")
		case r.URL.Path == "/repos/acme/api/issues/12/comments":
			n, _ := strconv.Atoi(r.URL.Query().Get("page"))
			var items []string
			for i := (n - 1) * 100; i < n*100 && i < 600; i++ {
				items = append(items, item(i+1))
			}
			_, _ = w.Write([]byte("[" + strings.Join(items, ",") + "]"))
		default:
			_, _ = w.Write([]byte(`[]`))
		}
	})
	d, err := c.PullRequest(context.Background(), repo, 12)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Comments) != 500 || !d.CommentsMore {
		t.Fatalf("comments %d, more %v", len(d.Comments), d.CommentsMore)
	}
	if d.Comments[0].ID != "issue-1" || d.Comments[499].ID != "issue-500" {
		t.Errorf("the oldest 500 must stay: %s .. %s", d.Comments[0].ID, d.Comments[499].ID)
	}
	issuePages := 0
	for _, p := range f.paths() {
		if p == "GET /repos/acme/api/issues/12/comments" {
			issuePages++
		}
	}
	if issuePages != 6 {
		t.Errorf("issue comment pages = %d, want 6", issuePages)
	}

	// Exactly 500 is not "more".
	_, c = newFake(t, func(fk *fake, w http.ResponseWriter, r *http.Request, b string) {
		switch {
		case r.URL.Path == "/graphql":
			okChecks(w)
		case r.URL.Path == "/repos/acme/api/pulls/12":
			send(t, w, "pr_detail.json")
		case r.URL.Path == "/repos/acme/api/issues/12/comments":
			n, _ := strconv.Atoi(r.URL.Query().Get("page"))
			var items []string
			for i := (n - 1) * 100; i < n*100 && i < 500; i++ {
				items = append(items, item(i+1))
			}
			_, _ = w.Write([]byte("[" + strings.Join(items, ",") + "]"))
		default:
			_, _ = w.Write([]byte(`[]`))
		}
	})
	d, err = c.PullRequest(context.Background(), repo, 12)
	if err != nil || len(d.Comments) != 500 || d.CommentsMore {
		t.Fatalf("exactly 500: %d, more %v, err %v", len(d.Comments), d.CommentsMore, err)
	}
}

func TestPaginationStaysOnTheAPIHost(t *testing.T) {
	// A Link header (or a link in the body) pointing elsewhere is never
	// followed: only page=N on the API host is requested.
	var other int
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { other++ }))
	defer evil.Close()
	f, c := newFake(t, func(fk *fake, w http.ResponseWriter, r *http.Request, b string) {
		w.Header().Set("Link", `<`+evil.URL+`/repos/acme/api/pulls/1/files?page=2>; rel="next"`)
		_, _ = w.Write([]byte(`[{"filename":"a","status":"added"}]`))
	})
	files, err := c.PullRequestFiles(context.Background(), repo, 1)
	if err != nil || len(files) != 1 || f.count() != 1 || other != 0 {
		t.Fatalf("files %d, requests %d, other host %d, err %v", len(files), f.count(), other, err)
	}
}

func TestPathsAreEscaped(t *testing.T) {
	f, c := newFake(t, func(fk *fake, w http.ResponseWriter, r *http.Request, b string) { _, _ = w.Write([]byte(`[]`)) })
	odd := hosting.Repo{Host: hosting.GitHub, Owner: "a/b", Name: "c d?x"}
	if _, err := c.PullRequestFiles(context.Background(), odd, 3); err != nil {
		t.Fatal(err)
	}
	got := f.all()[0].URL
	if got.EscapedPath() != "/repos/a%2Fb/c%20d%3Fx/pulls/3/files" {
		t.Errorf("path = %q", got.EscapedPath())
	}
}

func TestIsOID(t *testing.T) {
	for s, want := range map[string]bool{
		hashA: true, strings.ToUpper(hashA): true, strings.Repeat("0", 64): true,
		"": false, "abc": false, strings.Repeat("a", 39): false, strings.Repeat("a", 41): false,
		strings.Repeat("a", 39) + `"`: false, strings.Repeat("a", 39) + "g": false, strings.Repeat("a", 39) + "\n": false,
	} {
		if isOID(s) != want {
			t.Errorf("isOID(%q) = %v", s, !want)
		}
	}
}
