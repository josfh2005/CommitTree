package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git-ui/internal/merge"
	"git-ui/internal/repos"
	"git-ui/internal/testrepo"
)

// newMergeApp returns an App holding one repository where main and feature
// both changed greeting.txt, so merging feature conflicts.
func newMergeApp(t *testing.T) (*App, *testrepo.Repo, string) {
	t.Helper()
	store, err := repos.Open(filepath.Join(t.TempDir(), "repos.json"))
	if err != nil {
		t.Fatal(err)
	}
	r := testrepo.New(t)
	r.WriteFile("greeting.txt", "hello\n")
	r.Git("add", "greeting.txt")
	r.Git("commit", "-q", "-m", "add greeting")
	r.Git("switch", "-q", "-c", "feature")
	r.WriteFile("greeting.txt", "hola\n")
	r.Git("commit", "-q", "-am", "spanish")
	r.Git("switch", "-q", "main")
	r.WriteFile("greeting.txt", "hi\n")
	r.Git("commit", "-q", "-am", "informal")
	repo, err := store.Add(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	return New(store), r, repo.ID
}

func TestMergeBranchReportsConflicts(t *testing.T) {
	a, _, id := newMergeApp(t)
	got, err := a.MergeBranch(id, "feature")
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != merge.Conflicted || len(got.Conflicts) != 1 {
		t.Fatalf("result = %+v", got)
	}
}

func TestGetMergeStateDuringAMerge(t *testing.T) {
	a, _, id := newMergeApp(t)
	if _, err := a.MergeBranch(id, "feature"); err != nil {
		t.Fatal(err)
	}
	st, err := a.GetMergeState(id)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Merging || st.From != "feature" || st.Into != "main" {
		t.Fatalf("state = %+v", st)
	}
}

func TestGetConflictFileShowsMarkersThenTheDiff(t *testing.T) {
	a, r, id := newMergeApp(t)
	if _, err := a.MergeBranch(id, "feature"); err != nil {
		t.Fatal(err)
	}

	f, err := a.GetConflictFile(id, "greeting.txt")
	if err != nil {
		t.Fatal(err)
	}
	if f.Resolved || !strings.Contains(f.Text, "<<<<<<<") {
		t.Fatalf("while conflicted: resolved = %v, text = %q", f.Resolved, f.Text)
	}

	r.WriteFile("greeting.txt", "hi / hola\n")
	r.Git("add", "greeting.txt")
	f, err = a.GetConflictFile(id, "greeting.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !f.Resolved || !strings.Contains(f.Text, "+hi / hola") {
		t.Fatalf("after staging: resolved = %v, text = %q", f.Resolved, f.Text)
	}
}

func TestGetConflictFileRefusesAPathOutsideTheMerge(t *testing.T) {
	a, _, id := newMergeApp(t)
	if _, err := a.MergeBranch(id, "feature"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.GetConflictFile(id, "../escape.txt"); err == nil {
		t.Fatal("want an error for a path that is not part of the merge")
	}
}

func TestAbortMergeRestoresTheRepo(t *testing.T) {
	a, r, id := newMergeApp(t)
	before := r.Git("rev-parse", "HEAD")
	if _, err := a.MergeBranch(id, "feature"); err != nil {
		t.Fatal(err)
	}
	if err := a.AbortMerge(id); err != nil {
		t.Fatal(err)
	}
	if got := r.Git("rev-parse", "HEAD"); got != before {
		t.Errorf("HEAD moved: %s != %s", got, before)
	}
}

func TestCommitMergeCreatesTheMergeCommit(t *testing.T) {
	a, r, id := newMergeApp(t)
	if _, err := a.MergeBranch(id, "feature"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.Dir, "greeting.txt"), []byte("hi / hola\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.Git("add", "greeting.txt")
	if err := a.CommitMerge(id); err != nil {
		t.Fatal(err)
	}
	if n := len(strings.Fields(r.Git("rev-list", "--parents", "-n", "1", "HEAD"))); n != 3 {
		t.Errorf("want a merge commit with two parents, got %d fields", n)
	}
	st, err := a.GetMergeState(id)
	if err != nil || st.Merging {
		t.Errorf("state = %+v, err = %v", st, err)
	}
}
