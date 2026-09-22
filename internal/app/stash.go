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
// UnstageMergeFile or TakeMergeSide resolve it — see finishOwedDrop. The
// reminder is keyed by the entry's commit hash rather than index: another
// stash pushed or dropped before the conflict is resolved shifts every
// index below it, and a bare index remembered here would then point at the
// wrong entry when finishOwedDrop finally runs.
func (a *App) StashPop(id string, index int) error {
	return a.writeMerge(id, func(ctx context.Context, dir string) error {
		sha := stashHashAt(ctx, dir, index)
		err := stash.Pop(ctx, dir, index)
		if isStashConflict(ctx, dir, err) {
			if sha != "" {
				a.owedDrops.Store(id, sha)
			}
			return nil
		}
		return err
	})
}

func (a *App) StashDrop(id string, index int) error {
	return a.writeWorktree(id, func(ctx context.Context, dir string) error {
		sha := stashHashAt(ctx, dir, index)
		if err := stash.Drop(ctx, dir, index); err != nil {
			return err
		}
		// Dropping by hand settles the debt a conflicted Pop left, so
		// finishOwedDrop doesn't later drop an unrelated entry — matched by
		// hash, so it is right whichever index the owed entry has drifted
		// to since.
		if v, ok := a.owedDrops.Load(id); ok && sha != "" && v.(string) == sha {
			a.owedDrops.Delete(id)
		}
		return nil
	})
}

// OwedStashDrop is the index the stash entry a conflicted Pop is still
// waiting to drop currently sits at, or -1 when nothing is owed or the
// owed entry can no longer be found (already dropped some other way). The
// reminder itself is a hash, not an index — this re-resolves it against the
// stash list as it stands right now, so a shift since the Pop can never
// report a stale index. The conflict view shows its "Drop stash" button
// only when this is not -1.
func (a *App) OwedStashDrop(id string) int {
	v, ok := a.owedDrops.Load(id)
	if !ok {
		return -1
	}
	dir, err := a.dir(id)
	if err != nil {
		return -1
	}
	entries, err := stash.List(a.ctx, dir)
	if err != nil {
		return -1
	}
	sha := v.(string)
	for _, e := range entries {
		if e.Hash == sha {
			return e.Index
		}
	}
	return -1
}

// stashHashAt returns the commit hash of the stash entry currently at
// index, or "" when the list can't be read or has nothing there.
func stashHashAt(ctx context.Context, dir string, index int) string {
	entries, err := stash.List(ctx, dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.Index == index {
			return e.Hash
		}
	}
	return ""
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

func (a *App) GetStashFiles(id string, index int) ([]stash.File, error) {
	dir, err := a.dir(id)
	if err != nil {
		return nil, err
	}
	return stash.Files(a.ctx, dir, index)
}

func (a *App) GetStashFileDiff(id string, index int, path string) (string, error) {
	dir, err := a.dir(id)
	if err != nil {
		return "", err
	}
	out, err := stash.FileDiff(a.ctx, dir, index, path)
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
