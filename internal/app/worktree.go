package app

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"git-ui/internal/gitcmd"
	"git-ui/internal/worktree"
)

// EventWorktreeChanged tells the frontend the working tree moved, so the
// Changes view refreshes without polling.
const EventWorktreeChanged = "worktree:changed"

type WorktreeChangedEvent struct {
	RepoID string `json:"repoID"`
}

func (a *App) GetWorktreeState(id string) (worktree.State, error) {
	dir, err := a.dir(id)
	if err != nil {
		return worktree.State{}, err
	}
	return worktree.Status(a.ctx, dir)
}

func (a *App) StageFile(id, path string) error {
	return a.writeWorktree(id, func(ctx context.Context, dir string) error {
		return worktree.Stage(ctx, dir, path)
	})
}

func (a *App) UnstageFile(id, path string) error {
	return a.writeWorktree(id, func(ctx context.Context, dir string) error {
		return worktree.Unstage(ctx, dir, path)
	})
}

func (a *App) DiscardFile(id, path string) error {
	return a.writeWorktree(id, func(ctx context.Context, dir string) error {
		return worktree.Discard(ctx, dir, path)
	})
}

func (a *App) CommitChanges(id, message string, amend bool) error {
	return a.writeWorktree(id, func(ctx context.Context, dir string) error {
		return worktree.Commit(ctx, dir, message, amend)
	})
}

func (a *App) GetCommitPreview(id string) (worktree.CommitInfo, error) {
	dir, err := a.dir(id)
	if err != nil {
		return worktree.CommitInfo{}, err
	}
	return worktree.Preview(a.ctx, dir)
}

// GetWorktreeDiff returns what one changed file shows in the pane: the staged
// diff against HEAD, or the unstaged diff against the index. Only a path the
// status just listed can be read, so a path from the renderer cannot be used
// to read the disk.
func (a *App) GetWorktreeDiff(id, path string, staged bool) (string, error) {
	dir, err := a.dir(id)
	if err != nil {
		return "", err
	}
	st, err := worktree.Status(a.ctx, dir)
	if err != nil {
		return "", err
	}
	known := []string{}
	for _, list := range [][]worktree.FileStatus{st.Staged, st.Unstaged, st.Untracked} {
		for _, f := range list {
			known = append(known, f.Path)
		}
	}
	if !slices.Contains(known, path) {
		return "", fmt.Errorf("%q is not a changed file of this repository", path)
	}
	// An untracked file has no HEAD or index side to diff against; --no-index
	// against /dev/null is what shows its whole content as an addition. That
	// mode behaves like the plain diff(1) command it emulates: it exits 1
	// merely because the two sides differ, not because anything went wrong.
	if !staged && slices.ContainsFunc(st.Untracked, func(f worktree.FileStatus) bool { return f.Path == path }) {
		out, err := gitcmd.Run(a.ctx, dir, gitcmd.ReadTimeout, "--literal-pathspecs", "diff", "--no-index", "--", "/dev/null", path)
		var gerr *gitcmd.Error
		if errors.As(err, &gerr) && gerr.ExitCode == 1 {
			return out, nil
		}
		return out, err
	}
	args := []string{"--literal-pathspecs", "diff"}
	if staged {
		args = append(args, "--cached")
	}
	args = append(args, "--", path)
	return gitcmd.Run(a.ctx, dir, gitcmd.ReadTimeout, args...)
}

// writeWorktree runs fn under the repository's write lock, so it cannot
// interleave with a merge action or an agent tool call, then tells the
// frontend the working tree moved.
func (a *App) writeWorktree(id string, fn func(ctx context.Context, dir string) error) error {
	if err := a.write(id, fn); err != nil {
		return err
	}
	a.emit(EventWorktreeChanged, WorktreeChangedEvent{RepoID: id})
	return nil
}
