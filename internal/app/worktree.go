package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"git-ui/internal/ai/prompts"
	"git-ui/internal/ai/tasks"
	"git-ui/internal/ai/tools"
	"git-ui/internal/gitcmd"
	"git-ui/internal/refs"
	"git-ui/internal/worktree"
)

// worktreeDiffCap bounds what GetWorktreeDiff hands the diff pane. Without a
// cap, one click on a large untracked or changed file buffers the whole diff
// over IPC and renders one DOM node per line, which can freeze the app; this
// is generous enough for an ordinary diff to never be touched.
const worktreeDiffCap = 200 * 1024

// EventWorktreeChanged tells the frontend the working tree moved, so the
// Changes view refreshes without polling.
const EventWorktreeChanged = "worktree:changed"

// EventCommitDelta streams the generated commit message into the box, and
// EventCommitDone closes it. They mirror the chat's delta/done pair.
const (
	EventCommitDelta = "commit:delta"
	EventCommitDone  = "commit:done"
)

type WorktreeChangedEvent struct {
	RepoID string `json:"repoID"`
}

type CommitDeltaEvent struct {
	RepoID string `json:"repoID"`
	RunID  string `json:"runID"`
	Text   string `json:"text"`
}

type CommitDoneEvent struct {
	RepoID string `json:"repoID"`
	RunID  string `json:"runID"`
	Error  string `json:"error,omitempty"`
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
	// merely because the two sides differ, not because anything went wrong -
	// but git overloads that same exit code for "could not access the path",
	// which is exactly what a stale path from the renderer produces if the
	// file vanished between the Status read above and this call. Exit 1 can't
	// tell the two apart on its own, so the path is checked on disk first;
	// only once it's confirmed to exist is exit 1 read as "they differ".
	if !staged && slices.ContainsFunc(st.Untracked, func(f worktree.FileStatus) bool { return f.Path == path }) {
		if _, statErr := os.Lstat(filepath.Join(dir, path)); statErr != nil {
			return "", statErr
		}
		out, err := gitcmd.Run(a.ctx, dir, gitcmd.ReadTimeout, "--literal-pathspecs", "diff", "--no-index", "--", "/dev/null", path)
		var gerr *gitcmd.Error
		if errors.As(err, &gerr) && gerr.ExitCode == 1 {
			return tools.Truncate(out, worktreeDiffCap), nil
		}
		return out, err
	}
	args := []string{"--literal-pathspecs", "diff"}
	if staged {
		args = append(args, "--cached")
	}
	args = append(args, "--", path)
	out, err := gitcmd.Run(a.ctx, dir, gitcmd.ReadTimeout, args...)
	if err != nil {
		return out, err
	}
	return tools.Truncate(out, worktreeDiffCap), nil
}

// GenerateCommitMessage streams a commit message for the staged changes. It
// uses the task provider and model — the cheap one — and never commits.
func (a *App) GenerateCommitMessage(id, runID string) error {
	if a.ai == nil {
		return ErrAIDisabled
	}
	if runID == "" {
		return errors.New("run id is required")
	}
	dir, err := a.dir(id)
	if err != nil {
		return err
	}
	// a.dir already confirmed id exists, so the repo's name is available
	// with no further error to check.
	repo, _ := a.store.Get(id)
	st, err := worktree.Status(a.ctx, dir)
	if err != nil {
		return err
	}
	if len(st.Staged) == 0 {
		return worktree.ErrNothingStaged
	}
	cfg, err := a.aiSettings()
	if err != nil {
		return err
	}
	responder, err := a.responderFor(cfg.TaskProvider, cfg.TaskModel, cfg)
	if err != nil {
		return err
	}
	instructions, err := a.ai.deps.Prompts.Get(prompts.CommitMessage, prompts.Vars{
		Repo: repo.Name, Path: dir, Branch: refs.CurrentLabel(a.ctx, dir), Date: time.Now().Format("2006-01-02"),
	})
	if err != nil {
		return err
	}
	prompt, err := tasks.CommitContext(a.ctx, dir, tasks.OllamaDiffBudget)
	if err != nil {
		return err
	}

	go func() {
		stream, err := responder.Respond(a.ctx, instructions, prompt)
		if err != nil {
			a.emit(EventCommitDone, CommitDoneEvent{RepoID: id, RunID: runID, Error: err.Error()})
			return
		}
		done := CommitDoneEvent{RepoID: id, RunID: runID}
		for chunk := range stream {
			switch {
			case chunk.Err != nil:
				done.Error = chunk.Err.Error()
			case chunk.Delta != "":
				a.emit(EventCommitDelta, CommitDeltaEvent{RepoID: id, RunID: runID, Text: chunk.Delta})
			}
		}
		a.emit(EventCommitDone, done)
	}()
	return nil
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
