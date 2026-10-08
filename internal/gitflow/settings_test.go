package gitflow

import (
	"errors"
	"strings"
	"testing"

	"git-ui/internal/refs"
	"git-ui/internal/testrepo"
)

func TestReadSettingsOfASetUpRepository(t *testing.T) {
	r := newFlowRepo(t)
	r.Git("config", "gitflow.prefix.feature", "feat-")
	s, err := ReadSettings(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	want := Settings{Initialized: true, Master: "master", Develop: "develop",
		Prefixes: Prefixes{Feature: "feat-", Release: "release/", Hotfix: "hotfix/", Warmfix: "warmfix/"}}
	if s != want {
		t.Fatalf("settings = %+v, want %+v", s, want)
	}
}

func TestReadSettingsNotSetUpGivesTheAppDefaults(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("init") // testrepo's first branch is main
	s, err := ReadSettings(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	want := Settings{Master: "main", Develop: "develop", Prefixes: defaultPrefixes}
	if s != want {
		t.Fatalf("settings = %+v, want %+v", s, want)
	}

	r.Git("branch", "-m", "master")
	if s, _ = ReadSettings(ctx, r.Dir); s.Master != "master" || s.Initialized {
		t.Fatalf("master-only repo: %+v", s)
	}
}

func TestReadSettingsReportsAMissingBranch(t *testing.T) {
	r := newFlowRepo(t)
	r.Git("switch", "-q", "master")
	r.Git("branch", "-D", "develop")
	s, err := ReadSettings(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Initialized || s.Problem != "develop does not exist" {
		t.Fatalf("settings = %+v", s)
	}
}

func validConfig() Config {
	return Config{Master: "main", Develop: "dev", Prefixes: Prefixes{Feature: "feat-", Release: "rel/", Hotfix: "fix/", Warmfix: "warm/"}}
}

func TestSaveSettingsUpdatesKeysAndLeavesBranchesAlone(t *testing.T) {
	r := newFlowRepo(t)
	r.Git("config", "gitflow.prefix.bugfix", "bugfix/")
	before := r.Git("for-each-ref", "--format=%(refname) %(objectname)")
	head := r.Git("symbolic-ref", "HEAD")

	cfg := validConfig() // main and dev do not exist
	if err := SaveSettings(ctx, r.Dir, cfg); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"gitflow.branch.master": "main", "gitflow.branch.develop": "dev",
		"gitflow.prefix.feature": "feat-", "gitflow.prefix.release": "rel/",
		"gitflow.prefix.hotfix": "fix/", "gitflow.prefix.warmfix": "warm/",
		"gitflow.prefix.bugfix": "bugfix/",
	} {
		if got := r.Git("config", key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if after := r.Git("for-each-ref", "--format=%(refname) %(objectname)"); after != before {
		t.Fatalf("branches changed:\n%s\n--\n%s", before, after)
	}
	if r.Git("symbolic-ref", "HEAD") != head {
		t.Fatal("HEAD moved")
	}
}

func TestSaveSettingsIsSeenByTheOfficialRule(t *testing.T) {
	r := newFlowRepo(t)
	cfg := validConfig()
	if err := SaveSettings(ctx, r.Dir, cfg); err != nil {
		t.Fatal(err)
	}
	rule, err := refs.ReadOfficialRule(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{"main": true, "dev": true, "rel/1": true, "release/1": false, "master": false, "develop": false} {
		if rule.IsOfficial(name) != want {
			t.Errorf("IsOfficial(%q) = %v, want %v", name, !want, want)
		}
	}
}

func TestSaveSettingsRefusesWhenNotSetUp(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("init")
	err := SaveSettings(ctx, r.Dir, validConfig())
	if !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("err = %v", err)
	}
	if out := r.Git("config", "--list"); strings.Contains(out, "gitflow.") {
		t.Fatalf("keys written: %s", out)
	}
}

func TestSaveSettingsRefusesInvalidValues(t *testing.T) {
	cases := map[string]func(*Config){
		"empty production":        func(c *Config) { c.Master = "" },
		"blank development":       func(c *Config) { c.Develop = "  " },
		"invalid production":      func(c *Config) { c.Master = "a..b" },
		"space in development":    func(c *Config) { c.Develop = "my dev" },
		"dash development":        func(c *Config) { c.Develop = "-dev" },
		"same branch":             func(c *Config) { c.Develop = c.Master },
		"same branch, case":       func(c *Config) { c.Develop = strings.ToUpper(c.Master) },
		"at sign production":      func(c *Config) { c.Master = "@" },
		"previous-branch dev":     func(c *Config) { c.Develop = "@{-1}" },
		"at brace in name":        func(c *Config) { c.Master = "a@{b" },
		"same feature, release":   func(c *Config) { c.Prefixes.Release = c.Prefixes.Feature },
		"same hotfix, warmfix":    func(c *Config) { c.Prefixes.Warmfix = c.Prefixes.Hotfix },
		"prefixes differ by case": func(c *Config) { c.Prefixes.Hotfix = strings.ToUpper(c.Prefixes.Feature) },
		"empty feature prefix":    func(c *Config) { c.Prefixes.Feature = "" },
		"empty warmfix prefix":    func(c *Config) { c.Prefixes.Warmfix = "" },
		"space in release":        func(c *Config) { c.Prefixes.Release = "rel ease/" },
		"invalid hotfix prefix":   func(c *Config) { c.Prefixes.Hotfix = "fix~/" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r := newFlowRepo(t)
			cfg := validConfig()
			mutate(&cfg)
			if err := SaveSettings(ctx, r.Dir, cfg); err == nil {
				t.Fatal("want an error")
			}
			if got := r.Git("config", "gitflow.branch.master"); got != "master" {
				t.Fatalf("config changed: master = %q", got)
			}
			if got := r.Git("config", "gitflow.prefix.feature"); got != "feature/" {
				t.Fatalf("config changed: feature = %q", got)
			}
		})
	}
}

func TestSaveSettingsAcceptsPrefixesWithoutASlash(t *testing.T) {
	r := newFlowRepo(t)
	cfg := validConfig()
	cfg.Prefixes = Prefixes{Feature: "f-", Release: "r-", Hotfix: "h-", Warmfix: "w-"}
	if err := SaveSettings(ctx, r.Dir, cfg); err != nil {
		t.Fatal(err)
	}
}

func TestInitRefusesASpaceInAPrefix(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("init")
	cfg := Config{Master: "main", Develop: "develop", Prefixes: Prefixes{Feature: "my feat/", Release: "r/", Hotfix: "h/", Warmfix: "w/"}}
	if err := Init(ctx, r.Dir, cfg); err == nil {
		t.Fatal("want an error")
	}
}

// What counts as "git-flow is set up" is decided once, in refs: Read, the
// settings and the official rule must agree on every config.
func TestSetUpAgreesWithTheOfficialRule(t *testing.T) {
	cases := map[string][][2]string{
		"none":            nil,
		"master only":     {{"gitflow.branch.master", "prod"}},
		"develop only":    {{"gitflow.branch.develop", "dev"}},
		"master+prefix":   {{"gitflow.branch.master", "prod"}, {"gitflow.prefix.release", "rel/"}},
		"both":            {{"gitflow.branch.master", "prod"}, {"gitflow.branch.develop", "dev"}},
		"both and prefix": {{"gitflow.branch.master", "prod"}, {"gitflow.branch.develop", "dev"}, {"gitflow.prefix.release", "rel/"}},
	}
	for name, keys := range cases {
		t.Run(name, func(t *testing.T) {
			r := testrepo.New(t)
			r.Commit("init")
			for _, kv := range keys {
				r.Git("config", kv[0], kv[1])
			}
			rule, err := refs.ReadOfficialRule(ctx, r.Dir)
			if err != nil {
				t.Fatal(err)
			}
			flow, err := Read(ctx, r.Dir)
			if err != nil {
				t.Fatal(err)
			}
			settings, err := ReadSettings(ctx, r.Dir)
			if err != nil {
				t.Fatal(err)
			}
			if flow.Initialized != rule.Configured() || settings.Initialized != rule.Configured() {
				t.Fatalf("Read=%v ReadSettings=%v refs=%v", flow.Initialized, settings.Initialized, rule.Configured())
			}
			if rule.Configured() && (flow.Master != rule.Master || flow.Develop != rule.Develop || flow.Prefixes.Release != rule.ReleasePrefix) {
				t.Fatalf("flow %+v disagrees with rule %+v", flow, rule)
			}
		})
	}
}
