package app

import (
	"context"
	"errors"
	"time"

	"git-ui/internal/ai/prompts"
	"git-ui/internal/ai/tasks"
	"git-ui/internal/ai/tools"
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

// ApplyHunkSelection stages, unstages or discards part of one file (see
// worktree.ApplySelection). A discard's patch is kept as the repository's
// last discard, for UndoDiscard.
//
// sel is a plain slice, not worktree.Selection, so Wails generates its type
// (Array<worktree.HunkPick>) instead of a name models.ts never defines.
func (a *App) ApplyHunkSelection(id, path string, staged bool, hash string, sel []worktree.HunkPick, action string) error {
	return a.writeWorktree(id, func(ctx context.Context, dir string) error {
		patch, err := worktree.ApplySelection(ctx, dir, path, staged, hash, worktree.Selection(sel), worktree.Action(action))
		if err != nil {
			return err
		}
		if worktree.Action(action) == worktree.ActionDiscard {
			a.discards.Store(id, patch)
		}
		return nil
	})
}

// UndoDiscard puts the last hunk/line discard back. It is kept when the undo
// fails (the lines changed since), so it can be tried again.
func (a *App) UndoDiscard(id string) error {
	return a.writeWorktree(id, func(ctx context.Context, dir string) error {
		patch, ok := a.discards.Load(id)
		if !ok {
			return errors.New("nothing to undo")
		}
		if err := worktree.Reapply(ctx, dir, patch.(string)); err != nil {
			return err
		}
		a.discards.Delete(id)
		return nil
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

// WorktreeDiff is one changed file's diff as the pane shows it. Hash is the
// SHA-256 of the full (untruncated) diff; the pane sends it back with a hunk
// or line action so Go can refuse a diff that changed since. Patchable says
// whether the file can be acted on by hunk or line at all.
type WorktreeDiff struct {
	Text      string `json:"text"`
	Hash      string `json:"hash"`
	Truncated bool   `json:"truncated"`
	Patchable bool   `json:"patchable"`
}

// GetWorktreeDiff returns what one changed file shows in the pane: the staged
// diff against HEAD, or the unstaged diff against the index, capped. Only a
// path the status just listed can be read (see worktree.FileDiff).
func (a *App) GetWorktreeDiff(id, path string, staged bool) (WorktreeDiff, error) {
	dir, err := a.dir(id)
	if err != nil {
		return WorktreeDiff{}, err
	}
	out, st, err := worktree.FileDiffAndStatus(a.ctx, dir, path, staged)
	if err != nil {
		return WorktreeDiff{Text: out}, err
	}
	text := tools.Truncate(out, worktreeDiffCap)
	return WorktreeDiff{
		Text:      text,
		Hash:      worktree.DiffHash(out),
		Truncated: text != out,
		Patchable: worktree.Patchable(st, path, staged, out),
	}, nil
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
	repo, _ := a.repo(id)
	// Before the staged check: an off repository answers ErrAIOff first.
	cfg, o, err := a.aiSettingsFor(id)
	if err != nil {
		return err
	}
	st, err := worktree.Status(a.ctx, dir)
	if err != nil {
		return err
	}
	if len(st.Staged) == 0 {
		return worktree.ErrNothingStaged
	}
	responder, err := a.responderFor(cfg.TaskProvider, cfg.TaskModel, cfg)
	if err != nil {
		return repoNote(err, o.TaskProvider != "")
	}
	instructions, err := a.systemPrompt(repo, o, prompts.CommitMessage, prompts.Vars{
		Repo: repo.Name, Path: dir, Branch: refs.CurrentLabel(a.ctx, dir), Date: time.Now().Format("2006-01-02"),
	}, "")
	if err != nil {
		return err
	}
	prompt, err := tasks.CommitContext(a.ctx, dir, tasks.OllamaDiffBudget)
	if err != nil {
		return err
	}

	// The run is cancellable so turning the AI off stops it. A newer
	// generation replaces and cancels this one; each run removes only its
	// own entry when it ends.
	ctx, cancel := context.WithCancel(a.ctx)
	run := &commitRun{cancel: cancel}
	a.ai.mu.Lock()
	if old, ok := a.ai.commits[id]; ok {
		old.cancel()
	}
	a.ai.commits[id] = run
	a.ai.mu.Unlock()
	go func() {
		defer func() {
			a.ai.mu.Lock()
			if a.ai.commits[id] == run {
				delete(a.ai.commits, id)
			}
			a.ai.mu.Unlock()
			cancel()
		}()
		stream, err := responder.Respond(ctx, instructions, prompt)
		if err != nil {
			msg := err.Error()
			if ctx.Err() != nil {
				msg = ErrAIOff.Error()
			}
			a.emit(EventCommitDone, CommitDoneEvent{RepoID: id, RunID: runID, Error: msg})
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
		// Only the AI being turned off cancels a generation (or a newer one
		// replacing it, whose events carry another run id).
		if ctx.Err() != nil {
			done.Error = ErrAIOff.Error()
		}
		a.emit(EventCommitDone, done)
	}()
	return nil
}

// writeWorktree runs fn under the repository's write lock, so it cannot
// interleave with a merge action or an agent tool call, settles a stash
// drop a conflicted Pop still owes if fn happened to be what resolved it —
// staging a stash conflict's files through the Changes view rather than
// the merge view's StageMergeFile reaches this path instead of writeMerge,
// and would otherwise leave the popped stash entry orphaned forever — then
// tells the frontend the working tree moved. finishOwedDrop is a no-op when
// nothing is owed.
func (a *App) writeWorktree(id string, fn func(ctx context.Context, dir string) error) error {
	if err := a.write(id, func(ctx context.Context, dir string) error {
		if err := fn(ctx, dir); err != nil {
			return err
		}
		a.finishOwedDrop(id, ctx, dir)
		return nil
	}); err != nil {
		return err
	}
	a.emit(EventWorktreeChanged, WorktreeChangedEvent{RepoID: id})
	return nil
}
