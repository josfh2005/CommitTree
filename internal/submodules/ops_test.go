package submodules_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"git-ui/internal/submodules"
	"git-ui/internal/testrepo"
)

// setFileTransport allows `git submodule` to clone from a local path, the
// same GIT_CONFIG_* override the Global Constraints prescribe for cloning in
// tests.
func setFileTransport(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "protocol.file.allow")
	t.Setenv("GIT_CONFIG_VALUE_0", "always")
}

func TestInitInitialisesAnUninitialisedSubmodule(t *testing.T) {
	setFileTransport(t)
	parent, _ := withSub(t, "lib")

	// Clone without --recurse-submodules so lib starts uninitialised, as
	// internal/submodules.TestListUninitialised does.
	bareClone := testrepo.Clone(t, parent.Dir)

	list, err := submodules.List(context.Background(), bareClone.Dir)
	if err != nil {
		t.Fatal(err)
	}
	before := find(t, list, "lib")
	if before.Initialised {
		t.Fatalf("expected lib to start uninitialised: %+v", before)
	}

	if err := submodules.Init(context.Background(), bareClone.Dir, "lib"); err != nil {
		t.Fatal(err)
	}

	list, err = submodules.List(context.Background(), bareClone.Dir)
	if err != nil {
		t.Fatal(err)
	}
	after := find(t, list, "lib")
	if !after.Initialised || after.CheckedOut != after.Recorded {
		t.Fatalf("after init: %+v", after)
	}
}

func TestUpdateMovesBackToRecorded(t *testing.T) {
	setFileTransport(t)
	parent, _ := withSub(t, "lib")
	sub := filepath.Join(parent.Dir, "lib")
	run := func(args ...string) {
		parent.Git(append([]string{"-C", sub, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
	}
	os.WriteFile(filepath.Join(sub, "a.txt"), []byte("moved"), 0o644)
	run("commit", "-q", "-am", "moved on")

	list, err := submodules.List(context.Background(), parent.Dir)
	if err != nil {
		t.Fatal(err)
	}
	moved := find(t, list, "lib")
	if !moved.Moved {
		t.Fatalf("expected moved before Update: %+v", moved)
	}

	if err := submodules.Update(context.Background(), parent.Dir, "lib"); err != nil {
		t.Fatal(err)
	}

	list, err = submodules.List(context.Background(), parent.Dir)
	if err != nil {
		t.Fatal(err)
	}
	after := find(t, list, "lib")
	if after.Moved || after.CheckedOut != after.Recorded {
		t.Fatalf("after update: %+v", after)
	}
}

func TestUpdateWithDirtyFileReturnsErrDirty(t *testing.T) {
	setFileTransport(t)
	parent, _ := withSub(t, "lib")
	sub := filepath.Join(parent.Dir, "lib")
	run := func(args ...string) {
		parent.Git(append([]string{"-C", sub, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
	}
	// Move the submodule forward and dirty the same tracked file, so
	// checking back out to the recorded commit would overwrite it.
	os.WriteFile(filepath.Join(sub, "a.txt"), []byte("moved"), 0o644)
	run("commit", "-q", "-am", "moved on")
	os.WriteFile(filepath.Join(sub, "a.txt"), []byte("dirty"), 0o644)

	err := submodules.Update(context.Background(), parent.Dir, "lib")
	if err == nil {
		t.Fatal("expected an error")
	}
	var derr *submodules.ErrDirty
	if !errors.As(err, &derr) {
		t.Fatalf("expected *ErrDirty, got %v (%T)", err, err)
	}
	if derr.Path != "lib" {
		t.Fatalf("ErrDirty.Path = %q", derr.Path)
	}
}

func TestSyncUpdatesTheRemoteURL(t *testing.T) {
	setFileTransport(t)
	parent, lib := withSub(t, "lib")
	newURL := lib.Dir + string(filepath.Separator) // a distinct-but-valid URL for the same remote
	parent.Git("config", "-f", ".gitmodules", "submodule.lib.url", newURL)

	if err := submodules.Sync(context.Background(), parent.Dir, "lib"); err != nil {
		t.Fatal(err)
	}

	got := parent.Git("-C", filepath.Join(parent.Dir, "lib"), "config", "remote.origin.url")
	if got != newURL && got+string(filepath.Separator) != newURL {
		// git may or may not preserve the trailing separator; compare loosely.
		if filepath.Clean(got) != filepath.Clean(newURL) {
			t.Fatalf("remote.origin.url = %q, want %q", got, newURL)
		}
	}
}

func TestPathWithSpaceUsesLiteralPathspec(t *testing.T) {
	setFileTransport(t)
	parent, _ := withSub(t, "third party/lib")

	bareClone := testrepo.Clone(t, parent.Dir)
	if err := submodules.Init(context.Background(), bareClone.Dir, "third party/lib"); err != nil {
		t.Fatal(err)
	}
	list, err := submodules.List(context.Background(), bareClone.Dir)
	if err != nil {
		t.Fatal(err)
	}
	s := find(t, list, "third party/lib")
	if !s.Initialised {
		t.Fatalf("expected initialised: %+v", s)
	}
}
