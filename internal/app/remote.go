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
