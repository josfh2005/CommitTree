package ops_test

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"git-ui/internal/ops"
	"git-ui/internal/testrepo"
)

// aheadClone returns a clone of a remote with branches main, feature and
// quiet, all tracking origin; main and feature each have one commit the
// remote lacks, quiet has none.
func aheadClone(t *testing.T) (*testrepo.Repo, string) {
	t.Helper()
	src := testrepo.New(t)
	src.Commit("base")
	src.Git("branch", "feature")
	src.Git("branch", "quiet")
	bare := testrepo.NewBareFrom(t, src)
	a := testrepo.Clone(t, bare)
	a.Git("branch", "--track", "feature", "origin/feature")
	a.Git("branch", "--track", "quiet", "origin/quiet")
	a.Commit("main local")
	a.Git("switch", "-q", "feature")
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
		{Branch: "feature", Target: "origin/feature", Status: ops.PushPushed},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if remoteHash(a, "origin", "feature") != a.Git("rev-parse", "feature") {
		t.Error("feature did not reach the remote")
	}
}

func TestPushAllReportsTheCurrentBranchUpToDate(t *testing.T) {
	a, _ := aheadClone(t)
	a.Git("push", "-q", "origin", "main")
	got := pushAll(t, a)
	want := []ops.BranchPushResult{
		{Branch: "main", Target: "origin/main", Status: ops.PushUpToDate},
		{Branch: "feature", Target: "origin/feature", Status: ops.PushPushed},
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
		{Branch: "feature", Target: "origin/feature", Status: ops.PushPushed},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestPushAllPublishesACurrentBranchWithNoUpstream(t *testing.T) {
	a, _ := aheadClone(t)
	a.Git("switch", "-q", "-c", "topic")
	a.Commit("topic")
	got := pushAll(t, a)
	want := []ops.BranchPushResult{
		{Branch: "topic", Target: "origin/topic", Status: ops.PushPushed},
		{Branch: "feature", Target: "origin/feature", Status: ops.PushPushed},
		{Branch: "main", Target: "origin/main", Status: ops.PushPushed},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if up := a.Git("rev-parse", "--abbrev-ref", "topic@{upstream}"); up != "origin/topic" {
		t.Errorf("topic upstream = %q, want origin/topic", up)
	}
}

func TestPushAllLeavesOutBranchesWithNothingToPushOrNowhereToGo(t *testing.T) {
	a, bare := aheadClone(t)
	a.Git("push", "-q", "origin", "main") // current, up to date
	a.Git("branch", "loose")              // no upstream
	a.Git("branch", "--track", "child", "main")
	a.Git("switch", "-q", "child")
	a.Commit("child") // ahead of local main: must never be pushed
	a.Git("switch", "-q", "main")
	b := testrepo.Clone(t, bare)
	b.Git("push", "-q", "origin", "--delete", "feature")
	a.Git("fetch", "-q", "--prune") // feature: ahead, but its upstream is gone

	got := pushAll(t, a)
	want := []ops.BranchPushResult{{Branch: "main", Target: "origin/main", Status: ops.PushUpToDate}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestPushAllPushesToTheUpstreamName(t *testing.T) {
	a, _ := aheadClone(t)
	a.Git("push", "-q", "origin", "main")
	a.Git("push", "-q", "origin", "feature~1:refs/heads/renamed")
	a.Git("fetch", "-q", "origin")
	a.Git("branch", "-q", "--set-upstream-to=origin/renamed", "feature")
	got := pushAll(t, a)
	want := []ops.BranchPushResult{
		{Branch: "main", Target: "origin/main", Status: ops.PushUpToDate},
		{Branch: "feature", Target: "origin/renamed", Status: ops.PushPushed},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if remoteHash(a, "origin", "renamed") != a.Git("rev-parse", "feature") {
		t.Error("feature was not pushed to renamed")
	}
}

func TestPushAllGroupsByRemoteAndFailsOnlyTheBrokenOne(t *testing.T) {
	a, _ := aheadClone(t)
	backup := testrepo.NewBareFrom(t, a)
	a.Git("remote", "add", "backup", backup)
	a.Git("fetch", "-q", "backup")
	a.Git("branch", "-q", "--set-upstream-to=backup/quiet", "quiet")
	a.Git("switch", "-q", "quiet")
	a.Commit("quiet local")
	a.Git("switch", "-q", "main")
	a.Git("remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing.git"))

	got := pushAll(t, a)
	if len(got) != 3 || got[0].Branch != "main" || got[1].Branch != "feature" || got[2].Branch != "quiet" {
		t.Fatalf("got %+v, want main, feature, quiet", got)
	}
	for _, r := range got[:2] {
		if r.Status != ops.PushFailed || !strings.Contains(r.Reason, "does not appear to be a git repository") {
			t.Errorf("%s = %s %q, want failed with git's message", r.Branch, r.Status, r.Reason)
		}
	}
	if got[2] != (ops.BranchPushResult{Branch: "quiet", Target: "backup/quiet", Status: ops.PushPushed}) {
		t.Errorf("quiet = %+v, want pushed to backup/quiet", got[2])
	}
}

func TestPushAllWithADetachedHeadPushesOnlyTheOthers(t *testing.T) {
	a, _ := aheadClone(t)
	a.Git("switch", "-q", "--detach", "HEAD")
	got := pushAll(t, a)
	want := []ops.BranchPushResult{
		{Branch: "feature", Target: "origin/feature", Status: ops.PushPushed},
		{Branch: "main", Target: "origin/main", Status: ops.PushPushed},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
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
