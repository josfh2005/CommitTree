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
	wantLocal := []refs.Branch{{Name: "main", Hash: first, Current: true, Upstream: "origin/main"}}
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
