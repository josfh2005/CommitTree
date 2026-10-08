package refs_test

import (
	"context"
	"reflect"
	"testing"

	"git-ui/internal/refs"
	"git-ui/internal/testrepo"
)

var ctx = context.Background()

func TestListLocalRemotesTags(t *testing.T) {
	src := testrepo.New(t)
	first := src.Commit("first")
	src.Git("branch", "feature/x")
	src.Git("tag", "-a", "v1.0", "-m", "release")
	src.Git("tag", "light")
	r := testrepo.Clone(t, testrepo.NewBareFrom(t, src))

	got, err := refs.List(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Head != "main" || got.Detached || got.HeadHash != first {
		t.Fatalf("head = %q detached=%v hash=%q", got.Head, got.Detached, got.HeadHash)
	}
	wantLocal := []refs.Branch{{Name: "main", Hash: first, Current: true, Upstream: "origin/main", Official: true}}
	if !reflect.DeepEqual(got.Local, wantLocal) {
		t.Fatalf("local = %+v", got.Local)
	}
	wantRemotes := []refs.Remote{{Name: "origin", Branches: []refs.Branch{
		{Name: "feature/x", Remote: "origin", Hash: first},
		{Name: "main", Remote: "origin", Hash: first},
	}}}
	if !reflect.DeepEqual(got.Remotes, wantRemotes) {
		t.Fatalf("remotes = %+v", got.Remotes)
	}
	wantTags := []refs.Tag{{Name: "light", Hash: first}, {Name: "v1.0", Hash: first}}
	if !reflect.DeepEqual(got.Tags, wantTags) {
		t.Fatalf("tags = %+v", got.Tags)
	}
}

func TestListDetached(t *testing.T) {
	r := testrepo.New(t)
	a := r.Commit("a")
	r.Commit("b")
	r.Git("switch", "-q", "--detach", a)

	got, err := refs.List(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Detached || got.Head != "" || got.HeadHash != a {
		t.Fatalf("got %+v", got)
	}
	if label := refs.CurrentLabel(ctx, r.Dir); label != a[:7] {
		t.Fatalf("label = %q", label)
	}
}

func TestListEmptyRepo(t *testing.T) {
	r := testrepo.New(t)
	got, err := refs.List(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Head != "main" || got.HeadHash != "" || got.Detached || len(got.Local) != 0 || got.Local == nil {
		t.Fatalf("got %+v", got)
	}
}

func TestFingerprintAndLabel(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("a")
	if label := refs.CurrentLabel(ctx, r.Dir); label != "main" {
		t.Fatalf("label = %q", label)
	}

	before, err := refs.Fingerprint(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	same, _ := refs.Fingerprint(ctx, r.Dir)
	if same != before {
		t.Fatal("fingerprint not stable")
	}
	r.Git("branch", "other")
	after, _ := refs.Fingerprint(ctx, r.Dir)
	if after == before {
		t.Fatal("fingerprint did not change after creating a branch")
	}
	r.Git("switch", "-q", "other")
	switched, _ := refs.Fingerprint(ctx, r.Dir)
	if switched == after {
		t.Fatal("fingerprint did not change after switching branch")
	}
}

func TestParseTrack(t *testing.T) {
	cases := []struct {
		in            string
		ahead, behind int
		gone          bool
	}{
		{"", 0, 0, false},
		{"ahead 2", 2, 0, false},
		{"behind 3", 0, 3, false},
		{"ahead 2, behind 3", 2, 3, false},
		{"gone", 0, 0, true},
	}
	for _, c := range cases {
		a, b, g := refs.ParseTrack(c.in)
		if a != c.ahead || b != c.behind || g != c.gone {
			t.Errorf("ParseTrack(%q) = %d, %d, %v; want %d, %d, %v", c.in, a, b, g, c.ahead, c.behind, c.gone)
		}
	}
}

func TestListCountsAheadBehindGoneAndLocalUpstreams(t *testing.T) {
	src := testrepo.New(t)
	src.Commit("first")
	src.Git("branch", "feature")
	src.Git("branch", "doomed")
	bare := testrepo.NewBareFrom(t, src)
	r := testrepo.Clone(t, bare)
	r.Git("branch", "--track", "feature", "origin/feature")
	r.Git("branch", "--track", "doomed", "origin/doomed")
	r.Git("branch", "--track", "child", "main")
	r.Commit("local on main")

	other := testrepo.Clone(t, bare)
	other.Git("switch", "-q", "feature")
	other.Commit("remote on feature")
	other.Git("push", "-q", "origin", "feature")
	other.Git("push", "-q", "origin", "--delete", "doomed")
	r.Git("fetch", "-q", "--prune")

	got, err := refs.List(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]refs.Branch{}
	for _, b := range got.Local {
		by[b.Name] = b
	}
	if b := by["main"]; b.Ahead != 1 || b.Behind != 0 || b.UpstreamGone || b.UpstreamLocal {
		t.Errorf("main = %+v, want 1 ahead", b)
	}
	if b := by["feature"]; b.Ahead != 0 || b.Behind != 1 {
		t.Errorf("feature = %+v, want 1 behind", b)
	}
	if b := by["doomed"]; !b.UpstreamGone || b.Ahead != 0 || b.Behind != 0 {
		t.Errorf("doomed = %+v, want upstream gone", b)
	}
	if b := by["child"]; !b.UpstreamLocal || b.Behind != 1 {
		t.Errorf("child = %+v, want a local upstream 1 behind", b)
	}
}
