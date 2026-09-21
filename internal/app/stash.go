package app

import (
	"context"

	"git-ui/internal/ai/tools"
	"git-ui/internal/merge"
	"git-ui/internal/stash"
)

func (a *App) GetStashEntries(id string) ([]stash.Entry, error) {
	dir, err := a.dir(id)
	if err != nil {
		return nil, err
	}
	return stash.List(a.ctx, dir)
}

func (a *App) StashPush(id, message string, includeUntracked bool) error {
	return a.writeWorktree(id, func(ctx context.Context, dir string) error {
		return stash.Push(ctx, dir, message, includeUntracked)
	})
}

// StashApply may leave the repository conflicted; that is reported through
// GetMergeState (Kind stash), not as an error here, the same distinction
// merge.Start already draws for a conflicted merge.
func (a *App) StashApply(id string, index int) error {
	return a.writeMerge(id, func(ctx context.Context, dir string) error {
		return conflictOK(ctx, dir, stash.Apply(ctx, dir, index))
	})
}

// StashPop may also conflict; when it does, git leaves the stash entry in
// place on purpose, and this remembers to drop it once StageMergeFile,
// UnstageMergeFile or TakeMergeSide resolve it — see finishOwedDrop.
func (a *App) StashPop(id string, index int) error {
	return a.writeMerge(id, func(ctx context.Context, dir string) error {
		err := stash.Pop(ctx, dir, index)
		if isStashConflict(ctx, dir, err) {
			a.owedDrops.Store(id, index)
			return nil
		}
		return err
	})
}

func (a *App) StashDrop(id string, index int) error {
	return a.writeWorktree(id, func(ctx context.Context, dir string) error {
		if err := stash.Drop(ctx, dir, index); err != nil {
			return err
		}
		// Dropping by hand settles the debt a conflicted Pop left, so
		// finishOwedDrop doesn't later drop an unrelated entry that has
		// since shifted into this index.
		if v, ok := a.owedDrops.Load(id); ok && v.(int) == index {
			a.owedDrops.Delete(id)
		}
		return nil
	})
}

// OwedStashDrop is the stash index a conflicted Pop is still waiting to
// drop, or -1 when nothing is owed. The conflict view shows its "Drop
// stash" button only for the first case.
func (a *App) OwedStashDrop(id string) int {
	if v, ok := a.owedDrops.Load(id); ok {
		return v.(int)
	}
	return -1
}

func (a *App) GetStashDiff(id string, index int) (string, error) {
	dir, err := a.dir(id)
	if err != nil {
		return "", err
	}
	out, err := stash.Diff(a.ctx, dir, index)
	if err != nil {
		return "", err
	}
	return tools.Truncate(out, worktreeDiffCap), nil
}

// isStashConflict reports whether err is Apply/Pop leaving a conflict behind
// rather than a real failure.
func isStashConflict(ctx context.Context, dir string, err error) bool {
	if err == nil {
		return false
	}
	st, stErr := merge.Status(ctx, dir)
	return stErr == nil && st.Kind == merge.KindStash
}

func conflictOK(ctx context.Context, dir string, err error) error {
	if isStashConflict(ctx, dir, err) {
		return nil
	}
	return err
}
