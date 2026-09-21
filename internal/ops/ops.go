// Package ops runs checkout, fetch, pull, push and reset.
package ops

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"git-ui/internal/gitcmd"
	"git-ui/internal/merge"
)

var (
	ErrInvalidRef           = errors.New("invalid ref")
	ErrInvalidStrategy      = errors.New("ops: invalid pull strategy")
	ErrResolutionInProgress = errors.New("ops: finish the conflict in progress first")
)

const (
	StrategyAuto   = "auto"
	StrategyMerge  = "merge"
	StrategyRebase = "rebase"
)

// Outcome says how a Pull ended.
type Outcome int

const (
	UpToDate Outcome = iota
	Merged
	Rebased
	Conflicted
)

// Result is what Pull produces; Conflicts is set only when Conflicted.
type Result struct {
	Outcome   Outcome  `json:"outcome"`
	Conflicts []string `json:"conflicts"`
}

// AheadBehind is how far the current branch and its upstream have diverged.
type AheadBehind struct {
	Ahead  int `json:"ahead"`
	Behind int `json:"behind"`
}

func Checkout(ctx context.Context, dir, branch string) error {
	if err := checkRef(branch); err != nil {
		return err
	}
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "switch", branch)
	return err
}

func CheckoutRemote(ctx context.Context, dir, remote, name string) error {
	if err := checkRef(remote); err != nil {
		return err
	}
	if err := checkRef(name); err != nil {
		return err
	}
	if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", "refs/heads/"+name); err == nil {
		return Checkout(ctx, dir, name)
	}
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "switch", "-c", name, "--track", remote+"/"+name)
	return err
}

func CheckoutDetached(ctx context.Context, dir, hash string) error {
	if err := checkRef(hash); err != nil {
		return err
	}
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "switch", "--detach", hash)
	return err
}

func Fetch(ctx context.Context, dir string) error {
	_, err := gitcmd.Run(ctx, dir, gitcmd.NetworkTimeout, "fetch", "--all", "--prune")
	return err
}

// Pull fetches and integrates the upstream. "auto" runs a plain pull, so
// git's own resolved pull.rebase config decides merge or rebase, same as a
// pull typed at a terminal would; "merge"/"rebase" force this call's choice,
// the one-shot -c pattern merge.Start already uses for merge.conflictStyle.
// A conflict is reported as Result, not returned as an error — the same
// distinction merge.Start draws for a conflicted merge.
func Pull(ctx context.Context, dir, strategy string) (Result, error) {
	args := []string{"pull", "--"}
	switch strategy {
	case StrategyAuto:
	case StrategyMerge:
		args = append([]string{"-c", "pull.rebase=false"}, args...)
	case StrategyRebase:
		args = append([]string{"-c", "pull.rebase=true"}, args...)
	default:
		return Result{}, fmt.Errorf("%w: %q", ErrInvalidStrategy, strategy)
	}

	// A repository already mid-merge, mid-rebase or mid-anything cannot be
	// pulled into, and more importantly the conflict-detection below could
	// not tell a conflict this pull caused from one that was already there.
	// Refuse up front instead of guessing afterwards. (The frontend's
	// canSync already disables Pull in that state; this is the backend's own
	// guarantee, for an agent tool call or a race with a finishing merge.)
	if st, stErr := merge.Status(ctx, dir); stErr == nil && st.Merging {
		return Result{}, fmt.Errorf("%w: %s", ErrResolutionInProgress, st.Kind)
	}

	before, err := head(ctx, dir)
	if err != nil {
		return Result{}, err
	}
	_, err = gitcmd.Run(ctx, dir, gitcmd.NetworkTimeout, args...)
	if err == nil {
		after, headErr := head(ctx, dir)
		if headErr != nil {
			return Result{}, headErr
		}
		switch {
		case after == before:
			return Result{Outcome: UpToDate}, nil
		case before == "":
			return Result{Outcome: Merged}, nil // first pull into an empty repository
		default:
			if _, ffErr := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "merge-base", "--is-ancestor", before, after); ffErr == nil {
				return Result{Outcome: Merged}, nil
			}
			return Result{Outcome: Rebased}, nil
		}
	}
	// A conflict leaves the repository mid-merge or mid-rebase. Nothing was
	// in progress before this call (the guard above), so a Merging state
	// here is this pull's own doing. Anything else (network, auth, a dirty
	// worktree) is a real failure and is returned as-is.
	if st, statusErr := merge.Status(ctx, dir); statusErr == nil && st.Merging {
		return Result{Outcome: Conflicted, Conflicts: append(append([]string{}, st.Conflicts...), st.Manual...)}, nil
	}
	return Result{}, err
}

// Push publishes the current branch. One with no upstream yet gets one on
// origin, by convention; an existing upstream is pushed to as-is.
func Push(ctx context.Context, dir string) error {
	if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"); err != nil {
		branch, brErr := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "symbolic-ref", "-q", "--short", "HEAD")
		if brErr != nil {
			return brErr // detached HEAD: nothing to publish
		}
		_, err := gitcmd.Run(ctx, dir, gitcmd.NetworkTimeout, "push", "-u", "origin", "--", strings.TrimSpace(branch))
		return err
	}
	_, err := gitcmd.Run(ctx, dir, gitcmd.NetworkTimeout, "push", "--")
	return err
}

// Counts reports how far the current branch and its upstream have diverged,
// for the toolbar's badge. No upstream — or any other failure reading it —
// yields a zero AheadBehind rather than an error, the same convention
// worktree.Preview uses for its own @{upstream} lookup.
func Counts(ctx context.Context, dir string) (AheadBehind, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-list", "--left-right", "--count", "HEAD...@{upstream}")
	if err != nil {
		return AheadBehind{}, nil
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return AheadBehind{}, nil
	}
	ahead, err1 := strconv.Atoi(fields[0])
	behind, err2 := strconv.Atoi(fields[1])
	if err1 != nil || err2 != nil {
		return AheadBehind{}, nil
	}
	return AheadBehind{Ahead: ahead, Behind: behind}, nil
}

func checkRef(ref string) error {
	if ref == "" || strings.HasPrefix(ref, "-") {
		return fmt.Errorf("%w: %q", ErrInvalidRef, ref)
	}
	return nil
}

// head returns the current commit, or "" in a repository with no commits.
func head(ctx context.Context, dir string) (string, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil {
		var gerr *gitcmd.Error
		if errors.As(err, &gerr) && gerr.ExitCode == 1 {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(out), nil
}
