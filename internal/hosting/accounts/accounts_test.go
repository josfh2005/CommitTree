package accounts

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"git-ui/internal/ai/keys"
	"git-ui/internal/hosting"
)

// fakeKeychain is an in-memory keys.Store.
type fakeKeychain struct {
	mu    sync.Mutex
	items map[string]string
}

func (f *fakeKeychain) Get(service, user string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.items[service+"/"+user]
	if !ok {
		return "", keys.ErrNotFound
	}
	return v, nil
}

func (f *fakeKeychain) Set(service, user, password string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items[service+"/"+user] = password
	return nil
}

func (f *fakeKeychain) Delete(service, user string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.items, service+"/"+user)
	return nil
}

func newStore(t *testing.T) (*Store, *fakeKeychain, string) {
	t.Helper()
	kc := &fakeKeychain{items: map[string]string{}}
	t.Cleanup(keys.UseStore(kc))
	path := filepath.Join(t.TempDir(), "hosting.json")
	return NewStore(path), kc, path
}

// stubGH replaces the CLI lookup and run, and counts the runs.
func stubGH(t *testing.T, tokens ...string) *int {
	t.Helper()
	oldFind, oldRun := findGH, RunGH
	t.Cleanup(func() { findGH, RunGH = oldFind, oldRun })
	findGH = func() (string, bool) { return "/fake/gh", true }
	runs := new(int)
	var mu sync.Mutex
	RunGH = func(ctx context.Context, gh string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		i := *runs
		*runs++
		if i >= len(tokens) {
			i = len(tokens) - 1
		}
		return tokens[i], nil
	}
	return runs
}

func TestLoadMissingIsZero(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "hosting.json"))
	if err != nil || c != (Config{}) {
		t.Fatalf("Load = %+v, %v", c, err)
	}
}

func TestPathNextTo(t *testing.T) {
	if got := PathNextTo("/x/git-ui/ai.json"); got != "/x/git-ui/hosting.json" {
		t.Errorf("PathNextTo = %q", got)
	}
}

func TestSaveIsAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "hosting.json")
	want := Config{BitbucketEmail: "a@b.c", BitbucketName: "Ann", BitbucketID: "{u}", GitHubSource: "gh", GitHubLogin: "ann"}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || got != want {
		t.Fatalf("round trip = %+v, %v", got, err)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Errorf("want only hosting.json, got %d entries", len(entries))
	}
}

func TestAuthBitbucketBasic(t *testing.T) {
	s, _, _ := newStore(t)
	if err := s.SaveBitbucket(" a@b.c ", " tok ", hosting.Account{ID: "{u}", Name: "Ann"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Auth(hosting.Bitbucket)(context.Background())
	if err != nil || got != "Basic YUBiLmM6dG9r" {
		t.Fatalf("Auth = %q, %v", got, err)
	}
	cfg, _ := s.Config()
	if cfg.BitbucketEmail != "a@b.c" || cfg.BitbucketName != "Ann" || cfg.BitbucketID != "{u}" {
		t.Errorf("config = %+v", cfg)
	}
}

func TestAuthGitHubToken(t *testing.T) {
	s, _, _ := newStore(t)
	if err := s.SaveGitHub("token", " ghp_x ", hosting.Account{Login: "ann"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Auth(hosting.GitHub)(context.Background())
	if err != nil || got != "Bearer ghp_x" {
		t.Fatalf("Auth = %q, %v", got, err)
	}
}

func TestAuthNoAccount(t *testing.T) {
	s, _, _ := newStore(t)
	for _, h := range []hosting.Host{hosting.Bitbucket, hosting.GitHub} {
		if _, err := s.Auth(h)(context.Background()); !errors.Is(err, hosting.ErrNoAccount) {
			t.Errorf("%s: err = %v, want ErrNoAccount", h, err)
		}
	}
}

func TestGHTokenCachedAndRefreshedOnce(t *testing.T) {
	s, _, _ := newStore(t)
	runs := stubGH(t, "gh-1", "gh-2", "gh-3")
	if err := s.SaveGitHub("gh", "ignored", hosting.Account{Login: "ann"}); err != nil {
		t.Fatal(err)
	}
	auth := s.Auth(hosting.GitHub)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if got, err := auth(ctx); err != nil || got != "Bearer gh-1" {
			t.Fatalf("Auth = %q, %v", got, err)
		}
	}
	if *runs != 1 {
		t.Fatalf("gh ran %d times for two calls, want 1", *runs)
	}
	s.OnReject(hosting.GitHub)()
	if s.Rejected(hosting.GitHub) {
		t.Fatal("the first 401 must only drop the cached token")
	}
	if got, err := auth(ctx); err != nil || got != "Bearer gh-2" || *runs != 2 {
		t.Fatalf("after the first 401: %q, %v, runs %d", got, err, *runs)
	}
	s.OnReject(hosting.GitHub)()
	if !s.Rejected(hosting.GitHub) {
		t.Fatal("a second 401 must mark GitHub rejected")
	}
	if _, err := auth(ctx); !errors.Is(err, hosting.ErrRejected) || *runs != 2 {
		t.Fatalf("rejected Auth: %v, runs %d", err, *runs)
	}
	// The allowance resets only on SaveGitHub.
	if err := s.SaveGitHub("gh", "", hosting.Account{Login: "ann"}); err != nil {
		t.Fatal(err)
	}
	if got, err := auth(ctx); err != nil || got != "Bearer gh-3" {
		t.Fatalf("after save: %q, %v", got, err)
	}
	s.OnReject(hosting.GitHub)()
	if s.Rejected(hosting.GitHub) {
		t.Error("after a save the first 401 gets the re-run again")
	}
}

func TestGitHubTokenModeRejectsOnFirst401(t *testing.T) {
	s, _, _ := newStore(t)
	if err := s.SaveGitHub("token", "ghp_x", hosting.Account{Login: "ann"}); err != nil {
		t.Fatal(err)
	}
	s.OnReject(hosting.GitHub)()
	if !s.Rejected(hosting.GitHub) {
		t.Fatal("token mode: the first 401 marks GitHub rejected")
	}
}

func TestGHFailure(t *testing.T) {
	s, _, _ := newStore(t)
	oldFind, oldRun := findGH, RunGH
	t.Cleanup(func() { findGH, RunGH = oldFind, oldRun })
	findGH = func() (string, bool) { return "/fake/gh", true }
	RunGH = func(context.Context, string) (string, error) {
		return "", errors.New("exit status 1: secret-ish output")
	}
	if err := s.SaveGitHub("gh", "", hosting.Account{Login: "ann"}); err != nil {
		t.Fatal(err)
	}
	_, err := s.Auth(hosting.GitHub)(context.Background())
	if err == nil || err.Error() != "GitHub CLI isn't logged in (gh auth login)" {
		t.Fatalf("err = %v", err)
	}
	findGH = func() (string, bool) { return "", false }
	if _, err := s.Auth(hosting.GitHub)(context.Background()); err == nil || err.Error() != "GitHub CLI isn't logged in (gh auth login)" {
		t.Fatalf("no gh binary: err = %v", err)
	}
}

// Review Focus 3.
func TestSaveClearsRejected(t *testing.T) {
	s, _, _ := newStore(t)
	me := hosting.Account{ID: "{u}", Name: "Ann"}
	if err := s.SaveBitbucket("a@b.c", "old", me); err != nil {
		t.Fatal(err)
	}
	s.OnReject(hosting.Bitbucket)()
	if !s.Rejected(hosting.Bitbucket) {
		t.Fatal("want Bitbucket rejected after a 401")
	}
	if err := s.SaveBitbucket("a@b.c", "new", me); err != nil {
		t.Fatal(err)
	}
	if s.Rejected(hosting.Bitbucket) {
		t.Fatal("saving the account must clear the rejected flag")
	}
	if got, err := s.Auth(hosting.Bitbucket)(context.Background()); err != nil || got == "" {
		t.Fatalf("requests must resume: %q, %v", got, err)
	}
}

// Ruling 4: while rejected, Auth fails with ErrRejected so nothing is sent.
func TestAuthWhileRejected(t *testing.T) {
	s, _, _ := newStore(t)
	if err := s.SaveBitbucket("a@b.c", "tok", hosting.Account{}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveGitHub("token", "ghp_x", hosting.Account{}); err != nil {
		t.Fatal(err)
	}
	s.OnReject(hosting.Bitbucket)()
	s.OnReject(hosting.GitHub)()
	for h, want := range map[hosting.Host]string{
		hosting.Bitbucket: "Bitbucket rejected the token",
		hosting.GitHub:    "GitHub rejected the token",
	} {
		v, err := s.Auth(h)(context.Background())
		if !errors.Is(err, hosting.ErrRejected) || err.Error() != want || v != "" {
			t.Errorf("%s: %q, %v", h, v, err)
		}
	}
}

// Review Focus 4.
func TestSaveRefusesBlank(t *testing.T) {
	s, kc, path := newStore(t)
	me := hosting.Account{ID: "{u}", Name: "Ann", Login: "ann"}
	if err := s.SaveBitbucket("  ", "tok", me); err == nil {
		t.Error("blank email: want an error")
	}
	if err := s.SaveBitbucket("a@b.c", "   ", me); err == nil {
		t.Error("blank token: want an error")
	}
	if err := s.SaveGitHub("token", " ", me); err == nil {
		t.Error("blank GitHub token: want an error")
	}
	if err := s.SaveGitHub("nope", "t", me); err == nil {
		t.Error("unknown source: want an error")
	}
	if len(kc.items) != 0 {
		t.Errorf("keychain must stay empty, has %v", kc.items)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("hosting.json must not be written")
	}
}

func TestSaveGitHubGHDropsStoredToken(t *testing.T) {
	s, kc, _ := newStore(t)
	if err := s.SaveGitHub("token", "ghp_x", hosting.Account{Login: "ann"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveGitHub("gh", "ignored", hosting.Account{Login: "ann"}); err != nil {
		t.Fatal(err)
	}
	if len(kc.items) != 0 {
		t.Errorf("switching to gh must remove the pasted token, keychain has %v", kc.items)
	}
}

func TestRemoveClearsKeyAndConfig(t *testing.T) {
	s, kc, _ := newStore(t)
	if err := s.SaveBitbucket("a@b.c", "tok", hosting.Account{ID: "{u}", Name: "Ann"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveGitHub("token", "ghp_x", hosting.Account{Login: "ann"}); err != nil {
		t.Fatal(err)
	}
	s.OnReject(hosting.Bitbucket)()
	if err := s.Remove(hosting.Bitbucket); err != nil {
		t.Fatal(err)
	}
	if _, ok := kc.items["git-ui/bitbucket"]; ok {
		t.Error("the Bitbucket token must leave the keychain")
	}
	if _, ok := kc.items["git-ui/github"]; !ok {
		t.Error("the GitHub token must stay")
	}
	cfg, _ := s.Config()
	if cfg.BitbucketEmail != "" || cfg.BitbucketName != "" || cfg.BitbucketID != "" {
		t.Errorf("Bitbucket data left: %+v", cfg)
	}
	if cfg.GitHubSource != "token" || cfg.GitHubLogin != "ann" {
		t.Errorf("GitHub data must stay: %+v", cfg)
	}
	if s.Rejected(hosting.Bitbucket) {
		t.Error("Remove clears the rejected flag")
	}
	if _, err := s.Auth(hosting.Bitbucket)(context.Background()); !errors.Is(err, hosting.ErrNoAccount) {
		t.Errorf("err = %v, want ErrNoAccount", err)
	}
	if err := s.Remove(hosting.GitHub); err != nil {
		t.Fatal(err)
	}
	cfg, _ = s.Config()
	if cfg != (Config{}) {
		t.Errorf("config not empty: %+v", cfg)
	}
}

func TestConcurrentUse(t *testing.T) {
	s, _, _ := newStore(t)
	stubGH(t, "gh-1")
	if err := s.SaveGitHub("gh", "", hosting.Account{Login: "ann"}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s.Auth(hosting.GitHub)(context.Background())
			if i%7 == 0 {
				s.OnReject(hosting.GitHub)()
			}
			s.Rejected(hosting.GitHub)
		}(i)
	}
	wg.Wait()
}

func TestProbeRefusesBlankBeforeAnythingElse(t *testing.T) {
	s, kc, _ := newStore(t)
	for _, c := range [][2]string{{"", "tok"}, {"  ", "tok"}, {"a@b.c", ""}, {"a@b.c", " \t"}} {
		if _, err := s.ProbeBitbucket(c[0], c[1]); err == nil {
			t.Errorf("ProbeBitbucket(%q, %q) accepted", c[0], c[1])
		}
	}
	if _, err := s.ProbeGitHub(SourceToken, "  "); err == nil {
		t.Error("ProbeGitHub accepted a blank token")
	}
	if _, err := s.ProbeGitHub("nope", "x"); err == nil {
		t.Error("ProbeGitHub accepted an unknown source")
	}
	if len(kc.items) != 0 {
		t.Errorf("keychain touched: %v", kc.items)
	}
}

func TestProbeDoesNotStoreAnything(t *testing.T) {
	s, kc, path := newStore(t)
	auth, err := s.ProbeBitbucket(" a@b.c ", " tok ")
	if err != nil {
		t.Fatal(err)
	}
	v, err := auth(context.Background())
	if err != nil || v != "Basic "+base64.StdEncoding.EncodeToString([]byte("a@b.c:tok")) {
		t.Fatalf("auth = %q, %v", v, err)
	}
	gh, err := s.ProbeGitHub(SourceToken, "ghp_x")
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := gh(context.Background()); v != "Bearer ghp_x" {
		t.Fatalf("gh auth = %q", v)
	}
	if _, statErr := os.Stat(path); statErr == nil || len(kc.items) != 0 {
		t.Fatalf("something was stored: %v %v", statErr, kc.items)
	}
}

// A token the CLI handed over and GitHub rejected must not stick: after
// `gh auth login` the retry asks gh again.
func TestProbeGitHubCLIDoesNotCacheTheToken(t *testing.T) {
	s, _, _ := newStore(t)
	oldFind, oldRun := findGH, RunGH
	t.Cleanup(func() { findGH, RunGH = oldFind, oldRun })
	findGH = func() (string, bool) { return "/fake/gh", true }
	token := "old-token"
	runs := 0
	RunGH = func(context.Context, string) (string, error) { runs++; return token + "\n", nil }

	probe, err := s.ProbeGitHub(SourceGH, "")
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := probe(context.Background()); v != "Bearer old-token" {
		t.Fatalf("first = %q", v)
	}
	// GitHub rejected it; the user logs in again and retries.
	token = "new-token"
	probe, _ = s.ProbeGitHub(SourceGH, "")
	if v, err := probe(context.Background()); err != nil || v != "Bearer new-token" {
		t.Fatalf("retry = %q, %v", v, err)
	}
	if runs != 2 {
		t.Fatalf("gh ran %d times, want 2", runs)
	}
	if s.ghToken != "" {
		t.Fatal("the probe cached a token")
	}
}

func TestNormalizeJira(t *testing.T) {
	ok := []struct {
		site, keys string
		want       Jira
	}{
		{"https://escala.atlassian.net", "NEXO, SCL", Jira{Site: "https://escala.atlassian.net", Keys: []string{"NEXO", "SCL"}}},
		{"  https://escala.atlassian.net/// ", "nexo,scl nexo\tAB1", Jira{Site: "https://escala.atlassian.net", Keys: []string{"NEXO", "SCL", "AB1"}}},
		{"https://jira.example.com/jira/", "x1", Jira{Site: "https://jira.example.com/jira", Keys: []string{"X1"}}},
		{"", "", Jira{Keys: []string{}}},
		{"https://a.b", "", Jira{Site: "https://a.b", Keys: []string{}}},
		{"", "NEXO", Jira{Keys: []string{"NEXO"}}},
	}
	for _, c := range ok {
		got, err := NormalizeJira(c.site, c.keys)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("NormalizeJira(%q, %q) = %+v, %v; want %+v", c.site, c.keys, got, err, c.want)
		}
	}
	bad := []struct{ site, keys string }{
		{"http://escala.atlassian.net", "NEXO"},
		{"escala.atlassian.net", "NEXO"},
		{"https://", "NEXO"},
		{"javascript:alert(1)", "NEXO"},
		{"https://u:p@escala.atlassian.net", "NEXO"},
		{"https://escala.atlassian.net/?q=1", "NEXO"},
		{"https://escala.atlassian.net/#x", "NEXO"},
		{"https://escala.atlassian.net", "N"},
		{"https://escala.atlassian.net", "1AB"},
		{"https://escala.atlassian.net", "NE-XO"},
		{"https://escala.atlassian.net", "NEXO, a_b"},
	}
	for _, c := range bad {
		if got, err := NormalizeJira(c.site, c.keys); err == nil {
			t.Errorf("NormalizeJira(%q, %q) = %+v, want an error", c.site, c.keys, got)
		}
	}
}

func TestSaveJiraPersistsNextToAccounts(t *testing.T) {
	s, _, path := newStore(t)
	if err := s.SaveBitbucket("a@b.c", "tok", hosting.Account{ID: "{u}", Name: "Ann"}); err != nil {
		t.Fatal(err)
	}
	if j, err := s.Jira(); err != nil || j.Site != "" || len(j.Keys) != 0 {
		t.Fatalf("Jira before = %+v, %v", j, err)
	}
	saved, err := s.SaveJira("https://escala.atlassian.net/", "nexo scl")
	if err != nil || saved.Site != "https://escala.atlassian.net" || !reflect.DeepEqual(saved.Keys, []string{"NEXO", "SCL"}) {
		t.Fatalf("SaveJira = %+v, %v", saved, err)
	}
	// A new store on the same file sees it, and the account is untouched.
	again := NewStore(path)
	j, err := again.Jira()
	if err != nil || j.Site != "https://escala.atlassian.net" || !reflect.DeepEqual(j.Keys, []string{"NEXO", "SCL"}) {
		t.Fatalf("Jira after = %+v, %v", j, err)
	}
	cfg, _ := again.Config()
	if cfg.BitbucketEmail != "a@b.c" {
		t.Errorf("the account must stay: %+v", cfg)
	}
	// Saving an account or removing it leaves the Jira block alone.
	if err := again.Remove(hosting.Bitbucket); err != nil {
		t.Fatal(err)
	}
	if j, _ := again.Jira(); j.Site == "" || len(j.Keys) != 2 {
		t.Errorf("Remove must keep Jira: %+v", j)
	}
	// An invalid save changes nothing.
	if _, err := again.SaveJira("http://x.y", "NEXO"); err == nil {
		t.Fatal("http site must be refused")
	}
	if j, _ := again.Jira(); j.Site != "https://escala.atlassian.net" {
		t.Errorf("a refused save must keep the old settings: %+v", j)
	}
	// Clearing both fields turns the feature off.
	if _, err := again.SaveJira("", ""); err != nil {
		t.Fatal(err)
	}
	if j, _ := again.Jira(); j.Site != "" || len(j.Keys) != 0 {
		t.Errorf("cleared Jira = %+v", j)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "jira") {
		t.Errorf("cleared Jira must leave no keys in the file: %s", data)
	}
}
