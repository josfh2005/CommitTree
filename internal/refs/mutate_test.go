package refs_test

import (
	"errors"
	"testing"

	"git-ui/internal/gitcmd"
	"git-ui/internal/refs"
	"git-ui/internal/testrepo"
)

func TestCreateBranchAndCheckout(t *testing.T) {
	r := testrepo.New(t)
	first := r.Commit("first")
	r.Commit("second")

	if err := refs.CreateBranch(ctx, r.Dir, "feature/a", first, true); err != nil {
		t.Fatal(err)
	}
	if head := r.Git("rev-parse", "HEAD"); head != first {
		t.Fatalf("HEAD = %s", head)
	}
	if name := r.Git("symbolic-ref", "--short", "HEAD"); name != "feature/a" {
		t.Fatalf("branch = %s", name)
	}
}

func TestCreateBranchRejectsInvalidName(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("first")
	for _, name := range []string{"bad..name", "-x", ""} {
		if err := refs.CreateBranch(ctx, r.Dir, name, "HEAD", false); !errors.Is(err, refs.ErrInvalidName) {
			t.Errorf("%q: want ErrInvalidName, got %v", name, err)
		}
	}
}

func TestDeleteBranchNotMergedThenForce(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("switch", "-q", "-c", "topic")
	r.Commit("topic work")
	r.Git("switch", "-q", "main")

	err := refs.DeleteBranch(ctx, r.Dir, "topic", false)
	if !errors.Is(err, refs.ErrNotMerged) {
		t.Fatalf("want ErrNotMerged, got %v", err)
	}
	if err := refs.DeleteBranch(ctx, r.Dir, "topic", true); err != nil {
		t.Fatal(err)
	}
	if _, err := gitcmd.Run(ctx, r.Dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", "refs/heads/topic"); err == nil {
		t.Fatal("topic still exists")
	}
}

func TestDeleteCurrentBranchRefused(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	if err := refs.DeleteBranch(ctx, r.Dir, "main", true); !errors.Is(err, refs.ErrCurrentBranch) {
		t.Fatalf("want ErrCurrentBranch, got %v", err)
	}
}

func TestCreateAndDeleteTags(t *testing.T) {
	r := testrepo.New(t)
	h := r.Commit("base")

	if err := refs.CreateTag(ctx, r.Dir, "v1", h, ""); err != nil {
		t.Fatal(err)
	}
	if err := refs.CreateTag(ctx, r.Dir, "v2", h, "release notes"); err != nil {
		t.Fatal(err)
	}
	if kind := r.Git("cat-file", "-t", "v2"); kind != "tag" {
		t.Fatalf("v2 is %s, want annotated tag", kind)
	}
	if err := refs.CreateTag(ctx, r.Dir, "-bad", h, ""); !errors.Is(err, refs.ErrInvalidName) {
		t.Fatalf("want ErrInvalidName, got %v", err)
	}
	if err := refs.DeleteTag(ctx, r.Dir, "v1"); err != nil {
		t.Fatal(err)
	}
	list, _ := refs.List(ctx, r.Dir)
	if len(list.Tags) != 1 || list.Tags[0].Name != "v2" {
		t.Fatalf("tags = %+v", list.Tags)
	}
}

func TestDeleteRemoteBranch(t *testing.T) {
	src := testrepo.New(t)
	src.Commit("base")
	src.Git("branch", "old")
	bare := testrepo.NewBareFrom(t, src)
	r := testrepo.Clone(t, bare)

	if err := refs.DeleteRemoteBranch(ctx, r.Dir, "origin", "old"); err != nil {
		t.Fatal(err)
	}
	out, err := gitcmd.Run(ctx, bare, gitcmd.ReadTimeout, "branch", "--list", "old")
	if err != nil || out != "" {
		t.Fatalf("remote still has old: %q, err %v", out, err)
	}
}
