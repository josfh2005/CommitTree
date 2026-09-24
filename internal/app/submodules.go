package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"git-ui/internal/repos"
	"git-ui/internal/submodules"
)

// directParent finds path's direct parent among list — the submodule (if
// any) whose own Path is the longest proper prefix of path — and returns
// its absolute directory (top itself when path is first-level) plus path's
// own name relative to it. This must consult the list rather than just
// splitting path on its last "/": a submodule's Path inside its parent can
// itself hold intermediate directories (e.g. a submodule added at
// "deps/zlib" inside "vendor/lib" has the top-relative Path
// "vendor/lib/deps/zlib", whose direct parent is "vendor/lib", not
// "vendor/lib/deps" — that segment is just a subdirectory of lib, not a
// submodule boundary git or the two-lock rule can operate on.
func directParent(top string, list []submodules.Submodule, path string) (parentDir, rel string) {
	best := ""
	for _, s := range list {
		if s.Path != path && strings.HasPrefix(path, s.Path+"/") && len(s.Path) > len(best) {
			best = s.Path
		}
	}
	if best == "" {
		return top, path
	}
	return filepath.Join(top, filepath.FromSlash(best)), path[len(best)+1:]
}

// GetSubmodules reads id's submodules, flat and recursive.
func (a *App) GetSubmodules(id string) ([]submodules.Submodule, error) {
	dir, err := a.dir(id)
	if err != nil {
		return nil, err
	}
	return submodules.List(a.ctx, dir)
}

// submoduleWrite resolves id's directory and its submodule list, refuses a
// path that list does not contain, and runs fn — in the submodule's direct
// parent directory, with its path relative to that parent — under the
// write locks of the top repository, the direct parent (when it differs
// from the top, i.e. a nested submodule) and the submodule itself: the
// two-or-three-lock rule every per-submodule write follows.
func (a *App) submoduleWrite(id, path string, fn func(ctx context.Context, dir, rel string) error) error {
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
	parentDir, rel := directParent(dir, list, path)
	subID := repos.IDFor(filepath.Join(dir, filepath.FromSlash(path)))
	ids := []string{id}
	if parentDir != dir {
		ids = append(ids, repos.IDFor(parentDir))
	}
	ids = append(ids, subID)
	return a.writeAll(ids, func(ctx context.Context) error { return fn(ctx, parentDir, rel) })
}

// InitSubmodule initialises and checks out the submodule at path.
func (a *App) InitSubmodule(id, path string) error {
	return a.submoduleWrite(id, path, func(ctx context.Context, dir, rel string) error {
		return submodules.Init(ctx, dir, rel)
	})
}

// UpdateSubmodule checks the submodule at path back out to its recorded
// commit.
func (a *App) UpdateSubmodule(id, path string) error {
	err := a.submoduleWrite(id, path, func(ctx context.Context, dir, rel string) error {
		return submodules.Update(ctx, dir, rel)
	})
	// submodules.Update reports the *ErrDirty it returns by the name passed
	// to it, which is path relative to its direct parent (e.g. "inner" for
	// a nested submodule) — surface the caller's top-relative path instead,
	// so the message names the same path this whole app speaks in.
	var derr *submodules.ErrDirty
	if errors.As(err, &derr) {
		return &submodules.ErrDirty{Path: path}
	}
	return err
}

// SyncSubmodule writes .gitmodules' current URL for path into the
// submodule's own remote.
func (a *App) SyncSubmodule(id, path string) error {
	return a.submoduleWrite(id, path, func(ctx context.Context, dir, rel string) error {
		return submodules.Sync(ctx, dir, rel)
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
