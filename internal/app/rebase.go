package app

import (
	"context"

	"git-ui/internal/merge"
)

// RebaseOnto replays the current branch onto onto (a branch, remote branch
// or commit). A conflicted rebase is left for the Conflicts view.
func (a *App) RebaseOnto(id, onto string) (merge.Result, error) {
	var result merge.Result
	err := a.write(id, func(ctx context.Context, dir string) error {
		var err error
		result, err = merge.Rebase(ctx, dir, onto)
		return err
	})
	return result, err
}

// GetRebasePreview is what the rebase confirmation shows.
func (a *App) GetRebasePreview(id, onto string) (merge.Preview, error) {
	dir, err := a.dir(id)
	if err != nil {
		return merge.Preview{}, err
	}
	return merge.RebasePreview(a.ctx, dir, onto)
}

// CherryPick applies one commit on top of the current branch.
func (a *App) CherryPick(id, rev string) (merge.Result, error) {
	var result merge.Result
	err := a.write(id, func(ctx context.Context, dir string) error {
		var err error
		result, err = merge.CherryPick(ctx, dir, rev)
		return err
	})
	return result, err
}

// SkipStep drops the commit a rebase or cherry-pick is stopped on. Like
// CommitMerge and AbortMerge it stops any resolver run first.
func (a *App) SkipStep(id string) error {
	a.stopRun(id)
	return a.write(id, func(ctx context.Context, dir string) error { return merge.Skip(ctx, dir) })
}

// IsAncestorOfHead lets the menus disable a rebase or cherry-pick with
// nothing to do.
func (a *App) IsAncestorOfHead(id, rev string) (bool, error) {
	dir, err := a.dir(id)
	if err != nil {
		return false, err
	}
	return merge.IsAncestorOfHead(a.ctx, dir, rev)
}
