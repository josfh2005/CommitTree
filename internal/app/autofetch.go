package app

import (
	"context"

	"git-ui/internal/cmdlog"
	"git-ui/internal/ops"
)

// AutoFetch is one background fetch of repository id
// (docs/spec/05-remote-and-stash.md). It never waits: when another write
// holds the repository it is skipped. While it runs, a user write cancels
// it (see lockWrite). Its commands are logged as Auto.
func (a *App) AutoFetch(id string) (ops.AutoFetchResult, error) {
	dir, err := a.dir(id)
	if err != nil {
		return ops.AutoFetchResult{}, err
	}
	mu := a.writeMutex(id)
	if !mu.TryLock() {
		return ops.AutoFetchResult{Skipped: true}, nil
	}
	ctx, cancel := context.WithCancel(cmdlog.WithOrigin(a.ctx, cmdlog.OriginAuto))
	a.autoFetches.Store(id, cancel)
	defer func() {
		a.autoFetches.Delete(id)
		cancel()
		mu.Unlock()
	}()
	return ops.AutoFetch(ctx, dir)
}
