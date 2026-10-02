package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"git-ui/internal/cmdlog"
	"git-ui/internal/gitcmd"
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
