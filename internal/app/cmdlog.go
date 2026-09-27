package app

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"git-ui/internal/cmdlog"
	"git-ui/internal/gitcmd"
)

// EventCommand carries git commands to the Commands panel: a write when it
// starts (outcome "running") and every command when it ends, the end with
// the same ID as its start.
const EventCommand = "cmdlog:entry"

// beginGit is the recorder's start hook: a write appears in the panel as
// running as soon as git starts.
func (a *App) beginGit(s gitcmd.Start) int64 {
	s.Ctx = a.originContext(s.Ctx, s.Dir, s.Args)
	e, shown := a.cmds.Begin(s)
	if shown {
		a.emitCommandSafe(e)
	}
	return e.ID
}

// recordGit is the recorder's end hook: every command the app runs lands
// in the log, and once the window is up, in the panel.
func (a *App) recordGit(r gitcmd.Record) {
	r.Ctx = a.originContext(r.Ctx, r.Dir, r.Args)
	a.emitCommandSafe(a.cmds.Add(r))
}

// emitCommandSafe calls emitCommand recovering any panic — the Wails
// runtime call it wraps can panic after the window has gone away (its
// EventsEmit calls log.Fatalf on a shutdown context, and gitcmd's own begin
// recovers around the whole Recorder.Begin call, which would otherwise
// swallow the ID Log.Begin already returned and leave that entry running
// forever). Losing one emitted event to a shutdown race is fine; losing the
// ID is not.
func (a *App) emitCommandSafe(e cmdlog.Entry) {
	defer func() { _ = recover() }()
	a.emitCommand(e)
}

// originContext marks ctx as AI for a write run in a repository where an
// approved AI write is running: that write runs through the same App
// methods as the UI, whose ctx does not carry the AI origin (see
// markAIWrite).
func (a *App) originContext(ctx context.Context, dir string, args []string) context.Context {
	if o, ok := cmdlog.OriginFrom(ctx); ok && o == cmdlog.OriginAI {
		return ctx
	}
	if a.aiWriting(cmdlog.RepoKey(dir)) && cmdlog.Classify(args) == cmdlog.KindWrite {
		return cmdlog.WithOrigin(ctx, cmdlog.OriginAI)
	}
	return ctx
}

// emitCommand sends e to the panel. Not through a.emit: the AI deps' Emit
// is what tests watch for chat events, and every git command would flood
// it. Before Startup (and in tests) there is no Wails runtime to emit to.
func (a *App) emitCommand(e cmdlog.Entry) {
	if a.started.Load() && a.cmdEmit != nil {
		a.cmdEmit(e)
	}
}

// markAIWrite marks repository id as running an approved AI write until
// the returned func is called; writes run there meanwhile are logged as AI.
func (a *App) markAIWrite(id string) func() {
	dir, err := a.dir(id)
	if err != nil {
		return func() {}
	}
	key := cmdlog.RepoKey(dir)
	a.aiWrites.Store(key, struct{}{})
	return func() { a.aiWrites.Delete(key) }
}

func (a *App) aiWriting(repoKey string) bool {
	_, ok := a.aiWrites.Load(repoKey)
	return ok
}

// aiToolContext marks every git command an AI tool call runs as AI.
func aiToolContext(ctx context.Context) context.Context {
	return cmdlog.WithOrigin(ctx, cmdlog.OriginAI)
}

// CommandLogView is CommandLog's result: the key the backend logs this
// repository's commands under (RepoKey applied — a cleaned path, which the
// frontend cannot always reproduce byte-for-byte, e.g. on Windows) and the
// commands themselves.
type CommandLogView struct {
	Repo    string         `json:"repo"`
	Entries []cmdlog.Entry `json:"entries"`
}

// CommandLog is repository id's git commands, newest first, along with the
// key they are logged under — the frontend matches live cmdlog:entry events
// and its own cache against that key, not against its own repo path.
func (a *App) CommandLog(id string) (CommandLogView, error) {
	dir, err := a.dir(id)
	if err != nil {
		return CommandLogView{}, err
	}
	return CommandLogView{Repo: cmdlog.RepoKey(dir), Entries: a.cmds.List(dir)}, nil
}

// CommandLogOutput is what command entryID printed.
func (a *App) CommandLogOutput(id string, entryID int64) (cmdlog.Output, error) {
	dir, err := a.dir(id)
	if err != nil {
		return cmdlog.Output{}, err
	}
	return a.cmds.Output(dir, entryID)
}

// ClearCommandLog forgets repository id's commands.
func (a *App) ClearCommandLog(id string) error {
	dir, err := a.dir(id)
	if err != nil {
		return err
	}
	a.cmds.Clear(dir)
	return nil
}

// CancelCommand stops command entryID of repository id as Ctrl+C would;
// cmdlog.ErrNotRunning when it is no longer running.
func (a *App) CancelCommand(id string, entryID int64) error {
	dir, err := a.dir(id)
	if err != nil {
		return err
	}
	return a.cmds.Cancel(dir, entryID)
}

func wailsCommandEmitter(ctx context.Context) func(cmdlog.Entry) {
	return func(e cmdlog.Entry) { runtime.EventsEmit(ctx, EventCommand, e) }
}
