package app

import (
	"context"
	"fmt"
	"path/filepath"

	"git-ui/internal/repos"
	"git-ui/internal/submodules"
)

// GetSubmodules reads id's submodules, flat and recursive.
func (a *App) GetSubmodules(id string) ([]submodules.Submodule, error) {
	dir, err := a.dir(id)
	if err != nil {
		return nil, err
	}
	return submodules.List(a.ctx, dir)
}

// submoduleWrite resolves id's directory and its submodule list, refuses a
// path that list does not contain, and runs fn under both id's write lock
// and the lock of the submodule at path — the two-lock rule every
// per-submodule write follows.
func (a *App) submoduleWrite(id, path string, fn func(ctx context.Context, dir string) error) error {
	dir, err := a.dir(id)
	if err != nil {
		return err
	}
	list, err := submodules.List(a.ctx, dir)
	if err != nil {
		return err
	}
	found := false
	for _, s := range list {
		if s.Path == path {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("%q is not a submodule of this repository", path)
	}
	subID := repos.IDFor(filepath.Join(dir, filepath.FromSlash(path)))
	return a.writeAll([]string{id, subID}, func(ctx context.Context) error { return fn(ctx, dir) })
}

// InitSubmodule initialises and checks out the submodule at path.
func (a *App) InitSubmodule(id, path string) error {
	return a.submoduleWrite(id, path, func(ctx context.Context, dir string) error {
		return submodules.Init(ctx, dir, path)
	})
}

// UpdateSubmodule checks the submodule at path back out to its recorded
// commit.
func (a *App) UpdateSubmodule(id, path string) error {
	return a.submoduleWrite(id, path, func(ctx context.Context, dir string) error {
		return submodules.Update(ctx, dir, path)
	})
}

// SyncSubmodule writes .gitmodules' current URL for path into the
// submodule's own remote.
func (a *App) SyncSubmodule(id, path string) error {
	return a.submoduleWrite(id, path, func(ctx context.Context, dir string) error {
		return submodules.Sync(ctx, dir, path)
	})
}

// submoduleWriteAll resolves id's directory and submodule list, and runs fn
// under id's write lock and every listed submodule's lock at once.
func (a *App) submoduleWriteAll(id string, fn func(ctx context.Context, dir string) error) error {
	dir, err := a.dir(id)
	if err != nil {
		return err
	}
	list, err := submodules.List(a.ctx, dir)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(list)+1)
	ids = append(ids, id)
	for _, s := range list {
		ids = append(ids, repos.IDFor(filepath.Join(dir, filepath.FromSlash(s.Path))))
	}
	return a.writeAll(ids, func(ctx context.Context) error { return fn(ctx, dir) })
}

// InitAllSubmodules initialises and checks out every submodule of id,
// recursively.
func (a *App) InitAllSubmodules(id string) error {
	return a.submoduleWriteAll(id, func(ctx context.Context, dir string) error {
		return submodules.InitAll(ctx, dir)
	})
}

// UpdateAllSubmodules checks every submodule of id back out to its recorded
// commit, recursively.
func (a *App) UpdateAllSubmodules(id string) error {
	return a.submoduleWriteAll(id, func(ctx context.Context, dir string) error {
		return submodules.UpdateAll(ctx, dir)
	})
}
