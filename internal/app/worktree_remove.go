package app

import (
	"context"

	"git-ui/internal/refs"
	"git-ui/internal/repos"
	"git-ui/internal/worktrees"
)

// worktreeMainDir resolves id (a detected worktree's row id, as ListRepos
// reports it) to its main repository's directory and its own directory.
// Anything that isn't a worktree the last ListRepos detected — the main
// repository itself, a normal list entry, a submodule — refuses with
// ErrUnknownRepo: only a linked worktree can be removed this way.
func (a *App) worktreeMainDir(id string) (mainDir, wtDir string, err error) {
	a.wtMu.Lock()
	parentID, ok := a.wtParent[id]
	a.wtMu.Unlock()
	if !ok {
		return "", "", repos.ErrUnknownRepo
	}
	wtDir, err = a.dir(id)
	if err != nil {
		return "", "", err
	}
	mainDir, err = a.dir(parentID)
	if err != nil {
		return "", "", err
	}
	return mainDir, wtDir, nil
}

// WorktreeRemovalInfo reports what the "Remove worktree…" confirmation
// shows for id: its branch, uncommitted change count, lock state, and
// whether its branch is merged into the main working tree's HEAD.
func (a *App) WorktreeRemovalInfo(id string) (worktrees.RemovalInfo, error) {
	mainDir, wtDir, err := a.worktreeMainDir(id)
	if err != nil {
		return worktrees.RemovalInfo{}, err
	}
	return worktrees.Info(a.ctx, mainDir, wtDir)
}

// RemoveWorktree removes the linked worktree id, running `git worktree
// remove` from the main repository's directory under the main repository's
// write lock — the same lock a merge, rebase or cherry-pick on that
// repository holds, so a worktree removal can't interleave with one. force
// passes --force, needed when the worktree has uncommitted changes.
//
// When deleteBranch is set and the worktree is not detached, its branch is
// deleted with `git branch -d` (never -D) once the worktree itself is gone.
// If git refuses because the branch isn't merged, the worktree stays
// removed regardless — only the branch delete is undone — and the error
// wraps refs.ErrNotMerged so the frontend can offer the same force-delete
// confirmation it already shows for an ordinary branch delete, rather than
// ever deleting unmerged commits silently.
func (a *App) RemoveWorktree(id string, force, deleteBranch bool) error {
	mainDir, wtDir, err := a.worktreeMainDir(id)
	if err != nil {
		return err
	}
	a.wtMu.Lock()
	parentID := a.wtParent[id]
	a.wtMu.Unlock()

	var branch string
	if deleteBranch {
		info, ierr := worktrees.Info(a.ctx, mainDir, wtDir)
		if ierr == nil {
			branch = info.Branch
		}
	}

	err = a.write(parentID, func(ctx context.Context, dir string) error {
		if err := worktrees.Remove(ctx, dir, wtDir, force); err != nil {
			return err
		}
		if branch == "" {
			return nil
		}
		return refs.DeleteBranch(ctx, dir, branch, false)
	})

	a.forgetLog(id)
	a.term.CloseRepo(id)
	a.emit(EventWorktreeChanged, WorktreeChangedEvent{RepoID: parentID})
	return err
}
