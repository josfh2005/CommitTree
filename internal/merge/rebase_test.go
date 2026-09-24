package merge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git-ui/internal/testrepo"
)

// diverged: main and feature each have one commit of their own after base,
// touching different files, and feature is checked out.
func diverged(t *testing.T) *testrepo.Repo {
	t.Helper()
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("switch", "-q", "-c", "feature")
	r.Commit("on feature")
	r.Git("switch", "-q", "main")
	r.Commit("on main")
	r.Git("switch", "-q", "feature")
	return r
}

func TestRebaseReplaysCommits(t *testing.T) {
	r := diverged(t)
	got, err := Rebase(context.Background(), r.Dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != Rebased {
		t.Fatalf("outcome = %v, want Rebased", got.Outcome)
	}
	r.Git("merge-base", "--is-ancestor", "main", "HEAD")
	if n := r.Git("rev-list", "--count", "main..HEAD"); n != "1" {
		t.Errorf("commits on top of main = %s, want 1", n)
	}
}

func TestRebaseUpToDate(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("switch", "-q", "-c", "feature")
	r.Commit("on feature")
	got, err := Rebase(context.Background(), r.Dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != UpToDate {
		t.Fatalf("outcome = %v, want UpToDate", got.Outcome)
	}
}

func TestRebaseConflicted(t *testing.T) {
	r := conflicting(t)
	r.Git("switch", "-q", "feature")
	got, err := Rebase(context.Background(), r.Dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != Conflicted || strings.Join(got.Conflicts, ",") != "greeting.txt" {
		t.Fatalf("result = %+v, want Conflicted on greeting.txt", got)
	}
	if st := status(t, r.Dir); st.Kind != KindRebase {
		t.Fatalf("kind = %q, want rebase", st.Kind)
	}
	data, _ := os.ReadFile(filepath.Join(r.Dir, "greeting.txt"))
	if !strings.Contains(string(data), "|||||||") {
		t.Errorf("greeting.txt = %q, want zdiff3 markers with the ancestor", data)
	}
}

func TestRebaseOntoARemoteTrackingBranch(t *testing.T) {
	r := diverged(t)
	r.Git("update-ref", "refs/remotes/origin/main", r.Git("rev-parse", "main"))
	got, err := Rebase(context.Background(), r.Dir, "origin/main")
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != Rebased {
		t.Fatalf("outcome = %v, want Rebased", got.Outcome)
	}
}

func TestRebaseRefusesTrackedChangesEvenWithAutoStash(t *testing.T) {
	r := diverged(t)
	r.Git("config", "rebase.autoStash", "true")
	r.WriteFile("file-1.txt", "edited\n") // tracked since "base"
	before := r.Git("rev-parse", "HEAD")
	if _, err := Rebase(context.Background(), r.Dir, "main"); !errors.Is(err, ErrDirtyWorktree) {
		t.Fatalf("err = %v, want ErrDirtyWorktree", err)
	}
	if r.Git("rev-parse", "HEAD") != before {
		t.Error("HEAD moved on a refused rebase")
	}
}

func TestRebaseAllowsUntrackedFiles(t *testing.T) {
	r := diverged(t)
	r.WriteFile("scratch.txt", "not tracked\n")
	if _, err := Rebase(context.Background(), r.Dir, "main"); err != nil {
		t.Fatal(err)
	}
}

func TestRebaseStoppedByAnUntrackedFileLeavesNothingBehind(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("switch", "-q", "-c", "feature")
	r.WriteFile("clash.txt", "feature\n")
	r.Git("add", "clash.txt")
	r.Git("commit", "-q", "-m", "add clash")
	r.Git("switch", "-q", "main")
	r.Commit("on main")
	r.Git("switch", "-q", "feature")
	r.Git("rm", "-q", "--cached", "clash.txt")
	r.Git("commit", "-q", "-m", "untrack clash") // clash.txt stays on disk, untracked
	before := r.Git("rev-parse", "HEAD")
	_, err := Rebase(context.Background(), r.Dir, "main")
	if err == nil {
		t.Fatal("want git's refusal to overwrite the untracked file")
	}
	if st := status(t, r.Dir); st.Merging {
		t.Fatalf("state = %+v, want no rebase left behind", st)
	}
	if r.Git("rev-parse", "HEAD") != before {
		t.Error("HEAD moved")
	}
}

func TestRebaseRefusesWhileAnotherOperationIsInProgress(t *testing.T) {
	r := conflicting(t)
	r.GitFails("cherry-pick", "feature")
	_, err := Rebase(context.Background(), r.Dir, "feature")
	if !errors.Is(err, ErrOperationInProgress) {
		t.Fatalf("err = %v, want ErrOperationInProgress", err)
	}
	if err.Error() != "Finish the cherry-pick in progress first" {
		t.Errorf("err.Error() = %q, want exactly %q", err.Error(), "Finish the cherry-pick in progress first")
	}
}

// TestSentinelErrorsCarryExactUserFacingCopy pins the literal text the user
// sees for each preflight failure: no wrapping, no appended sentinel detail.
func TestSentinelErrorsCarryExactUserFacingCopy(t *testing.T) {
	if got, want := ErrDirtyWorktree.Error(), "Commit or stash your changes first"; got != want {
		t.Errorf("ErrDirtyWorktree.Error() = %q, want %q", got, want)
	}
	if got, want := ErrDetachedHead.Error(), "No branch is checked out"; got != want {
		t.Errorf("ErrDetachedHead.Error() = %q, want %q", got, want)
	}
	inProgress := &operationInProgressError{kind: KindRebase}
	if got, want := inProgress.Error(), "Finish the rebase in progress first"; got != want {
		t.Errorf("operationInProgressError.Error() = %q, want %q", got, want)
	}
	if !errors.Is(inProgress, ErrOperationInProgress) {
		t.Error("operationInProgressError does not satisfy errors.Is(_, ErrOperationInProgress)")
	}
}

func TestRebaseRefusesADetachedHead(t *testing.T) {
	r := diverged(t)
	r.Git("switch", "-q", "--detach")
	if _, err := Rebase(context.Background(), r.Dir, "main"); !errors.Is(err, ErrDetachedHead) {
		t.Fatalf("err = %v, want ErrDetachedHead", err)
	}
}

func TestRebaseUnknownRefLeavesNothing(t *testing.T) {
	r := diverged(t)
	if _, err := Rebase(context.Background(), r.Dir, "nope"); err == nil {
		t.Fatal("want an error")
	}
	if _, err := Rebase(context.Background(), r.Dir, "--root"); !errors.Is(err, ErrInvalidRef) {
		t.Fatalf("err = %v, want ErrInvalidRef", err)
	}
	if st := status(t, r.Dir); st.Merging {
		t.Fatalf("state = %+v", st)
	}
}

func TestRebasePreview(t *testing.T) {
	r := diverged(t) // feature: 1 commit ahead of main
	r.Commit("second on feature")
	r.Git("branch", "pub", "HEAD~1") // "published": the first feature commit
	r.Git("config", "branch.feature.remote", ".")
	r.Git("config", "branch.feature.merge", "refs/heads/pub")
	r.Git("switch", "-q", "-c", "side", "HEAD~1")
	r.Commit("on side")
	r.Git("switch", "-q", "feature")
	r.Git("merge", "-q", "--no-ff", "--no-edit", "side")

	p, err := RebasePreview(context.Background(), r.Dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	// Replayed: "on feature", "second on feature", "on side" (merges are flattened, not replayed).
	if p.Commits != 3 || p.Merges != 1 || p.Published != 1 || p.Upstream != "pub" {
		t.Fatalf("preview = %+v, want 3 commits, 1 merge, 1 published on pub", p)
	}
}

func TestRebasePreviewWithoutUpstream(t *testing.T) {
	r := diverged(t)
	p, err := RebasePreview(context.Background(), r.Dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if p.Commits != 1 || p.Published != 0 || p.Upstream != "" {
		t.Fatalf("preview = %+v", p)
	}
}

func TestIsAncestorOfHead(t *testing.T) {
	r := diverged(t)
	base := r.Git("rev-parse", "HEAD~1")
	for rev, want := range map[string]bool{base: true, "main": false, "HEAD": true} {
		got, err := IsAncestorOfHead(context.Background(), r.Dir, rev)
		if err != nil || got != want {
			t.Errorf("IsAncestorOfHead(%s) = %v, %v; want %v", rev, got, err, want)
		}
	}
	if _, err := IsAncestorOfHead(context.Background(), r.Dir, "nope"); err == nil {
		t.Error("want an error for an unknown rev")
	}
}
