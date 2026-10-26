package bitbucket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

var repo = hosting.Repo{Host: hosting.Bitbucket, Owner: "acme", Name: "api", Remote: "origin"}

var me = hosting.Account{ID: "{aaaa-1111}", Name: "Ana Dev"}

var (
	hashA = strings.Repeat("a", 40)
	hashB = strings.Repeat("b", 40)
	hashC = strings.Repeat("c", 40)
	hashD = strings.Repeat("d", 40)
	hashZ = strings.Repeat("e", 40)
)

// fake is an httptest server that records every request (path and query) and
// answers through a handler.
type fake struct {
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []*url.URL
}

func newFake(t *testing.T, h func(f *fake, w http.ResponseWriter, r *http.Request)) (*fake, hosting.Client) {
	t.Helper()
	f := &fake{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.reqs = append(f.reqs, r.URL)
		f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "no auth", http.StatusUnauthorized)
			return
		}
		h(f, w, r)
	}))
	t.Cleanup(f.srv.Close)
	d := &hosting.Doer{Host: hosting.Bitbucket, BaseURL: f.srv.URL + "/2.0",
		Auth: func(context.Context) (string, error) { return "Bearer tok", nil }}
	return f, New(d)
}

// fixture sends testdata/name with HOST replaced by the server's address.
func (f *fake) fixture(t *testing.T, w http.ResponseWriter, name string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(strings.ReplaceAll(string(b), "HOST", f.srv.URL)))
}

func (f *fake) paths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, u := range f.reqs {
		out = append(out, u.Path)
	}
	return out
}

func (f *fake) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reqs)
}

func (f *fake) last() *url.URL {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reqs[len(f.reqs)-1]
}

const base = "/2.0/repositories/acme/api"

func pipelinesHandler(t *testing.T) func(*fake, http.ResponseWriter, *http.Request) {
	return func(f *fake, w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != base+"/pipelines" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		if q.Get("sort") != "-created_on" || q.Get("pagelen") != "100" {
			t.Errorf("pipelines query = %q", r.URL.RawQuery)
		}
		f.fixture(t, w, "pipelines_"+q.Get("page")+".json")
	}
}

func TestUser(t *testing.T) {
	_, c := newFake(t, func(f *fake, w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/2.0/user" {
			t.Errorf("path = %q", r.URL.Path)
		}
		f.fixture(t, w, "user.json")
	})
	got, err := c.User(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != (hosting.Account{ID: "{aaaa-1111}", Name: "Ana Dev"}) {
		t.Fatalf("account = %+v", got)
	}
}

func TestUserForbiddenIsAnAccountMessage(t *testing.T) {
	_, c := newFake(t, func(f *fake, w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"Your credentials lack one or more required privilege scopes."}}`))
	})
	_, err := c.User(context.Background())
	if err == nil || strings.Contains(err.Error(), "No access to") ||
		!strings.HasPrefix(err.Error(), "Bitbucket token can't read your account") ||
		!strings.Contains(err.Error(), "privilege scopes") {
		t.Fatalf("err = %v", err)
	}
	var na *hosting.NoAccessError
	if errors.As(err, &na) {
		t.Fatalf("still a NoAccessError: %v", err)
	}
}

func TestStatusesFromPipelines(t *testing.T) {
	f, c := newFake(t, pipelinesHandler(t))
	got, err := c.Statuses(context.Background(), repo, []string{hashA, hashB, hashC, hashD, hashZ})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]hosting.State{
		hashA: hosting.Success, // failed (#10) then re-run green (#11) on the same ref
		hashB: hosting.Success,
		hashC: hosting.Stopped, // EXPIRED, from page 2
		hashD: hosting.Running,
		hashZ: hosting.None, // on no page
	}
	for h, st := range want {
		if got[h].State != st || got[h].Hash != h {
			t.Errorf("%s: got %+v, want state %s", h[:1], got[h], st)
		}
	}
	if n := len(got[hashA].Checks); n != 1 || got[hashA].Checks[0].Name != "Pipeline #11" {
		t.Errorf("A checks = %+v, want only the re-run", got[hashA].Checks)
	}
	if got[hashZ].Checks != nil {
		t.Errorf("Z checks = %+v", got[hashZ].Checks)
	}
	if n := f.count(); n != 3 {
		t.Errorf("requests = %d (%v), want 3 (never past the third page)", n, f.paths())
	}
	b := got[hashB].Checks[0]
	if b.Key != "pipeline_ref_target|main|branches:main" || b.Name != "Pipeline #12" ||
		b.URL != "https://bitbucket.org/acme/api/pipelines/results/12" || b.DurationSec != 95 ||
		b.Created.IsZero() {
		t.Errorf("check B = %+v", b)
	}
	// A running pipeline has a start time (its creation) so the tooltip can say
	// how long ago; Duration is not known yet.
	if d := got[hashD].Checks[0]; d.Started.IsZero() || !d.Started.Equal(d.Created) {
		t.Errorf("running pipeline Started = %v, Created = %v", d.Started, d.Created)
	}
	if k := got[hashC].Checks[0].Key; k != "pipeline_ref_target|release|custom:deploy" {
		t.Errorf("custom pipeline key = %q", k)
	}
}

func TestPipelineStartedPrefersStartedOn(t *testing.T) {
	p := wirePipeline{BuildNumber: 4}
	p.State.Name = "IN_PROGRESS"
	p.CreatedOn = time.Date(2026, 10, 20, 11, 0, 0, 0, time.UTC)
	p.StartedOn = time.Date(2026, 10, 20, 11, 0, 30, 0, time.UTC)
	ck, ok := pipelineCheck(repo, p)
	if !ok || !ck.Started.Equal(p.StartedOn) || !ck.Created.Equal(p.CreatedOn) {
		t.Fatalf("check = %+v", ck)
	}
	p.StartedOn = time.Time{}
	ck, _ = pipelineCheck(repo, p)
	if !ck.Started.Equal(p.CreatedOn) {
		t.Fatalf("without started_on, Started = %v, want created_on", ck.Started)
	}
}

func TestStatusesStopsEarly(t *testing.T) {
	f, c := newFake(t, pipelinesHandler(t))
	got, err := c.Statuses(context.Background(), repo, []string{hashA, hashB})
	if err != nil {
		t.Fatal(err)
	}
	if got[hashA].State != hosting.Success || got[hashB].State != hosting.Success {
		t.Errorf("got %+v", got)
	}
	if n := f.count(); n != 1 {
		t.Errorf("requests = %d, want 1", n)
	}
	if _, err := c.Statuses(context.Background(), repo, nil); err != nil || f.count() != 1 {
		t.Errorf("no hashes must cost no request (err %v, requests %d)", err, f.count())
	}
}

func TestStatusesPerCommitLatest(t *testing.T) {
	// A re-run of one commit must not hide a failure on another commit that
	// shares the trigger.
	_, c := newFake(t, func(f *fake, w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"values":[
		 {"build_number":3,"created_on":"2026-10-20T10:00:00+00:00","state":{"name":"COMPLETED","result":{"name":"SUCCESSFUL"}},
		  "target":{"type":"pipeline_ref_target","ref_name":"main","selector":{"type":"branches","pattern":"main"},"commit":{"hash":"` + hashB + `"}}},
		 {"build_number":2,"created_on":"2026-10-20T09:00:00+00:00","state":{"name":"COMPLETED","result":{"name":"FAILED"}},
		  "target":{"type":"pipeline_ref_target","ref_name":"main","selector":{"type":"branches","pattern":"main"},"commit":{"hash":"` + hashA + `"}}}]}`))
	})
	got, err := c.Statuses(context.Background(), repo, []string{hashA, hashB})
	if err != nil {
		t.Fatal(err)
	}
	if got[hashA].State != hosting.Failed || got[hashB].State != hosting.Success {
		t.Errorf("got A=%s B=%s", got[hashA].State, got[hashB].State)
	}
}

func TestChecksMergesStatusesAndPipelines(t *testing.T) {
	f, c := newFake(t, func(f *fake, w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case base + "/pipelines":
			if r.URL.Query().Get("page") != "1" {
				t.Errorf("pipelines query = %q", r.URL.RawQuery)
			}
			pipelinesHandler(t)(f, w, r)
		case base + "/commit/" + hashA + "/statuses":
			if r.URL.Query().Get("pagelen") != "100" {
				t.Errorf("statuses query = %q", r.URL.RawQuery)
			}
			f.fixture(t, w, "statuses_a.json")
		default:
			http.NotFound(w, r)
		}
	})
	got, err := c.Checks(context.Background(), repo, hashA)
	if err != nil {
		t.Fatal(err)
	}
	if f.count() != 2 {
		t.Errorf("requests = %v", f.paths())
	}
	// Pipelines #10 and #11 also come back as build statuses, same URL (one
	// as a fragment link): they must show once, and the re-run wins.
	names := map[string]hosting.State{}
	for _, ck := range got.Checks {
		names[ck.Name] = ck.State
	}
	want := map[string]hosting.State{
		"Pipeline #11":  hosting.Success,
		"Jenkins build": hosting.Failed,
		"SonarCloud":    hosting.Running,
	}
	if len(got.Checks) != len(want) {
		t.Fatalf("checks = %+v", got.Checks)
	}
	for n, st := range want {
		if names[n] != st {
			t.Errorf("%s = %q, want %q (all: %v)", n, names[n], st, names)
		}
	}
	if got.State != hosting.Failed || got.Hash != hashA {
		t.Errorf("aggregate = %+v", got)
	}
	for _, ck := range got.Checks {
		// A build status carries its creation time; the one without leaves
		// Started zero rather than inventing a moment.
		if ck.Name == "SonarCloud" && (ck.Started.IsZero() || ck.Started.Year() != 2026) {
			t.Errorf("SonarCloud Started = %v, want its created_on", ck.Started)
		}
		if ck.Name == "Jenkins build" && !ck.Started.IsZero() {
			t.Errorf("Jenkins Started = %v, want zero (no created_on)", ck.Started)
		}
		if ck.Name == "Jenkins build" && (ck.Key != "jenkins-build" || ck.URL != "https://ci.example.com/job/api/42/" || ck.Created.IsZero()) {
			t.Errorf("status check = %+v", ck)
		}
	}
}

func TestPullRequestsMineQuery(t *testing.T) {
	f, c := newFake(t, func(f *fake, w http.ResponseWriter, r *http.Request) {
		f.fixture(t, w, "prs_fork.json")
	})
	prs, more, err := c.PullRequests(context.Background(), repo, hosting.FilterMine, me)
	if err != nil || more {
		t.Fatalf("err %v more %v", err, more)
	}
	u := f.last()
	if u.Path != base+"/pullrequests" {
		t.Errorf("path = %q", u.Path)
	}
	q := u.Query()
	for k, v := range map[string]string{
		"q": `author.uuid="{aaaa-1111}"`, "state": "OPEN", "sort": "-updated_on",
		"pagelen": "50", "fields": "+values.participants",
	} {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
	if !strings.Contains(u.RawQuery, "fields=%2Bvalues.participants") || strings.Contains(u.RawQuery, `"`) {
		t.Errorf("raw query is not URL-encoded: %q", u.RawQuery)
	}
	if len(prs) != 3 {
		t.Fatalf("prs = %d", len(prs))
	}
	// Open adds no filter.
	if _, _, err := c.PullRequests(context.Background(), repo, hosting.FilterOpen, me); err != nil {
		t.Fatal(err)
	}
	if f.last().Query().Has("q") {
		t.Errorf("open filter sent q=%q", f.last().Query().Get("q"))
	}
}

func TestPullRequestsReviewDropsApproved(t *testing.T) {
	f, c := newFake(t, func(f *fake, w http.ResponseWriter, r *http.Request) {
		f.fixture(t, w, "prs_review.json")
	})
	prs, _, err := c.PullRequests(context.Background(), repo, hosting.FilterReview, me)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.last().Query().Get("q"); got != `reviewers.uuid="{aaaa-1111}"` {
		t.Errorf("q = %q", got)
	}
	var nums []int
	for _, p := range prs {
		nums = append(nums, p.Number)
	}
	if fmt.Sprint(nums) != "[30 29]" {
		t.Fatalf("numbers = %v, want [30 29] (31 is approved by me)", nums)
	}
	if prs[0].Approvals != 1 || prs[1].Approvals != 0 || !prs[1].Draft {
		t.Errorf("prs = %+v", prs)
	}
	p := prs[0]
	if p.Title != "Needs my review" || p.Author != "Bo" || p.AuthorID != "{bbbb-2222}" || p.Source != "f/b" ||
		p.Target != "main" || p.HeadHash != hashB || p.Comments != 1 || p.Updated.IsZero() || p.State != "" ||
		p.URL != "https://bitbucket.org/acme/api/pull-requests/30" || p.Fork {
		t.Errorf("pr = %+v", p)
	}
}

func TestPullRequestsFork(t *testing.T) {
	_, c := newFake(t, func(f *fake, w http.ResponseWriter, r *http.Request) {
		f.fixture(t, w, "prs_fork.json")
	})
	prs, _, err := c.PullRequests(context.Background(), repo, hosting.FilterOpen, me)
	if err != nil {
		t.Fatal(err)
	}
	fork := map[int]bool{}
	appr := map[int]int{}
	for _, p := range prs {
		fork[p.Number] = p.Fork
		appr[p.Number] = p.Approvals
	}
	if !fork[42] || !fork[41] || fork[40] {
		t.Errorf("fork = %v (42 and the deleted fork 41 are forks, 40 is not)", fork)
	}
	if appr[40] != 2 {
		t.Errorf("approvals = %v", appr)
	}
}

// listServer serves total open PRs in pages of 50, with absolute next links.
func listServer(t *testing.T, total int) (*fake, hosting.Client) {
	return newFake(t, func(f *fake, w http.ResponseWriter, r *http.Request) {
		pg, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if pg == 0 {
			pg = 1
		}
		var vals []map[string]any
		for i := (pg-1)*50 + 1; i <= pg*50 && i <= total; i++ {
			vals = append(vals, map[string]any{"id": i, "title": "PR " + strconv.Itoa(i)})
		}
		out := map[string]any{"values": vals}
		if pg*50 < total {
			out["next"] = f.srv.URL + base + "/pullrequests?page=" + strconv.Itoa(pg+1)
		}
		_ = json.NewEncoder(w).Encode(out)
	})
}

func TestPullRequestsCapsAt100(t *testing.T) {
	f, c := listServer(t, 130)
	prs, more, err := c.PullRequests(context.Background(), repo, hosting.FilterOpen, me)
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 100 || !more {
		t.Errorf("len %d more %v, want 100 and more", len(prs), more)
	}
	if f.count() != 3 {
		t.Errorf("requests = %d, want 3", f.count())
	}
	_, c = listServer(t, 100)
	prs, more, err = c.PullRequests(context.Background(), repo, hosting.FilterOpen, me)
	if err != nil || len(prs) != 100 || more {
		t.Errorf("exactly 100: len %d more %v err %v", len(prs), more, err)
	}
}

func TestPullRequestsStopsAtForeignNext(t *testing.T) {
	f, c := newFake(t, func(f *fake, w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"values":[{"id":1,"title":"one"}],"next":"https://evil.example.com/2.0/repositories/acme/api/pullrequests?page=2"}`))
	})
	prs, more, err := c.PullRequests(context.Background(), repo, hosting.FilterOpen, me)
	if err != nil || more || len(prs) != 1 {
		t.Fatalf("prs %d more %v err %v", len(prs), more, err)
	}
	if f.count() != 1 {
		t.Errorf("requests = %d", f.count())
	}
}

func TestCountUsesSize(t *testing.T) {
	f, c := newFake(t, func(f *fake, w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"size":37,"pagelen":1,"page":1,"values":[{"id":1}],"next":"x"}`))
	})
	got, err := c.PullRequestCount(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if got != (hosting.Count{N: 37}) {
		t.Errorf("count = %+v", got)
	}
	if f.count() != 1 || f.last().Query().Get("pagelen") != "1" || f.last().Query().Get("state") != "OPEN" {
		t.Errorf("requests %v, last query %q", f.paths(), f.last().RawQuery)
	}
}

func TestCountWithoutSize(t *testing.T) {
	for _, withNext := range []bool{false, true} {
		f, c := newFake(t, func(f *fake, w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("pagelen") == "1" {
				_, _ = w.Write([]byte(`{"pagelen":1,"page":1,"values":[{"id":1}],"next":"x"}`))
				return
			}
			var vals []string
			for i := 0; i < 100; i++ {
				vals = append(vals, `{"id":`+strconv.Itoa(i+1)+`}`)
			}
			next := ""
			if withNext {
				next = `,"next":"https://api.bitbucket.org/2.0/x?page=2"`
			}
			_, _ = w.Write([]byte(`{"pagelen":100,"values":[` + strings.Join(vals, ",") + `]` + next + `}`))
		})
		got, err := c.PullRequestCount(context.Background(), repo)
		if err != nil {
			t.Fatal(err)
		}
		if got != (hosting.Count{N: 100, More: withNext}) {
			t.Errorf("next=%v: count = %+v", withNext, got)
		}
		if f.count() != 2 || f.last().Query().Get("pagelen") != "100" {
			t.Errorf("requests %v last %q", f.paths(), f.last().RawQuery)
		}
	}
}

func detailHandler(t *testing.T) func(*fake, http.ResponseWriter, *http.Request) {
	return func(f *fake, w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case base + "/pullrequests/7":
			f.fixture(t, w, "pr_detail.json")
		case base + "/pullrequests/7/commits":
			if r.URL.Query().Get("pagelen") != "100" {
				t.Errorf("commits query = %q", r.URL.RawQuery)
			}
			f.fixture(t, w, "pr_commits.json")
		case base + "/pullrequests/7/comments":
			if r.URL.Query().Get("page") == "2" {
				f.fixture(t, w, "pr_comments_2.json")
			} else {
				f.fixture(t, w, "pr_comments_1.json")
			}
		case base + "/pipelines":
			f.fixture(t, w, "pipelines_1.json")
		case base + "/commit/" + hashA + "/statuses":
			f.fixture(t, w, "statuses_empty.json")
		default:
			http.NotFound(w, r)
		}
	}
}

func TestPullRequestDetail(t *testing.T) {
	_, c := newFake(t, detailHandler(t))
	d, err := c.PullRequest(context.Background(), repo, 7)
	if err != nil {
		t.Fatal(err)
	}
	if d.Number != 7 || d.Title != "Add pipelines client" || d.HeadHash != hashA {
		t.Errorf("header = %+v", d.PullRequest)
	}
	if d.Description != "Adds the **client**." {
		t.Errorf("description = %q (falls back to summary.raw)", d.Description)
	}
	if d.Created.IsZero() {
		t.Error("created is zero")
	}
	wantRev := []hosting.Reviewer{{Name: "Cy", Verdict: "approved"}, {Name: "Di", Verdict: "changes"}, {Name: "Ed", Verdict: "pending"}}
	if fmt.Sprint(d.Reviewers) != fmt.Sprint(wantRev) {
		t.Errorf("reviewers = %+v (the author, a PARTICIPANT, is not one)", d.Reviewers)
	}
	if len(d.Commits) != 2 || d.Commits[0] != (hosting.PRCommit{Hash: hashA, Short: "aaaaaaa", Subject: "feat: client", Author: "Bo"}) ||
		d.Commits[1].Author != "Zed" || d.Commits[1].Subject != "fix: typo" {
		t.Errorf("commits = %+v", d.Commits)
	}
	if d.CommentsMore || len(d.Comments) != 4 {
		t.Fatalf("comments = %+v more %v (the deleted one is skipped, page 2 is followed)", d.Comments, d.CommentsMore)
	}
	byID := map[string]hosting.Comment{}
	for _, cm := range d.Comments {
		byID[cm.ID] = cm
	}
	if _, ok := byID["104"]; ok {
		t.Error("deleted comment kept")
	}
	if g := byID["101"]; g.Path != "" || g.Line != 0 || g.Parent != "" || g.Body != "General note" || g.Author != "Cy" || g.Created.IsZero() {
		t.Errorf("general = %+v", g)
	}
	if g := byID["102"]; g.Path != "src/a.go" || g.Line != 12 {
		t.Errorf("inline = %+v", g)
	}
	if g := byID["103"]; g.Path != "src/b.go" || g.Line != 4 {
		t.Errorf("inline from = %+v (from is used when there is no to)", g)
	}
	if g := byID["105"]; g.Parent != "102" || g.Author != "Bo" {
		t.Errorf("reply = %+v", g)
	}
	if d.Comments[0].ID != "101" || d.Comments[3].ID != "105" {
		t.Errorf("not chronological: %+v", d.Comments)
	}
	// Checks of the head commit: pipelines A (#10 failed, #11 green re-run).
	if d.Checks.Hash != hashA || d.Checks.State != hosting.Success || len(d.Checks.Checks) != 1 {
		t.Errorf("checks = %+v", d.Checks)
	}
}

func TestPullRequestDetailSurvivesChecksFailure(t *testing.T) {
	h := detailHandler(t)
	status := http.StatusForbidden
	_, c := newFake(t, func(f *fake, w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == base+"/commit/"+hashA+"/statuses" {
			http.Error(w, `{"error":{"message":"no scope"}}`, status)
			return
		}
		h(f, w, r)
	})
	d, err := c.PullRequest(context.Background(), repo, 7)
	if err != nil {
		t.Fatalf("a failing statuses call must not fail the detail: %v", err)
	}
	if d.Number != 7 || len(d.Commits) != 2 || d.Checks.State != hosting.None || d.Checks.Hash != hashA || len(d.Checks.Checks) != 0 {
		t.Errorf("detail = %+v", d)
	}
	status = http.StatusUnauthorized
	if _, err := c.PullRequest(context.Background(), repo, 7); !errors.Is(err, hosting.ErrRejected) {
		t.Errorf("a rejected token must still fail: %v", err)
	}
}

func TestPullRequestCommentsCap(t *testing.T) {
	_, c := newFake(t, func(f *fake, w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/comments") {
			f.fixture(t, w, "statuses_empty.json")
			return
		}
		pg, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if pg == 0 {
			pg = 1
		}
		var vals []map[string]any
		for i := 1; i <= 100; i++ {
			vals = append(vals, map[string]any{"id": (pg-1)*100 + i, "content": map[string]string{"raw": "c"}})
		}
		out := map[string]any{"values": vals}
		if pg < 6 {
			out["next"] = f.srv.URL + base + "/pullrequests/7/comments?page=" + strconv.Itoa(pg+1)
		}
		_ = json.NewEncoder(w).Encode(out)
	})
	got, more, err := c.(*client).comments(context.Background(), repo, base+"/pullrequests/7")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 500 || !more {
		t.Errorf("len %d more %v, want 500 and more", len(got), more)
	}
}

func TestNoAccessNamesTheRepository(t *testing.T) {
	_, c := newFake(t, func(f *fake, w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"Repository not found"}}`, http.StatusNotFound)
	})
	ctx := context.Background()
	calls := map[string]func() error{
		"Statuses":         func() error { _, err := c.Statuses(ctx, repo, []string{hashA}); return err },
		"Checks":           func() error { _, err := c.Checks(ctx, repo, hashA); return err },
		"PullRequests":     func() error { _, _, err := c.PullRequests(ctx, repo, hosting.FilterOpen, me); return err },
		"PullRequestCount": func() error { _, err := c.PullRequestCount(ctx, repo); return err },
		"PullRequest":      func() error { _, err := c.PullRequest(ctx, repo, 7); return err },
		"PullRequestFiles": func() error { _, err := c.PullRequestFiles(ctx, repo, 7); return err },
	}
	for name, call := range calls {
		err := call()
		var na *hosting.NoAccessError
		if !errors.As(err, &na) || err.Error() != "No access to acme/api with this account" {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func filesHandler(t *testing.T, diff func(f *fake, w http.ResponseWriter)) func(*fake, http.ResponseWriter, *http.Request) {
	return func(f *fake, w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case base + "/pullrequests/7/diffstat":
			if r.URL.Query().Get("pagelen") != "500" {
				t.Errorf("diffstat query = %q", r.URL.RawQuery)
			}
			f.fixture(t, w, "diffstat.json")
		case base + "/pullrequests/7/diff":
			// The real API redirects to a diff on the same host.
			http.Redirect(w, r, f.srv.URL+"/2.0/repositories/acme/api/diff/"+hashA+".."+hashB, http.StatusFound)
		case "/2.0/repositories/acme/api/diff/" + hashA + ".." + hashB:
			diff(f, w)
		default:
			http.NotFound(w, r)
		}
	}
}

func TestPullRequestFilesSplitsDiff(t *testing.T) {
	_, c := newFake(t, filesHandler(t, func(f *fake, w http.ResponseWriter) {
		b, _ := os.ReadFile("testdata/pr.diff")
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write(b)
	}))
	files, err := c.PullRequestFiles(context.Background(), repo, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 5 {
		t.Fatalf("files = %+v", files)
	}
	by := map[string]hosting.PRFile{}
	for _, f := range files {
		by[f.Path] = f
	}
	if f := by["src/a.go"]; f.Status != "modified" || f.OldPath != "src/a.go" || f.NoDiff || !strings.HasPrefix(f.Patch, "diff --git a/src/a.go b/src/a.go\n") || !strings.Contains(f.Patch, "+var y = 3") || strings.Contains(f.Patch, "docs/new.md") {
		t.Errorf("a.go = %+v", f)
	}
	if f := by["docs/new.md"]; f.Status != "added" || f.OldPath != "" || f.NoDiff || !strings.Contains(f.Patch, "+# New") {
		t.Errorf("new.md = %+v", f)
	}
	if f := by["old.txt"]; f.Status != "removed" || f.OldPath != "old.txt" || f.NoDiff || !strings.Contains(f.Patch, "-one") {
		t.Errorf("removed file keyed by its old path: %+v", f)
	}
	if f := by["lib/y.go"]; f.Status != "renamed" || f.OldPath != "lib/x.go" || f.NoDiff {
		t.Errorf("renamed = %+v", f)
	}
	if f := by["img/logo.png"]; !f.NoDiff || f.Patch != "" {
		t.Errorf("a file missing from the diff must be NoDiff: %+v", f)
	}
}

func TestPullRequestFilesTooLarge(t *testing.T) {
	_, c := newFake(t, filesHandler(t, func(f *fake, w http.ResponseWriter) {
		chunk := []byte(strings.Repeat("x", 1<<20))
		for i := 0; i < 11; i++ {
			_, _ = w.Write(chunk)
		}
	}))
	files, err := c.PullRequestFiles(context.Background(), repo, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 5 {
		t.Fatalf("files = %+v", files)
	}
	for _, f := range files {
		if !f.NoDiff || f.Patch != "" {
			t.Errorf("%s: want NoDiff, got %+v", f.Path, f)
		}
	}
}

func TestPullRequestFilesBinarySectionIsNoDiff(t *testing.T) {
	// A binary file's section is a header and "Binary files … differ": no
	// hunk, nothing to show. A pure rename also has no hunk but its header is
	// the whole story, so it keeps its patch.
	_, c := newFake(t, filesHandler(t, func(f *fake, w http.ResponseWriter) {
		b, _ := os.ReadFile("testdata/pr.diff")
		_, _ = w.Write(append(b, []byte("diff --git a/img/logo.png b/img/logo.png\nnew file mode 100644\nindex 0000000..555\nBinary files /dev/null and b/img/logo.png differ\n")...))
	}))
	files, err := c.PullRequestFiles(context.Background(), repo, 7)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]hosting.PRFile{}
	for _, f := range files {
		by[f.Path] = f
	}
	if f := by["img/logo.png"]; !f.NoDiff || f.Patch != "" {
		t.Errorf("binary = %+v, want NoDiff and no patch", f)
	}
	if f := by["lib/y.go"]; f.NoDiff || f.Patch == "" {
		t.Errorf("pure rename = %+v, want its header kept", f)
	}
	if f := by["src/a.go"]; f.NoDiff {
		t.Errorf("a.go = %+v", f)
	}
}
