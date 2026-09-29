package gitflow

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"git-ui/internal/testrepo"
)

func started(t *testing.T, r *testrepo.Repo, typ, name, base string) string {
	t.Helper()
	res, err := Start(ctx, r.Dir, typ, name, base)
	if err != nil {
		t.Fatal(err)
	}
	return res.Branch
}

func mustFinish(t *testing.T, r *testrepo.Repo, branch string, releases []string) FinishResult {
	t.Helper()
	res, err := Finish(ctx, r.Dir, branch, releases)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func contains(t *testing.T, r *testrepo.Repo, a, b string) bool {
	t.Helper()
	ok, err := isAncestor(ctx, r.Dir, a, b)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func TestFinishFeature(t *testing.T) {
	r := newFlowRepo(t)
	b := started(t, r, Feature, "f1", "")
	r.Commit("work")
	tip := r.Git("rev-parse", "HEAD")
	res := mustFinish(t, r, b, nil)
	if res.Outcome != "finished" || !slices.Equal(res.Merged, []string{"develop"}) {
		t.Fatalf("res = %+v", res)
	}
	if currentBranch(ctx, r.Dir) != "develop" || branchExists(ctx, r.Dir, b) {
		t.Fatal("not on develop, or branch not deleted")
	}
	if !contains(t, r, tip, "develop") || contains(t, r, tip, "master") {
		t.Fatal("wrong targets")
	}
	if got := r.Git("log", "-1", "--format=%s", "develop"); got != "Merge branch 'feature/f1' into develop" {
		t.Fatalf("message = %q", got)
	}
	if out := r.GitFails("config", "gitflow.branch.feature/f1.base"); strings.TrimSpace(out) != "" {
		t.Fatalf("base key left: %q", out)
	}
}

func TestFinishReleaseIntoMasterThenDevelop(t *testing.T) {
	r := newFlowRepo(t)
	b := started(t, r, Release, "r1", "")
	r.Commit("bump")
	tip := r.Git("rev-parse", "HEAD")
	res := mustFinish(t, r, b, nil)
	if !slices.Equal(res.Merged, []string{"master", "develop"}) {
		t.Fatalf("merged = %v", res.Merged)
	}
	if !contains(t, r, tip, "master") || !contains(t, r, tip, "develop") || currentBranch(ctx, r.Dir) != "develop" {
		t.Fatal("release not in both")
	}
}

func TestFinishHotfixWithAndWithoutRelease(t *testing.T) {
	r := newFlowRepo(t)
	r.Git("branch", "release/r1")
	r.Git("branch", "release/r2")
	h1 := started(t, r, Hotfix, "h1", "")
	r.Commit("fix 1")
	tip1 := r.Git("rev-parse", "HEAD")
	if res := mustFinish(t, r, h1, nil); !slices.Equal(res.Merged, []string{"master", "develop"}) {
		t.Fatalf("merged = %v", res.Merged)
	}
	if contains(t, r, tip1, "release/r1") {
		t.Fatal("unticked release got the hotfix")
	}
	h2 := started(t, r, Hotfix, "h2", "")
	r.Commit("fix 2")
	if res := mustFinish(t, r, h2, []string{"release/r2"}); !slices.Equal(res.Merged, []string{"master", "release/r2", "develop"}) {
		t.Fatalf("merged = %v", res.Merged)
	}
}

func TestFinishWarmfixIntoItsRelease(t *testing.T) {
	r := newFlowRepo(t)
	r.Git("branch", "release/r1")
	w := started(t, r, Warmfix, "w1", "release/r1")
	r.Commit("warm")
	tip := r.Git("rev-parse", "HEAD")
	res := mustFinish(t, r, w, nil)
	if !slices.Equal(res.Merged, []string{"release/r1"}) || currentBranch(ctx, r.Dir) != "release/r1" {
		t.Fatalf("res = %+v on %s", res, currentBranch(ctx, r.Dir))
	}
	if contains(t, r, tip, "develop") {
		t.Fatal("warmfix leaked into develop")
	}
}

func TestFinishWarmfixCapitalPrefix(t *testing.T) {
	r := newFlowRepo(t)
	r.Git("branch", "release/r1")
	r.Git("switch", "-q", "-c", "Warmfix/NEXO-39", "release/r1")
	r.Git("config", "gitflow.branch.Warmfix/NEXO-39.base", "release/r1")
	r.Commit("warm")
	if res := mustFinish(t, r, "Warmfix/NEXO-39", nil); !slices.Equal(res.Merged, []string{"release/r1"}) {
		t.Fatalf("merged = %v", res.Merged)
	}
}

func TestFinishWarmfixWithoutBaseNeedsARelease(t *testing.T) {
	r := newFlowRepo(t)
	r.Git("switch", "-q", "-c", "warmfix/w", "develop")
	r.Commit("warm")
	if _, err := Finish(ctx, r.Dir, "warmfix/w", nil); !errors.Is(err, ErrNoRelease) {
		t.Fatalf("err = %v", err)
	}
}

// conflictingHotfix leaves a hotfix that merges cleanly into master but
// conflicts with develop on shared.txt.
func conflictingHotfix(t *testing.T) (*testrepo.Repo, string) {
	r := newFlowRepo(t)
	r.Git("switch", "-q", "master")
	writeCommit(r, "shared.txt", "base\n", "shared")
	r.Git("switch", "-q", "develop")
	r.Git("merge", "-q", "--no-edit", "master")
	writeCommit(r, "shared.txt", "develop\n", "develop edit")
	h := started(t, r, Hotfix, "h", "")
	writeCommit(r, "shared.txt", "hotfix\n", "hotfix edit")
	return r, h
}

func TestFinishConflictThenFinishAgain(t *testing.T) {
	r, h := conflictingHotfix(t)
	res := mustFinish(t, r, h, nil)
	if res.Outcome != "conflicted" || res.Target != "develop" || !slices.Equal(res.Merged, []string{"master"}) || !slices.Equal(res.Conflicts, []string{"shared.txt"}) {
		t.Fatalf("res = %+v", res)
	}
	if currentBranch(ctx, r.Dir) != "develop" {
		t.Fatal("not left on the conflicted target")
	}
	f, _ := Read(ctx, r.Dir)
	if f.Branches[0].Name != h || !f.Branches[0].InProgress {
		t.Fatalf("branches = %+v", f.Branches)
	}
	if _, err := Finish(ctx, r.Dir, h, nil); !errors.Is(err, ErrInProgress) {
		t.Fatalf("finish during conflict: err = %v", err)
	}
	r.WriteFile("shared.txt", "resolved\n")
	r.Git("add", "shared.txt")
	r.Git("commit", "-q", "--no-edit")

	res = mustFinish(t, r, h, nil)
	if res.Outcome != "finished" || len(res.Merged) != 0 || branchExists(ctx, r.Dir, h) {
		t.Fatalf("res = %+v", res)
	}
	if n := r.Git("rev-list", "--count", "--merges", "master"); n != "1" {
		t.Fatalf("master merged twice: %s merges", n)
	}
}

func TestPlanMarksDoneTargets(t *testing.T) {
	r, h := conflictingHotfix(t)
	mustFinish(t, r, h, nil) // master merged, develop conflicted
	r.Git("merge", "--abort")
	r.Git("branch", "release/r1")
	p, err := PlanFinish(ctx, r.Dir, h, []string{"release/r1"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Step{{"master", true}, {"release/r1", false}, {"develop", false}}
	if !slices.Equal(p.Steps, want) || p.Ending != "develop" || p.Type != Hotfix {
		t.Fatalf("plan = %+v", p)
	}
}

func TestFinishRefusesTrackedChanges(t *testing.T) {
	r := newFlowRepo(t)
	b := started(t, r, Feature, "f", "")
	writeCommit(r, "a.txt", "one\n", "a")
	r.WriteFile("a.txt", "two\n")
	if _, err := Finish(ctx, r.Dir, b, nil); !errors.Is(err, ErrDirty) {
		t.Fatalf("err = %v", err)
	}
}

func TestFinishIgnoresUntrackedFiles(t *testing.T) {
	r := newFlowRepo(t)
	b := started(t, r, Feature, "f", "")
	r.Commit("work")
	r.WriteFile("scratch.txt", "untracked\n")
	if res := mustFinish(t, r, b, nil); res.Outcome != "finished" {
		t.Fatalf("res = %+v", res)
	}
}

func TestFinishDivergedTargetChangesNothing(t *testing.T) {
	r := newFlowRepo(t)
	bare := withRemote(t, r)
	other := testrepo.Clone(t, bare)
	other.Git("switch", "-q", "develop")
	other.Commit("upstream develop")
	other.Git("push", "-q", "origin", "develop")
	other.Git("switch", "-q", "master")
	other.Commit("upstream master")
	other.Git("push", "-q", "origin", "master")

	b := started(t, r, Hotfix, "h", "") // master fast-forwarded here
	r.Commit("fix")
	r.Git("switch", "-q", "develop")
	r.Commit("local develop") // develop now diverged from origin/develop
	r.Git("switch", "-q", b)
	masterBefore := r.Git("rev-parse", "master")

	_, err := Finish(ctx, r.Dir, b, nil)
	var div *DivergedError
	if !errors.As(err, &div) || div.Branch != "develop" {
		t.Fatalf("err = %v", err)
	}
	if r.Git("rev-parse", "master") != masterBefore || currentBranch(ctx, r.Dir) != b {
		t.Fatal("finish changed something before stopping")
	}
}

func TestFinishUnknownBranch(t *testing.T) {
	r := newFlowRepo(t)
	r.Git("branch", "topic")
	if _, err := Finish(ctx, r.Dir, "topic", nil); !errors.Is(err, ErrNotFlowBranch) {
		t.Fatalf("err = %v", err)
	}
}
