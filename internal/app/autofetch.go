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
	key := cmdlog.RepoKey(dir)
	res, err := ops.AutoFetch(ctx, dir, func(remote string) bool { return a.paused.paused(key, remote) })
	a.paused.pause(key, res.AuthFailed)
	return res, err
}

// AutoFetchPaused is the remotes of repository id that background fetches
// skip for want of credentials, for the toolbar's Fetch dot.
func (a *App) AutoFetchPaused(id string) ([]string, error) {
	dir, err := a.dir(id)
	if err != nil {
		return nil, err
	}
	return a.paused.list(cmdlog.RepoKey(dir)), nil
}
