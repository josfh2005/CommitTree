package merge

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"git-ui/internal/gitcmd"
)

var (
	// ErrOperationInProgress: git refuses to start one sequencer operation
	// inside another, and its conflicts would not be this one's. preflight
	// wraps it with the kind in progress, so the text the user sees reads
	// "Finish the rebase in progress first" and the like; errors.Is still
	// matches the sentinel through the wrap.
	ErrOperationInProgress = errors.New("operation in progress")
	// ErrDirtyWorktree is checked before git runs, so a user's
	// rebase.autoStash never stashes behind the app's back.
	ErrDirtyWorktree = errors.New("Commit or stash your changes first")
	ErrDetachedHead  = errors.New("No branch is checked out")
)

// operationInProgressError is ErrOperationInProgress with the kind filled
// in, so Error() reads exactly "Finish the <kind> in progress first" — no
// appended sentinel text — while errors.Is(err, ErrOperationInProgress)
// still holds through Unwrap.
type operationInProgressError struct {
	kind Kind
}

func (e *operationInProgressError) Error() string {
	return fmt.Sprintf("Finish the %s in progress first", e.kind)
}

func (e *operationInProgressError) Unwrap() error {
	return ErrOperationInProgress
}

// HasTrackedChanges reports staged or unstaged changes to tracked files.
// Untracked files don't count (git refuses on its own if one is in the way),
// nor do submodules, which `git rebase` ignores when it checks for a clean tree.
func HasTrackedChanges(ctx context.Context, dir string) (bool, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "status", "--porcelain", "--untracked-files=no", "--ignore-submodules=all")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// preflight is what a rebase or a cherry-pick needs before git runs: a
// branch checked out, nothing else in progress, and a clean tracked tree.
func preflight(ctx context.Context, dir string) error {
	if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "symbolic-ref", "-q", "HEAD"); err != nil {
		return ErrDetachedHead
	}
	st, err := Status(ctx, dir)
	if err != nil {
		return err
	}
	if st.Merging {
		return &operationInProgressError{kind: st.Kind}
	}
	dirty, err := HasTrackedChanges(ctx, dir)
	if err != nil {
		return err
	}
	if dirty {
		return ErrDirtyWorktree
	}
	return nil
}

// Rebase replays the current branch's commits onto onto. A rebase stopped on
// conflicts is left for the Conflicts view; one git stopped for any other
// reason (an untracked file in the way, a hook) is aborted so the user is
// never left in a half-started rebase they did not see begin.
func Rebase(ctx context.Context, dir, onto string) (Result, error) {
	if err := checkRef(onto); err != nil {
		return Result{}, err
	}
	if err := preflight(ctx, dir); err != nil {
		return Result{}, err
	}
	before, err := head(ctx, dir)
	if err != nil {
		return Result{}, err
	}
	_, err = gitcmd.RunEnv(ctx, dir, gitcmd.HookTimeout, noEditor,
		"-c", "merge.conflictStyle=zdiff3", "rebase", onto)
	if err == nil {
		after, err := head(ctx, dir)
		if err != nil {
			return Result{}, err
		}
		if after == before {
			return Result{Outcome: UpToDate}, nil
		}
		return Result{Outcome: Rebased}, nil
	}
	if _, ok := inRebase(ctx, dir); ok {
		if conflicts, listErr := Unmerged(ctx, dir); listErr == nil && len(conflicts) > 0 {
			return Result{Outcome: Conflicted, Conflicts: conflicts}, nil
		}
		_, _ = gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rebase", "--abort")
	}
	return Result{}, err
}

// Preview is what the rebase confirmation says: how many commits will be
// replayed, how many merges flattened, and how many are already on the
// upstream (so a force-push would be needed).
type Preview struct {
	Commits   int    `json:"commits"`
	Merges    int    `json:"merges"`
	Published int    `json:"published"`
	Upstream  string `json:"upstream"`
}

func RebasePreview(ctx context.Context, dir, onto string) (Preview, error) {
	var p Preview
	if err := checkRef(onto); err != nil {
		return p, err
	}
	var err error
	if p.Commits, err = countRevs(ctx, dir, "--no-merges", "HEAD", "^"+onto); err != nil {
		return p, err
	}
	if p.Merges, err = countRevs(ctx, dir, "--merges", "HEAD", "^"+onto); err != nil {
		return p, err
	}
	up, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil {
		return p, nil // no upstream: nothing can have been pushed
	}
	p.Upstream = strings.TrimSpace(up)
	// ^@{upstream}, not the abbreviated name, which a local branch could share.
	local, err := countRevs(ctx, dir, "--no-merges", "HEAD", "^"+onto, "^@{upstream}")
	if err != nil {
		return p, err
	}
	p.Published = p.Commits - local
	return p, nil
}

// IsAncestorOfHead reports whether rev is already contained in HEAD: a
// rebase onto it has nothing to do, and a cherry-pick of it nothing to apply.
func IsAncestorOfHead(ctx context.Context, dir, rev string) (bool, error) {
	if err := checkRef(rev); err != nil {
		return false, err
	}
	if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", rev+"^{commit}"); err != nil {
		return false, err
	}
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "merge-base", "--is-ancestor", rev, "HEAD")
	if err == nil {
		return true, nil
	}
	var gerr *gitcmd.Error
	if errors.As(err, &gerr) && gerr.ExitCode == 1 {
		return false, nil
	}
	return false, err
}

func countRevs(ctx context.Context, dir string, args ...string) (int, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, append([]string{"rev-list", "--count"}, args...)...)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(out))
}
