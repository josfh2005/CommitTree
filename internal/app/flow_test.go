package app

import (
	"context"
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
