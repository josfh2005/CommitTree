package merge

import (
	"context"
	"errors"
	"strings"

	"git-ui/internal/gitcmd"
)

var (
	// ErrMergeCommit's text is user-facing copy shown as-is when a cherry-pick
	// target has more than one parent.
	ErrMergeCommit   = errors.New("Cherry-picking a merge commit isn't supported")
	ErrNothingToSkip = errors.New("only a rebase or a cherry-pick step can be skipped")
)

// CherryPick applies one commit on top of the current branch. An emptied
// pick (the branch already has those changes) is skipped here and reported
// as NothingToApply, so the user is never left holding a CHERRY_PICK_HEAD
// with nothing to resolve. Any other stop that is not a conflict is aborted.
func CherryPick(ctx context.Context, dir, rev string) (Result, error) {
	if err := checkRef(rev); err != nil {
		return Result{}, err
	}
	parents, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-list", "--parents", "-n", "1", rev+"^{commit}")
	if err != nil {
		return Result{}, err
	}
	if len(strings.Fields(parents)) > 2 {
		return Result{}, ErrMergeCommit
	}
	if err := preflight(ctx, dir); err != nil {
		return Result{}, err
	}
	before, err := head(ctx, dir)
	if err != nil {
		return Result{}, err
	}
	_, err = gitcmd.RunEnv(ctx, dir, gitcmd.HookTimeout, noEditor,
		"-c", "merge.conflictStyle=zdiff3", "cherry-pick", rev)
	if err == nil {
		if after, headErr := head(ctx, dir); headErr == nil && after == before {
			return Result{Outcome: NothingToApply}, nil
		}
		return Result{Outcome: Picked}, nil
	}
	if pickedCommit(ctx, dir, "CHERRY_PICK_HEAD") == "" {
		return Result{}, err
	}
	if conflicts, listErr := Unmerged(ctx, dir); listErr == nil && len(conflicts) > 0 {
		return Result{Outcome: Conflicted, Conflicts: conflicts}, nil
	}
	// No conflicts and an index identical to HEAD: the pick came out empty.
	if _, diffErr := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "diff", "--cached", "--quiet", "HEAD"); diffErr == nil {
		if _, skipErr := gitcmd.RunEnv(ctx, dir, gitcmd.HookTimeout, noEditor, "cherry-pick", "--skip"); skipErr == nil {
			return Result{Outcome: NothingToApply}, nil
		}
	}
	_, _ = gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "cherry-pick", "--abort")
	return Result{}, err
}

// Skip drops the commit a rebase or a cherry-pick is stopped on and moves on.
func Skip(ctx context.Context, dir string) error {
	st, err := Status(ctx, dir)
	if err != nil {
		return err
	}
	if st.Kind != KindRebase && st.Kind != KindCherryPick {
		return ErrNothingToSkip
	}
	_, err = gitcmd.RunEnv(ctx, dir, gitcmd.HookTimeout, noEditor, sequencer[st.Kind], "--skip")
	return err
}
