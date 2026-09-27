package app

import (
	"context"
	"errors"
	"sync"
	"testing"

	"git-ui/internal/cmdlog"
)

func TestCommandLogRecordsAppCommands(t *testing.T) {
	a, id := newTestApp(t)
	if err := a.CreateBranch(id, "logged", "HEAD", false); err != nil {
		t.Fatal(err)
	}
	list, err := a.CommandLog(id)
	if err != nil {
		t.Fatal(err)
	}
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
	if list, _ := a.CommandLog(id); len(list) != 0 {
		t.Fatalf("not cleared: %d", len(list))
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
	list, _ := a.CommandLog(id)
	origins := map[string]cmdlog.Origin{}
	for _, e := range list {
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

func TestAIToolContextCarriesOrigin(t *testing.T) {
	ctx := aiToolContext(context.Background())
	if o, ok := cmdlog.OriginFrom(ctx); !ok || o != cmdlog.OriginAI {
		t.Fatalf("got %q %v", o, ok)
	}
}
