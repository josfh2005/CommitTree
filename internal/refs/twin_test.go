package refs_test

import (
	"strings"
	"testing"

	"git-ui/internal/gitcmd"
	"git-ui/internal/refs"
	"git-ui/internal/testrepo"
)

// cloneWithTopic returns a clone of a remote that has main and topic, topic
// two commits ahead of the base main started from; only main is local.
func cloneWithTopic(t *testing.T) *testrepo.Repo {
	t.Helper()
	src := testrepo.New(t)
	src.Commit("base")
	src.Git("switch", "-q", "-c", "topic")
	src.Commit("topic 1")
	src.Commit("topic 2")
	src.Git("switch", "-q", "main")
	return testrepo.Clone(t, testrepo.NewBareFrom(t, src))
}

// upstream is branch's upstream, "" when it has none.
func upstream(r *testrepo.Repo, branch string) string {
	out, err := gitcmd.Run(ctx, r.Dir, gitcmd.ReadTimeout, "rev-parse", "--abbrev-ref", branch+"@{upstream}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func TestTwinPrefersOriginThenTheOnlyRemoteThatHasTheBranch(t *testing.T) {
	r := cloneWithTopic(t)
	if got := refs.Twin(ctx, r.Dir, "topic"); got != "origin/topic" {
		t.Errorf("Twin(topic) = %q, want origin/topic", got)
	}
	if got := refs.Twin(ctx, r.Dir, "nothing"); got != "" {
		t.Errorf("Twin(nothing) = %q, want none", got)
	}

	other := testrepo.New(t)
	other.Commit("other base")
	other.Git("branch", "only-there")
	r.Git("remote", "add", "upstream", testrepo.NewBareFrom(t, other))
	r.Git("fetch", "-q", "upstream")
	if got := refs.Twin(ctx, r.Dir, "only-there"); got != "upstream/only-there" {
		t.Errorf("Twin(only-there) = %q, want upstream/only-there", got)
	}
	// main is on both remotes: origin wins.
	if got := refs.Twin(ctx, r.Dir, "main"); got != "origin/main" {
		t.Errorf("Twin(main) = %q, want origin/main", got)
	}
}

func TestTwinIsNoneWhenSeveralRemotesHaveItAndNoneIsOrigin(t *testing.T) {
	src := testrepo.New(t)
	src.Commit("base")
	src.Git("branch", "shared")
	r := testrepo.New(t)
	r.Commit("own")
	r.Git("remote", "add", "one", testrepo.NewBareFrom(t, src))
	r.Git("remote", "add", "two", testrepo.NewBareFrom(t, src))
	r.Git("fetch", "-q", "--all")
	if got := refs.Twin(ctx, r.Dir, "shared"); got != "" {
		t.Errorf("Twin(shared) = %q, want none (ambiguous)", got)
	}
}

func TestCreateBranchTracksItsTwinWhenTheStartIsAtOrBehindIt(t *testing.T) {
	for _, start := range []string{"origin/topic", "origin/topic~1"} {
		r := cloneWithTopic(t)
		got, err := refs.CreateBranch(ctx, r.Dir, "topic", start, false)
		if err != nil {
			t.Fatal(err)
		}
		if got != (refs.Tracking{Twin: "origin/topic", Tracked: true}) {
			t.Errorf("from %s: result = %+v, want tracked origin/topic", start, got)
		}
		if up := upstream(r, "topic"); up != "origin/topic" {
			t.Errorf("from %s: upstream = %q, want origin/topic", start, up)
		}
	}
}

func TestCreateBranchTracksItsTwinWhenTheStartIsAheadOfIt(t *testing.T) {
	r := cloneWithTopic(t)
	r.Git("switch", "-q", "-c", "work", "origin/topic")
	ahead := r.Commit("local on top")
	r.Git("switch", "-q", "main")

	got, err := refs.CreateBranch(ctx, r.Dir, "topic", ahead, false)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Tracked || upstream(r, "topic") != "origin/topic" {
		t.Errorf("result = %+v, upstream %q; want tracking origin/topic", got, upstream(r, "topic"))
	}
}

func TestCreateBranchLeavesADivergedBranchWithoutUpstream(t *testing.T) {
	r := cloneWithTopic(t)
	r.Git("switch", "-q", "-c", "work", "origin/topic~1")
	diverged := r.Commit("not on origin/topic") // origin/topic has "topic 2", this lacks it
	r.Git("switch", "-q", "main")

	got, err := refs.CreateBranch(ctx, r.Dir, "topic", diverged, false)
	if err != nil {
		t.Fatal(err)
	}
	if got != (refs.Tracking{Twin: "origin/topic", Tracked: false}) {
		t.Errorf("result = %+v, want origin/topic untracked (diverged)", got)
	}
	if up := upstream(r, "topic"); up != "" {
		t.Errorf("upstream = %q, want none", up)
	}
}

func TestCreateBranchWithoutTwinReportsNothing(t *testing.T) {
	r := cloneWithTopic(t)
	got, err := refs.CreateBranch(ctx, r.Dir, "brand-new", "main", true)
	if err != nil {
		t.Fatal(err)
	}
	if got != (refs.Tracking{}) {
		t.Errorf("result = %+v, want empty", got)
	}
	if up := upstream(r, "brand-new"); up != "" {
		t.Errorf("upstream = %q, want none", up)
	}
}

func TestTrackTwinLeavesABranchThatAlreadyHasAnUpstream(t *testing.T) {
	r := cloneWithTopic(t)
	r.Git("branch", "--track", "topic", "origin/main")
	got, err := refs.TrackTwin(ctx, r.Dir, "topic")
	if err != nil {
		t.Fatal(err)
	}
	if got != (refs.Tracking{}) || upstream(r, "topic") != "origin/main" {
		t.Errorf("result = %+v, upstream %q; want untouched origin/main", got, upstream(r, "topic"))
	}
}
