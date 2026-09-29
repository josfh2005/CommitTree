package gitflow

import (
	"errors"
	"strings"
	"testing"

	"git-ui/internal/testrepo"
)

func TestStartFeatureFromDevelop(t *testing.T) {
	r := newFlowRepo(t)
	r.Git("switch", "-q", "master")
	res, err := Start(ctx, r.Dir, Feature, "NEXO-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Branch != "feature/NEXO-1" || currentBranch(ctx, r.Dir) != "feature/NEXO-1" {
		t.Fatalf("res = %+v, on %s", res, currentBranch(ctx, r.Dir))
	}
	if r.Git("rev-parse", "HEAD") != r.Git("rev-parse", "develop") {
		t.Fatal("not started from develop")
	}
	if got := r.Git("config", "gitflow.branch.feature/NEXO-1.base"); got != "develop" {
		t.Fatalf("base = %q", got)
	}
}

func TestStartHotfixFromMasterAndReleaseFromDevelop(t *testing.T) {
	r := newFlowRepo(t)
	r.Commit("develop only")
	if _, err := Start(ctx, r.Dir, Hotfix, "h1", ""); err != nil {
		t.Fatal(err)
	}
	if r.Git("rev-parse", "HEAD") != r.Git("rev-parse", "master") {
		t.Fatal("hotfix not from master")
	}
	if _, err := Start(ctx, r.Dir, Release, "r1", ""); err != nil {
		t.Fatal(err)
	}
	if r.Git("rev-parse", "HEAD") != r.Git("rev-parse", "develop") {
		t.Fatal("release not from develop")
	}
}

func TestStartWarmfixFromChosenRelease(t *testing.T) {
	r := newFlowRepo(t)
	r.Git("branch", "release/r1")
	if _, err := Start(ctx, r.Dir, Warmfix, "w1", "develop"); !errors.Is(err, ErrNoRelease) {
		t.Fatalf("base develop: err = %v", err)
	}
	res, err := Start(ctx, r.Dir, Warmfix, "w1", "release/r1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Branch != "warmfix/w1" || r.Git("config", "gitflow.branch.warmfix/w1.base") != "release/r1" {
		t.Fatalf("res = %+v", res)
	}
}

func TestStartRejectsBadAndExistingNames(t *testing.T) {
	r := newFlowRepo(t)
	for _, name := range []string{"", "a..b", "bad name"} {
		if _, err := Start(ctx, r.Dir, Feature, name, ""); !errors.Is(err, ErrInvalidName) {
			t.Fatalf("%q: err = %v", name, err)
		}
	}
	r.Git("branch", "feature/x")
	if _, err := Start(ctx, r.Dir, Feature, "x", ""); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("existing: err = %v", err)
	}
}

func TestStartNotInitialized(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("init")
	if _, err := Start(ctx, r.Dir, Feature, "x", ""); !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("err = %v", err)
	}
}

func TestStartWithoutRemoteHasNoNotes(t *testing.T) {
	r := newFlowRepo(t)
	res, err := Start(ctx, r.Dir, Feature, "x", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Notes) != 0 {
		t.Fatalf("notes = %v", res.Notes)
	}
}

func TestStartFastForwardsBehindBase(t *testing.T) {
	r := newFlowRepo(t)
	bare := withRemote(t, r)
	other := testrepo.Clone(t, bare)
	other.Git("switch", "-q", "develop")
	other.Commit("upstream work")
	other.Git("push", "-q", "origin", "develop")
	upstream := other.Git("rev-parse", "HEAD")

	r.Git("switch", "-q", "master") // develop is not checked out: fetch . src:dst path
	res, err := Start(ctx, r.Dir, Feature, "x", "")
	if err != nil {
		t.Fatal(err)
	}
	if r.Git("rev-parse", "HEAD") != upstream || r.Git("rev-parse", "develop") != upstream {
		t.Fatal("develop not fast-forwarded before branching")
	}
	if len(res.Notes) != 1 || res.Notes[0] != "develop fast-forwarded to origin/develop" {
		t.Fatalf("notes = %v", res.Notes)
	}
}

func TestStartFastForwardsCheckedOutBase(t *testing.T) {
	r := newFlowRepo(t)
	bare := withRemote(t, r)
	other := testrepo.Clone(t, bare)
	other.Git("switch", "-q", "develop")
	other.Commit("upstream work")
	other.Git("push", "-q", "origin", "develop")
	// r is on develop: merge --ff-only path
	if _, err := Start(ctx, r.Dir, Feature, "x", ""); err != nil {
		t.Fatal(err)
	}
	if r.Git("rev-parse", "develop") != other.Git("rev-parse", "HEAD") {
		t.Fatal("develop not fast-forwarded")
	}
}

func TestStartStopsOnDivergedBase(t *testing.T) {
	r := newFlowRepo(t)
	bare := withRemote(t, r)
	other := testrepo.Clone(t, bare)
	other.Git("switch", "-q", "develop")
	other.Commit("upstream work")
	other.Git("push", "-q", "origin", "develop")
	r.Commit("local work")
	_, err := Start(ctx, r.Dir, Feature, "x", "")
	var div *DivergedError
	if !errors.As(err, &div) || err.Error() != "develop has diverged from origin/develop; pull it first" {
		t.Fatalf("err = %v", err)
	}
	if branchExists(ctx, r.Dir, "feature/x") {
		t.Fatal("branch created despite divergence")
	}
}

func TestStartFetchFailureIsANote(t *testing.T) {
	r := newFlowRepo(t)
	r.Git("remote", "add", "origin", "/nonexistent/repo.git")
	res, err := Start(ctx, r.Dir, Feature, "x", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Notes) != 1 || !strings.HasPrefix(res.Notes[0], "Fetch failed, used local branches: ") {
		t.Fatalf("notes = %v", res.Notes)
	}
}
