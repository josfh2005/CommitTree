package writetools_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"git-ui/internal/ai"
	"git-ui/internal/ai/writetools"
	"git-ui/internal/testrepo"
)

var ctx = context.Background()

func prep(dir, name string, args map[string]any) (writetools.Proposal, error) {
	return writetools.Prepare(ctx, dir, ai.ToolCall{ID: "c1", Name: name, Args: args}, writetools.Env{PullStrategy: "merge"})
}

func TestSpecsListTheTenTools(t *testing.T) {
	var names []string
	for _, s := range writetools.Specs() {
		names = append(names, s.Name)
		if s.Description == "" || s.Parameters["type"] != "object" || !writetools.IsWrite(s.Name) {
			t.Errorf("%s: incomplete spec", s.Name)
		}
	}
	want := "stage_files,unstage_files,commit,create_branch,checkout_branch,stash_push,fetch,push,pull,merge_branch,cherry_pick"
	if strings.Join(names, ",") != want {
		t.Fatalf("names = %v", names)
	}
	if writetools.IsWrite("list_refs") {
		t.Fatal("list_refs is not a write tool")
	}
}

func TestCommitNeedsAMessageAndStagedFiles(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	if _, err := prep(r.Dir, "commit", map[string]any{"message": "feat: x"}); err == nil || !strings.Contains(err.Error(), "nothing is staged") {
		t.Fatalf("err = %v", err)
	}
	r.WriteFile("a.txt", "a\n")
	r.Git("add", "a.txt")
	if _, err := prep(r.Dir, "commit", map[string]any{"message": "  "}); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("err = %v", err)
	}
	p, err := prep(r.Dir, "commit", map[string]any{"message": "feat: add a\n\nbody"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Commit 1 staged file(s)" || p.Message != "feat: add a\n\nbody" || strings.Join(p.Staged, ",") != "a.txt" || p.Fingerprint == "" {
		t.Fatalf("proposal = %+v", p)
	}
}

func TestPushProposals(t *testing.T) {
	src := testrepo.New(t)
	src.Commit("base")
	remote := testrepo.NewBareFrom(t, src)
	r := testrepo.Clone(t, remote)
	if _, err := prep(r.Dir, "push", nil); err == nil || !strings.Contains(err.Error(), "nothing to push") {
		t.Fatalf("err = %v", err)
	}
	r.Commit("one")
	p, err := prep(r.Dir, "push", nil)
	if err != nil || p.Title != "Push main to origin/main" || len(p.Details) != 1 {
		t.Fatalf("p = %+v err = %v", p, err)
	}
	r.Git("switch", "-q", "-c", "topic")
	p, err = prep(r.Dir, "push", nil)
	if err != nil || !strings.HasPrefix(p.Title, "Publish topic to origin") {
		t.Fatalf("p = %+v err = %v", p, err)
	}
	r.Git("switch", "-q", "--detach")
	if _, err := prep(r.Dir, "push", nil); err == nil || !strings.Contains(err.Error(), "detached") {
		t.Fatalf("err = %v", err)
	}
}

func TestCheckoutAndMergeRejectRevisionSuffixesAsBranchNames(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.Commit("second")
	for _, bad := range []string{"main~2", "main@{u}"} {
		if _, err := prep(r.Dir, "checkout_branch", map[string]any{"name": bad}); err == nil || !strings.Contains(err.Error(), fmt.Sprintf("no branch %q", bad)) {
			t.Fatalf("checkout_branch(%q): err = %v", bad, err)
		}
		if _, err := prep(r.Dir, "merge_branch", map[string]any{"branch": bad}); err == nil || !strings.Contains(err.Error(), fmt.Sprintf("no branch %q", bad)) {
			t.Fatalf("merge_branch(%q): err = %v", bad, err)
		}
	}
}

func TestMergeBranchOnADetachedHead(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("branch", "topic")
	r.Git("switch", "-q", "--detach")
	if _, err := prep(r.Dir, "merge_branch", map[string]any{"branch": "topic"}); err == nil || !strings.Contains(err.Error(), "detached") {
		t.Fatalf("err = %v", err)
	}
}

func TestPushNamesThePushRemoteInATriangularSetup(t *testing.T) {
	src := testrepo.New(t)
	src.Commit("base")
	origin := testrepo.NewBareFrom(t, src)
	fork := testrepo.NewBareFrom(t, src)
	r := testrepo.Clone(t, origin)
	r.Git("remote", "add", "fork", fork)
	r.Git("fetch", "-q", "fork")
	r.Git("config", "branch.main.pushRemote", "fork")
	// push.default=simple refuses to resolve @{push} at all when the push
	// remote differs from the upstream remote ("cannot resolve 'simple'
	// push to a single destination"); "current" is what makes a triangular
	// setup like this push anywhere.
	r.Git("config", "push.default", "current")
	r.Commit("new")

	p, err := prep(r.Dir, "push", nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Push main to fork/main" {
		t.Fatalf("p = %+v", p)
	}
}

func TestPushRefusesWhenPushDefaultIsMatching(t *testing.T) {
	src := testrepo.New(t)
	src.Commit("base")
	remote := testrepo.NewBareFrom(t, src)
	r := testrepo.Clone(t, remote)
	r.Git("config", "push.default", "matching")
	r.Commit("new")

	_, err := prep(r.Dir, "push", nil)
	if err == nil || !strings.Contains(err.Error(), `push.default is "matching"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestRecheckDetectsAMovedRefAndAChangedIndex(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.WriteFile("a.txt", "a\n")
	r.Git("add", "a.txt")
	p, err := prep(r.Dir, "commit", map[string]any{"message": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if err := writetools.Recheck(ctx, r.Dir, p, "commit"); err != nil {
		t.Fatalf("unchanged repo: %v", err)
	}
	r.WriteFile("b.txt", "b\n")
	r.Git("add", "b.txt")
	if err := writetools.Recheck(ctx, r.Dir, p, "commit"); !errors.Is(err, writetools.ErrChanged) {
		t.Fatalf("staged set changed: %v", err)
	}
	p2, _ := prep(r.Dir, "create_branch", map[string]any{"name": "x"})
	r.Commit("moved")
	if err := writetools.Recheck(ctx, r.Dir, p2, "create_branch"); !errors.Is(err, writetools.ErrChanged) {
		t.Fatalf("ref moved: %v", err)
	}
}

func TestWrongArgumentType(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	if _, err := prep(r.Dir, "stage_files", map[string]any{"paths": "a.txt"}); err == nil || !strings.Contains(err.Error(), `argument "paths" must be`) {
		t.Fatalf("err = %v", err)
	}
}

func TestStageFilesProposals(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.WriteFile("a.txt", "a\n")
	if _, err := prep(r.Dir, "stage_files", map[string]any{"paths": []any{}}); err == nil || !strings.Contains(err.Error(), "give at least one path") {
		t.Fatalf("err = %v", err)
	}
	if _, err := prep(r.Dir, "stage_files", map[string]any{"paths": []any{"missing.txt"}}); err == nil || !strings.Contains(err.Error(), `"missing.txt" has no unstaged change`) {
		t.Fatalf("err = %v", err)
	}
	p, err := prep(r.Dir, "stage_files", map[string]any{"paths": []any{"a.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Stage 1 file(s)" || strings.Join(p.Paths, ",") != "a.txt" || strings.Join(p.Details, ",") != "a.txt" {
		t.Fatalf("p = %+v", p)
	}
}

func TestUnstageFilesProposals(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.WriteFile("a.txt", "a\n")
	r.Git("add", "a.txt")
	if _, err := prep(r.Dir, "unstage_files", map[string]any{"paths": []any{"b.txt"}}); err == nil || !strings.Contains(err.Error(), `"b.txt" is not staged`) {
		t.Fatalf("err = %v", err)
	}
	p, err := prep(r.Dir, "unstage_files", map[string]any{"paths": []any{"a.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Unstage 1 file(s)" || strings.Join(p.Paths, ",") != "a.txt" {
		t.Fatalf("p = %+v", p)
	}
}

func TestCreateBranchProposals(t *testing.T) {
	r := testrepo.New(t)
	hash := r.Commit("base")
	short := hash[:7]

	if _, err := prep(r.Dir, "create_branch", map[string]any{"name": "bad name"}); err == nil || !strings.Contains(err.Error(), "is not a valid branch name") {
		t.Fatalf("err = %v", err)
	}
	r.Git("branch", "existing")
	if _, err := prep(r.Dir, "create_branch", map[string]any{"name": "existing"}); err == nil || !strings.Contains(err.Error(), `branch "existing" already exists`) {
		t.Fatalf("err = %v", err)
	}
	if _, err := prep(r.Dir, "create_branch", map[string]any{"name": "x", "start": "nope"}); err == nil || !strings.Contains(err.Error(), `"nope" is not a commit`) {
		t.Fatalf("err = %v", err)
	}
	p, err := prep(r.Dir, "create_branch", map[string]any{"name": "feature", "checkout": true})
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Create branch feature at "+short+" and switch to it" || p.Start != hash || !p.Checkout || p.Name != "feature" {
		t.Fatalf("p = %+v", p)
	}
	if len(p.Details) != 1 || !strings.HasPrefix(p.Details[0], short+" base") {
		t.Fatalf("details = %v", p.Details)
	}

	p, err = prep(r.Dir, "create_branch", map[string]any{"name": "feature2"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Create branch feature2 at "+short || p.Checkout {
		t.Fatalf("p = %+v", p)
	}
}

func TestCheckoutBranchProposals(t *testing.T) {
	src := testrepo.New(t)
	src.Commit("base")
	remote := testrepo.NewBareFrom(t, src)
	r := testrepo.Clone(t, remote)
	r.Git("branch", "local")

	p, err := prep(r.Dir, "checkout_branch", map[string]any{"name": "local"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Switch from main to local" || p.Name != "local" || p.Remote != "" {
		t.Fatalf("p = %+v", p)
	}

	if _, err := prep(r.Dir, "checkout_branch", map[string]any{"name": "main"}); err == nil || !strings.Contains(err.Error(), "already on main") {
		t.Fatalf("err = %v", err)
	}

	if _, err := prep(r.Dir, "checkout_branch", map[string]any{"name": "nope"}); err == nil || !strings.Contains(err.Error(), `no branch "nope"`) {
		t.Fatalf("err = %v", err)
	}

	src.Git("branch", "topic")
	src.Git("push", "-q", remote, "topic:topic")
	r.Git("fetch", "-q", "origin")
	p, err = prep(r.Dir, "checkout_branch", map[string]any{"name": "origin/topic"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Remote != "origin" || p.Name != "topic" || len(p.Details) != 1 || p.Details[0] != "creates local branch topic tracking origin/topic" {
		t.Fatalf("p = %+v", p)
	}
}

func TestCheckoutBranchOfARemoteWithAnExistingLocalBranch(t *testing.T) {
	src := testrepo.New(t)
	src.Commit("base")
	src.Git("branch", "topic")
	remote := testrepo.NewBareFrom(t, src)
	src.Git("push", "-q", remote, "topic:topic")
	r := testrepo.Clone(t, remote)
	r.Git("fetch", "-q", "origin", "topic")
	// A local "topic" already exists, ahead of origin/topic by one commit
	// and behind by none: the card must say it switches to that existing
	// branch, not that it creates one.
	r.Git("branch", "topic", "origin/topic")
	r.Git("switch", "-q", "topic")
	r.Commit("local work")
	r.Git("switch", "-q", "main")

	p, err := prep(r.Dir, "checkout_branch", map[string]any{"name": "origin/topic"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Remote != "" || p.Name != "topic" {
		t.Fatalf("p = %+v", p)
	}
	if len(p.Details) != 1 || p.Details[0] != "switches to existing local branch topic (1 ahead, 0 behind origin/topic)" {
		t.Fatalf("details = %v", p.Details)
	}
}

func TestCheckoutBranchResolvesTheRemoteByItsConfiguredName(t *testing.T) {
	src := testrepo.New(t)
	src.Commit("base")
	remote := testrepo.NewBareFrom(t, src)
	r := testrepo.Clone(t, remote)
	r.Git("remote", "rename", "origin", "my/remote")
	src.Git("branch", "topic")
	src.Git("push", "-q", remote, "topic:topic")
	r.Git("fetch", "-q", "my/remote")

	p, err := prep(r.Dir, "checkout_branch", map[string]any{"name": "my/remote/topic"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Remote != "my/remote" || p.Name != "topic" {
		t.Fatalf("p = %+v", p)
	}
}

func TestStashPushProposals(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	if _, err := prep(r.Dir, "stash_push", nil); err == nil || !strings.Contains(err.Error(), "nothing to stash") {
		t.Fatalf("err = %v", err)
	}
	r.WriteFile("untracked.txt", "u\n")
	if _, err := prep(r.Dir, "stash_push", map[string]any{}); err == nil || !strings.Contains(err.Error(), "nothing to stash") {
		t.Fatalf("err = %v", err)
	}
	p, err := prep(r.Dir, "stash_push", map[string]any{"include_untracked": true})
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Stash 1 file(s)" || !p.IncludeUntracked || strings.Join(p.Details, ",") != "includes untracked files" {
		t.Fatalf("p = %+v", p)
	}

	r.WriteFile("file-1.txt", "changed\n")
	p, err = prep(r.Dir, "stash_push", map[string]any{"message": "wip"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Message != "wip" || p.Title != "Stash 1 file(s)" || strings.Join(p.Details, ",") != "wip" {
		t.Fatalf("p = %+v", p)
	}
}

func TestFetchProposals(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	if _, err := prep(r.Dir, "fetch", nil); err == nil || !strings.Contains(err.Error(), "no remote is configured") {
		t.Fatalf("err = %v", err)
	}
	remote := testrepo.NewBareFrom(t, r)
	r.Git("remote", "add", "origin", remote)
	p, err := prep(r.Dir, "fetch", nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Fetch from origin" {
		t.Fatalf("p = %+v", p)
	}
}

func TestPullProposals(t *testing.T) {
	src := testrepo.New(t)
	src.Commit("base")
	remote := testrepo.NewBareFrom(t, src)
	r := testrepo.Clone(t, remote)

	if _, err := prep(r.Dir, "pull", nil); err == nil || !strings.Contains(err.Error(), "up to date") {
		t.Fatalf("err = %v", err)
	}

	src.Commit("two")
	src.Git("push", "-q", remote, "main:main")
	r.Git("fetch", "-q", "origin")
	p, err := prep(r.Dir, "pull", nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Pull origin/main into main (merge)" || len(p.Details) != 1 {
		t.Fatalf("p = %+v", p)
	}

	r.Git("switch", "-q", "-c", "orphan")
	if _, err := prep(r.Dir, "pull", nil); err == nil || !strings.Contains(err.Error(), "has no upstream") {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateBranchRejectsOptionLikeStart(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	if _, err := prep(r.Dir, "create_branch", map[string]any{"name": "x", "start": "-5"}); err == nil || !strings.Contains(err.Error(), `"-5" is not a commit`) {
		t.Fatalf("err = %v", err)
	}
	if _, err := prep(r.Dir, "create_branch", map[string]any{"name": "y", "start": "--all"}); err == nil || !strings.Contains(err.Error(), `"--all" is not a commit`) {
		t.Fatalf("err = %v", err)
	}
}

func TestCheckoutAndMergeRejectLeadingDashBranchNamesCleanly(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	if _, err := prep(r.Dir, "checkout_branch", map[string]any{"name": "-x"}); err == nil || !strings.Contains(err.Error(), `no branch "-x"`) {
		t.Fatalf("err = %v", err)
	}
	if _, err := prep(r.Dir, "merge_branch", map[string]any{"branch": "-x"}); err == nil || !strings.Contains(err.Error(), `no branch "-x"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestPushPublishCountsOnlyCommitsNotOnAnyRemote(t *testing.T) {
	src := testrepo.New(t)
	src.Commit("base")
	remote := testrepo.NewBareFrom(t, src)
	r := testrepo.Clone(t, remote)
	r.Git("branch", "topic")
	r.Git("switch", "-q", "topic")

	p, err := prep(r.Dir, "push", nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Publish topic to origin (sets upstream origin/topic)" || strings.Join(p.Details, "|") != "no new commits; creates origin/topic" {
		t.Fatalf("p = %+v", p)
	}

	r.Commit("new")
	p, err = prep(r.Dir, "push", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Details) != 1 {
		t.Fatalf("details = %v", p.Details)
	}
}

func TestPushWithUnbornHeadAndOrigin(t *testing.T) {
	remote := testrepo.NewBareFrom(t, testrepo.New(t))
	r := testrepo.New(t)
	r.Git("remote", "add", "origin", remote)
	if _, err := prep(r.Dir, "push", nil); err == nil || !strings.Contains(err.Error(), "has no commits to push") {
		t.Fatalf("err = %v", err)
	}
}

func TestMergeBranchProposals(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("branch", "topic")
	r.Git("switch", "-q", "topic")
	r.Commit("topic work")
	r.Git("switch", "-q", "main")

	if _, err := prep(r.Dir, "merge_branch", map[string]any{"branch": "main"}); err == nil || !strings.Contains(err.Error(), "cannot merge main into itself") {
		t.Fatalf("err = %v", err)
	}
	if _, err := prep(r.Dir, "merge_branch", map[string]any{"branch": "nope"}); err == nil || !strings.Contains(err.Error(), `no branch "nope"`) {
		t.Fatalf("err = %v", err)
	}
	p, err := prep(r.Dir, "merge_branch", map[string]any{"branch": "topic"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Merge topic into main" || p.Branch != "topic" || strings.Join(p.Details, "|") != "1 commit(s)|a merge commit is always created" {
		t.Fatalf("p = %+v", p)
	}
	r.Git("merge", "-q", "--no-ff", "-m", "merge topic", "topic")
	if _, err := prep(r.Dir, "merge_branch", map[string]any{"branch": "topic"}); err == nil || !strings.Contains(err.Error(), "is already merged into main") {
		t.Fatalf("err = %v", err)
	}
}

func TestCherryPickProposal(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("switch", "-q", "-c", "feature")
	picked := r.Commit("fix login")
	r.Git("switch", "-q", "main")
	short := r.Git("rev-parse", "--short", picked)

	p, err := prep(r.Dir, "cherry_pick", map[string]any{"commit": short})
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Cherry-pick "+short+" fix login onto main" || p.Commit != picked {
		t.Fatalf("proposal = %+v", p)
	}
}

func TestCherryPickRefusals(t *testing.T) {
	r := testrepo.New(t)
	base := r.Commit("base")
	r.Git("switch", "-q", "-c", "side")
	r.Commit("on side")
	r.Git("switch", "-q", "main")
	r.Commit("on main")
	r.Git("merge", "-q", "--no-ff", "--no-edit", "side")
	mergeCommit := r.Git("rev-parse", "HEAD")
	r.Git("switch", "-q", "-c", "other", base)
	picked := r.Commit("pick me")
	r.Git("switch", "-q", "main")

	for _, c := range []struct{ commit, want string }{
		{"-x", "invalid commit"},
		{"nope", `no commit "nope"`},
		{base, "already on main"},
		{mergeCommit, "merge commit"},
	} {
		if _, err := prep(r.Dir, "cherry_pick", map[string]any{"commit": c.commit}); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("cherry_pick(%s): err = %v, want %q", c.commit, err, c.want)
		}
	}
	r.WriteFile("file-1.txt", "dirty\n")
	if _, err := prep(r.Dir, "cherry_pick", map[string]any{"commit": picked}); err == nil || !strings.Contains(err.Error(), "commit or stash") {
		t.Errorf("dirty: err = %v", err)
	}
	r.Git("checkout", "--", "file-1.txt")
	r.Git("switch", "-q", "--detach")
	if _, err := prep(r.Dir, "cherry_pick", map[string]any{"commit": picked}); err == nil || !strings.Contains(err.Error(), "detached") {
		t.Errorf("detached: err = %v", err)
	}
}
