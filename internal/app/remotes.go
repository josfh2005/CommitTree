package app

import (
	"context"
	"strings"

	"git-ui/internal/cmdlog"
	"git-ui/internal/ops"
)

// The Remotes tab of Repository settings (docs/spec/12-repository-settings.md).

func (a *App) ListRemotes(id string) ([]ops.Remote, error) {
	dir, err := a.dir(id)
	if err != nil {
		return nil, err
	}
	return ops.ListRemotes(a.ctx, dir)
}

func (a *App) AddRemote(id, name, url string) error {
	return a.write(id, func(ctx context.Context, dir string) error { return ops.AddRemote(ctx, dir, name, url) })
}

// SetRemoteURL points a remote elsewhere; its old credential failure no
// longer says anything, so its background-fetch pause ends.
func (a *App) SetRemoteURL(id, name, url string) error {
	return a.write(id, func(ctx context.Context, dir string) error {
		if err := ops.SetRemoteURL(ctx, dir, name, url); err != nil {
			return err
		}
		a.paused.forget(cmdlog.RepoKey(dir), strings.TrimSpace(name))
		return nil
	})
}

func (a *App) RemoveRemote(id, name string) error {
	return a.write(id, func(ctx context.Context, dir string) error {
		if err := ops.RemoveRemote(ctx, dir, name); err != nil {
			return err
		}
		a.paused.forget(cmdlog.RepoKey(dir), strings.TrimSpace(name))
		return nil
	})
}

// TestRemote is a read: it never takes the write lock, so a slow server
// can't block the repository's other operations.
func (a *App) TestRemote(id, name string) (ops.RemoteTest, error) {
	dir, err := a.dir(id)
	if err != nil {
		return ops.RemoteTest{}, err
	}
	return ops.TestRemote(a.ctx, dir, name)
}
