package app

import (
	"context"
	"path/filepath"
	"testing"

	"git-ui/internal/gitsettings"
	"git-ui/internal/ops"
	"git-ui/internal/repos"
	"git-ui/internal/testrepo"
)

// newPlainApp builds an App over a fresh one-commit repository, with no
// merge scaffolding — for tests that don't want newAIMergeApp's conflicting
// history.
//
// WithAI is NOT optional here, even though these tests use no AI: App.emit
// falls through to wails runtime.EventsEmit when a.ai is nil, and that
// runtime's getEvents calls log.Fatalf on a context with no "events" value —
// os.Exit(1) in the middle of the suite, taking every other test in
// internal/app with it. Every stash mutation goes through writeMerge or
// writeWorktree, both of which emit. Giving it an events sink (the same
// `events` helper newAIMergeApp uses, defined in ai_test.go) is what keeps
// `go test ./internal/app/` alive. gitSettingsPath is likewise always set to
// a temp file so no test ever reads or writes the developer's real
// ~/Library/Application Support/git-ui/git.json.
func newPlainApp(t *testing.T) (a *App, r *testrepo.Repo, id string) {
	t.Helper()
	dir := t.TempDir()
	store, err := repos.Open(filepath.Join(dir, "repos.json"))
	if err != nil {
		t.Fatal(err)
	}
	a = New(store)
	WithAI(a, AIDeps{Emit: newEvents().emit})
	a.gitSettingsPath = filepath.Join(dir, "git.json")
	r = testrepo.New(t)
	r.Commit("base")
	repo, err := store.Add(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	return a, r, repo.ID
}

func TestPushSetsUpstreamThroughTheAppLayer(t *testing.T) {
	a, r, id := newPlainApp(t)
	bare := testrepo.NewBareFrom(t, r)
	r.Git("remote", "add", "origin", bare)
	r.Git("switch", "-q", "-c", "topic")
	r.Commit("on topic")

	if err := a.Push(id); err != nil {
		t.Fatal(err)
	}
	if up := r.Git("rev-parse", "--abbrev-ref", "topic@{upstream}"); up != "origin/topic" {
		t.Errorf("upstream = %q, want origin/topic", up)
	}
}

func TestPullThroughTheAppLayer(t *testing.T) {
	a, r, id := newPlainApp(t)
	bare := testrepo.NewBareFrom(t, r)
	r.Git("remote", "add", "origin", bare)
	r.Git("push", "-q", "-u", "origin", "main")

	clone := testrepo.Clone(t, bare)
	clone.Commit("from clone")
	clone.Git("push", "-q", "origin", "main")

	result, err := a.Pull(id)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != ops.Merged {
		t.Errorf("outcome = %v, want Merged", result.Outcome)
	}
}

func TestGetRemoteInfoWithNoUpstream(t *testing.T) {
	a, _, id := newPlainApp(t)
	info, err := a.GetRemoteInfo(id)
	if err != nil {
		t.Fatal(err)
	}
	if info.Ahead != 0 || info.Behind != 0 {
		t.Errorf("info = %+v, want zero with no upstream", info)
	}
}

func TestGitSettingsDefaultAndSaveRoundTrip(t *testing.T) {
	a, _, _ := newPlainApp(t) // gitSettingsPath is already a temp file

	got, err := a.GetGitSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.PullStrategy != gitsettings.PullAuto {
		t.Errorf("default = %+v, want auto", got)
	}
	if err := a.SaveGitSettings(gitsettings.Settings{PullStrategy: gitsettings.PullRebase}); err != nil {
		t.Fatal(err)
	}
	got, err = a.GetGitSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.PullStrategy != gitsettings.PullRebase {
		t.Errorf("after save = %+v, want rebase", got)
	}
}
