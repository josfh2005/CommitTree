package ops_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
	"time"

	"git-ui/internal/gitcmd"
	"git-ui/internal/ops"
	"git-ui/internal/testrepo"
)

// tracked returns a repo whose main tracks origin/main on a bare remote, and
// a clone of that remote to push new commits from.
func tracked(t *testing.T) (r, other *testrepo.Repo) {
	t.Helper()
	r = testrepo.New(t)
	r.Commit("base")
	bare := testrepo.NewBareFrom(t, r)
	r.Git("remote", "add", "origin", bare)
	r.Git("fetch", "-q", "origin")
	r.Git("branch", "-q", "--set-upstream-to=origin/main", "main")
	return r, testrepo.Clone(t, bare)
}

func TestAutoFetchCountsCommitsTheFetchBrought(t *testing.T) {
	r, other := tracked(t)
	other.Commit("one")
	other.Commit("two")
	other.Git("push", "-q", "origin", "main")

	res, err := ops.AutoFetch(context.Background(), r.Dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := ops.AutoFetchResult{Branch: "main", Upstream: "origin/main", NewCommits: 2, RefsChanged: true}
	if !reflect.DeepEqual(res, want) {
		t.Errorf("res = %+v, want %+v", res, want)
	}
}

func TestAutoFetchAfterAManualFetchReportsNothing(t *testing.T) {
	r, other := tracked(t)
	other.Commit("one")
	other.Git("push", "-q", "origin", "main")
	r.Git("fetch", "-q", "origin") // the user's manual Fetch

	res, err := ops.AutoFetch(context.Background(), r.Dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.NewCommits != 0 || res.RefsChanged {
		t.Errorf("res = %+v, want nothing new", res)
	}
}

func TestAutoFetchIgnoresCommitsHEADAlreadyHas(t *testing.T) {
	r, _ := tracked(t)
	r.Commit("mine")
	bare := r.Git("remote", "get-url", "origin")
	// Update the remote's main without moving r's origin/main, as a push
	// from another clone of the same work would.
	r.Git("push", "-q", bare, "main:main")

	res, err := ops.AutoFetch(context.Background(), r.Dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.NewCommits != 0 || !res.RefsChanged {
		t.Errorf("res = %+v, want 0 new commits but refs changed", res)
	}
}

func TestAutoFetchDetachedOrNoUpstream(t *testing.T) {
	r, other := tracked(t)
	other.Commit("one")
	other.Git("push", "-q", "origin", "main")
	r.Git("switch", "-q", "--detach", "HEAD")

	res, err := ops.AutoFetch(context.Background(), r.Dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Branch != "" || res.Upstream != "" || res.NewCommits != 0 || !res.RefsChanged {
		t.Errorf("res = %+v", res)
	}
}

func TestAutoFetchSkipsARepositoryWithNoRemote(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	res, err := ops.AutoFetch(context.Background(), r.Dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Skipped {
		t.Errorf("res = %+v, want Skipped", res)
	}
}

// lockedServer is a remote that always asks for credentials.
func lockedServer(t *testing.T) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="x"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/repo.git"
}

// hermetic keeps the developer's credential helpers and URL rewrites out.
func hermetic(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

func TestAutoFetchNeverPromptsAndKeepsFetchingTheOtherRemotes(t *testing.T) {
	hermetic(t)
	r, other := tracked(t)
	r.Git("remote", "add", "locked", lockedServer(t))
	other.Commit("one")
	other.Git("push", "-q", "origin", "main")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	start := time.Now()
	res, err := ops.AutoFetch(ctx, r.Dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 15*time.Second {
		t.Errorf("took %v: something waited for input", d)
	}
	if !reflect.DeepEqual(res.AuthFailed, []string{"locked"}) {
		t.Errorf("AuthFailed = %v, want [locked]", res.AuthFailed)
	}
	if res.NewCommits != 1 {
		t.Errorf("NewCommits = %d, want 1 from origin", res.NewCommits)
	}
}

func TestAutoFetchSkipsPausedRemotes(t *testing.T) {
	r, other := tracked(t)
	other.Commit("one")
	other.Git("push", "-q", "origin", "main")
	res, err := ops.AutoFetch(context.Background(), r.Dir, func(remote string) bool { return remote == "origin" })
	if err != nil || !res.Skipped || res.NewCommits != 0 {
		t.Errorf("res = %+v, err = %v; want Skipped with nothing fetched", res, err)
	}
}

func TestIsAuthError(t *testing.T) {
	for _, stderr := range []string{
		"fatal: Authentication failed for 'https://x/'",
		"fatal: could not read Username for 'https://x': terminal prompts disabled",
		"fatal: could not read Password for 'https://x'",
		"git@x: Permission denied (publickey).",
		"Host key verification failed.",
	} {
		if !ops.IsAuthError(&gitcmd.Error{Stderr: stderr}) {
			t.Errorf("IsAuthError(%q) = false", stderr)
		}
	}
	if ops.IsAuthError(&gitcmd.Error{Stderr: "fatal: unable to access 'https://x/': Could not resolve host: x"}) {
		t.Error("a network error counted as auth")
	}
	if ops.IsAuthError(errors.New("plain")) {
		t.Error("a non-git error counted as auth")
	}
}
