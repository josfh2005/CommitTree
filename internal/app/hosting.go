package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"git-ui/internal/ai/settings"
	"git-ui/internal/gitcmd"
	"git-ui/internal/hosting"
	"git-ui/internal/hosting/accounts"
	"git-ui/internal/hosting/bitbucket"
	"git-ui/internal/hosting/github"
	"git-ui/internal/ops"
)

// The Bitbucket / GitHub integration (docs/spec/14-hosting.md).

const (
	// EventHostingLimit tells the frontend a host's request limit started or
	// ended: HostingLimit.
	EventHostingLimit = "hosting:limit"
	// EventHostingStatus tells the frontend the statuses of a repository's
	// commits changed on their own (a push being watched).
	EventHostingStatus = "hosting:status"

	bitbucketAPI = "https://api.bitbucket.org/2.0"
	githubAPI    = "https://api.github.com"
)

// authFunc is what a hosting.Doer calls before each request.
type authFunc = func(ctx context.Context) (string, error)

// hostingState is what the integration keeps for the whole session: the
// accounts, one rate-limit state per host shared by every repository, and
// the status cache.
type hostingState struct {
	mu     sync.Mutex
	path   string // hosting.json; "" until WithAI or first use sets it
	store  *accounts.Store
	limits *hosting.Limits
	cache  *hosting.Cache
	// announced is which hosts the frontend was last told are blocked.
	announced map[hosting.Host]bool
	// newClient builds the client for a host. auth is the Authorization
	// source to use instead of the saved account (nil: the saved one); it is
	// how an account is verified before it is stored. Tests replace it.
	newClient func(h hosting.Host, auth authFunc) hosting.Client
	// watching is the pushed commits being watched, by watchKey. At most one
	// watch runs per repository and commit.
	watching map[string]bool
	// watch runs one watch of a pushed commit until it ends (blocking). Tests
	// replace it.
	watch func(ctx context.Context, repoID, branch, hash, noun string)
}

func (a *App) initHosting() {
	a.hosting = &hostingState{
		limits:    hosting.NewLimits(nil),
		cache:     hosting.NewCache(nil),
		announced: map[hosting.Host]bool{},
		watching:  map[string]bool{},
	}
	a.hosting.newClient = a.defaultHostingClient
	a.hosting.watch = a.runWatch
}

// accountStore returns the accounts, creating them on first use next to
// ai.json.
func (a *App) accountStore() (*accounts.Store, error) {
	h := a.hosting
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.store != nil {
		return h.store, nil
	}
	if h.path == "" {
		p, err := settings.DefaultPath()
		if err != nil {
			return nil, err
		}
		h.path = accounts.PathNextTo(p)
	}
	h.store = accounts.NewStore(h.path)
	return h.store, nil
}

func (a *App) defaultHostingClient(host hosting.Host, auth authFunc) hosting.Client {
	d := &hosting.Doer{Host: host, Limits: a.hosting.limits}
	store, err := a.accountStore()
	switch {
	case auth != nil:
		d.Auth = auth
	case err != nil:
		d.Auth = func(context.Context) (string, error) { return "", err }
	default:
		d.Auth = store.Auth(host)
		d.OnReject = store.OnReject(host)
	}
	if host == hosting.GitHub {
		d.BaseURL = githubAPI
		return github.New(d)
	}
	d.BaseURL = bitbucketAPI
	return bitbucket.New(d)
}

// hostingClient is the client for a host, using the saved account.
func (a *App) hostingClient(h hosting.Host) hosting.Client {
	return a.hosting.newClient(h, nil)
}

// HostingInfo is what the frontend knows about a repository's code host.
type HostingInfo struct {
	Supported bool `json:"supported"`
	hosting.Repo
	WebURL   string `json:"webURL"`
	PullsURL string `json:"pullsURL"`
	Noun     string `json:"noun"`    // "Pipelines" | "Checks"
	Account  string `json:"account"` // "none" | "connected" | "rejected"
}

// HostingAccount is one host's account for Settings.
type HostingAccount struct {
	Host        hosting.Host `json:"host"`
	Connected   bool         `json:"connected"`
	Rejected    bool         `json:"rejected"`
	Name        string       `json:"name"`
	Email       string       `json:"email"`
	Source      string       `json:"source"`
	GHAvailable bool         `json:"ghAvailable"`
}

// HostingLimit is the payload of EventHostingLimit. Until is null when the
// limit has ended.
type HostingLimit struct {
	Host  hosting.Host `json:"host"`
	Until *time.Time   `json:"until"`
	Known bool         `json:"known"`
}

// detectHosting finds the hosted repository behind id's remotes, reading them
// afresh each time. A linked worktree has its main repository's remotes.
func (a *App) detectHosting(id string) (hosting.Repo, bool, error) {
	dir, err := a.dir(a.settingsKey(id))
	if err != nil {
		return hosting.Repo{}, false, err
	}
	remotes, err := ops.ListRemotes(a.ctx, dir)
	if err != nil {
		return hosting.Repo{}, false, err
	}
	r, ok := hosting.Detect(remotes)
	return r, ok, nil
}

func hasAccount(cfg accounts.Config, h hosting.Host) bool {
	if h == hosting.GitHub {
		return cfg.GitHubSource != ""
	}
	return cfg.BitbucketEmail != ""
}

// accountState is "none", "rejected" or "connected" for a host. It never
// touches the network.
func (a *App) accountState(h hosting.Host) (string, error) {
	store, err := a.accountStore()
	if err != nil {
		return "", err
	}
	cfg, err := store.Config()
	if err != nil {
		return "", err
	}
	switch {
	case !hasAccount(cfg, h):
		return "none", nil
	case store.Rejected(h):
		return "rejected", nil
	}
	return "connected", nil
}

// HostingInfo says whether the repository is on a supported host and how the
// account for it stands. The remotes are read on every call, so an edited
// remote is picked up at once.
func (a *App) HostingInfo(id string) (HostingInfo, error) {
	r, ok, err := a.detectHosting(id)
	if err != nil || !ok {
		return HostingInfo{}, err
	}
	acc, err := a.accountState(r.Host)
	if err != nil {
		return HostingInfo{}, err
	}
	noun := "Pipelines"
	if r.Host == hosting.GitHub {
		noun = "Checks"
	}
	return HostingInfo{Supported: true, Repo: r, WebURL: r.WebURL(), PullsURL: r.PullsURL(), Noun: noun, Account: acc}, nil
}

func isHex(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// CommitStatuses returns the pipeline or check state of the given commits.
// Answers still fresh in the cache are not asked again. When the repository
// is not on a supported host, has no usable account, or the host cannot be
// reached or has reached its request limit, it returns what it has (nothing
// shown in the log) and no error. Only "no access to this repository" is an
// error.
func (a *App) CommitStatuses(id string, hashes []string) (map[string]hosting.CommitStatus, error) {
	out := map[string]hosting.CommitStatus{}
	r, ok, err := a.detectHosting(id)
	if err != nil || !ok {
		return out, err
	}
	acc, err := a.accountState(r.Host)
	if err != nil || acc != "connected" {
		return out, err
	}
	var valid []string
	for _, h := range hashes {
		if isHex(h) {
			valid = append(valid, h)
		}
	}
	key := hosting.RepoKey(r)
	have, missing := a.hosting.cache.Split(key, valid)
	for h, st := range have {
		out[h] = st
	}
	if len(missing) == 0 {
		return out, nil
	}
	got, err := a.hostingClient(r.Host).Statuses(a.ctx, r, missing)
	a.announceLimit(r.Host)
	if err != nil {
		var na *hosting.NoAccessError
		if errors.As(err, &na) {
			if na.Repo == "" {
				na.Repo = r.FullName()
			}
			return out, na
		}
		// Rate limit, rejected token, network trouble: show what is known.
		return out, nil
	}
	a.hosting.cache.Put(key, got)
	for h, st := range got {
		out[h] = st
	}
	return out, nil
}

// CommitChecks asks the host for one commit's checks, whatever the cache
// holds, and caches the aggregate.
func (a *App) CommitChecks(id, hash string) (hosting.CommitStatus, error) {
	if !isHex(hash) {
		return hosting.CommitStatus{}, fmt.Errorf("%q is not a commit id", hash)
	}
	r, ok, err := a.detectHosting(id)
	if err != nil {
		return hosting.CommitStatus{}, err
	}
	if !ok {
		return hosting.CommitStatus{}, errors.New("this repository is not on Bitbucket or GitHub")
	}
	st, err := a.hostingClient(r.Host).Checks(a.ctx, r, hash)
	a.announceLimit(r.Host)
	if err != nil {
		var na *hosting.NoAccessError
		if errors.As(err, &na) {
			if na.Repo == "" {
				na.Repo = r.FullName()
			}
		}
		return hosting.CommitStatus{}, err
	}
	a.hosting.cache.Put(hosting.RepoKey(r), map[string]hosting.CommitStatus{hash: st})
	return st, nil
}

// announceLimit tells the frontend when a host's request limit has just
// started or ended, comparing with what it was last told. A limit that ends
// by the clock is announced by the first call that notices.
func (a *App) announceLimit(h hosting.Host) {
	a.hosting.mu.Lock()
	rl, blocked := a.hosting.limits.Blocked(h) // lock order: hosting.mu, then limits
	was := a.hosting.announced[h]
	a.hosting.announced[h] = blocked
	a.hosting.mu.Unlock()
	if blocked == was {
		return
	}
	ev := HostingLimit{Host: h}
	if blocked {
		until := rl.Until
		ev.Until, ev.Known = &until, rl.Known
	}
	a.emitUnlessShutdown(EventHostingLimit, ev)
}

func (a *App) accountFor(h hosting.Host) (HostingAccount, error) {
	store, err := a.accountStore()
	if err != nil {
		return HostingAccount{}, err
	}
	cfg, err := store.Config()
	if err != nil {
		return HostingAccount{}, err
	}
	acc := HostingAccount{Host: h, Connected: hasAccount(cfg, h)}
	acc.Rejected = acc.Connected && store.Rejected(h)
	if h == hosting.GitHub {
		acc.Name, acc.Source = cfg.GitHubLogin, cfg.GitHubSource
		_, acc.GHAvailable = accounts.FindGH()
	} else {
		acc.Name, acc.Email = cfg.BitbucketName, cfg.BitbucketEmail
		if acc.Connected {
			acc.Source = accounts.SourceToken
		}
	}
	return acc, nil
}

// GetHostingAccounts returns the Bitbucket account, then the GitHub one.
func (a *App) GetHostingAccounts() ([]HostingAccount, error) {
	out := make([]HostingAccount, 0, 2)
	for _, h := range []hosting.Host{hosting.Bitbucket, hosting.GitHub} {
		acc, err := a.accountFor(h)
		if err != nil {
			return nil, err
		}
		out = append(out, acc)
	}
	return out, nil
}

// SaveBitbucketAccount checks the email and token against Bitbucket, and only
// then stores them. Nothing is stored when the check fails.
func (a *App) SaveBitbucketAccount(email, token string) (HostingAccount, error) {
	store, err := a.accountStore()
	if err != nil {
		return HostingAccount{}, err
	}
	auth, err := store.ProbeBitbucket(email, token)
	if err != nil {
		return HostingAccount{}, err
	}
	me, err := a.hosting.newClient(hosting.Bitbucket, auth).User(a.ctx)
	a.announceLimit(hosting.Bitbucket)
	if err != nil {
		return HostingAccount{}, err
	}
	if err := store.SaveBitbucket(email, token, me); err != nil {
		return HostingAccount{}, err
	}
	return a.accountFor(hosting.Bitbucket)
}

// SaveGitHubAccount checks the token (source "token") or the GitHub CLI's
// (source "gh") against GitHub, and only then stores the account.
func (a *App) SaveGitHubAccount(source, token string) (HostingAccount, error) {
	store, err := a.accountStore()
	if err != nil {
		return HostingAccount{}, err
	}
	auth, err := store.ProbeGitHub(source, token)
	if err != nil {
		return HostingAccount{}, err
	}
	me, err := a.hosting.newClient(hosting.GitHub, auth).User(a.ctx)
	a.announceLimit(hosting.GitHub)
	if err != nil {
		return HostingAccount{}, err
	}
	if err := store.SaveGitHub(source, token, me); err != nil {
		return HostingAccount{}, err
	}
	return a.accountFor(hosting.GitHub)
}

// RemoveHostingAccount forgets the account of "bitbucket" or "github".
func (a *App) RemoveHostingAccount(host string) error {
	h := hosting.Host(strings.ToLower(strings.TrimSpace(host)))
	if h != hosting.Bitbucket && h != hosting.GitHub {
		return fmt.Errorf("unknown host %q", host)
	}
	store, err := a.accountStore()
	if err != nil {
		return err
	}
	return store.Remove(h)
}

// errFork is what checking out a pull request from a fork says: its branch
// is not in this repository's remote.
var errFork = errors.New("From a fork. Open it in the browser.")

// PullRequestList is a page of the pull request list. More is set when the
// host has more than the 100 shown.
type PullRequestList struct {
	Items []hosting.PullRequest `json:"items"`
	More  bool                  `json:"more"`
}

func hostLabel(h hosting.Host) string {
	if h == hosting.GitHub {
		return "GitHub"
	}
	return "Bitbucket"
}

// hostedClient is the hosted repository behind id and the client for its
// host. A repository that is not on a supported host, or whose host has no
// account or a rejected one, is an error the frontend shows.
func (a *App) hostedClient(id string) (hosting.Repo, hosting.Client, error) {
	r, ok, err := a.detectHosting(id)
	if err != nil {
		return hosting.Repo{}, nil, err
	}
	if !ok {
		return hosting.Repo{}, nil, errors.New("this repository is not on Bitbucket or GitHub")
	}
	acc, err := a.accountState(r.Host)
	if err != nil {
		return hosting.Repo{}, nil, err
	}
	switch acc {
	case "none":
		return hosting.Repo{}, nil, fmt.Errorf("%s: %w", hostLabel(r.Host), hosting.ErrNoAccount)
	case "rejected":
		return hosting.Repo{}, nil, fmt.Errorf("%s %w", hostLabel(r.Host), hosting.ErrRejected)
	}
	return r, a.hostingClient(r.Host), nil
}

// hostCallErr announces a changed request limit after a host call and names
// the repository in a "no access" error.
func (a *App) hostCallErr(r hosting.Repo, err error) error {
	a.announceLimit(r.Host)
	var na *hosting.NoAccessError
	if errors.As(err, &na) && na.Repo == "" {
		na.Repo = r.FullName()
	}
	return err
}

// ListPullRequests returns the open pull requests of the repository for a
// filter: "open", "mine" or "review". On Bitbucket each row's State comes
// from the status cache only, never from extra requests.
func (a *App) ListPullRequests(id, filter string) (PullRequestList, error) {
	f := hosting.Filter(filter)
	switch f {
	case hosting.FilterOpen, hosting.FilterMine, hosting.FilterReview:
	default:
		return PullRequestList{}, fmt.Errorf("unknown pull request filter %q", filter)
	}
	r, client, err := a.hostedClient(id)
	if err != nil {
		return PullRequestList{}, err
	}
	me, err := a.hostingMe(r.Host)
	if err != nil {
		return PullRequestList{}, err
	}
	prs, more, err := client.PullRequests(a.ctx, r, f, me)
	if err != nil {
		return PullRequestList{}, a.hostCallErr(r, err)
	}
	a.announceLimit(r.Host)
	if prs == nil {
		prs = []hosting.PullRequest{}
	}
	if r.Host == hosting.Bitbucket {
		key := hosting.RepoKey(r)
		for i := range prs {
			if prs[i].HeadHash == "" {
				continue
			}
			if st, ok := a.hosting.cache.Get(key, prs[i].HeadHash); ok {
				prs[i].State = st.State
			}
		}
	}
	return PullRequestList{Items: prs, More: more}, nil
}

// hostingMe is the signed-in user of a host as saved in hosting.json, for the
// Mine and Review filters.
func (a *App) hostingMe(h hosting.Host) (hosting.Account, error) {
	store, err := a.accountStore()
	if err != nil {
		return hosting.Account{}, err
	}
	cfg, err := store.Config()
	if err != nil {
		return hosting.Account{}, err
	}
	if h == hosting.GitHub {
		return hosting.Account{ID: cfg.GitHubLogin, Name: cfg.GitHubLogin, Login: cfg.GitHubLogin}, nil
	}
	return hosting.Account{ID: cfg.BitbucketID, Name: cfg.BitbucketName}, nil
}

// PullRequestCount is the number of open pull requests, for the sidebar
// badge. It is a lower bound when More is set.
func (a *App) PullRequestCount(id string) (hosting.Count, error) {
	r, client, err := a.hostedClient(id)
	if err != nil {
		return hosting.Count{}, err
	}
	n, err := client.PullRequestCount(a.ctx, r)
	if err != nil {
		return hosting.Count{}, a.hostCallErr(r, err)
	}
	a.announceLimit(r.Host)
	return n, nil
}

// GetPullRequest returns one pull request with its description, reviewers,
// commits, comments and the checks of its last commit.
func (a *App) GetPullRequest(id string, number int) (hosting.PullRequestDetail, error) {
	_, d, err := a.pullRequestDetail(id, number)
	return d, err
}

// pullRequestDetail is GetPullRequest with the repository it was read from.
func (a *App) pullRequestDetail(id string, number int) (hosting.Repo, hosting.PullRequestDetail, error) {
	if number <= 0 {
		return hosting.Repo{}, hosting.PullRequestDetail{}, fmt.Errorf("%d is not a pull request number", number)
	}
	r, client, err := a.hostedClient(id)
	if err != nil {
		return hosting.Repo{}, hosting.PullRequestDetail{}, err
	}
	d, err := client.PullRequest(a.ctx, r, number)
	if err != nil {
		return hosting.Repo{}, hosting.PullRequestDetail{}, a.hostCallErr(r, err)
	}
	a.announceLimit(r.Host)
	return r, d, nil
}

// GetPullRequestFiles returns the changed files of a pull request with their
// diffs, as the host gives them.
func (a *App) GetPullRequestFiles(id string, number int) ([]hosting.PRFile, error) {
	if number <= 0 {
		return nil, fmt.Errorf("%d is not a pull request number", number)
	}
	r, client, err := a.hostedClient(id)
	if err != nil {
		return nil, err
	}
	files, err := client.PullRequestFiles(a.ctx, r, number)
	if err != nil {
		return nil, a.hostCallErr(r, err)
	}
	a.announceLimit(r.Host)
	if files == nil {
		files = []hosting.PRFile{}
	}
	return files, nil
}

// CheckoutPullRequest fetches the pull request's remote and checks out its
// source branch the way the sidebar does (CheckoutRemote), returning that
// call's outcome. A pull request from a fork is refused. The pull request is
// read from the host first, with no lock held; FetchRemote and CheckoutRemote
// take the repository's write lock themselves.
func (a *App) CheckoutPullRequest(id string, number int) (string, error) {
	r, pr, err := a.pullRequestDetail(id, number)
	if err != nil {
		return "", err
	}
	if pr.Fork {
		return "", errFork
	}
	if pr.Source == "" {
		return "", errors.New("this pull request has no source branch")
	}
	if err := a.FetchRemote(id, r.Remote); err != nil {
		return "", err
	}
	return a.CheckoutRemote(id, r.Remote, pr.Source)
}

// runWatch is the default hostingState.watch: it polls the commit's checks
// every 30 s and emits EventHostingStatus on each change.
func (a *App) runWatch(ctx context.Context, repoID, branch, hash, noun string) {
	w := &hosting.Watcher{
		Fetch: a.watchFetch,
		Emit:  func(e hosting.WatchEvent) { a.emitUnlessShutdown(EventHostingStatus, e) },
	}
	w.Watch(ctx, repoID, branch, hash, noun)
}

// watchFetch reads one commit's checks for a watch and caches the answer, so
// the log shows the new state at once. The remotes and the account are read
// on every poll: an edited remote or a rejected token is noticed.
func (a *App) watchFetch(ctx context.Context, id, hash string) (hosting.CommitStatus, error) {
	r, client, err := a.hostedClient(id)
	if err != nil {
		return hosting.CommitStatus{}, err
	}
	st, err := client.Checks(ctx, r, hash)
	a.announceLimit(r.Host)
	if err != nil {
		return hosting.CommitStatus{}, err
	}
	a.hosting.cache.Put(hosting.RepoKey(r), map[string]hosting.CommitStatus{hash: st})
	return st, nil
}

// currentBranch is the checked-out branch of repository id, "" on a detached
// HEAD or when it cannot be read.
func (a *App) currentBranch(id string) string {
	dir, err := a.dir(id)
	if err != nil {
		return ""
	}
	out, err := gitcmd.Run(a.ctx, dir, gitcmd.ReadTimeout, "symbolic-ref", "-q", "--short", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// watchPushed starts watching the pipeline of each pushed branch's tip, when
// the repository is on a supported host with a connected account (spec 14,
// Watching a push). It is called after the push returned and the write lock
// was released, and it does not wait for the watches. A second push of a tip
// already being watched starts nothing.
func (a *App) watchPushed(id string, branches []string) {
	if len(branches) == 0 || a.ctx.Err() != nil {
		return
	}
	info, err := a.HostingInfo(id)
	if err != nil || !info.Supported || info.Account != "connected" {
		return
	}
	dir, err := a.dir(id)
	if err != nil {
		return
	}
	for _, branch := range branches {
		out, err := gitcmd.Run(a.ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
		hash := strings.TrimSpace(out)
		if err != nil || !isHex(hash) {
			continue
		}
		key := id + "\x00" + hash
		a.hosting.mu.Lock()
		dup := a.hosting.watching[key]
		a.hosting.watching[key] = true
		watch := a.hosting.watch
		a.hosting.mu.Unlock()
		if dup {
			continue
		}
		go func(branch, hash string) {
			defer func() {
				a.hosting.mu.Lock()
				delete(a.hosting.watching, key)
				a.hosting.mu.Unlock()
			}()
			watch(a.ctx, id, branch, hash, info.Noun)
		}(branch, hash)
	}
}
