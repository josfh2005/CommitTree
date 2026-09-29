package gitflow

import (
	"slices"
	"testing"

	"git-ui/internal/testrepo"
)

func TestReadSourceTreeConfig(t *testing.T) {
	r := newFlowRepo(t)
	r.Git("branch", "feature/NEXO-1")
	r.Git("config", "gitflow.branch.feature/NEXO-1.base", "develop")
	r.Git("branch", "release/r21")
	r.Git("branch", "Warmfix/NEXO-39")
	r.Git("branch", "other")
	r.Git("switch", "-q", "feature/NEXO-1")

	f, err := Read(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if !f.Initialized || f.Problem != "" || f.Master != "master" || f.Develop != "develop" {
		t.Fatalf("flow = %+v", f)
	}
	want := Prefixes{Feature: "feature/", Release: "release/", Hotfix: "hotfix/", Warmfix: "warmfix/"}
	if f.Prefixes != want {
		t.Fatalf("prefixes = %+v", f.Prefixes)
	}
	if f.Current == nil || f.Current.Type != Feature || f.Current.Short != "NEXO-1" || f.Current.Base != "develop" {
		t.Fatalf("current = %+v", f.Current)
	}
	var names []string
	for _, b := range f.Branches {
		names = append(names, b.Type+":"+b.Short)
	}
	if !slices.Equal(names, []string{"warmfix:NEXO-39", "feature:NEXO-1", "release:r21"}) {
		t.Fatalf("branches = %v", names)
	}
	if !slices.Equal(f.Releases, []string{"release/r21"}) {
		t.Fatalf("releases = %v", f.Releases)
	}
}

func TestReadNotInitialized(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("init")
	f, err := Read(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if f.Initialized || f.Branches == nil || f.Releases == nil {
		t.Fatalf("flow = %+v", f)
	}
}

func TestReadReportsMissingDevelop(t *testing.T) {
	r := newFlowRepo(t)
	r.Git("switch", "-q", "master")
	r.Git("branch", "-D", "develop")
	f, err := Read(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if f.Problem != "develop does not exist" {
		t.Fatalf("problem = %q", f.Problem)
	}
}

func TestReadInProgressNeedsAMerge(t *testing.T) {
	r := newFlowRepo(t)
	r.Git("branch", "feature/fresh") // no commits of its own: not in progress
	r.Git("switch", "-q", "-c", "hotfix/h1", "master")
	r.Commit("fix")
	r.Git("switch", "-q", "master")
	r.Git("merge", "-q", "--no-ff", "--no-edit", "hotfix/h1") // in master, not yet in develop
	f, err := Read(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, b := range f.Branches {
		got[b.Name] = b.InProgress
	}
	if got["feature/fresh"] || !got["hotfix/h1"] {
		t.Fatalf("inProgress = %v", got)
	}
}

func TestInitWritesKeysAndCreatesDevelop(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("init")
	r.Git("config", "gitflow.prefix.bugfix", "bugfix/")
	cfg := Config{Master: "main", Develop: "develop", Prefixes: Prefixes{Feature: "f/", Release: "r/", Hotfix: "h/", Warmfix: "w/"}}
	if err := Init(ctx, r.Dir, cfg); err != nil {
		t.Fatal(err)
	}
	if got := r.Git("config", "gitflow.prefix.warmfix"); got != "w/" {
		t.Fatalf("warmfix prefix = %q", got)
	}
	if got := r.Git("config", "gitflow.prefix.bugfix"); got != "bugfix/" {
		t.Fatalf("bugfix prefix touched: %q", got)
	}
	if r.Git("rev-parse", "develop") != r.Git("rev-parse", "main") {
		t.Fatal("develop not created from main")
	}
}

func TestInitRejectsMissingMaster(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("init")
	err := Init(ctx, r.Dir, Config{Master: "nope", Develop: "develop", Prefixes: defaultPrefixes})
	if err == nil {
		t.Fatal("want error")
	}
}
