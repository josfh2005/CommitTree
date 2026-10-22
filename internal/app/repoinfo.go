package app

import (
	"context"

	"git-ui/internal/repoinfo"
)

// RepoInfo is the line under the repository's path in the log header: the
// object database's size and objects, and whether git would garbage-collect
// it (docs/spec/01-repositories-and-sidebar.md, "Repository info").
func (a *App) RepoInfo(id string) (repoinfo.Info, error) {
	dir, err := a.dir(id)
	if err != nil {
		return repoinfo.Info{}, err
	}
	return repoinfo.Read(a.ctx, dir)
}

// WorktreeSize is what the repository's files take on disk outside .git,
// for the repository info popover; it walks the whole tree, so it is asked
// for only when the popover opens.
func (a *App) WorktreeSize(id string) (int64, error) {
	dir, err := a.dir(id)
	if err != nil {
		return 0, err
	}
	return repoinfo.WorktreeSize(a.ctx, dir)
}

// CollectGarbage runs git gc under the repository's write lock.
func (a *App) CollectGarbage(id string) error {
	return a.write(id, func(ctx context.Context, dir string) error { return repoinfo.Collect(ctx, dir) })
}
