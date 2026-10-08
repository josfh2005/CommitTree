package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"git-ui/internal/gitflow"
	"git-ui/internal/repos"
	"git-ui/internal/testrepo"
)

func TestFlowThroughTheApp(t *testing.T) {
	store, err := repos.Open(filepath.Join(t.TempDir(), "repos.json"))
	if err != nil {
		t.Fatal(err)
	}
	r := testrepo.New(t)
	r.Commit("init")
	repo, err := store.Add(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	a := New(store)

	f, err := a.GetFlow(repo.ID)
	if err != nil || f.Initialized {
		t.Fatalf("before init: %+v, %v", f, err)
	}
	cfg := gitflow.Config{Master: "main", Develop: "develop", Prefixes: gitflow.Prefixes{Feature: "feature/", Release: "release/", Hotfix: "hotfix/", Warmfix: "warmfix/"}}
	if err := a.InitFlow(repo.ID, cfg); err != nil {
		t.Fatal(err)
	}
	started, err := a.StartFlow(repo.ID, gitflow.Feature, "x", "")
	if err != nil || started.Branch != "feature/x" {
		t.Fatalf("start: %+v, %v", started, err)
	}
	r.Commit("work")
	plan, err := a.PlanFinish(repo.ID, "feature/x", nil)
	if err != nil || len(plan.Steps) != 1 || plan.Steps[0].Target != "develop" {
		t.Fatalf("plan: %+v, %v", plan, err)
	}
	res, err := a.FinishFlow(repo.ID, "feature/x", nil)
	if err != nil || res.Outcome != "finished" {
		t.Fatalf("finish: %+v, %v", res, err)
	}
}

func TestFlowSettingsThroughTheApp(t *testing.T) {
	a, r, id := newPlainApp(t)

	s, err := a.GetFlowSettings(id)
	if err != nil || s.Initialized || s.Master != "main" || s.Develop != "develop" || s.Prefixes.Feature != "feature/" {
		t.Fatalf("not set up: %+v, %v", s, err)
	}
	cfg := gitflow.Config{Master: "main", Develop: "dev", Prefixes: gitflow.Prefixes{Feature: "f/", Release: "r/", Hotfix: "h/", Warmfix: "w/"}}
	if err := a.SaveFlowSettings(id, cfg); !errors.Is(err, gitflow.ErrNotInitialized) {
		t.Fatalf("save before set up: %v", err)
	}

	// Set up goes through InitFlow; afterwards Save only rewrites keys.
	if err := a.InitFlow(id, cfg); err != nil {
		t.Fatal(err)
	}
	cfg.Develop, cfg.Prefixes.Release = "integration", "rel-"
	if err := a.SaveFlowSettings(id, cfg); err != nil {
		t.Fatal(err)
	}
	if s, err = a.GetFlowSettings(id); err != nil || !s.Initialized || s.Develop != "integration" || s.Prefixes.Release != "rel-" {
		t.Fatalf("after save: %+v, %v", s, err)
	}
	if got := r.Git("branch", "--list", "integration"); got != "" {
		t.Fatalf("save created a branch: %q", got)
	}

	cfg.Develop = cfg.Master
	if err := a.SaveFlowSettings(id, cfg); err == nil {
		t.Fatal("want a refusal for production == development")
	}
	if _, err := a.GetFlowSettings("no-such-id"); err == nil {
		t.Error("want an error for an unknown repository")
	}
	if err := a.SaveFlowSettings("no-such-id", cfg); err == nil {
		t.Error("want an error for an unknown repository")
	}
}

func TestSavingFlowSettingsChangesTheMainBranchesPushed(t *testing.T) {
	a, r, id := newPlainApp(t)
	r.Git("branch", "develop")
	r.Git("branch", "integration")
	cfg := gitflow.Config{Master: "main", Develop: "develop", Prefixes: gitflow.Prefixes{Feature: "feature/", Release: "release/", Hotfix: "hotfix/", Warmfix: "warmfix/"}}
	if err := a.InitFlow(id, cfg); err != nil {
		t.Fatal(err)
	}
	official := func() map[string]bool {
		refs, err := a.GetRefs(id)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]bool{}
		for _, b := range refs.Local {
			got[b.Name] = b.Official
		}
		return got
	}
	if got := official(); !got["develop"] || got["integration"] {
		t.Fatalf("before: %v", got)
	}
	cfg.Develop = "integration"
	if err := a.SaveFlowSettings(id, cfg); err != nil {
		t.Fatal(err)
	}
	if got := official(); got["develop"] || !got["integration"] {
		t.Fatalf("after: %v", got)
	}
}
