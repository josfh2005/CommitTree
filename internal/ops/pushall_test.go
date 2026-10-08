package ops_test

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"git-ui/internal/ops"
	"git-ui/internal/testrepo"
)

// aheadClone returns a clone of a remote with branches main, develop and
// release/1.0, all tracking origin and all official; main and develop each
// have one commit the remote lacks, release/1.0 has none. feature/x is
// ahead too but is not official.
func aheadClone(t *testing.T) (*testrepo.Repo, string) {
	t.Helper()
	src := testrepo.New(t)
	src.Commit("base")
	src.Git("branch", "develop")
	src.Git("branch", "release/1.0")
	src.Git("branch", "feature/x")
	bare := testrepo.NewBareFrom(t, src)
	a := testrepo.Clone(t, bare)
	a.Git("branch", "--track", "develop", "origin/develop")
	a.Git("branch", "--track", "release/1.0", "origin/release/1.0")
	a.Git("branch", "--track", "feature/x", "origin/feature/x")
	a.Commit("main local")
	a.Git("switch", "-q", "develop")
	a.Commit("develop local")
	a.Git("switch", "-q", "feature/x")
	a.Commit("feature local")
	a.Git("switch", "-q", "main")
	return a, bare
}

// remoteHash is what the remote's branch points at, from ls-remote.
func remoteHash(r *testrepo.Repo, remote, branch string) string {
	return strings.Fields(r.Git("ls-remote", remote, "refs/heads/"+branch) + " ")[0]
}

func pushAll(t *testing.T, r *testrepo.Repo) []ops.BranchPushResult {
	t.Helper()
	got, err := ops.PushAll(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestPushAllPushesTheCurrentBranchAndTheOthersAhead(t *testing.T) {
	a, _ := aheadClone(t)
	got := pushAll(t, a)
	want := []ops.BranchPushResult{
		{Branch: "main", Target: "origin/main", Status: ops.PushPushed},
		{Branch: "develop", Target: "origin/develop", Status: ops.PushPushed},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if remoteHash(a, "origin", "develop") != a.Git("rev-parse", "develop") {
		t.Error("develop did not reach the remote")
	}
}

func TestPushAllReportsTheCurrentBranchUpToDate(t *testing.T) {
	a, _ := aheadClone(t)
	a.Git("push", "-q", "origin", "main")
	got := pushAll(t, a)
	want := []ops.BranchPushResult{
		{Branch: "main", Target: "origin/main", Status: ops.PushUpToDate},
		{Branch: "develop", Target: "origin/develop", Status: ops.PushPushed},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestPushAllPushesTheOthersWhenOneIsRejected(t *testing.T) {
	a, bare := aheadClone(t)
	b := testrepo.Clone(t, bare)
	b.Commit("remote main")
	b.Git("push", "-q", "origin", "main")

	got := pushAll(t, a) // a never fetched: its badge still says "ahead"
	want := []ops.BranchPushResult{
		{Branch: "main", Target: "origin/main", Status: ops.PushRejected, Reason: "The remote has commits you don't have — pull main first"},
		{Branch: "develop", Target: "origin/develop", Status: ops.PushPushed},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestPushAllPublishesACurrentBranchWithNoUpstream(t *testing.T) {
	a, _ := aheadClone(t)
	a.Git("switch", "-q", "-c", "release/1.3")
	a.Commit("release")
	got := pushAll(t, a)
	want := []ops.BranchPushResult{
		{Branch: "release/1.3", Target: "origin/release/1.3", Status: ops.PushPushed},
		{Branch: "develop", Target: "origin/develop", Status: ops.PushPushed},
		{Branch: "main", Target: "origin/main", Status: ops.PushPushed},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if up := a.Git("rev-parse", "--abbrev-ref", "release/1.3@{upstream}"); up != "origin/release/1.3" {
		t.Errorf("release/1.3 upstream = %q, want origin/release/1.3", up)
	}
}

func TestPushAllLeavesOutBranchesWithNothingToPushOrNowhereToGo(t *testing.T) {
	a, bare := aheadClone(t)
	a.Git("push", "-q", "origin", "main") // current, up to date
	a.Git("branch", "release/loose")      // no upstream
	a.Git("branch", "--track", "release/child", "main")
	a.Git("switch", "-q", "release/child")
	a.Commit("release/child") // ahead of local main: must never be pushed
	a.Git("switch", "-q", "main")
	b := testrepo.Clone(t, bare)
	b.Git("push", "-q", "origin", "--delete", "develop")
	a.Git("fetch", "-q", "--prune") // develop: ahead, but its upstream is gone

	got := pushAll(t, a)
	want := []ops.BranchPushResult{{Branch: "main", Target: "origin/main", Status: ops.PushUpToDate}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestPushAllPushesToTheUpstreamName(t *testing.T) {
	a, _ := aheadClone(t)
	a.Git("push", "-q", "origin", "main")
	a.Git("push", "-q", "origin", "develop~1:refs/heads/renamed")
	a.Git("fetch", "-q", "origin")
	a.Git("branch", "-q", "--set-upstream-to=origin/renamed", "develop")
	got := pushAll(t, a)
	want := []ops.BranchPushResult{
		{Branch: "main", Target: "origin/main", Status: ops.PushUpToDate},
		{Branch: "develop", Target: "origin/renamed", Status: ops.PushPushed},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if remoteHash(a, "origin", "renamed") != a.Git("rev-parse", "develop") {
		t.Error("develop was not pushed to renamed")
	}
}

func TestPushAllGroupsByRemoteAndFailsOnlyTheBrokenOne(t *testing.T) {
	a, _ := aheadClone(t)
	backup := testrepo.NewBareFrom(t, a)
	a.Git("remote", "add", "backup", backup)
	a.Git("fetch", "-q", "backup")
	a.Git("branch", "-q", "--set-upstream-to=backup/release/1.0", "release/1.0")
	a.Git("switch", "-q", "release/1.0")
	a.Commit("release local")
	a.Git("switch", "-q", "main")
	a.Git("remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing.git"))

	got := pushAll(t, a)
	if len(got) != 3 || got[0].Branch != "main" || got[1].Branch != "develop" || got[2].Branch != "release/1.0" {
		t.Fatalf("got %+v, want main, develop, release/1.0", got)
	}
	for _, r := range got[:2] {
		if r.Status != ops.PushFailed || !strings.Contains(r.Reason, "does not appear to be a git repository") {
			t.Errorf("%s = %s %q, want failed with git's message", r.Branch, r.Status, r.Reason)
		}
	}
	if got[2] != (ops.BranchPushResult{Branch: "release/1.0", Target: "backup/release/1.0", Status: ops.PushPushed}) {
		t.Errorf("release/1.0 = %+v, want pushed to backup/release/1.0", got[2])
	}
}

func TestPushAllWithADetachedHeadPushesOnlyTheOthers(t *testing.T) {
	a, _ := aheadClone(t)
	a.Git("switch", "-q", "--detach", "HEAD")
	got := pushAll(t, a)
	want := []ops.BranchPushResult{
		{Branch: "develop", Target: "origin/develop", Status: ops.PushPushed},
		{Branch: "main", Target: "origin/main", Status: ops.PushPushed},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestPushAllNeverPushesACurrentBranchTrackingALocalBranch(t *testing.T) {
	a, _ := aheadClone(t)
	a.Git("branch", "--track", "release/child", "main")
	a.Git("switch", "-q", "release/child")
	a.Commit("release/child") // ahead of local main; checked out, but its upstream is "."
	mainBefore := a.Git("rev-parse", "main")

	got := pushAll(t, a)
	want := []ops.BranchPushResult{
		{Branch: "develop", Target: "origin/develop", Status: ops.PushPushed},
		{Branch: "main", Target: "origin/main", Status: ops.PushPushed},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if a.Git("rev-parse", "main") != mainBefore {
		t.Error("main moved: the local upstream was pushed to")
	}
}

func TestPushAllWithNothingToPushReturnsNoResults(t *testing.T) {
	src := testrepo.New(t)
	src.Commit("base")
	a := testrepo.Clone(t, testrepo.NewBareFrom(t, src))
	a.Git("switch", "-q", "--detach", "HEAD")
	got := pushAll(t, a)
	if got == nil || len(got) != 0 {
		t.Fatalf("got %#v, want an empty, non-nil slice", got)
	}
}

func TestPushAllLeavesOutBranchesThatAreNotOfficial(t *testing.T) {
	a, _ := aheadClone(t) // feature/x is ahead of its upstream, like main and develop
	got := pushAll(t, a)
	for _, r := range got {
		if r.Branch == "feature/x" {
			t.Fatalf("feature/x was pushed: %+v", got)
		}
	}
	want := []ops.BranchPushResult{
		{Branch: "main", Target: "origin/main", Status: ops.PushPushed},
		{Branch: "develop", Target: "origin/develop", Status: ops.PushPushed},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if remoteHash(a, "origin", "feature/x") == a.Git("rev-parse", "feature/x") {
		t.Error("feature/x reached the remote")
	}
}

func TestPushAllPushesMasterDevelopAndReleaseBranchesAhead(t *testing.T) {
	src := testrepo.New(t)
	src.Commit("base")
	src.Git("branch", "-m", "master")
	for _, b := range []string{"develop", "release/2.0", "hotfix/1"} {
		src.Git("branch", b)
	}
	a := testrepo.Clone(t, testrepo.NewBareFrom(t, src))
	for _, b := range []string{"develop", "release/2.0", "hotfix/1"} {
		a.Git("branch", "--track", b, "origin/"+b)
		a.Git("switch", "-q", b)
		a.Commit(b + " local")
	}
	a.Git("switch", "-q", "master")
	a.Commit("master local")

	got := pushAll(t, a)
	want := []ops.BranchPushResult{
		{Branch: "master", Target: "origin/master", Status: ops.PushPushed},
		{Branch: "develop", Target: "origin/develop", Status: ops.PushPushed},
		{Branch: "release/2.0", Target: "origin/release/2.0", Status: ops.PushPushed},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestPushAllHonoursCustomGitFlowNames(t *testing.T) {
	src := testrepo.New(t)
	src.Commit("base")
	for _, b := range []string{"prod", "dev", "rel/1", "develop", "release/1", "master"} {
		src.Git("branch", b)
	}
	a := testrepo.Clone(t, testrepo.NewBareFrom(t, src))
	a.Git("config", "gitflow.branch.master", "prod")
	a.Git("config", "gitflow.branch.develop", "dev")
	a.Git("config", "gitflow.prefix.release", "rel/")
	// main and master are ahead too: with git-flow configured they are not
	// official, like develop and release/1.
	a.Commit("main local")
	for _, b := range []string{"prod", "dev", "rel/1", "develop", "release/1", "master"} {
		a.Git("branch", "--track", b, "origin/"+b)
		a.Git("switch", "-q", b)
		a.Commit(b + " local")
	}
	a.Git("switch", "-q", "prod")

	got := pushAll(t, a)
	want := []ops.BranchPushResult{
		{Branch: "prod", Target: "origin/prod", Status: ops.PushPushed},
		{Branch: "dev", Target: "origin/dev", Status: ops.PushPushed},
		{Branch: "rel/1", Target: "origin/rel/1", Status: ops.PushPushed},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestPushAllLeavesOutANonOfficialCurrentBranch(t *testing.T) {
	a, _ := aheadClone(t)
	a.Git("switch", "-q", "feature/x") // ahead, with an upstream
	got := pushAll(t, a)
	want := []ops.BranchPushResult{
		{Branch: "develop", Target: "origin/develop", Status: ops.PushPushed},
		{Branch: "main", Target: "origin/main", Status: ops.PushPushed},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if remoteHash(a, "origin", "feature/x") == a.Git("rev-parse", "feature/x") {
		t.Error("feature/x reached the remote")
	}
}

func TestPushAllDoesNotPublishANonOfficialCurrentBranchWithNoUpstream(t *testing.T) {
	a, _ := aheadClone(t)
	a.Git("switch", "-q", "-c", "topic")
	a.Commit("topic")
	got := pushAll(t, a)
	for _, r := range got {
		if r.Branch == "topic" {
			t.Fatalf("topic was published: %+v", got)
		}
	}
	if out := a.Git("ls-remote", "origin", "refs/heads/topic"); strings.TrimSpace(out) != "" {
		t.Errorf("topic reached the remote: %q", out)
	}
}
