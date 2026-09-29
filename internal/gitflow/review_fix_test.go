package gitflow

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"git-ui/internal/testrepo"
)

// A diverged target stops the finish before any other target is moved,
// even one that could have been fast-forwarded.
func TestFinishDivergedTargetLeavesOthersUntouched(t *testing.T) {
	r := newFlowRepo(t)
	b := started(t, r, Release, "r1", "")
	r.Commit("bump")
	bare := withRemote(t, r)
	other := testrepo.Clone(t, bare)
	other.Git("switch", "-q", "master")
	other.Commit("upstream master")
	other.Git("push", "-q", "origin", "master")
	other.Git("switch", "-q", "develop")
	other.Commit("upstream develop")
	other.Git("push", "-q", "origin", "develop")
	r.Git("switch", "-q", "develop")
	r.Commit("local develop")
	r.Git("switch", "-q", b)
	masterBefore := r.Git("rev-parse", "master")

	_, err := Finish(ctx, r.Dir, b, nil)
	var div *DivergedError
	if !errors.As(err, &div) || div.Branch != "develop" {
		t.Fatalf("err = %v", err)
	}
	if got := r.Git("rev-parse", "master"); got != masterBefore {
		t.Fatalf("master moved from %s to %s", masterBefore, got)
	}
}

// SourceTree spells some prefixes with a capital letter; the base key and
// the checked-out branch must still be found when their case differs.
func TestWarmfixBaseKeyCaseMismatch(t *testing.T) {
	r := newFlowRepo(t)
	r.Git("branch", "release/1.0")
	r.Git("branch", "release/2.0")
	r.Git("switch", "-q", "-c", "Warmfix/NEW", "release/2.0")
	r.Commit("warm")
	r.Git("config", "gitflow.branch.warmfix/NEW.base", "release/2.0")

	f, err := Read(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if f.Current == nil || f.Current.Base != "release/2.0" {
		t.Fatalf("current = %+v", f.Current)
	}
	res := mustFinish(t, r, "Warmfix/NEW", nil)
	if !slices.Equal(res.Merged, []string{"release/2.0"}) {
		t.Fatalf("merged = %v", res.Merged)
	}
	if out := strings.TrimSpace(r.GitFails("config", "--get-regexp", `^gitflow\.branch\..*\.base$`)); out != "" {
		t.Fatalf("base key left: %q", out)
	}
}

// On a case-insensitive filesystem HEAD may name the branch in another case
// than for-each-ref lists it; it is still the current flow branch.
func TestReadCurrentIgnoresCase(t *testing.T) {
	r := newFlowRepo(t)
	r.Git("switch", "-q", "-c", "Warmfix/X", "develop")
	r.Git("symbolic-ref", "HEAD", "refs/heads/warmfix/X")
	if strings.Contains(r.GitFails("rev-parse", "--verify", "HEAD"), "fatal") {
		t.Skip("case-sensitive filesystem")
	}
	f, err := Read(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if f.Current == nil || f.Current.Name != "Warmfix/X" {
		t.Fatalf("current = %+v", f.Current)
	}
}

func TestFinishRefusesBranchCheckedOutElsewhere(t *testing.T) {
	r := newFlowRepo(t)
	b := started(t, r, Feature, "f", "")
	r.Commit("work")
	r.Git("switch", "-q", "develop")
	developBefore := r.Git("rev-parse", "develop")
	r.Git("worktree", "add", "-q", filepath.Join(t.TempDir(), "wt"), b)

	_, err := Finish(ctx, r.Dir, b, nil)
	if err == nil || !strings.Contains(err.Error(), "checked out in another worktree") {
		t.Fatalf("err = %v", err)
	}
	if r.Git("rev-parse", "develop") != developBefore {
		t.Fatal("develop merged despite the refusal")
	}
}

func TestFinishRefusesTargetCheckedOutElsewhere(t *testing.T) {
	r := newFlowRepo(t)
	b := started(t, r, Release, "r", "")
	r.Commit("bump")
	masterBefore := r.Git("rev-parse", "master")
	r.Git("worktree", "add", "-q", filepath.Join(t.TempDir(), "wt"), "develop")

	_, err := Finish(ctx, r.Dir, b, nil)
	if err == nil || !strings.Contains(err.Error(), "develop is checked out in another worktree") {
		t.Fatalf("err = %v", err)
	}
	if r.Git("rev-parse", "master") != masterBefore {
		t.Fatal("master merged despite the refusal")
	}
}
