package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git-ui/internal/ai/keys"
	"git-ui/internal/hosting"
	"git-ui/internal/hosting/accounts"
	"git-ui/internal/hosting/bitbucket"
	"git-ui/internal/ops"
	"git-ui/internal/testrepo"
)

// fakeHost is a hosting.Client that records its calls.
type fakeHost struct {
	mu          sync.Mutex
	statusCalls [][]string
	checkCalls  []string
	userCalls   int
	statuses    func(hashes []string) (map[string]hosting.CommitStatus, error)
	user        func() (hosting.Account, error)
	checks      func(hash string) (hosting.CommitStatus, error)

	pulls       func(f hosting.Filter, me hosting.Account) ([]hosting.PullRequest, bool, error)
	pullCount   func() (hosting.Count, error)
	pull        func(number int) (hosting.PullRequestDetail, error)
	pullFiles   func(number int) ([]hosting.PRFile, error)
	pullFilters []hosting.Filter
	pullMe      []hosting.Account
}

func (f *fakeHost) User(context.Context) (hosting.Account, error) {
	f.mu.Lock()
	f.userCalls++
	f.mu.Unlock()
	if f.user != nil {
		return f.user()
	}
	return hosting.Account{ID: "{u}", Name: "Ada", Login: "ada"}, nil
}

func (f *fakeHost) Statuses(_ context.Context, _ hosting.Repo, hashes []string) (map[string]hosting.CommitStatus, error) {
	f.mu.Lock()
	f.statusCalls = append(f.statusCalls, append([]string(nil), hashes...))
	f.mu.Unlock()
	if f.statuses != nil {
		return f.statuses(hashes)
	}
	out := map[string]hosting.CommitStatus{}
	for _, h := range hashes {
		out[h] = hosting.CommitStatus{Hash: h, State: hosting.Success}
	}
	return out, nil
}

func (f *fakeHost) Checks(_ context.Context, _ hosting.Repo, hash string) (hosting.CommitStatus, error) {
	f.mu.Lock()
	f.checkCalls = append(f.checkCalls, hash)
	f.mu.Unlock()
	if f.checks != nil {
		return f.checks(hash)
	}
	return hosting.CommitStatus{Hash: hash, State: hosting.Success, Checks: []hosting.Check{{Name: "build", State: hosting.Success}}}, nil
}

func (f *fakeHost) PullRequests(_ context.Context, _ hosting.Repo, fl hosting.Filter, me hosting.Account) ([]hosting.PullRequest, bool, error) {
	f.mu.Lock()
	f.pullFilters = append(f.pullFilters, fl)
	f.pullMe = append(f.pullMe, me)
	f.mu.Unlock()
	if f.pulls != nil {
		return f.pulls(fl, me)
	}
	return nil, false, nil
}
func (f *fakeHost) PullRequestCount(context.Context, hosting.Repo) (hosting.Count, error) {
	if f.pullCount != nil {
		return f.pullCount()
	}
	return hosting.Count{}, nil
}
func (f *fakeHost) PullRequest(_ context.Context, _ hosting.Repo, n int) (hosting.PullRequestDetail, error) {
	if f.pull != nil {
		return f.pull(n)
	}
	return hosting.PullRequestDetail{}, nil
}
func (f *fakeHost) PullRequestFiles(_ context.Context, _ hosting.Repo, n int) ([]hosting.PRFile, error) {
	if f.pullFiles != nil {
		return f.pullFiles(n)
	}
	return nil, nil
}

func (f *fakeHost) statusCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.statusCalls)
}

// newHostingApp is a test app whose accounts live in a temp directory and an
// in-memory keychain, and whose host clients are the returned fake.
func newHostingApp(t *testing.T) (*App, string, *fakeHost, *events) {
	t.Helper()
	t.Cleanup(keys.UseStore(&fakeKeys{items: map[string]string{}}))
	a, id := newTestApp(t)
	ev := newEvents()
	dir := t.TempDir()
	WithAI(a, AIDeps{SettingsPath: filepath.Join(dir, "ai.json"), Emit: ev.emit})
	fake := &fakeHost{}
	a.hosting.newClient = func(hosting.Host, authFunc) hosting.Client { return fake }
	return a, id, fake, ev
}

func connectBitbucket(t *testing.T, a *App) {
	t.Helper()
	if _, err := a.SaveBitbucketAccount("ada@example.com", "tok"); err != nil {
		t.Fatal(err)
	}
}

const (
	hashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hashB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestHostingInfoUnsupported(t *testing.T) {
	a, id, _, _ := newHostingApp(t)
	info, err := a.HostingInfo(id)
	if err != nil {
		t.Fatal(err)
	}
	if info.Supported {
		t.Fatalf("a repository with no remote is supported: %+v", info)
	}
}

func TestHostingInfoBitbucket(t *testing.T) {
	a, id, _, _ := newHostingApp(t)
	if err := a.AddRemote(id, "origin", "git@bitbucket.org:acme/api.git"); err != nil {
		t.Fatal(err)
	}
	info, err := a.HostingInfo(id)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Supported || info.Host != hosting.Bitbucket || info.Owner != "acme" || info.Name != "api" || info.Remote != "origin" {
		t.Fatalf("info = %+v", info)
	}
	if info.Noun != "Pipelines" || info.Account != "none" {
		t.Fatalf("noun %q account %q", info.Noun, info.Account)
	}
	if info.WebURL != "https://bitbucket.org/acme/api" || info.PullsURL != "https://bitbucket.org/acme/api/pull-requests" {
		t.Fatalf("urls %q %q", info.WebURL, info.PullsURL)
	}
}

// Review focus 1: the remote is edited after the integration detected it.
func TestHostingInfoFollowsRemoteEdit(t *testing.T) {
	a, id, _, _ := newHostingApp(t)
	if err := a.AddRemote(id, "origin", "git@bitbucket.org:acme/api.git"); err != nil {
		t.Fatal(err)
	}
	if info, _ := a.HostingInfo(id); info.Owner != "acme" {
		t.Fatalf("before: %+v", info)
	}
	if err := a.SetRemoteURL(id, "origin", "https://github.com/o/r"); err != nil {
		t.Fatal(err)
	}
	info, err := a.HostingInfo(id)
	if err != nil {
		t.Fatal(err)
	}
	if info.Host != hosting.GitHub || info.Owner != "o" || info.Name != "r" || info.Noun != "Checks" {
		t.Fatalf("after the edit: %+v", info)
	}
	if err := a.RemoveRemote(id, "origin"); err != nil {
		t.Fatal(err)
	}
	if info, _ := a.HostingInfo(id); info.Supported {
		t.Fatalf("after removing the remote: %+v", info)
	}
}

func TestHostingInfoAccountStates(t *testing.T) {
	a, id, _, _ := newHostingApp(t)
	if err := a.AddRemote(id, "origin", "git@bitbucket.org:acme/api.git"); err != nil {
		t.Fatal(err)
	}
	connectBitbucket(t, a)
	if info, _ := a.HostingInfo(id); info.Account != "connected" {
		t.Fatalf("connected: %+v", info)
	}
	store, _ := a.accountStore()
	store.OnReject(hosting.Bitbucket)()
	if info, _ := a.HostingInfo(id); info.Account != "rejected" {
		t.Fatalf("rejected: %+v", info)
	}
	connectBitbucket(t, a) // saved again: the flag clears
	if info, _ := a.HostingInfo(id); info.Account != "connected" {
		t.Fatalf("saved again: %+v", info)
	}
}

func TestHostingInfoOfLinkedWorktreeUsesMainRepository(t *testing.T) {
	a, id, _, _ := newHostingApp(t)
	if err := a.AddRemote(id, "origin", "https://github.com/o/r.git"); err != nil {
		t.Fatal(err)
	}
	wt := testrepo.New(t)
	wt.Commit("wt")
	repo, err := a.store.Add(context.Background(), wt.Dir)
	if err != nil {
		t.Fatal(err)
	}
	a.wtMu.Lock()
	a.settingsParent = map[string]string{repo.ID: id}
	a.wtMu.Unlock()
	info, err := a.HostingInfo(repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if info.Host != hosting.GitHub || info.Owner != "o" {
		t.Fatalf("worktree info = %+v", info)
	}
}

func TestCommitStatusesUsesCache(t *testing.T) {
	a, id, fake, _ := newHostingApp(t)
	a.AddRemote(id, "origin", "git@bitbucket.org:acme/api.git")
	connectBitbucket(t, a)

	got, err := a.CommitStatuses(id, []string{hashA, hashB})
	if err != nil || len(got) != 2 || got[hashA].State != hosting.Success {
		t.Fatalf("first: %v %v", got, err)
	}
	got, err = a.CommitStatuses(id, []string{hashA, hashB})
	if err != nil || len(got) != 2 {
		t.Fatalf("second: %v %v", got, err)
	}
	if n := fake.statusCallCount(); n != 1 {
		t.Fatalf("client called %d times, want 1", n)
	}
	// A new commit asks only for itself.
	hashC := "cccccccccccccccccccccccccccccccccccccccc"
	if _, err := a.CommitStatuses(id, []string{hashA, hashC}); err != nil {
		t.Fatal(err)
	}
	if n := fake.statusCallCount(); n != 2 || len(fake.statusCalls[1]) != 1 || fake.statusCalls[1][0] != hashC {
		t.Fatalf("calls = %v", fake.statusCalls)
	}
}

func TestCommitStatusesSkipsPseudoHashAndNeedsAnAccount(t *testing.T) {
	a, id, fake, _ := newHostingApp(t)
	a.AddRemote(id, "origin", "git@bitbucket.org:acme/api.git")

	got, err := a.CommitStatuses(id, []string{hashA})
	if err != nil || len(got) != 0 || fake.statusCallCount() != 0 {
		t.Fatalf("no account: %v %v calls=%d", got, err, fake.statusCallCount())
	}

	connectBitbucket(t, a)
	got, err = a.CommitStatuses(id, []string{"uncommitted", "", "zz", hashA})
	if err != nil || len(got) != 1 || fake.statusCallCount() != 1 || len(fake.statusCalls[0]) != 1 {
		t.Fatalf("pseudo hash: %v %v calls=%v", got, err, fake.statusCalls)
	}

	store, _ := a.accountStore()
	store.OnReject(hosting.Bitbucket)()
	got, err = a.CommitStatuses(id, []string{hashB})
	if err != nil || len(got) != 0 || fake.statusCallCount() != 1 {
		t.Fatalf("rejected: %v %v calls=%d", got, err, fake.statusCallCount())
	}
}

func TestCommitStatusesUnsupportedRepositoryIsEmpty(t *testing.T) {
	a, id, fake, _ := newHostingApp(t)
	got, err := a.CommitStatuses(id, []string{hashA})
	if err != nil || len(got) != 0 || fake.statusCallCount() != 0 {
		t.Fatalf("%v %v", got, err)
	}
}

func TestCommitStatusesRateLimitedReturnsPartial(t *testing.T) {
	a, id, fake, _ := newHostingApp(t)
	a.AddRemote(id, "origin", "git@bitbucket.org:acme/api.git")
	connectBitbucket(t, a)
	if _, err := a.CommitStatuses(id, []string{hashA}); err != nil {
		t.Fatal(err)
	}
	fake.statuses = func([]string) (map[string]hosting.CommitStatus, error) {
		return nil, &hosting.RateLimitError{Host: hosting.Bitbucket}
	}
	got, err := a.CommitStatuses(id, []string{hashA, hashB})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(got) != 1 || got[hashA].State != hosting.Success {
		t.Fatalf("got %v, want only the cached commit", got)
	}
	// A network error does the same.
	fake.statuses = func([]string) (map[string]hosting.CommitStatus, error) {
		return nil, errors.New("Bitbucket: request failed")
	}
	if got, err := a.CommitStatuses(id, []string{hashA, hashB}); err != nil || len(got) != 1 {
		t.Fatalf("network error: %v %v", got, err)
	}
}

func TestCommitStatusesNoAccessIsAnError(t *testing.T) {
	a, id, fake, _ := newHostingApp(t)
	a.AddRemote(id, "origin", "git@bitbucket.org:acme/api.git")
	connectBitbucket(t, a)
	fake.statuses = func([]string) (map[string]hosting.CommitStatus, error) { return nil, &hosting.NoAccessError{} }
	_, err := a.CommitStatuses(id, []string{hashA})
	var na *hosting.NoAccessError
	if !errors.As(err, &na) || na.Repo != "acme/api" {
		t.Fatalf("err = %v", err)
	}
}

// A real Doer behind the test seam, so the shared Limits sees the 429.
func TestCommitStatusesEmitsLimitOnTransitionsOnly(t *testing.T) {
	a, id, _, ev := newHostingApp(t)
	a.AddRemote(id, "origin", "git@bitbucket.org:acme/api.git")
	connectBitbucket(t, a)

	clk := &testClock{t: time.Unix(1_800_000_000, 0)}
	a.hosting.limits = hosting.NewLimits(clk.Now)
	status := http.StatusTooManyRequests
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if status == http.StatusOK {
			w.Write([]byte(`{"values":[]}`))
			return
		}
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(status)
	}))
	defer srv.Close()
	a.hosting.newClient = func(h hosting.Host, _ authFunc) hosting.Client {
		return bitbucket.New(&hosting.Doer{
			Host: h, BaseURL: srv.URL, Limits: a.hosting.limits,
			Auth: func(context.Context) (string, error) { return "Basic x", nil },
		})
	}

	limitEvents := func() []HostingLimit {
		ev.mu.Lock()
		defer ev.mu.Unlock()
		var out []HostingLimit
		for _, e := range ev.list {
			if e.name == EventHostingLimit {
				out = append(out, e.data.(HostingLimit))
			}
		}
		return out
	}

	a.CommitStatuses(id, []string{hashA})
	evs := limitEvents()
	if len(evs) != 1 || evs[0].Host != hosting.Bitbucket || evs[0].Until == nil || !evs[0].Known {
		t.Fatalf("after the 429: %+v", evs)
	}
	// Still blocked: no network call, no second event.
	before := hits.Load()
	a.CommitStatuses(id, []string{hashA})
	if hits.Load() != before || len(limitEvents()) != 1 {
		t.Fatalf("hits %d->%d, events %+v", before, hits.Load(), limitEvents())
	}
	// The block ends: the next call announces it.
	clk.Advance(31 * time.Second)
	status = http.StatusOK
	a.CommitStatuses(id, []string{hashA})
	evs = limitEvents()
	if len(evs) != 2 || evs[1].Until != nil {
		t.Fatalf("after the block ended: %+v", evs)
	}
}

type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func TestCommitChecksAlwaysAsksAndCaches(t *testing.T) {
	a, id, fake, _ := newHostingApp(t)
	a.AddRemote(id, "origin", "https://github.com/o/r")
	if _, err := a.SaveGitHubAccount("token", "ghp_x"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		st, err := a.CommitChecks(id, hashA)
		if err != nil || st.State != hosting.Success || len(st.Checks) != 1 {
			t.Fatalf("checks: %+v %v", st, err)
		}
	}
	if len(fake.checkCalls) != 2 {
		t.Fatalf("Checks called %d times, want 2", len(fake.checkCalls))
	}
	if _, err := a.CommitStatuses(id, []string{hashA}); err != nil || fake.statusCallCount() != 0 {
		t.Fatalf("CommitStatuses should use the cached aggregate: calls=%d err=%v", fake.statusCallCount(), err)
	}
	fake.checks = func(string) (hosting.CommitStatus, error) { return hosting.CommitStatus{}, hosting.ErrRejected }
	if _, err := a.CommitChecks(id, hashA); !errors.Is(err, hosting.ErrRejected) {
		t.Fatalf("explicit call should return the error: %v", err)
	}
	if _, err := a.CommitChecks(id, "not a hash"); err == nil {
		t.Fatal("a non-hex id was accepted")
	}
}

func TestSaveBitbucketAccountVerifiesFirst(t *testing.T) {
	a, _, fake, _ := newHostingApp(t)
	fake.user = func() (hosting.Account, error) { return hosting.Account{}, hosting.ErrRejected }
	_, err := a.SaveBitbucketAccount("ada@example.com", "bad")
	if !errors.Is(err, hosting.ErrRejected) {
		t.Fatalf("err = %v", err)
	}
	accs, err := a.GetHostingAccounts()
	if err != nil {
		t.Fatal(err)
	}
	if accs[0].Connected || accs[0].Email != "" {
		t.Fatalf("something was stored: %+v", accs[0])
	}
	if v, err := keys.Get("bitbucket"); err == nil && v != "" {
		t.Fatal("the token reached the keychain")
	}
}

func TestSaveBitbucketAccountRefusesBlankBeforeTheNetwork(t *testing.T) {
	a, _, fake, _ := newHostingApp(t)
	for _, c := range [][2]string{{"", "tok"}, {"  ", "tok"}, {"a@b.c", ""}, {"a@b.c", "   "}} {
		if _, err := a.SaveBitbucketAccount(c[0], c[1]); err == nil {
			t.Errorf("(%q, %q) accepted", c[0], c[1])
		}
	}
	if _, err := a.SaveGitHubAccount("token", " "); err == nil {
		t.Error("blank GitHub token accepted")
	}
	if fake.userCalls != 0 {
		t.Fatalf("User called %d times", fake.userCalls)
	}
}

func TestSaveBitbucketAccountStoresName(t *testing.T) {
	a, _, _, _ := newHostingApp(t)
	acc, err := a.SaveBitbucketAccount(" ada@example.com ", " tok ")
	if err != nil {
		t.Fatal(err)
	}
	if !acc.Connected || acc.Name != "Ada" || acc.Email != "ada@example.com" || acc.Host != hosting.Bitbucket {
		t.Fatalf("saved = %+v", acc)
	}
	accs, err := a.GetHostingAccounts()
	if err != nil || len(accs) != 2 || accs[0].Host != hosting.Bitbucket || accs[1].Host != hosting.GitHub {
		t.Fatalf("accounts = %+v %v", accs, err)
	}
	if !accs[0].Connected || accs[0].Name != "Ada" || accs[1].Connected {
		t.Fatalf("accounts = %+v", accs)
	}
	if tok, _ := keys.Get("bitbucket"); tok != "tok" {
		t.Fatalf("token in keychain = %q", tok)
	}
}

func TestSaveGitHubAccountAndRemove(t *testing.T) {
	a, _, _, _ := newHostingApp(t)
	acc, err := a.SaveGitHubAccount("token", "ghp_x")
	if err != nil {
		t.Fatal(err)
	}
	if !acc.Connected || acc.Name != "ada" || acc.Source != accounts.SourceToken || acc.Host != hosting.GitHub {
		t.Fatalf("saved = %+v", acc)
	}
	if err := a.RemoveHostingAccount("github"); err != nil {
		t.Fatal(err)
	}
	accs, _ := a.GetHostingAccounts()
	if accs[1].Connected {
		t.Fatalf("still connected: %+v", accs[1])
	}
	if err := a.RemoveHostingAccount("gitlab"); err == nil {
		t.Fatal("unknown host accepted")
	}
}

func TestHostingFileSitsNextToAISettings(t *testing.T) {
	a, _, _, _ := newHostingApp(t)
	connectBitbucket(t, a)
	want := accounts.PathNextTo(a.ai.deps.SettingsPath)
	if a.hosting.path != want {
		t.Fatalf("path = %q, want %q", a.hosting.path, want)
	}
}

// withBitbucketOrigin makes id's origin fetch from a local bare repository
// that has the branches of src, while HostingInfo sees acme/api on Bitbucket.
// `git remote -v` shows insteadOf-rewritten URLs, so the Bitbucket URL is the
// push URL: the detection reads it when the fetch URL is not on a supported
// host, and a fetch only uses the fetch URL.
func withBitbucketOrigin(t *testing.T, a *App, id string, src *testrepo.Repo) {
	t.Helper()
	dir, err := a.dir(id)
	if err != nil {
		t.Fatal(err)
	}
	// newTestApp's local branch "feature" would block a branch feature/x.
	gitC(t, dir, "branch", "-q", "-D", "feature")
	gitC(t, dir, "remote", "add", "origin", testrepo.NewBareFrom(t, src))
	gitC(t, dir, "remote", "set-url", "--push", "origin", "git@bitbucket.org:acme/api.git")
	if info, err := a.HostingInfo(id); err != nil || info.Host != hosting.Bitbucket || info.Remote != "origin" || info.Name != "api" {
		t.Fatalf("the test remote is not detected as Bitbucket: %+v %v", info, err)
	}
}

// remoteWithBranch is a repository with a branch feature/x ahead of main.
func remoteWithBranch(t *testing.T) *testrepo.Repo {
	t.Helper()
	src := testrepo.New(t)
	src.Commit("base")
	src.Git("switch", "-q", "-c", "feature/x")
	src.WriteFile("file-1.txt", "changed on the remote\n")
	src.Git("commit", "-q", "-am", "x work")
	src.Git("switch", "-q", "main")
	return src
}

func TestListPullRequestsFillsStateFromCache(t *testing.T) {
	a, id, fake, _ := newHostingApp(t)
	a.AddRemote(id, "origin", "git@bitbucket.org:acme/api.git")
	connectBitbucket(t, a)
	a.hosting.cache.Put("bitbucket:acme/api", map[string]hosting.CommitStatus{hashA: {Hash: hashA, State: hosting.Failed}})
	fake.pulls = func(hosting.Filter, hosting.Account) ([]hosting.PullRequest, bool, error) {
		return []hosting.PullRequest{
			{Number: 1, Title: "cached", HeadHash: hashA},
			{Number: 2, Title: "unknown", HeadHash: hashB},
			{Number: 3, Title: "no head"},
		}, true, nil
	}
	got, err := a.ListPullRequests(id, "mine")
	if err != nil {
		t.Fatal(err)
	}
	if !got.More || len(got.Items) != 3 {
		t.Fatalf("list = %+v", got)
	}
	if got.Items[0].State != hosting.Failed || got.Items[1].State != "" || got.Items[2].State != "" {
		t.Fatalf("states = %q %q %q", got.Items[0].State, got.Items[1].State, got.Items[2].State)
	}
	if fake.statusCallCount() != 0 || len(fake.checkCalls) != 0 {
		t.Fatalf("extra requests: statuses %v checks %v", fake.statusCalls, fake.checkCalls)
	}
	// The saved account is who "mine" and "review" are about.
	if fake.pullFilters[0] != hosting.FilterMine || fake.pullMe[0].ID != "{u}" {
		t.Fatalf("asked %v as %+v", fake.pullFilters, fake.pullMe)
	}
}

func TestListPullRequestsGitHubKeepsTheHostsState(t *testing.T) {
	a, id, fake, _ := newHostingApp(t)
	a.AddRemote(id, "origin", "https://github.com/o/r")
	if _, err := a.SaveGitHubAccount("token", "ghp_x"); err != nil {
		t.Fatal(err)
	}
	a.hosting.cache.Put("github:o/r", map[string]hosting.CommitStatus{hashA: {Hash: hashA, State: hosting.Failed}})
	fake.pulls = func(_ hosting.Filter, me hosting.Account) ([]hosting.PullRequest, bool, error) {
		if me.ID != "ada" {
			t.Errorf("me = %+v", me)
		}
		return []hosting.PullRequest{{Number: 1, HeadHash: hashA, State: hosting.Success}}, false, nil
	}
	got, err := a.ListPullRequests(id, "review")
	if err != nil || got.Items[0].State != hosting.Success {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestListPullRequestsEmptyIsAnEmptyList(t *testing.T) {
	a, id, _, _ := newHostingApp(t)
	a.AddRemote(id, "origin", "git@bitbucket.org:acme/api.git")
	connectBitbucket(t, a)
	got, err := a.ListPullRequests(id, "open")
	if err != nil || got.Items == nil || len(got.Items) != 0 || got.More {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestListPullRequestsNoAccount(t *testing.T) {
	a, id, fake, _ := newHostingApp(t)
	a.AddRemote(id, "origin", "git@bitbucket.org:acme/api.git")
	_, err := a.ListPullRequests(id, "open")
	if !errors.Is(err, hosting.ErrNoAccount) {
		t.Fatalf("err = %v", err)
	}
	if len(fake.pullFilters) != 0 {
		t.Fatal("the host was asked without an account")
	}
	store, _ := a.accountStore()
	connectBitbucket(t, a)
	store.OnReject(hosting.Bitbucket)()
	if _, err := a.ListPullRequests(id, "open"); !errors.Is(err, hosting.ErrRejected) || len(fake.pullFilters) != 0 {
		t.Fatalf("rejected: %v", err)
	}
}

func TestListPullRequestsErrors(t *testing.T) {
	a, id, fake, _ := newHostingApp(t)
	if _, err := a.ListPullRequests(id, "open"); err == nil {
		t.Fatal("a repository with no host listed pull requests")
	}
	a.AddRemote(id, "origin", "git@bitbucket.org:acme/api.git")
	connectBitbucket(t, a)
	if _, err := a.ListPullRequests(id, "everything"); err == nil {
		t.Fatal("an unknown filter was accepted")
	}
	fake.pulls = func(hosting.Filter, hosting.Account) ([]hosting.PullRequest, bool, error) {
		return nil, false, &hosting.NoAccessError{}
	}
	_, err := a.ListPullRequests(id, "open")
	var na *hosting.NoAccessError
	if !errors.As(err, &na) || na.Repo != "acme/api" {
		t.Fatalf("err = %v", err)
	}
}

func TestPullRequestCountDetailAndFiles(t *testing.T) {
	a, id, fake, _ := newHostingApp(t)
	a.AddRemote(id, "origin", "git@bitbucket.org:acme/api.git")
	connectBitbucket(t, a)
	fake.pullCount = func() (hosting.Count, error) { return hosting.Count{N: 100, More: true}, nil }
	fake.pull = func(n int) (hosting.PullRequestDetail, error) {
		return hosting.PullRequestDetail{PullRequest: hosting.PullRequest{Number: n}, Description: "d"}, nil
	}
	fake.pullFiles = func(n int) ([]hosting.PRFile, error) { return []hosting.PRFile{{Path: "a.go"}}, nil }
	if c, err := a.PullRequestCount(id); err != nil || c.N != 100 || !c.More {
		t.Fatalf("count %+v %v", c, err)
	}
	if d, err := a.GetPullRequest(id, 7); err != nil || d.Number != 7 || d.Description != "d" {
		t.Fatalf("detail %+v %v", d, err)
	}
	if fs, err := a.GetPullRequestFiles(id, 7); err != nil || len(fs) != 1 {
		t.Fatalf("files %+v %v", fs, err)
	}
	if _, err := a.GetPullRequest(id, 0); err == nil {
		t.Fatal("pull request 0 accepted")
	}
	fake.pull = func(int) (hosting.PullRequestDetail, error) {
		return hosting.PullRequestDetail{}, &hosting.NoAccessError{}
	}
	var na *hosting.NoAccessError
	if _, err := a.GetPullRequest(id, 7); !errors.As(err, &na) || na.Repo != "acme/api" {
		t.Fatalf("err = %v", err)
	}
}

func TestCheckoutPullRequestFork(t *testing.T) {
	a, id, fake, _ := newHostingApp(t)
	withBitbucketOrigin(t, a, id, remoteWithBranch(t))
	connectBitbucket(t, a)
	fake.pull = func(n int) (hosting.PullRequestDetail, error) {
		return hosting.PullRequestDetail{PullRequest: hosting.PullRequest{Number: n, Source: "feature/x", Fork: true}}, nil
	}
	_, err := a.CheckoutPullRequest(id, 4)
	if err == nil || err.Error() != "From a fork. Open it in the browser." {
		t.Fatalf("err = %v", err)
	}
	dir, _ := a.dir(id)
	if out := gitC(t, dir, "for-each-ref", "refs/remotes/origin"); strings.TrimSpace(out) != "" {
		t.Fatalf("a fork pull request fetched the remote: %s", out)
	}
}

func TestCheckoutPullRequestSameRepo(t *testing.T) {
	a, id, fake, _ := newHostingApp(t)
	withBitbucketOrigin(t, a, id, remoteWithBranch(t))
	connectBitbucket(t, a)
	fake.pull = func(n int) (hosting.PullRequestDetail, error) {
		// The host is asked with no repository lock held.
		unlock, err := a.lockWrite(id)
		if err != nil {
			t.Errorf("the write lock was held while asking the host: %v", err)
		} else {
			unlock()
		}
		return hosting.PullRequestDetail{PullRequest: hosting.PullRequest{Number: n, Source: "feature/x"}}, nil
	}
	outcome, err := a.CheckoutPullRequest(id, 4)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != string(ops.CheckoutCreated) {
		t.Fatalf("outcome = %q", outcome)
	}
	dir, _ := a.dir(id)
	if got := strings.TrimSpace(gitC(t, dir, "rev-parse", "--abbrev-ref", "HEAD")); got != "feature/x" {
		t.Fatalf("HEAD = %q", got)
	}
	if got := strings.TrimSpace(gitC(t, dir, "rev-parse", "--abbrev-ref", "feature/x@{upstream}")); got != "origin/feature/x" {
		t.Fatalf("upstream = %q", got)
	}
	// A second checkout of the same branch reuses it.
	gitC(t, dir, "switch", "-q", "main")
	if outcome, err := a.CheckoutPullRequest(id, 4); err != nil || outcome != string(ops.CheckoutSwitched) {
		t.Fatalf("again: %q %v", outcome, err)
	}
}

func TestCheckoutPullRequestRefusesABadBranchName(t *testing.T) {
	a, id, fake, _ := newHostingApp(t)
	withBitbucketOrigin(t, a, id, remoteWithBranch(t))
	connectBitbucket(t, a)
	dir, _ := a.dir(id)
	for _, name := range []string{"feature/..x", "has space", "-x", "@{-1}", "a~b", "x.lock", "ends/"} {
		fake.pull = func(n int) (hosting.PullRequestDetail, error) {
			return hosting.PullRequestDetail{PullRequest: hosting.PullRequest{Number: n, Source: name}}, nil
		}
		_, err := a.CheckoutPullRequest(id, 4)
		if !errors.Is(err, ops.ErrInvalidRef) {
			t.Errorf("%q: err = %v, want ErrInvalidRef", name, err)
		}
	}
	if out := gitC(t, dir, "for-each-ref", "refs/remotes/origin"); strings.TrimSpace(out) != "" {
		t.Fatalf("a bad branch name fetched the remote: %s", out)
	}
}

func TestCheckoutPullRequestDirtyTree(t *testing.T) {
	a, id, fake, _ := newHostingApp(t)
	withBitbucketOrigin(t, a, id, remoteWithBranch(t))
	connectBitbucket(t, a)
	fake.pull = func(n int) (hosting.PullRequestDetail, error) {
		return hosting.PullRequestDetail{PullRequest: hosting.PullRequest{Number: n, Source: "feature/x"}}, nil
	}
	dir, _ := a.dir(id)
	if err := os.WriteFile(filepath.Join(dir, "file-1.txt"), []byte("local edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := a.CheckoutPullRequest(id, 4)
	if err == nil {
		t.Fatal("a dirty tree was switched away from")
	}
	_, want := a.CheckoutRemote(id, "origin", "feature/x")
	if want == nil || err.Error() != want.Error() {
		t.Fatalf("err = %v, want CheckoutRemote's %v", err, want)
	}
	if got := strings.TrimSpace(gitC(t, dir, "rev-parse", "--abbrev-ref", "HEAD")); got != "main" {
		t.Fatalf("HEAD = %q", got)
	}
}

func TestCheckoutPullRequestPassesErrorsThrough(t *testing.T) {
	a, id, fake, _ := newHostingApp(t)
	a.AddRemote(id, "origin", "git@bitbucket.org:acme/api.git")
	if _, err := a.CheckoutPullRequest(id, 4); !errors.Is(err, hosting.ErrNoAccount) {
		t.Fatalf("no account: %v", err)
	}
	connectBitbucket(t, a)
	fake.pull = func(int) (hosting.PullRequestDetail, error) {
		return hosting.PullRequestDetail{}, &hosting.NoAccessError{}
	}
	var na *hosting.NoAccessError
	if _, err := a.CheckoutPullRequest(id, 4); !errors.As(err, &na) {
		t.Fatalf("no access: %v", err)
	}
}

// watchSpy stands in for hostingState.watch: it records each watch started
// and holds it open until release is closed.
type watchSpy struct {
	started chan [4]string // repoID, branch, hash, noun
	release chan struct{}
}

func newWatchSpy() *watchSpy {
	return &watchSpy{started: make(chan [4]string, 16), release: make(chan struct{})}
}

func (s *watchSpy) watch(_ context.Context, repoID, branch, hash, noun string) {
	s.started <- [4]string{repoID, branch, hash, noun}
	<-s.release
}

func (s *watchSpy) none(t *testing.T) {
	t.Helper()
	select {
	case got := <-s.started:
		t.Fatalf("a watch started: %v", got)
	case <-time.After(150 * time.Millisecond):
	}
}

func (s *watchSpy) next(t *testing.T) [4]string {
	t.Helper()
	select {
	case got := <-s.started:
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("no watch started")
		return [4]string{}
	}
}

// watchApp is a GitHub-hosted repository (github.com/o/r) whose pushes go to a
// local bare repository, with a connected account.
func watchApp(t *testing.T) (*App, *testrepo.Repo, string, *watchSpy) {
	t.Helper()
	t.Cleanup(keys.UseStore(&fakeKeys{items: map[string]string{}}))
	a, r, id := newPlainApp(t)
	a.hosting.newClient = func(hosting.Host, authFunc) hosting.Client { return &fakeHost{} }
	bare := testrepo.NewBareFrom(t, r)
	r.Git("remote", "add", "origin", "https://github.com/o/r")
	r.Git("config", "url."+bare+".pushInsteadOf", "https://github.com/o/r")
	if _, err := a.SaveGitHubAccount("token", "ghp_x"); err != nil {
		t.Fatal(err)
	}
	spy := newWatchSpy()
	a.hosting.watch = spy.watch
	t.Cleanup(func() { close(spy.release) })
	return a, r, id, spy
}

func TestPushBranchStartsWatch(t *testing.T) {
	a, r, id, spy := watchApp(t)
	r.Git("switch", "-q", "-c", "topic")
	r.Commit("on topic")
	res, err := a.PushBranch(id, "topic")
	if err != nil || res.Status != ops.PushPushed {
		t.Fatalf("push: %+v %v", res, err)
	}
	got := spy.next(t)
	if got != [4]string{id, "topic", r.Git("rev-parse", "topic"), "Checks"} {
		t.Fatalf("watch started with %v", got)
	}
	// Up to date: nothing was pushed, so nothing to watch.
	r.Git("switch", "-q", "main")
	res, err = a.PushBranch(id, "topic")
	if err != nil || res.Status != ops.PushUpToDate {
		t.Fatalf("second push: %+v %v", res, err)
	}
	spy.none(t)
}

func TestPushWatchesTheCurrentBranch(t *testing.T) {
	a, r, id, spy := watchApp(t)
	r.Git("switch", "-q", "-c", "topic")
	r.Commit("on topic")
	if err := a.Push(id); err != nil {
		t.Fatal(err)
	}
	first := spy.next(t)
	if first != [4]string{id, "topic", r.Git("rev-parse", "topic"), "Checks"} {
		t.Fatalf("watch started with %v", first)
	}
	// Pushing again moves nothing: no watch of the old tip.
	if err := a.Push(id); err != nil {
		t.Fatal(err)
	}
	spy.none(t)
	// A new commit pushed is a new watch of the new tip.
	r.Commit("more")
	if err := a.Push(id); err != nil {
		t.Fatal(err)
	}
	if second := spy.next(t); second[2] != r.Git("rev-parse", "topic") || second[2] == first[2] {
		t.Fatalf("second watch %v, want the new tip", second)
	}
}

func TestPushOfAnUpToDateBranchStartsNoWatch(t *testing.T) {
	a, r, id, spy := watchApp(t)
	r.Git("push", "-q", "-u", "origin", "main")
	if err := a.Push(id); err != nil {
		t.Fatal(err)
	}
	spy.none(t)
}

func TestPushFromDetachedHeadWatchesNothing(t *testing.T) {
	a, r, id, spy := watchApp(t)
	r.Git("push", "-q", "-u", "origin", "main")
	r.Git("switch", "-q", "--detach")
	r.Commit("detached")
	if err := a.Push(id); err == nil {
		t.Log("push from a detached HEAD succeeded")
	}
	spy.none(t)
}

func TestWatchPushedOncePerTipUntilItEnds(t *testing.T) {
	a, r, id, spy := watchApp(t)
	r.Git("switch", "-q", "-c", "topic")
	r.Commit("on topic")
	tip := r.Git("rev-parse", "topic")
	a.watchPushed(id, []string{"topic"})
	if got := spy.next(t); got[2] != tip {
		t.Fatalf("watch started with %v", got)
	}
	a.watchPushed(id, []string{"topic"})
	spy.none(t) // the same tip is already being watched
	spy.release <- struct{}{}
	// Once the watch has ended the tip can be watched again.
	deadline := time.Now().Add(5 * time.Second)
	for {
		a.watchPushed(id, []string{"topic"})
		select {
		case got := <-spy.started:
			if got[2] != tip {
				t.Fatalf("watch started with %v", got)
			}
			return
		case <-time.After(50 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatal("the tip was never watched again after its watch ended")
		}
	}
}

func TestPushAllWatchesEveryPushedBranch(t *testing.T) {
	a, r, id, spy := watchApp(t)
	r.Git("push", "-q", "-u", "origin", "main")
	r.Commit("ahead")
	results, err := a.PushAll(id)
	if err != nil {
		t.Fatal(err)
	}
	pushed := 0
	for _, res := range results {
		if res.Status == ops.PushPushed {
			pushed++
		}
	}
	if pushed != 1 {
		t.Fatalf("results %+v, want main pushed", results)
	}
	if got := spy.next(t); got[1] != "main" || got[2] != r.Git("rev-parse", "main") {
		t.Fatalf("watch started with %v", got)
	}
	spy.none(t)
}

func TestPushDoesNotWatchUnsupported(t *testing.T) {
	t.Cleanup(keys.UseStore(&fakeKeys{items: map[string]string{}}))
	a, r, id := newPlainApp(t)
	spy := newWatchSpy()
	a.hosting.watch = spy.watch
	t.Cleanup(func() { close(spy.release) })
	bare := testrepo.NewBareFrom(t, r)
	r.Git("remote", "add", "origin", bare)
	r.Git("switch", "-q", "-c", "topic")
	r.Commit("on topic")
	if err := a.Push(id); err != nil {
		t.Fatal(err)
	}
	spy.none(t)
}

func TestPushDoesNotWatchWithoutAnAccount(t *testing.T) {
	a, r, id, spy := watchApp(t)
	if err := a.RemoveHostingAccount("github"); err != nil {
		t.Fatal(err)
	}
	r.Git("switch", "-q", "-c", "topic")
	r.Commit("on topic")
	if err := a.Push(id); err != nil {
		t.Fatal(err)
	}
	spy.none(t)
}

func TestFailedPushDoesNotWatch(t *testing.T) {
	a, r, id, spy := watchApp(t)
	r.Git("config", "remote.origin.pushurl", filepath.Join(t.TempDir(), "missing.git"))
	r.Git("switch", "-q", "-c", "topic")
	r.Commit("on topic")
	if err := a.Push(id); err == nil {
		t.Fatal("push to a missing remote succeeded")
	}
	spy.none(t)
}

func TestWatchFetchCachesTheNewStateAndStopsOnRejection(t *testing.T) {
	a, id, fake, _ := newHostingApp(t)
	a.AddRemote(id, "origin", "https://github.com/o/r")
	if _, err := a.SaveGitHubAccount("token", "ghp_x"); err != nil {
		t.Fatal(err)
	}
	fake.checks = func(h string) (hosting.CommitStatus, error) {
		return hosting.CommitStatus{Hash: h, State: hosting.Failed}, nil
	}
	st, err := a.watchFetch(context.Background(), id, hashA)
	if err != nil || st.State != hosting.Failed {
		t.Fatalf("watchFetch: %+v %v", st, err)
	}
	if _, err := a.CommitStatuses(id, []string{hashA}); err != nil || fake.statusCallCount() != 0 {
		t.Fatalf("the log should read the watched state from the cache: calls=%d err=%v", fake.statusCallCount(), err)
	}
	fake.checks = func(string) (hosting.CommitStatus, error) { return hosting.CommitStatus{}, hosting.ErrRejected }
	if _, err := a.watchFetch(context.Background(), id, hashB); !errors.Is(err, hosting.ErrRejected) {
		t.Fatalf("a rejected token should come back as ErrRejected: %v", err)
	}
	if err := a.RemoveHostingAccount("github"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.watchFetch(context.Background(), id, hashB); !errors.Is(err, hosting.ErrNoAccount) {
		t.Fatalf("no account should come back as ErrNoAccount: %v", err)
	}
}

func TestRunWatchEmitsHostingStatus(t *testing.T) {
	a, id, fake, ev := newHostingApp(t)
	a.AddRemote(id, "origin", "https://github.com/o/r")
	if _, err := a.SaveGitHubAccount("token", "ghp_x"); err != nil {
		t.Fatal(err)
	}
	fake.checks = func(h string) (hosting.CommitStatus, error) {
		return hosting.CommitStatus{Hash: h, State: hosting.Success}, nil
	}
	a.runWatch(context.Background(), id, "main", hashA, "Checks")
	got, ok := ev.wait(t, EventHostingStatus).data.(hosting.WatchEvent)
	want := hosting.WatchEvent{RepoID: id, Branch: "main", Hash: hashA, State: hosting.Success, Final: true, Noun: "Checks"}
	if !ok || got != want {
		t.Fatalf("event %+v, want %+v", got, want)
	}
}

func TestJiraSettingsRoundTrip(t *testing.T) {
	a, _, _, _ := newHostingApp(t)
	j, err := a.GetJiraSettings()
	if err != nil || j.Site != "" || len(j.Keys) != 0 {
		t.Fatalf("GetJiraSettings = %+v, %v", j, err)
	}
	if _, err := a.SaveJiraSettings("ftp://x", "NEXO"); err == nil {
		t.Fatal("a non-https site must be refused")
	}
	saved, err := a.SaveJiraSettings("https://escala.atlassian.net/", "nexo, scl")
	if err != nil || saved.Site != "https://escala.atlassian.net" || len(saved.Keys) != 2 {
		t.Fatalf("SaveJiraSettings = %+v, %v", saved, err)
	}
	got, err := a.GetJiraSettings()
	if err != nil || got.Site != saved.Site || len(got.Keys) != 2 || got.Keys[0] != "NEXO" {
		t.Fatalf("GetJiraSettings = %+v, %v", got, err)
	}
}
