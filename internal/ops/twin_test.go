package ops_test

import (
	"errors"
	"testing"

	"git-ui/internal/ops"
)

func TestPullGivesABranchWithoutUpstreamItsRemoteBranchFirst(t *testing.T) {
	a, b := clones(t)
	b.Git("switch", "-q", "-c", "feature", "origin/feature")
	h := b.Commit("remote feature work")
	b.Git("push", "-q", "origin", "feature")
	a.Git("switch", "-q", "--no-track", "-c", "feature", "origin/feature") // before b's push: behind once fetched

	result, err := ops.Pull(ctx, a.Dir, ops.StrategyAuto)
	if err != nil {
		t.Fatal(err)
	}
	if result.Tracked != "origin/feature" || result.Outcome != ops.Merged {
		t.Errorf("result = %+v, want Merged and tracking origin/feature", result)
	}
	if got := a.Git("rev-parse", "--abbrev-ref", "feature@{upstream}"); got != "origin/feature" {
		t.Errorf("upstream = %q, want origin/feature", got)
	}
	if got := a.Git("rev-parse", "HEAD"); got != h {
		t.Errorf("HEAD = %s, want %s", got, h)
	}
}

func TestPullWithoutUpstreamOrRemoteBranchStillFails(t *testing.T) {
	a, _ := clones(t)
	a.Git("switch", "-q", "-c", "lonely")
	if _, err := ops.Pull(ctx, a.Dir, ops.StrategyAuto); err == nil {
		t.Fatal("Pull succeeded on a branch with no upstream and no remote branch")
	}
}

func TestPullLeavesAnExistingUpstreamAlone(t *testing.T) {
	a, _ := clones(t)
	result, err := ops.Pull(ctx, a.Dir, ops.StrategyAuto)
	if err != nil {
		t.Fatal(err)
	}
	if result.Tracked != "" {
		t.Errorf("Tracked = %q, want empty for a branch that already tracks", result.Tracked)
	}
}

func TestFastForwardBranchGivesABranchWithoutUpstreamItsRemoteBranchFirst(t *testing.T) {
	a, b := clones(t)
	b.Git("switch", "-q", "-c", "feature", "origin/feature")
	h := b.Commit("remote feature work")
	b.Git("push", "-q", "origin", "feature")
	a.Git("branch", "--no-track", "feature", "origin/feature")

	moved, err := ops.FastForwardBranch(ctx, a.Dir, "feature")
	if err != nil || !moved {
		t.Fatalf("moved = %v, err = %v; want moved", moved, err)
	}
	if got := a.Git("rev-parse", "--abbrev-ref", "feature@{upstream}"); got != "origin/feature" {
		t.Errorf("upstream = %q, want origin/feature", got)
	}
	if got := a.Git("rev-parse", "feature"); got != h {
		t.Errorf("feature = %s, want %s", got, h)
	}
}

func TestFastForwardBranchWithoutUpstreamOrRemoteBranchStillRefuses(t *testing.T) {
	a, _ := clones(t)
	a.Git("branch", "loose")
	if _, err := ops.FastForwardBranch(ctx, a.Dir, "loose"); !errors.Is(err, ops.ErrNoUpstream) {
		t.Errorf("err = %v, want ErrNoUpstream", err)
	}
}
