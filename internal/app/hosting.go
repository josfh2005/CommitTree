package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"git-ui/internal/ai/settings"
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
}

func (a *App) initHosting() {
	a.hosting = &hostingState{
		limits:    hosting.NewLimits(nil),
		cache:     hosting.NewCache(nil),
		announced: map[hosting.Host]bool{},
	}
	a.hosting.newClient = a.defaultHostingClient
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
	rl, blocked := a.hosting.limits.Blocked(h)
	a.hosting.mu.Lock()
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
