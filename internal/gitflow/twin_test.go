package gitflow

import (
	"slices"
	"strings"
	"testing"

	"git-ui/internal/gitcmd"
	"git-ui/internal/testrepo"
)

// remoteFlow is a clone of a remote that has master and develop, develop one
// commit ahead of master; only master is local in the clone, as after a
// plain git clone.
func remoteFlow(t *testing.T) *testrepo.Repo {
	t.Helper()
	src := testrepo.New(t)
	src.Commit("init")
	src.Git("branch", "-m", "master")
	src.Git("switch", "-q", "-c", "develop")
	src.Commit("develop work")
	src.Git("switch", "-q", "master")
	return testrepo.Clone(t, testrepo.NewBareFrom(t, src))
}

func trackedBy(r *testrepo.Repo, branch string) string {
	out, err := gitcmd.Run(ctx, r.Dir, gitcmd.ReadTimeout, "rev-parse", "--abbrev-ref", branch+"@{upstream}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

var flowCfg = Config{Master: "master", Develop: "develop", Prefixes: defaultPrefixes}

func TestInitCreatesDevelopFromItsRemoteBranchAndTracksIt(t *testing.T) {
	r := remoteFlow(t)
	notes, err := Init(ctx, r.Dir, flowCfg)
	if err != nil {
		t.Fatal(err)
	}
	if r.Git("rev-parse", "develop") != r.Git("rev-parse", "origin/develop") {
		t.Error("develop not created from origin/develop")
	}
	if up := trackedBy(r, "develop"); up != "origin/develop" {
		t.Errorf("develop upstream = %q, want origin/develop", up)
	}
	if !slices.Equal(notes, []string{"develop created from origin/develop, tracking it"}) {
		t.Errorf("notes = %q", notes)
	}
}

func TestInitCreatesAMissingProductionBranchFromItsRemoteBranch(t *testing.T) {
	r := remoteFlow(t)
	r.Git("switch", "-q", "-c", "elsewhere")
	r.Git("branch", "-D", "master")

	notes, err := Init(ctx, r.Dir, flowCfg)
	if err != nil {
		t.Fatal(err)
	}
	if up := trackedBy(r, "master"); up != "origin/master" {
		t.Errorf("master upstream = %q, want origin/master", up)
	}
	if !slices.Contains(notes, "master created from origin/master, tracking it") {
		t.Errorf("notes = %q", notes)
	}
}

func TestInitGivesExistingBranchesWithoutUpstreamTheirRemoteBranch(t *testing.T) {
	r := remoteFlow(t)
	r.Git("branch", "--unset-upstream", "master")
	r.Git("branch", "--no-track", "develop", "origin/develop~1") // behind origin/develop

	notes, err := Init(ctx, r.Dir, flowCfg)
	if err != nil {
		t.Fatal(err)
	}
	if trackedBy(r, "master") != "origin/master" || trackedBy(r, "develop") != "origin/develop" {
		t.Errorf("upstreams = %q, %q", trackedBy(r, "master"), trackedBy(r, "develop"))
	}
	want := []string{"master now tracks origin/master", "develop now tracks origin/develop"}
	if !slices.Equal(notes, want) {
		t.Errorf("notes = %q, want %q", notes, want)
	}
}

func TestInitLeavesADivergedDevelopWithoutUpstreamAndSaysSo(t *testing.T) {
	r := remoteFlow(t)
	r.Git("switch", "-q", "-c", "develop", "--no-track", "master")
	r.Commit("local develop work") // develop and origin/develop both have their own commit
	r.Git("switch", "-q", "master")

	notes, err := Init(ctx, r.Dir, flowCfg)
	if err != nil {
		t.Fatal(err)
	}
	if up := trackedBy(r, "develop"); up != "" {
		t.Errorf("develop upstream = %q, want none", up)
	}
	want := []string{"origin/develop exists but develop has diverged from it, so develop has no upstream"}
	if !slices.Equal(notes, want) {
		t.Errorf("notes = %q, want %q", notes, want)
	}
}

func TestInitWithoutRemoteStillCreatesDevelopFromMaster(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("init")
	r.Git("branch", "-m", "master")
	notes, err := Init(ctx, r.Dir, flowCfg)
	if err != nil {
		t.Fatal(err)
	}
	if r.Git("rev-parse", "develop") != r.Git("rev-parse", "master") || len(notes) != 0 {
		t.Errorf("develop not from master, or notes %q", notes)
	}
}

func TestStartContinuesABranchThatIsAlreadyOnTheRemote(t *testing.T) {
	r := newFlowRepo(t)
	bare := withRemote(t, r)
	other := testrepo.Clone(t, bare)
	other.Git("switch", "-q", "-c", "feature/shared", "origin/develop")
	shared := other.Commit("someone else's feature work")
	other.Git("push", "-q", "origin", "feature/shared")

	res, err := Start(ctx, r.Dir, Feature, "shared", "")
	if err != nil {
		t.Fatal(err)
	}
	if r.Git("rev-parse", "HEAD") != shared {
		t.Error("feature/shared not created from origin/feature/shared")
	}
	if up := trackedBy(r, "feature/shared"); up != "origin/feature/shared" {
		t.Errorf("upstream = %q, want origin/feature/shared", up)
	}
	if base := r.Git("config", "gitflow.branch.feature/shared.base"); base != "develop" {
		t.Errorf("base = %q, want develop", base)
	}
	if !slices.Contains(res.Notes, "Continued feature/shared from origin/feature/shared") {
		t.Errorf("notes = %q", res.Notes)
	}
}
