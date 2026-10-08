package refs_test

import (
	"testing"

	"git-ui/internal/refs"
	"git-ui/internal/testrepo"
)

func TestOfficialDefaultNames(t *testing.T) {
	rule := refs.OfficialRule{}
	for name, want := range map[string]bool{
		"main": true, "master": true, "develop": true, "release/1.3": true, "release/": false,
		"feature/x": false, "hotfix/1": false, "mainline": false, "releases/1": false, "Main": false,
	} {
		if got := rule.IsOfficial(name); got != want {
			t.Errorf("IsOfficial(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestOfficialGitFlowNamesReplaceTheDefaults(t *testing.T) {
	rule := refs.OfficialRule{Master: "prod", Develop: "dev", ReleasePrefix: "rel/"}
	for name, want := range map[string]bool{
		"prod": true, "dev": true, "rel/2": true,
		"main": false, "master": false, "develop": false, "release/1": false, "feature/x": false,
	} {
		if got := rule.IsOfficial(name); got != want {
			t.Errorf("IsOfficial(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestReadOfficialRule(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	rule, err := refs.ReadOfficialRule(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if rule != (refs.OfficialRule{}) {
		t.Fatalf("no git-flow config: rule = %+v, want zero", rule)
	}

	// Only one of master/develop: git-flow is not configured.
	r.Git("config", "gitflow.branch.master", "prod")
	if rule, _ = refs.ReadOfficialRule(ctx, r.Dir); rule != (refs.OfficialRule{}) {
		t.Fatalf("half a config: rule = %+v, want zero", rule)
	}

	r.Git("config", "gitflow.branch.develop", "dev")
	rule, _ = refs.ReadOfficialRule(ctx, r.Dir)
	if want := (refs.OfficialRule{Master: "prod", Develop: "dev", ReleasePrefix: "release/"}); rule != want {
		t.Fatalf("rule = %+v, want %+v", rule, want)
	}

	r.Git("config", "gitflow.prefix.release", "rel/")
	rule, _ = refs.ReadOfficialRule(ctx, r.Dir)
	if want := (refs.OfficialRule{Master: "prod", Develop: "dev", ReleasePrefix: "rel/"}); rule != want {
		t.Fatalf("rule = %+v, want %+v", rule, want)
	}
}

func TestListMarksOfficialLocalBranches(t *testing.T) {
	src := testrepo.New(t)
	src.Commit("first")
	src.Git("branch", "develop")
	src.Git("branch", "release/1.0")
	src.Git("branch", "feature/x")
	r := testrepo.Clone(t, testrepo.NewBareFrom(t, src))
	for _, b := range []string{"develop", "release/1.0", "feature/x"} {
		r.Git("branch", b, "origin/"+b)
	}
	got, err := refs.List(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	official := map[string]bool{}
	for _, b := range got.Local {
		official[b.Name] = b.Official
	}
	want := map[string]bool{"main": true, "develop": true, "release/1.0": true, "feature/x": false}
	for n, w := range want {
		if official[n] != w {
			t.Errorf("%s official = %v, want %v", n, official[n], w)
		}
	}
	for _, rem := range got.Remotes {
		for _, b := range rem.Branches {
			if b.Official {
				t.Errorf("remote branch %s marked official", b.Name)
			}
		}
	}

	r.Git("config", "gitflow.branch.master", "prod")
	r.Git("config", "gitflow.branch.develop", "dev")
	r.Git("config", "gitflow.prefix.release", "rel/")
	got, _ = refs.List(ctx, r.Dir)
	for _, b := range got.Local {
		if b.Official {
			t.Errorf("with custom git-flow names %s is official", b.Name)
		}
	}
}
