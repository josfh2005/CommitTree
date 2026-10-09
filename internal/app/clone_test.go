package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git-ui/internal/clone"
	"git-ui/internal/testrepo"
)

func cloneApp(t *testing.T) (*App, chan CloneDone) {
	t.Helper()
	a, _ := newTestApp(t)
	a.ctx = context.Background()
	done := make(chan CloneDone, 4)
	WithAI(a, AIDeps{Emit: func(name string, data any) {
		if name == "clone:done" {
			done <- data.(CloneDone)
		}
	}})
	return a, done
}

func waitDone(t *testing.T, ch chan CloneDone) CloneDone {
	t.Helper()
	select {
	case d := <-ch:
		return d
	case <-time.After(30 * time.Second):
		t.Fatal("no clone:done")
		return CloneDone{}
	}
}

func TestCloneRepoAddsTheRepository(t *testing.T) {
	a, done := cloneApp(t)
	src := testrepo.New(t)
	src.Commit("base")
	bare := testrepo.NewBareFrom(t, src)
	parent := t.TempDir()

	if err := a.CloneRepo("file://"+bare, parent, "copy"); err != nil {
		t.Fatal(err)
	}
	d := waitDone(t, done)
	if d.Repo == nil || d.Error != "" || d.Cancelled {
		t.Fatalf("done = %+v", d)
	}
	if d.Repo.Path != canonical(filepath.Join(parent, "copy")) {
		t.Fatalf("path = %q", d.Repo.Path)
	}
	if _, ok := a.store.Get(d.Repo.ID); !ok {
		t.Fatal("repo not in the store")
	}
	if s := a.CloneStatus(); s.Running || s.LastError != "" {
		t.Fatalf("status = %+v", s)
	}
}

func TestCloneRepoExpandsATypedParent(t *testing.T) {
	a, done := cloneApp(t)
	src := testrepo.New(t)
	src.Commit("base")
	bare := testrepo.NewBareFrom(t, src)
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.Mkdir(filepath.Join(home, "code"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := a.CloneRepo("file://"+bare, "  ~/code ", "copy"); err != nil {
		t.Fatal(err)
	}
	d := waitDone(t, done)
	if d.Repo == nil || d.Error != "" {
		t.Fatalf("done = %+v", d)
	}
	if d.Repo.Path != canonical(filepath.Join(home, "code", "copy")) {
		t.Fatalf("path = %q", d.Repo.Path)
	}
	if err := a.CloneRepo("file://"+bare, "code", "other"); !errors.Is(err, clone.ErrParentNotAbsolute) {
		t.Fatalf("relative parent err = %v", err)
	}
}

func TestCloneRepoOneAtATimeAndCancel(t *testing.T) {
	t.Setenv("GIT_SSH_COMMAND", "sleep 30;:")
	a, done := cloneApp(t)
	parent := t.TempDir()

	if err := a.CloneRepo("ssh://example.invalid/x.git", parent, "x"); err != nil {
		t.Fatal(err)
	}
	if s := a.CloneStatus(); !s.Running || s.Dest != filepath.Join(parent, "x") {
		t.Fatalf("status = %+v", s)
	}
	if err := a.CloneRepo("ssh://example.invalid/y.git", parent, "y"); !errors.Is(err, ErrCloneRunning) {
		t.Fatalf("second clone err = %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	a.CancelClone()
	d := waitDone(t, done)
	if !d.Cancelled || d.Error != "" || d.Repo != nil {
		t.Fatalf("done = %+v", d)
	}
	if _, err := os.Stat(filepath.Join(parent, "x")); !os.IsNotExist(err) {
		t.Fatalf("dest left behind: %v", err)
	}
	if s := a.CloneStatus(); s.Running {
		t.Fatalf("still running: %+v", s)
	}
}

func TestCloneRepoValidatesFirst(t *testing.T) {
	a, done := cloneApp(t)
	parent := t.TempDir()
	os.MkdirAll(filepath.Join(parent, "full", "x"), 0o755)

	if err := a.CloneRepo("file:///nowhere", parent, "full"); !errors.Is(err, clone.ErrDestNotEmpty) {
		t.Fatalf("err = %v", err)
	}
	if err := a.CloneRepo("-x", parent, "new"); !errors.Is(err, clone.ErrBadURL) {
		t.Fatalf("err = %v", err)
	}
	select {
	case d := <-done:
		t.Fatalf("unexpected clone:done %+v", d)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestCloneRepoFailureIsKeptInStatus(t *testing.T) {
	a, done := cloneApp(t)
	missing := filepath.Join(t.TempDir(), "nope")
	if err := a.CloneRepo(missing, t.TempDir(), "x"); err != nil {
		t.Fatal(err)
	}
	d := waitDone(t, done)
	want := "Repository not found at " + missing + "."
	if d.Error != want || a.CloneStatus().LastError != want {
		t.Fatalf("done = %+v, status = %+v", d, a.CloneStatus())
	}
}

func TestCloneRepoPartialAddsTheRepositoryWithTheError(t *testing.T) {
	a, done := cloneApp(t)
	sub := testrepo.New(t)
	sub.Commit("sub base")
	subBare := testrepo.NewBareFrom(t, sub)
	top := testrepo.New(t)
	top.Commit("base")
	top.Git("-c", "protocol.file.allow=always", "submodule", "add", "-q", "file://"+subBare, "lib")
	top.Git("commit", "-q", "-m", "add lib")
	bare := testrepo.NewBareFrom(t, top)
	if err := os.RemoveAll(subBare); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "protocol.file.allow")
	t.Setenv("GIT_CONFIG_VALUE_0", "always")
	parent := t.TempDir()

	if err := a.CloneRepo("file://"+bare, parent, "top"); err != nil {
		t.Fatal(err)
	}
	d := waitDone(t, done)
	if d.Repo == nil || d.Error == "" || d.Cancelled {
		t.Fatalf("done = %+v", d)
	}
	if !strings.HasPrefix(d.Error, "Cloned, but some submodules or files could not be checked out: ") {
		t.Fatalf("error = %q", d.Error)
	}
	if _, ok := a.store.Get(d.Repo.ID); !ok {
		t.Fatal("repo not in the store")
	}
	if s := a.CloneStatus(); s.Running || s.LastError != d.Error {
		t.Fatalf("status = %+v", s)
	}
}

func TestCloneStatusShowsTheURLWithoutCredentials(t *testing.T) {
	t.Setenv("GIT_SSH_COMMAND", "sleep 30;:")
	a, done := cloneApp(t)
	if err := a.CloneRepo("https://user:tok3n@127.0.0.1:1/x.git", t.TempDir(), "x"); err != nil {
		t.Fatal(err)
	}
	if s := a.CloneStatus(); !s.Running || strings.Contains(s.URL, "tok3n") || s.URL == "" {
		t.Fatalf("status = %+v", s)
	}
	a.CancelClone()
	waitDone(t, done)
}

func TestCloneRepoRunningIsCheckedBeforeValidating(t *testing.T) {
	t.Setenv("GIT_SSH_COMMAND", "sleep 30;:")
	a, done := cloneApp(t)
	parent := t.TempDir()
	if err := a.CloneRepo("ssh://example.invalid/x.git", parent, "x"); err != nil {
		t.Fatal(err)
	}
	if err := a.CloneRepo("-x", parent, ""); !errors.Is(err, ErrCloneRunning) {
		t.Fatalf("err = %v, want ErrCloneRunning", err)
	}
	a.CancelClone()
	waitDone(t, done)
}

func TestShutdownCancelsARunningClone(t *testing.T) {
	t.Setenv("GIT_SSH_COMMAND", "sleep 30;:")
	a, done := cloneApp(t)
	if err := a.CloneRepo("ssh://example.invalid/x.git", t.TempDir(), "x"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	a.Shutdown(context.Background())
	if d := waitDone(t, done); !d.Cancelled {
		t.Fatalf("done = %+v", d)
	}
}

func TestProgressThrottleNeverDropsAPhasesLastUpdate(t *testing.T) {
	now := time.Now()
	same := clone.Progress{Phase: "Receiving objects", Percent: 50}
	if !throttled(same, "Receiving objects", now) {
		t.Error("a quick update of the same phase should be dropped")
	}
	if throttled(clone.Progress{Phase: "Receiving objects", Percent: 100}, "Receiving objects", now) {
		t.Error("100% must be sent")
	}
	if throttled(same, "Counting objects", now) {
		t.Error("a new phase must be sent")
	}
	if throttled(same, "Receiving objects", now.Add(-time.Second)) {
		t.Error("an update after the interval must be sent")
	}
}
