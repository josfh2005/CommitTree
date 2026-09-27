package app

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"git-ui/internal/cmdlog"
	"git-ui/internal/gitcmd"
)

// EventCommand carries each finished git command to the Commands panel.
const EventCommand = "cmdlog:entry"

// recordGit is the gitcmd recorder: every command the app runs lands in
// the log, and once the window is up, in the panel.
func (a *App) recordGit(r gitcmd.Record) {
	if o, ok := cmdlog.OriginFrom(r.Ctx); !ok || o != cmdlog.OriginAI {
		if a.aiWriting(cmdlog.RepoKey(r.Dir)) && cmdlog.Classify(r.Args) == cmdlog.KindWrite {
			r.Ctx = cmdlog.WithOrigin(r.Ctx, cmdlog.OriginAI)
		}
	}
	e := a.cmds.Add(r)
	// Not through a.emit: the AI deps' Emit is what tests watch for chat
	// events, and every git command would flood it. Before Startup (and in
	// tests) there is no Wails runtime to emit to.
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

// CommandLog is repository id's git commands, newest first.
func (a *App) CommandLog(id string) ([]cmdlog.Entry, error) {
	dir, err := a.dir(id)
	if err != nil {
		return nil, err
	}
	return a.cmds.List(dir), nil
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

func wailsCommandEmitter(ctx context.Context) func(cmdlog.Entry) {
	return func(e cmdlog.Entry) { runtime.EventsEmit(ctx, EventCommand, e) }
}
