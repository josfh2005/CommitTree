package worktree_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git-ui/internal/testrepo"
	"git-ui/internal/worktree"
)

// installHook points core.hooksPath at an isolated directory (so the
// machine's own global hooks configuration cannot interfere) and writes an
// executable pre-commit hook there with the given body.
func installHook(t *testing.T, r *testrepo.Repo, body string) {
	t.Helper()
	hooks := filepath.Join(t.TempDir(), "hooks")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	r.Git("config", "core.hooksPath", hooks)
	path := filepath.Join(hooks, "pre-commit")
	script := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// absoluteGitDir returns the repository's real git directory, the same way
// commit.go locates it for the temporary message file.
func absoluteGitDir(t *testing.T, r *testrepo.Repo) string {
	t.Helper()
	return r.Git("rev-parse", "--absolute-git-dir")
}

// leftoverMessageFiles reports any git-ui-commit-* temp file still sitting in
// the repository's git directory.
func leftoverMessageFiles(t *testing.T, r *testrepo.Repo) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(absoluteGitDir(t, r), "git-ui-commit-*"))
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

func TestCommitStagedChanges(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "changed\n")
	r.Git("add", "a.txt")

	if err := worktree.Commit(ctx, r.Dir, "change a", false); err != nil {
		t.Fatal(err)
	}
	if got := r.Git("log", "-1", "--pretty=%s"); got != "change a" {
		t.Errorf("subject = %q", got)
	}
	if st := status(t, r.Dir); len(st.Staged) != 0 {
		t.Errorf("staged = %v after committing", paths(st.Staged))
	}
}

// A real message has a subject, a blank line, a body, and often quotes —
// none of which may reach git through a command line.
func TestCommitKeepsAMultiLineMessageIntact(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "changed\n")
	r.Git("add", "a.txt")
	message := "fix: don't \"quote\" the shell\n\nIt broke on `backticks` and $VARS.\nSecond line.\n"

	if err := worktree.Commit(ctx, r.Dir, message, false); err != nil {
		t.Fatal(err)
	}
	got := r.Git("log", "-1", "--pretty=%B")
	for _, want := range []string{`don't "quote" the shell`, "`backticks` and $VARS", "Second line."} {
		if !strings.Contains(got, want) {
			t.Errorf("message = %q, missing %q", got, want)
		}
	}
}

func TestCommitRefusesWithNothingStaged(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "changed but not staged\n")
	if err := worktree.Commit(ctx, r.Dir, "nope", false); !errors.Is(err, worktree.ErrNothingStaged) {
		t.Errorf("err = %v, want ErrNothingStaged", err)
	}
}

func TestCommitRefusesAnEmptyMessage(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "changed\n")
	r.Git("add", "a.txt")
	if err := worktree.Commit(ctx, r.Dir, "   \n", false); err == nil {
		t.Error("want an error for a blank message")
	}
}

func TestAmendReplacesTheLastCommit(t *testing.T) {
	r := base(t)
	before := r.Git("rev-parse", "HEAD")
	r.WriteFile("a.txt", "amended\n")
	r.Git("add", "a.txt")

	if err := worktree.Commit(ctx, r.Dir, "add a, properly", true); err != nil {
		t.Fatal(err)
	}
	if got := r.Git("log", "-1", "--pretty=%s"); got != "add a, properly" {
		t.Errorf("subject = %q", got)
	}
	if got := r.Git("rev-list", "--count", "HEAD"); got != "1" {
		t.Errorf("commit count = %s, want the commit replaced, not added", got)
	}
	if r.Git("rev-parse", "HEAD") == before {
		t.Error("HEAD did not move")
	}
}

// Amending with nothing staged only rewrites the message, which is the most
// common use of it.
func TestAmendWithNothingStagedRewritesTheMessage(t *testing.T) {
	r := base(t)
	if err := worktree.Commit(ctx, r.Dir, "better subject", true); err != nil {
		t.Fatal(err)
	}
	if got := r.Git("log", "-1", "--pretty=%s"); got != "better subject" {
		t.Errorf("subject = %q", got)
	}
}

func TestPreviewCountsStagedFilesAndOffersTheLastMessage(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "changed\n")
	r.Git("add", "a.txt")
	r.WriteFile("b.txt", "new\n")
	r.Git("add", "b.txt")

	info, err := worktree.Preview(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.StagedCount != 2 || !info.CanAmend || info.LastMessage != "add a" {
		t.Errorf("info = %+v", info)
	}
	if info.Pushed || info.Upstream != "" {
		t.Errorf("info = %+v, want no upstream", info)
	}
}

func TestPreviewCannotAmendAnEmptyRepository(t *testing.T) {
	r := testrepo.New(t)
	info, err := worktree.Preview(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.CanAmend {
		t.Errorf("info = %+v, want CanAmend false with no commits", info)
	}
}

// Amending a commit the upstream already has rewrites shared history; the
// dialog needs to know.
func TestPreviewReportsAPushedCommit(t *testing.T) {
	src := testrepo.New(t)
	src.Commit("base")
	bare := testrepo.NewBareFrom(t, src)
	clone := testrepo.Clone(t, bare)

	info, err := worktree.Preview(ctx, clone.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Pushed || info.Upstream != "origin/main" {
		t.Errorf("info = %+v, want pushed on origin/main", info)
	}
}

// Preview must not warn about a force-push when the branch is simply ahead
// of its upstream: that is the case that decides NOT to warn.
func TestPreviewReportsAnUnpushedCommitWhenAheadOfUpstream(t *testing.T) {
	src := testrepo.New(t)
	src.Commit("base")
	bare := testrepo.NewBareFrom(t, src)
	clone := testrepo.Clone(t, bare)
	clone.WriteFile("a.txt", "changed\n")
	clone.Git("add", "a.txt")
	clone.Git("commit", "-q", "-m", "ahead of upstream")

	info, err := worktree.Preview(ctx, clone.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Pushed || info.Upstream != "origin/main" {
		t.Errorf("info = %+v, want unpushed on origin/main", info)
	}
}

// A pre-commit hook running lint or tests routinely takes longer than an
// ordinary git read; Commit must not time out on it.
func TestCommitSucceedsWithASlowPreCommitHook(t *testing.T) {
	r := base(t)
	installHook(t, r, "sleep 2")
	r.WriteFile("a.txt", "changed\n")
	r.Git("add", "a.txt")

	if err := worktree.Commit(ctx, r.Dir, "change a", false); err != nil {
		t.Fatal(err)
	}
	if got := r.Git("log", "-1", "--pretty=%s"); got != "change a" {
		t.Errorf("subject = %q", got)
	}
}

// Amending in a repository with no commits must be refused before any
// temporary file is written, with a typed error rather than git's own
// message, which names that temporary file's path.
func TestAmendRefusesInAnEmptyRepository(t *testing.T) {
	r := testrepo.New(t)
	err := worktree.Commit(ctx, r.Dir, "nothing to amend yet", true)
	if !errors.Is(err, worktree.ErrNothingToAmend) {
		t.Errorf("err = %v, want ErrNothingToAmend", err)
	}
	if err != nil && strings.Contains(err.Error(), "git-ui-commit") {
		t.Errorf("err = %q, must not name the temporary message file", err)
	}
	if matches := leftoverMessageFiles(t, r); len(matches) != 0 {
		t.Errorf("leftover message files = %v", matches)
	}
}

// The temporary message file must not survive a successful commit, and must
// not appear as an untracked path in the meantime.
func TestCommitCleansUpTheMessageFileOnSuccess(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "changed\n")
	r.Git("add", "a.txt")

	if err := worktree.Commit(ctx, r.Dir, "change a", false); err != nil {
		t.Fatal(err)
	}
	if matches := leftoverMessageFiles(t, r); len(matches) != 0 {
		t.Errorf("leftover message files = %v", matches)
	}
	if out := r.Git("status", "--porcelain"); strings.Contains(out, "git-ui-commit") {
		t.Errorf("git status = %q, must not list the message file", out)
	}
}

// The temporary message file must also not survive a commit a hook rejects.
func TestCommitCleansUpTheMessageFileOnHookFailure(t *testing.T) {
	r := base(t)
	installHook(t, r, "exit 1")
	r.WriteFile("a.txt", "changed\n")
	r.Git("add", "a.txt")

	if err := worktree.Commit(ctx, r.Dir, "change a", false); err == nil {
		t.Fatal("want an error when the hook rejects the commit")
	}
	if matches := leftoverMessageFiles(t, r); len(matches) != 0 {
		t.Errorf("leftover message files = %v", matches)
	}
	if out := r.Git("status", "--porcelain"); strings.Contains(out, "git-ui-commit") {
		t.Errorf("git status = %q, must not list the message file", out)
	}
}
