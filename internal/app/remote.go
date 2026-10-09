package app

import (
	"context"

	"git-ui/internal/cmdlog"
	"git-ui/internal/gitsettings"
	"git-ui/internal/ops"
)

func (a *App) Fetch(id string) error {
	return a.write(id, func(ctx context.Context, dir string) error { return ops.Fetch(ctx, dir) })
}

// FetchRemote fetches one remote, for the branch context menu's "Fetch <remote>".
func (a *App) FetchRemote(id, remote string) error {
	return a.write(id, func(ctx context.Context, dir string) error { return ops.FetchRemote(ctx, dir, remote) })
}

// FastForwardBranch is the branch menu's Pull on a branch that is not
// checked out: it moves the branch up to its upstream when that is a
// fast-forward (docs/spec/05-remote-and-stash.md) and reports whether it moved.
func (a *App) FastForwardBranch(id, branch string) (bool, error) {
	var moved bool
	err := a.write(id, func(ctx context.Context, dir string) error {
		var ffErr error
		moved, ffErr = ops.FastForwardBranch(ctx, dir, branch)
		return ffErr
	})
	return moved, err
}

// PushBranch is the branch menu's Push: one local branch to its upstream,
// or published to origin when it has none. The outcome comes back as a
// result; only a branch that cannot be pushed at all is an error.
func (a *App) PushBranch(id, branch string) (ops.BranchPushResult, error) {
	var result ops.BranchPushResult
	err := a.write(id, func(ctx context.Context, dir string) error {
		var pushErr error
		result, pushErr = ops.PushBranch(ctx, dir, branch)
		return pushErr
	})
	return result, err
}

func (a *App) Push(id string) error {
	return a.write(id, func(ctx context.Context, dir string) error { return ops.Push(ctx, dir) })
}

// PushAll pushes the main (official) branches ahead of their
// upstreams (docs/spec/05-remote-and-stash.md). Each branch's outcome comes
// back as a result; only a failure to start (busy, missing) is an error.
func (a *App) PushAll(id string) ([]ops.BranchPushResult, error) {
	var results []ops.BranchPushResult
	err := a.write(id, func(ctx context.Context, dir string) error {
		var pushErr error
		results, pushErr = ops.PushAll(ctx, dir)
		return pushErr
	})
	return results, err
}

// Pull reads the configured strategy once per call and runs it under the
// write lock; the result (up to date, merged, rebased, or conflicted) goes
// back to the caller the same way MergeBranch's Result does.
func (a *App) Pull(id string) (ops.Result, error) {
	cfg, err := a.gitSettings()
	if err != nil {
		return ops.Result{}, err
	}
	var result ops.Result
	err = a.write(id, func(ctx context.Context, dir string) error {
		var pullErr error
		result, pullErr = ops.Pull(ctx, dir, cfg.PullStrategy)
		// A pull that stopped on conflicts exits non-zero, but its fetch
		// reached the remote: credentials work, so background fetches resume.
		if pullErr == nil && result.Outcome == ops.Conflicted {
			a.paused.resume(cmdlog.RepoKey(dir))
		}
		return pullErr
	})
	return result, err
}

func (a *App) GetRemoteInfo(id string) (ops.AheadBehind, error) {
	dir, err := a.dir(id)
	if err != nil {
		return ops.AheadBehind{}, err
	}
	return ops.Counts(a.ctx, dir)
}

// BranchCounts is how far a local branch is from its upstream; the merge
// confirmation uses it to warn about merging a stale branch.
func (a *App) BranchCounts(id, branch string) (ops.AheadBehind, error) {
	dir, err := a.dir(id)
	if err != nil {
		return ops.AheadBehind{}, err
	}
	return ops.BranchCounts(a.ctx, dir, branch)
}

func (a *App) gitSettingsFile() (string, error) {
	if a.gitSettingsPath != "" {
		return a.gitSettingsPath, nil
	}
	return gitsettings.DefaultPath()
}

func (a *App) gitSettings() (gitsettings.Settings, error) {
	path, err := a.gitSettingsFile()
	if err != nil {
		return gitsettings.Settings{}, err
	}
	return gitsettings.Load(path)
}

func (a *App) GetGitSettings() (gitsettings.Settings, error) { return a.gitSettings() }

func (a *App) SaveGitSettings(s gitsettings.Settings) error {
	path, err := a.gitSettingsFile()
	if err != nil {
		return err
	}
	return gitsettings.Save(path, s)
}
