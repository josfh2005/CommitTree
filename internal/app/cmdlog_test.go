package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"git-ui/internal/ai"
	"git-ui/internal/ai/tools"
	"git-ui/internal/cmdlog"
	"git-ui/internal/gitcmd"
	"git-ui/internal/gitlog"
)

func TestCommandLogRecordsAppCommands(t *testing.T) {
	a, id := newTestApp(t)
	if err := a.CreateBranch(id, "logged", "HEAD", false); err != nil {
		t.Fatal(err)
	}
	view, err := a.CommandLog(id)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := a.dir(id)
	if err != nil {
		t.Fatal(err)
	}
	if view.Repo != cmdlog.RepoKey(dir) {
		t.Fatalf("view.Repo = %q, want %q", view.Repo, cmdlog.RepoKey(dir))
	}
	list := view.Entries
	var found *cmdlog.Entry
	for i, e := range list {
		if e.Kind == cmdlog.KindWrite && len(e.Args) > 0 && e.Args[0] == "branch" {
			found = &list[i]
		}
	}
	if found == nil || found.Origin != cmdlog.OriginYou || found.Outcome != cmdlog.OutcomeOK {
		t.Fatalf("branch command not logged as a user write: %+v", list)
	}
	if _, err := a.CommandLogOutput(id, found.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CommandLogOutput(id, -1); !errors.Is(err, cmdlog.ErrNotFound) {
		t.Fatalf("unknown id: got %v", err)
	}
	if err := a.ClearCommandLog(id); err != nil {
		t.Fatal(err)
	}
	if view, _ := a.CommandLog(id); len(view.Entries) != 0 {
		t.Fatalf("not cleared: %d", len(view.Entries))
	}
}

func TestWriteDuringAIExecutionIsAI(t *testing.T) {
	a, id := newTestApp(t)
	done := a.markAIWrite(id)
	err := a.CreateBranch(id, "by-ai", "HEAD", false)
	done()
	if err != nil {
		t.Fatal(err)
	}
	if err := a.CreateBranch(id, "by-user", "HEAD", false); err != nil {
		t.Fatal(err)
	}
	view, _ := a.CommandLog(id)
	origins := map[string]cmdlog.Origin{}
	for _, e := range view.Entries {
		if e.Kind == cmdlog.KindWrite && len(e.Args) > 1 && e.Args[0] == "branch" {
			origins[e.Args[1]] = e.Origin // git branch <name> HEAD
		}
	}
	if origins["by-ai"] != cmdlog.OriginAI || origins["by-user"] != cmdlog.OriginYou {
		t.Fatalf("origins: %v", origins)
	}
}

func TestCommandEventOnlyAfterStartup(t *testing.T) {
	a, id := newTestApp(t)
	var mu sync.Mutex
	var n int
	a.cmdEmit = func(cmdlog.Entry) { mu.Lock(); n++; mu.Unlock() }
	// Before Startup nothing may be emitted: Wails' EventsEmit on a
	// non-Wails ctx calls log.Fatalf.
	a.started.Store(false)
	a.GetRefs(id)
	mu.Lock()
	before := n
	mu.Unlock()
	a.started.Store(true)
	a.GetRefs(id)
	mu.Lock()
	defer mu.Unlock()
	if before != 0 || n == 0 {
		t.Fatalf("before=%d after=%d", before, n)
	}
}

// TestRefreshPathsLogNoWrites guards against read-only refresh commands
// (e.g. "git remote", "git check-ref-format") being misclassified as
// writes, which would flood the default (reads-hidden) view with rows.
func TestRefreshPathsLogNoWrites(t *testing.T) {
	a, id := newTestApp(t)
	if err := a.ClearCommandLog(id); err != nil {
		t.Fatal(err)
	}
	if _, err := a.GetRefs(id); err != nil {
		t.Fatal(err)
	}
	if _, err := a.GetLog(id, gitlog.Filters{}, gitlog.OrderTopo, 0, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := a.GetWorktreeState(id); err != nil {
		t.Fatal(err)
	}
	view, err := a.CommandLog(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range view.Entries {
		if e.Kind == cmdlog.KindWrite {
			t.Fatalf("refresh path logged a write: %+v", e)
		}
	}
}

func TestAIToolContextCarriesOrigin(t *testing.T) {
	ctx := aiToolContext(context.Background())
	if o, ok := cmdlog.OriginFrom(ctx); !ok || o != cmdlog.OriginAI {
		t.Fatalf("got %q %v", o, ok)
	}
}

// TestAIToolRunIsLoggedAsAI runs a read tool through the same path the chat
// agent uses (aiToolContext + tools.Run — see the RunTool closure in
// internal/app/ai.go) and checks the git commands it issues land in the
// command log tagged AI, not You or Auto.
func TestAIToolRunIsLoggedAsAI(t *testing.T) {
	a, id := newTestApp(t)
	dir, err := a.dir(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ClearCommandLog(id); err != nil {
		t.Fatal(err)
	}
	out := tools.Run(aiToolContext(context.Background()), dir, ai.ToolCall{Name: "list_refs"})
	if strings.HasPrefix(out, "error:") {
		t.Fatalf("tool call failed: %s", out)
	}
	view, err := a.CommandLog(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Entries) == 0 {
		t.Fatal("list_refs ran no git commands")
	}
	for _, e := range view.Entries {
		if e.Origin != cmdlog.OriginAI {
			t.Fatalf("entry not logged as AI: %+v", e)
		}
	}
}

func TestCancelCommandUnknownErrors(t *testing.T) {
	a, id := newTestApp(t)
	if err := a.CancelCommand(id, 123456); !errors.Is(err, cmdlog.ErrNotRunning) {
		t.Fatalf("got %v", err)
	}
}

func TestRunningEntryEmittedBeforeFinished(t *testing.T) {
	a, id := newTestApp(t)
	var mu sync.Mutex
	var got []cmdlog.Entry
	a.cmdEmit = func(e cmdlog.Entry) {
		mu.Lock()
		got = append(got, e)
		mu.Unlock()
	}
	a.started.Store(true)
	if err := a.CreateBranch(id, "running", "HEAD", false); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	var branch []cmdlog.Entry
	for _, e := range got {
		if len(e.Args) > 0 && e.Args[0] == "branch" {
			branch = append(branch, e)
		}
	}
	if len(branch) != 2 || branch[0].Outcome != cmdlog.OutcomeRunning || branch[1].Outcome != cmdlog.OutcomeOK || branch[0].ID != branch[1].ID {
		t.Fatalf("branch events: %+v", branch)
	}
	for _, e := range got {
		if e.Kind == cmdlog.KindRead && e.Outcome == cmdlog.OutcomeRunning {
			t.Fatalf("a running read was emitted: %+v", e)
		}
	}
}

// TestBeginGitReturnsIDEvenWhenEmitPanics covers M1: emitCommand can panic
// (Wails' EventsEmit calls log.Fatalf after the window has gone away), and
// that must not cost the caller the ID cmds.Begin already reserved and
// stored as running — losing it would leave that entry running forever,
// since gitcmd's own begin() recovers around the whole Recorder.Begin call
// and would otherwise turn the panic into a returned ID of 0.
func TestBeginGitReturnsIDEvenWhenEmitPanics(t *testing.T) {
	a, id := newTestApp(t)
	dir, err := a.dir(id)
	if err != nil {
		t.Fatal(err)
	}
	a.started.Store(true)
	a.cmdEmit = func(cmdlog.Entry) { panic("boom") }

	got := a.beginGit(gitcmd.Start{Ctx: context.Background(), Dir: dir, Args: []string{"branch", "x"}, Start: time.Now()})
	if got == 0 {
		t.Fatal("beginGit returned ID 0 despite a stored running entry")
	}

	a.cmdEmit = func(cmdlog.Entry) { panic("boom") }
	a.recordGit(gitcmd.Record{ID: got, Ctx: context.Background(), Dir: dir, Args: []string{"branch", "x"}, Start: time.Now()})
	// recordGit must not itself panic even though its own emit does.
}

func TestAIWriteMarkAppliesWhileRunning(t *testing.T) {
	a, id := newTestApp(t)
	var mu sync.Mutex
	var running []cmdlog.Entry
	a.cmdEmit = func(e cmdlog.Entry) {
		mu.Lock()
		if e.Outcome == cmdlog.OutcomeRunning {
			running = append(running, e)
		}
		mu.Unlock()
	}
	a.started.Store(true)
	done := a.markAIWrite(id)
	err := a.CreateBranch(id, "by-ai-running", "HEAD", false)
	done()
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(running) == 0 || running[len(running)-1].Origin != cmdlog.OriginAI {
		t.Fatalf("running entries: %+v", running)
	}
}
