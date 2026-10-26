// Package accounts holds the Bitbucket and GitHub accounts of the hosting
// integration: tokens in the operating system's secret store (through the
// keys package), the non-secret rest in hosting.json next to ai.json, and the
// GitHub CLI as an alternative source of the GitHub token.
//
// A token is never written to hosting.json, to an error text or to a log.
package accounts

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"git-ui/internal/ai/keys"
	"git-ui/internal/hosting"
)

// GitHub token sources.
const (
	SourceGH    = "gh"
	SourceToken = "token"
)

// errGHNotLoggedIn is what the user sees when the GitHub CLI cannot give a
// token. It carries no output of the CLI.
var errGHNotLoggedIn = errors.New("GitHub CLI isn't logged in (gh auth login)")

// Config is the non-secret part of the accounts, stored in hosting.json.
type Config struct {
	BitbucketEmail string `json:"bitbucketEmail,omitempty"`
	BitbucketName  string `json:"bitbucketName,omitempty"` // "Connected as"
	BitbucketID    string `json:"bitbucketID,omitempty"`   // uuid, for the Mine/Review filters
	GitHubSource   string `json:"githubSource,omitempty"`  // "gh" | "token" | ""
	GitHubLogin    string `json:"githubLogin,omitempty"`
	JiraSite       string `json:"jiraSite,omitempty"` // https://<site>, no trailing slash
	JiraKeys       string `json:"jiraKeys,omitempty"` // "NEXO,SCL"
}

// PathNextTo returns the hosting.json that sits in the same directory as the
// given ai.json.
func PathNextTo(aiSettingsPath string) string {
	return filepath.Join(filepath.Dir(aiSettingsPath), "hosting.json")
}

// Load reads hosting.json. A missing file is a zero Config.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("accounts: %s is not valid: %w", filepath.Base(path), err)
	}
	return c, nil
}

// Save writes hosting.json atomically: a temp file in the same directory,
// then a rename.
func Save(path string, c Config) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "hosting-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(append(data, '\n'))
	cerr := f.Close()
	if werr != nil || cerr != nil {
		os.Remove(tmp)
		return errors.Join(werr, cerr)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// FindGH looks for the GitHub CLI: on PATH first, then where Homebrew and
// the system packages put it (a GUI app often has a short PATH).
func FindGH() (path string, ok bool) {
	if p, err := exec.LookPath("gh"); err == nil {
		return p, true
	}
	for _, p := range []string{"/opt/homebrew/bin/gh", "/usr/local/bin/gh", "/usr/bin/gh"} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, true
		}
	}
	return "", false
}

// findGH is FindGH, replaceable by tests.
var findGH = FindGH

// RunGH asks the GitHub CLI for its token. It is a variable so tests can
// replace it.
var RunGH = func(ctx context.Context, gh string) (string, error) {
	out, err := exec.CommandContext(ctx, gh, "auth", "token", "--hostname", "github.com").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Store gives the Doers their Authorization header and remembers which hosts
// answered 401. It is safe for concurrent use.
type Store struct {
	path string

	mu       sync.Mutex
	ghToken  string // token from the CLI, cached
	ghGen    int    // bumped when the cache is dropped, so a slow run cannot restore a stale token
	ghRerun  bool   // the one allowed re-run of gh after a 401 is used up
	rejected map[hosting.Host]bool
}

// NewStore returns a Store whose non-secret data lives at path (hosting.json).
func NewStore(path string) *Store {
	return &Store{path: path, rejected: map[hosting.Host]bool{}}
}

// Config returns the saved non-secret data.
func (s *Store) Config() (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Load(s.path)
}

func label(h hosting.Host) string {
	if h == hosting.GitHub {
		return "GitHub"
	}
	return "Bitbucket"
}

func keyName(h hosting.Host) string {
	if h == hosting.GitHub {
		return "github"
	}
	return "bitbucket"
}

// Auth returns the function a Doer calls before each request. It yields the
// full Authorization value, or an error wrapping hosting.ErrNoAccount when
// there is no account, or hosting.ErrRejected while the host is marked
// rejected (nothing is sent until the account is saved again).
func (s *Store) Auth(h hosting.Host) func(ctx context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		if s.Rejected(h) {
			return "", fmt.Errorf("%s %w", label(h), hosting.ErrRejected)
		}
		cfg, err := s.Config()
		if err != nil {
			return "", err
		}
		switch h {
		case hosting.Bitbucket:
			return s.bitbucketAuth(cfg)
		case hosting.GitHub:
			return s.githubAuth(ctx, cfg)
		}
		return "", fmt.Errorf("accounts: unknown host %q", h)
	}
}

func noAccount(h hosting.Host) error {
	return fmt.Errorf("%s: %w", label(h), hosting.ErrNoAccount)
}

func (s *Store) bitbucketAuth(cfg Config) (string, error) {
	if cfg.BitbucketEmail == "" {
		return "", noAccount(hosting.Bitbucket)
	}
	token, err := keys.Get("bitbucket")
	if err != nil {
		return "", err
	}
	if token == "" {
		return "", noAccount(hosting.Bitbucket)
	}
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(cfg.BitbucketEmail+":"+token)), nil
}

func (s *Store) githubAuth(ctx context.Context, cfg Config) (string, error) {
	switch cfg.GitHubSource {
	case SourceToken:
		token, err := keys.Get("github")
		if err != nil {
			return "", err
		}
		if token == "" {
			return "", noAccount(hosting.GitHub)
		}
		return "Bearer " + token, nil
	case SourceGH:
		token, err := s.ghTokenFor(ctx)
		if err != nil {
			return "", err
		}
		return "Bearer " + token, nil
	}
	return "", noAccount(hosting.GitHub)
}

// ghTokenFor returns the cached CLI token, running gh when there is none. The
// lock is never held while gh runs.
func (s *Store) ghTokenFor(ctx context.Context) (string, error) {
	s.mu.Lock()
	token, gen := s.ghToken, s.ghGen
	s.mu.Unlock()
	if token != "" {
		return token, nil
	}
	gh, ok := findGH()
	if !ok {
		return "", errGHNotLoggedIn
	}
	token, err := RunGH(ctx, gh)
	token = strings.TrimSpace(token)
	if err != nil || token == "" {
		return "", errGHNotLoggedIn
	}
	s.mu.Lock()
	if s.ghGen == gen {
		s.ghToken = token
	}
	s.mu.Unlock()
	return token, nil
}

// OnReject returns the function a Doer calls on a 401. In token mode (and for
// Bitbucket) the host is marked rejected. In GitHub CLI mode the first 401
// drops the cached token and allows one more run of gh, since the CLI may
// have been re-logged in; a second 401 before the account is saved again
// marks GitHub rejected. The store cannot see successful requests, so the
// allowance resets only on SaveGitHub.
func (s *Store) OnReject(h hosting.Host) func() {
	return func() {
		var gh bool
		if h == hosting.GitHub {
			cfg, err := s.Config()
			gh = err == nil && cfg.GitHubSource == SourceGH
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if gh && !s.ghRerun {
			s.ghRerun = true
			s.ghToken = ""
			s.ghGen++
			return
		}
		s.rejected[h] = true
	}
}

// Rejected reports whether the host has answered 401 since the account was
// last saved.
func (s *Store) Rejected(h hosting.Host) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rejected[h]
}

// SaveBitbucket stores the token in the keychain and the email and identity
// in hosting.json, and clears the rejected flag. A blank email or token is
// refused before anything is written.
func (s *Store) SaveBitbucket(email, token string, me hosting.Account) error {
	email, token = strings.TrimSpace(email), strings.TrimSpace(token)
	if email == "" {
		return errors.New("accounts: the Bitbucket email is empty")
	}
	if token == "" {
		return errors.New("accounts: the Bitbucket token is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, err := Load(s.path)
	if err != nil {
		return err
	}
	if err := keys.Set("bitbucket", token); err != nil {
		return err
	}
	cfg.BitbucketEmail, cfg.BitbucketName, cfg.BitbucketID = email, me.Name, me.ID
	if err := Save(s.path, cfg); err != nil {
		return err
	}
	delete(s.rejected, hosting.Bitbucket)
	return nil
}

// SaveGitHub saves the GitHub account. source is "gh" (the token comes from
// the GitHub CLI and token is ignored) or "token" (token goes to the
// keychain). It clears the rejected flag and the CLI re-run allowance.
func (s *Store) SaveGitHub(source, token string, me hosting.Account) error {
	token = strings.TrimSpace(token)
	switch source {
	case SourceToken:
		if token == "" {
			return errors.New("accounts: the GitHub token is empty")
		}
	case SourceGH:
	default:
		return fmt.Errorf("accounts: unknown GitHub source %q", source)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, err := Load(s.path)
	if err != nil {
		return err
	}
	if source == SourceToken {
		err = keys.Set("github", token)
	} else {
		err = keys.Delete("github") // an older pasted token must not outlive the switch
	}
	if err != nil {
		return err
	}
	cfg.GitHubSource, cfg.GitHubLogin = source, me.Login
	if err := Save(s.path, cfg); err != nil {
		return err
	}
	delete(s.rejected, hosting.GitHub)
	s.ghToken, s.ghRerun = "", false
	s.ghGen++
	return nil
}

// Remove deletes the host's token and its data in hosting.json.
func (s *Store) Remove(h hosting.Host) error {
	if h != hosting.Bitbucket && h != hosting.GitHub {
		return fmt.Errorf("accounts: unknown host %q", h)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, err := Load(s.path)
	if err != nil {
		return err
	}
	if err := keys.Delete(keyName(h)); err != nil {
		return err
	}
	if h == hosting.Bitbucket {
		cfg.BitbucketEmail, cfg.BitbucketName, cfg.BitbucketID = "", "", ""
	} else {
		cfg.GitHubSource, cfg.GitHubLogin = "", ""
		s.ghToken, s.ghRerun = "", false
		s.ghGen++
	}
	if err := Save(s.path, cfg); err != nil {
		return err
	}
	delete(s.rejected, h)
	return nil
}

// ProbeBitbucket is the Authorization source for checking an email and token
// before they are saved. Blank values are refused here, before anything is
// sent.
func (s *Store) ProbeBitbucket(email, token string) (func(ctx context.Context) (string, error), error) {
	email, token = strings.TrimSpace(email), strings.TrimSpace(token)
	if email == "" {
		return nil, errors.New("the Bitbucket email is empty")
	}
	if token == "" {
		return nil, errors.New("the Bitbucket token is empty")
	}
	v := "Basic " + base64.StdEncoding.EncodeToString([]byte(email+":"+token))
	return func(context.Context) (string, error) { return v, nil }, nil
}

// ProbeGitHub is the Authorization source for checking a GitHub token (source
// "token") or the GitHub CLI's (source "gh") before the account is saved.
func (s *Store) ProbeGitHub(source, token string) (func(ctx context.Context) (string, error), error) {
	switch source {
	case SourceToken:
		token = strings.TrimSpace(token)
		if token == "" {
			return nil, errors.New("the GitHub token is empty")
		}
		v := "Bearer " + token
		return func(context.Context) (string, error) { return v, nil }, nil
	case SourceGH:
		// Runs gh directly and keeps nothing: a token GitHub then rejects
		// must not be reused by the retry after `gh auth login`.
		return func(ctx context.Context) (string, error) {
			gh, ok := findGH()
			if !ok {
				return "", errGHNotLoggedIn
			}
			t, err := RunGH(ctx, gh)
			t = strings.TrimSpace(t)
			if err != nil || t == "" {
				return "", errGHNotLoggedIn
			}
			return "Bearer " + t, nil
		}, nil
	}
	return nil, fmt.Errorf("unknown GitHub source %q", source)
}
