package merge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git-ui/internal/testrepo"
)

// conflicting builds a repo where main and feature both changed greeting.txt.
func conflicting(t *testing.T) *testrepo.Repo {
	t.Helper()
	r := testrepo.New(t)
	r.WriteFile("greeting.txt", "hello\n")
	r.Git("add", "greeting.txt")
	r.Git("commit", "-q", "-m", "add greeting")
	r.Git("switch", "-q", "-c", "feature")
	r.WriteFile("greeting.txt", "hola\n")
	r.Git("commit", "-q", "-am", "greet in spanish")
	r.Git("switch", "-q", "main")
	r.WriteFile("greeting.txt", "hi\n")
	r.Git("commit", "-q", "-am", "greet informally")
	return r
}

func TestStartCleanMerge(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("switch", "-q", "-c", "feature")
	r.Commit("on feature")
	r.Git("switch", "-q", "main")

	got, err := Start(context.Background(), r.Dir, "feature")
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != Merged {
		t.Fatalf("outcome = %v, want Merged", got.Outcome)
	}
	// --no-ff means a merge commit even when a fast-forward was possible.
	parents := r.Git("rev-list", "--parents", "-n", "1", "HEAD")
	if len(strings.Fields(parents)) != 3 {
		t.Errorf("want a merge commit with two parents, got %q", parents)
	}
}

func TestStartConflicted(t *testing.T) {
	r := conflicting(t)
	got, err := Start(context.Background(), r.Dir, "feature")
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != Conflicted {
		t.Fatalf("outcome = %v, want Conflicted", got.Outcome)
	}
	if len(got.Conflicts) != 1 || got.Conflicts[0] != "greeting.txt" {
		t.Fatalf("conflicts = %v", got.Conflicts)
	}
	// zdiff3 was requested, so the markers carry the common ancestor.
	data, err := os.ReadFile(filepath.Join(r.Dir, "greeting.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "|||||||") {
		t.Errorf("want a zdiff3 base section, got:\n%s", data)
	}
}

func TestStartAlreadyUpToDate(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("branch", "feature")

	got, err := Start(context.Background(), r.Dir, "feature")
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != UpToDate {
		t.Fatalf("outcome = %v, want UpToDate", got.Outcome)
	}
}

func TestStartRejectsAnOptionLikeRef(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	if _, err := Start(context.Background(), r.Dir, "--exec=rm -rf /"); err == nil {
		t.Fatal("want an error for a ref starting with a dash")
	}
}

func TestAbortRestoresTheBranch(t *testing.T) {
	r := conflicting(t)
	before := r.Git("rev-parse", "HEAD")
	if _, err := Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	if err := Abort(context.Background(), r.Dir); err != nil {
		t.Fatal(err)
	}
	if got := r.Git("rev-parse", "HEAD"); got != before {
		t.Errorf("HEAD = %s, want %s", got, before)
	}
	if data, _ := os.ReadFile(filepath.Join(r.Dir, "greeting.txt")); string(data) != "hi\n" {
		t.Errorf("greeting.txt = %q, want the pre-merge content", data)
	}
}

func TestCommitClosesTheMerge(t *testing.T) {
	r := conflicting(t)
	if _, err := Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	r.WriteFile("greeting.txt", "hi there\n")
	r.Git("add", "greeting.txt")
	if err := Commit(context.Background(), r.Dir); err != nil {
		t.Fatal(err)
	}
	parents := r.Git("rev-list", "--parents", "-n", "1", "HEAD")
	if len(strings.Fields(parents)) != 3 {
		t.Errorf("want two parents, got %q", parents)
	}
}
