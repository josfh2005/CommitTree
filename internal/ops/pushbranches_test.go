package ops_test

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"git-ui/internal/ops"
	"git-ui/internal/testrepo"
)

func pushBranches(t *testing.T, r *testrepo.Repo, req ops.PushRequest) []ops.BranchPushResult {
	t.Helper()
	got, err := ops.PushBranches(ctx, r.Dir, req)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// addEmptyRemote adds an empty bare repository to r as remote name.
func addEmptyRemote(t *testing.T, r *testrepo.Repo, name string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name+".git")
	r.Git("init", "-q", "--bare", dir)
	r.Git("remote", "add", name, dir)
}

func TestPushBranchesCreatesASameNameRemoteBranchAndTracksIt(t *testing.T) {
	a, _ := aheadClone(t)
	a.Git("switch", "-q", "-c", "feature/y")
	a.Commit("feature y")
	got := pushBranches(t, a, ops.PushRequest{Remote: "origin", Branches: []ops.PushSpec{{Local: "feature/y", Track: true}}})
	want := []ops.BranchPushResult{{Branch: "feature/y", Target: "origin/feature/y", Status: ops.PushPushed}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if remoteHash(a, "origin", "feature/y") != a.Git("rev-parse", "feature/y") {
		t.Error("feature/y did not reach the remote")
	}
	if up := a.Git("rev-parse", "--abbrev-ref", "feature/y@{u}"); up != "origin/feature/y" {
		t.Errorf("upstream = %q, want origin/feature/y", up)
	}
}

func TestPushBranchesUntrackedGetsNoUpstreamAndKeepsAnExistingOne(t *testing.T) {
	a, _ := aheadClone(t)
	addEmptyRemote(t, a, "other")
	a.Git("switch", "-q", "-c", "topic")
	a.Commit("topic")
	got := pushBranches(t, a, ops.PushRequest{Remote: "other", Branches: []ops.PushSpec{
		{Local: "topic"},
		{Local: "develop"},
	}})
	want := []ops.BranchPushResult{
		{Branch: "topic", Target: "other/topic", Status: ops.PushPushed},
		{Branch: "develop", Target: "other/develop", Status: ops.PushPushed},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if out := a.GitFails("rev-parse", "--abbrev-ref", "topic@{u}"); strings.HasPrefix(out, "other/") {
		t.Errorf("topic got an upstream: %q", out)
	}
	if a.GitFails("config", "branch.topic.remote") != "" {
		t.Error("topic has a remote configured")
	}
	if up := a.Git("rev-parse", "--abbrev-ref", "develop@{u}"); up != "origin/develop" {
		t.Errorf("develop upstream = %q, want origin/develop", up)
	}
}

func TestPushBranchesTrackRepointsTheUpstream(t *testing.T) {
	a, _ := aheadClone(t)
	addEmptyRemote(t, a, "dev2")
	got := pushBranches(t, a, ops.PushRequest{Remote: "dev2", Branches: []ops.PushSpec{{Local: "develop", Track: true}}})
	want := []ops.BranchPushResult{{Branch: "develop", Target: "dev2/develop", Status: ops.PushPushed}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if up := a.Git("rev-parse", "--abbrev-ref", "develop@{u}"); up != "dev2/develop" {
		t.Errorf("develop upstream = %q, want dev2/develop", up)
	}
}

func TestPushBranchesPushesToADifferentRemoteName(t *testing.T) {
	a, _ := aheadClone(t)
	got := pushBranches(t, a, ops.PushRequest{Remote: "origin", Branches: []ops.PushSpec{{Local: "develop", Remote: "renamed", Track: true}}})
	want := []ops.BranchPushResult{{Branch: "develop", Target: "origin/renamed", Status: ops.PushPushed}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if remoteHash(a, "origin", "renamed") != a.Git("rev-parse", "develop") {
		t.Error("develop did not reach origin/renamed")
	}
	if up := a.Git("rev-parse", "--abbrev-ref", "develop@{u}"); up != "origin/renamed" {
		t.Errorf("develop upstream = %q, want origin/renamed", up)
	}
}

func TestPushBranchesForceWithLeaseOverwritesARewrittenBranch(t *testing.T) {
	a, _ := aheadClone(t)
	a.Git("push", "-q", "origin", "main")
	a.Git("commit", "-q", "--amend", "-m", "main rewritten")
	spec := []ops.PushSpec{{Local: "main"}}

	got := pushBranches(t, a, ops.PushRequest{Remote: "origin", Branches: spec})
	want := []ops.BranchPushResult{{Branch: "main", Target: "origin/main", Status: ops.PushRejected, Reason: "The remote has commits you don't have — pull main first"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("without force: got %+v, want %+v", got, want)
	}

	got = pushBranches(t, a, ops.PushRequest{Remote: "origin", Branches: spec, Force: true})
	want = []ops.BranchPushResult{{Branch: "main", Target: "origin/main", Status: ops.PushPushed}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("with force: got %+v, want %+v", got, want)
	}
	if remoteHash(a, "origin", "main") != a.Git("rev-parse", "main") {
		t.Error("main was not overwritten on the remote")
	}
}

func TestPushBranchesForceRefusesWhenTheRemoteMovedSinceTheLastFetch(t *testing.T) {
	a, bare := aheadClone(t)
	b := testrepo.Clone(t, bare)
	b.Commit("remote main")
	b.Git("push", "-q", "origin", "main")
	moved := remoteHash(a, "origin", "main")
	a.Git("commit", "-q", "--amend", "-m", "main rewritten")

	got := pushBranches(t, a, ops.PushRequest{Remote: "origin", Force: true, Branches: []ops.PushSpec{
		{Local: "main"},
		{Local: "develop"},
	}})
	if len(got) != 2 {
		t.Fatalf("got %+v, want two results", got)
	}
	if got[0].Branch != "main" || got[0].Status != ops.PushRejected || got[0].Reason == "" {
		t.Errorf("main = %+v, want rejected with a reason", got[0])
	}
	if got[1] != (ops.BranchPushResult{Branch: "develop", Target: "origin/develop", Status: ops.PushPushed}) {
		t.Errorf("develop = %+v, want pushed", got[1])
	}
	if remoteHash(a, "origin", "main") != moved {
		t.Error("main on the remote changed despite the lease")
	}
}

func TestPushBranchesPushesTagsAsOneRow(t *testing.T) {
	a, _ := aheadClone(t)
	a.Git("tag", "v1")
	a.Git("tag", "-a", "-m", "second", "v2")
	req := ops.PushRequest{Remote: "origin", Tags: true}

	got := pushBranches(t, a, req)
	want := []ops.BranchPushResult{{Branch: "Tags", Target: "origin", Status: ops.PushPushed, Tags: true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	remoteTags := a.Git("ls-remote", "--tags", "origin")
	if !strings.Contains(remoteTags, "refs/tags/v1") || !strings.Contains(remoteTags, "refs/tags/v2") {
		t.Errorf("tags on the remote: %q", remoteTags)
	}

	got = pushBranches(t, a, req)
	want[0].Status = ops.PushUpToDate
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("second push: got %+v, want %+v", got, want)
	}
}

func TestPushBranchesPushesBranchesThenTags(t *testing.T) {
	a, _ := aheadClone(t)
	a.Git("tag", "v1")
	got := pushBranches(t, a, ops.PushRequest{Remote: "origin", Tags: true, Branches: []ops.PushSpec{{Local: "main"}}})
	want := []ops.BranchPushResult{
		{Branch: "main", Target: "origin/main", Status: ops.PushPushed},
		{Branch: "Tags", Target: "origin", Status: ops.PushPushed, Tags: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestPushBranchesRefusesBadRequests(t *testing.T) {
	a, _ := aheadClone(t)
	before := remoteHash(a, "origin", "main")
	for name, req := range map[string]ops.PushRequest{
		"unknown remote": {Remote: "nowhere", Branches: []ops.PushSpec{{Local: "main"}}},
		"empty remote":   {Branches: []ops.PushSpec{{Local: "main"}}},
		"empty request":  {Remote: "origin"},
		"missing branch": {Remote: "origin", Branches: []ops.PushSpec{{Local: "main"}, {Local: "nope"}}},
	} {
		if got, err := ops.PushBranches(ctx, a.Dir, req); err == nil {
			t.Errorf("%s: got %+v, want an error", name, got)
		}
	}
	if remoteHash(a, "origin", "main") != before {
		t.Error("a refused request still pushed")
	}
}
