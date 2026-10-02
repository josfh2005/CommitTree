package app

import (
	"context"

	"git-ui/internal/cmdlog"
	"git-ui/internal/gitcmd"
	"git-ui/internal/ops"
)

// AutoFetch is one background fetch of repository id
// (docs/spec/05-remote-and-stash.md). It never waits: when another write
// holds the repository it is skipped. While it runs, a user write cancels
// it (see writeLock). Its commands are logged as Auto.
func (a *App) AutoFetch(id string) (ops.AutoFetchResult, error) {
	dir, err := a.dir(id)
	if err != nil {
		return ops.AutoFetchResult{}, err
	}
	// Cancelled with gitcmd.ErrCancelled as the cause, so the Commands
	// panel shows the stopped fetch as cancelled, not failed.
	ctx, cancel := context.WithCancelCause(cmdlog.WithOrigin(a.ctx, cmdlog.OriginAuto))
	defer cancel(nil)
	l := a.lockFor(id)
	if !l.tryLockAuto(func() { cancel(gitcmd.ErrCancelled) }) {
		return ops.AutoFetchResult{Skipped: true}, nil
	}
	defer l.unlock()
	return ops.AutoFetch(ctx, dir, nil)
}
