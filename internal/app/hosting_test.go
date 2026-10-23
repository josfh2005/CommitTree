package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"git-ui/internal/ai/keys"
	"git-ui/internal/hosting"
	"git-ui/internal/hosting/accounts"
	"git-ui/internal/hosting/bitbucket"
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

func (f *fakeHost) PullRequests(context.Context, hosting.Repo, hosting.Filter, hosting.Account) ([]hosting.PullRequest, bool, error) {
	return nil, false, nil
}
func (f *fakeHost) PullRequestCount(context.Context, hosting.Repo) (hosting.Count, error) {
	return hosting.Count{}, nil
}
func (f *fakeHost) PullRequest(context.Context, hosting.Repo, int) (hosting.PullRequestDetail, error) {
	return hosting.PullRequestDetail{}, nil
}
func (f *fakeHost) PullRequestFiles(context.Context, hosting.Repo, int) ([]hosting.PRFile, error) {
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
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
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
	before := hits
	a.CommitStatuses(id, []string{hashA})
	if hits != before || len(limitEvents()) != 1 {
		t.Fatalf("hits %d->%d, events %+v", before, hits, limitEvents())
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
