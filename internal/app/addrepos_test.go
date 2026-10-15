package app

import (
	"os"
	"path/filepath"
	"testing"

	"git-ui/internal/repos"
	"git-ui/internal/testrepo"
)

func newEmptyApp(t *testing.T) *App {
	t.Helper()
	store, err := repos.Open(filepath.Join(t.TempDir(), "repos.json"))
	if err != nil {
		t.Fatal(err)
	}
	return New(store)
}

func TestScanReposInMarksListedThroughSymlinks(t *testing.T) {
	a := newEmptyApp(t)
	listed := testrepo.New(t)
	listed.Commit("init")
	if _, err := a.store.Add(a.ctx, listed.Dir); err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	// The listed repo appears in parent under another spelling (a symlink);
	// the stored path is git's resolved top-level.
	if err := os.Symlink(listed.Dir, filepath.Join(parent, "a")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(parent, "b", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := a.ScanReposIn(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Repos) != 2 || !got.Repos[0].Added || got.Repos[1].Added {
		t.Fatalf("got %+v", got)
	}
}

func TestScanReposInPassesErrors(t *testing.T) {
	a := newEmptyApp(t)
	if _, err := a.ScanReposIn("relative"); err == nil {
		t.Fatal("relative parent accepted")
	}
}

func TestAddReposNeverNilSlices(t *testing.T) {
	a := newEmptyApp(t)
	r := testrepo.New(t)
	r.Commit("init")

	res, err := a.AddRepos([]string{r.Dir}, "")
	if err != nil || len(res.Added) != 1 || res.Failed == nil {
		t.Fatalf("res %+v err %v", res, err)
	}
	res, err = a.AddRepos([]string{r.Dir}, "")
	if err != nil || res.Added == nil || res.Failed == nil || len(res.Added) != 0 {
		t.Fatalf("second add %+v err %v", res, err)
	}
}
