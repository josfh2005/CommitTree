package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"git-ui/internal/cmdlog"
	"git-ui/internal/gitcmd"
	"git-ui/internal/ops"
	"git-ui/internal/testrepo"
)

func lockedRemote(t *testing.T) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="x"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/repo.git"
}

func TestAutoFetchPausesOnlyTheRemoteThatNeedsCredentials(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	a, r, id := newPlainApp(t)
	bare := testrepo.NewBareFrom(t, r)
	r.Git("remote", "add", "origin", bare)
	r.Git("remote", "add", "locked", lockedRemote(t))
	dir, err := a.dir(id) // the store's path (macOS resolves /var to /private/var)
	if err != nil {
		t.Fatal(err)
	}
	key := cmdlog.RepoKey(dir)

	if _, err := a.AutoFetch(id); err != nil {
		t.Fatal(err)
	}
	if !a.paused.paused(key, "locked") || a.paused.paused(key, "origin") {
		t.Fatal("want locked paused and origin not")
	}

	// A successful Fetch by the user unpauses the repository.
	r.Git("remote", "set-url", "locked", bare)
	if err := a.Fetch(id); err != nil {
		t.Fatal(err)
	}
	if a.paused.paused(key, "locked") {
		t.Error("a successful Fetch left locked paused")
	}
}

func TestOnlyASuccessfulNonAutoRemoteUpdateUnpauses(t *testing.T) {
	a, r, _ := newPlainApp(t)
	key := cmdlog.RepoKey(r.Dir)
	rec := func(ctx context.Context, exit int, args ...string) gitcmd.Record {
		return gitcmd.Record{Ctx: ctx, Dir: r.Dir, Args: args, ExitCode: exit}
	}
	auto := cmdlog.WithOrigin(context.Background(), cmdlog.OriginAuto)
	for _, c := range []gitcmd.Record{
		rec(context.Background(), 0, "fetch", ".", "origin/main:refs/heads/main"),
		rec(context.Background(), 1, "fetch", "--all", "--prune"),
		rec(auto, 0, "fetch", "--prune", "origin"),
		rec(context.Background(), 0, "push"),
	} {
		a.paused.pause(key, []string{"origin"})
		a.noteRemoteUpdate(c)
		if !a.paused.paused(key, "origin") {
			t.Errorf("%v (exit %d) unpaused", c.Args, c.ExitCode)
		}
	}
	a.noteRemoteUpdate(rec(context.Background(), 0, "pull", "--"))
	if a.paused.paused(key, "origin") {
		t.Error("a successful pull did not unpause")
	}
}

// conflictingClone sets up r with origin, and pushes a commit to origin that
// conflicts with a local commit of r on the same file.
func conflictingClone(t *testing.T, r *testrepo.Repo) {
	t.Helper()
	bare := testrepo.NewBareFrom(t, r)
	r.Git("remote", "add", "origin", bare)
	r.Git("push", "-q", "-u", "origin", "main")
	clone := testrepo.Clone(t, bare)
	clone.WriteFile("same.txt", "theirs\n")
	clone.Git("add", "same.txt")
	clone.Git("commit", "-q", "-m", "theirs")
	clone.Git("push", "-q", "origin", "main")
	r.WriteFile("same.txt", "ours\n")
	r.Git("add", "same.txt")
	r.Git("commit", "-q", "-m", "ours")
	// The default "auto" strategy runs a plain pull; without this, git
	// refuses divergent branches before merging and there is no conflict.
	r.Git("config", "pull.rebase", "false")
}

func TestAConflictedPullUnpauses(t *testing.T) {
	a, r, id := newPlainApp(t)
	conflictingClone(t, r)
	dir, err := a.dir(id)
	if err != nil {
		t.Fatal(err)
	}
	key := cmdlog.RepoKey(dir)
	a.paused.pause(key, []string{"origin"})

	res, err := a.Pull(id)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != ops.Conflicted {
		t.Fatalf("outcome = %v, want Conflicted", res.Outcome)
	}
	if a.paused.paused(key, "origin") {
		t.Error("a pull that reached the remote and stopped on conflicts left origin paused")
	}
}

func TestAFailedPullKeepsThePause(t *testing.T) {
	a, r, id := newPlainApp(t)
	r.Git("remote", "add", "origin", filepath.Join(t.TempDir(), "missing.git"))
	r.Git("config", "branch.main.remote", "origin")
	r.Git("config", "branch.main.merge", "refs/heads/main")
	dir, err := a.dir(id)
	if err != nil {
		t.Fatal(err)
	}
	key := cmdlog.RepoKey(dir)
	a.paused.pause(key, []string{"origin"})

	if _, err := a.Pull(id); err == nil {
		t.Fatal("want the pull to fail")
	}
	if !a.paused.paused(key, "origin") {
		t.Error("a failed pull unpaused origin")
	}
}
