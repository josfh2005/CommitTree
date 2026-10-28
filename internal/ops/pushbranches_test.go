package ops_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"git-ui/internal/gitcmd"
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

func TestPushBranchesPushesTrackedAndUntrackedNewBranchesInOneRequest(t *testing.T) {
	a, _ := aheadClone(t)
	a.Git("switch", "-q", "-c", "new-tracked")
	a.Commit("tracked")
	a.Git("switch", "-q", "-c", "new-untracked")
	a.Commit("untracked")
	got := pushBranches(t, a, ops.PushRequest{Remote: "origin", Branches: []ops.PushSpec{
		{Local: "new-tracked", Track: true},
		{Local: "new-untracked"},
	}})
	want := []ops.BranchPushResult{
		{Branch: "new-tracked", Target: "origin/new-tracked", Status: ops.PushPushed},
		{Branch: "new-untracked", Target: "origin/new-untracked", Status: ops.PushPushed},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if up := a.Git("rev-parse", "--abbrev-ref", "new-tracked@{u}"); up != "origin/new-tracked" {
		t.Errorf("new-tracked upstream = %q, want origin/new-tracked", up)
	}
	if a.GitFails("config", "branch.new-untracked.remote") != "" {
		t.Error("new-untracked has a remote configured")
	}
	for _, b := range []string{"new-tracked", "new-untracked"} {
		if remoteHash(a, "origin", b) != a.Git("rev-parse", b) {
			t.Errorf("%s did not reach the remote", b)
		}
	}
}

func TestPushBranchesRefusesInvalidAndDuplicateNames(t *testing.T) {
	a, _ := aheadClone(t)
	before := remoteHash(a, "origin", "main")
	for name, tc := range map[string]struct {
		req  ops.PushRequest
		want string
	}{
		"invalid remote name": {
			ops.PushRequest{Remote: "origin", Branches: []ops.PushSpec{{Local: "main", Remote: "bad..name"}}},
			`invalid branch name "bad..name"`,
		},
		"remote name with a space": {
			ops.PushRequest{Remote: "origin", Branches: []ops.PushSpec{{Local: "main", Remote: "a b"}}},
			`invalid branch name "a b"`,
		},
		"invalid local name": {
			ops.PushRequest{Remote: "origin", Branches: []ops.PushSpec{{Local: "main"}, {Local: "x~1"}}},
			`invalid branch name "x~1"`,
		},
		"two to one remote branch": {
			ops.PushRequest{Remote: "origin", Branches: []ops.PushSpec{{Local: "main", Remote: "same"}, {Local: "develop", Remote: "same"}}},
			`two branches push to "same"`,
		},
		"explicit and implied name collide": {
			ops.PushRequest{Remote: "origin", Branches: []ops.PushSpec{{Local: "main"}, {Local: "develop", Remote: "main"}}},
			`two branches push to "main"`,
		},
	} {
		got, err := ops.PushBranches(ctx, a.Dir, tc.req)
		if err == nil || err.Error() != tc.want {
			t.Errorf("%s: got %+v, %v; want error %q", name, got, err, tc.want)
		}
	}
	if remoteHash(a, "origin", "main") != before {
		t.Error("a refused request still pushed")
	}
	if a.Git("ls-remote", "origin", "refs/heads/same") != "" {
		t.Error("a refused request created a remote branch")
	}
}

// sessionFailing is a credentials prompt that was cancelled: every git
// command that fails ends with "No credentials entered.".
type sessionFailing struct{}

func (sessionFailing) Env() []string { return nil }
func (sessionFailing) End(failed bool) error {
	if failed {
		return &gitcmd.PromptError{Msg: "No credentials entered."}
	}
	return nil
}

func TestPushBranchesStopsAfterACancelledCredentialPrompt(t *testing.T) {
	a, _ := aheadClone(t)
	// A pre-push hook that fails and counts its runs stands in for a remote
	// whose authentication was refused: git exits without any ref line.
	counter := filepath.Join(t.TempDir(), "runs")
	hooks := t.TempDir()
	a.Git("config", "core.hooksPath", hooks)
	hook := filepath.Join(hooks, "pre-push")
	script := "#!/bin/sh\necho x >> '" + counter + "'\nexit 1\n"
	if err := os.WriteFile(hook, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	a.Git("switch", "-q", "-c", "fresh")
	a.Commit("fresh")
	a.Git("tag", "v1")
	ctx := gitcmd.WithPrompts(ctx, func() gitcmd.PromptSession { return sessionFailing{} })

	got, err := ops.PushBranches(ctx, a.Dir, ops.PushRequest{Remote: "origin", Tags: true, Branches: []ops.PushSpec{
		{Local: "fresh", Track: true},
		{Local: "develop"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := []ops.BranchPushResult{
		{Branch: "fresh", Target: "origin/fresh", Status: ops.PushFailed, Reason: "No credentials entered."},
		{Branch: "develop", Target: "origin/develop", Status: ops.PushFailed, Reason: "No credentials entered."},
		{Branch: "Tags", Target: "origin", Status: ops.PushFailed, Reason: "No credentials entered.", Tags: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	runs, _ := os.ReadFile(counter)
	if n := strings.Count(string(runs), "x"); n != 1 {
		t.Errorf("git push ran %d times, want 1", n)
	}
}
