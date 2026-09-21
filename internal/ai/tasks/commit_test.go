package tasks_test

import (
	"context"
	"strings"
	"testing"

	"git-ui/internal/ai/tasks"
	"git-ui/internal/testrepo"
)

// The message must describe what is being committed, so the context carries
// the staged diff and not the unstaged one.
func TestCommitContextCarriesOnlyTheStagedDiff(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile("a.txt", "one\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-m", "base")
	r.WriteFile("a.txt", "STAGED CHANGE\n")
	r.Git("add", "a.txt")
	r.WriteFile("b.txt", "UNSTAGED CHANGE\n")

	got, err := tasks.CommitContext(context.Background(), r.Dir, 6000)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "STAGED CHANGE") {
		t.Errorf("context = %q, missing the staged change", got)
	}
	if strings.Contains(got, "UNSTAGED CHANGE") {
		t.Errorf("context = %q, leaks the unstaged change", got)
	}
}

// A truncated diff must say so, or the model invents what it could not see.
func TestCommitContextSaysWhenItTruncated(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile("a.txt", "one\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-m", "base")
	r.WriteFile("a.txt", strings.Repeat("a long line of change\n", 500))
	r.Git("add", "a.txt")

	got, err := tasks.CommitContext(context.Background(), r.Dir, 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > 2000 {
		t.Errorf("context is %d bytes, want it cut to the budget", len(got))
	}
	if !strings.Contains(strings.ToLower(got), "truncated") {
		t.Errorf("context = %q, does not say it was truncated", got)
	}
}
