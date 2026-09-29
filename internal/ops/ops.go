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
	return checkedOutElsewhere(branch, err)
}

// ErrCheckedOutElsewhere is git refusing to switch to a branch that another
// worktree of the same repository has checked out.
type ErrCheckedOutElsewhere struct {
	Branch, Path string
}

func (e *ErrCheckedOutElsewhere) Error() string {
	return fmt.Sprintf("%s is checked out in another worktree (%s)", e.Branch, e.Path)
}

// checkedOutElsewhere turns git's "is already used by worktree at '<path>'"
// (older git: "is already checked out at '<path>'") into
// ErrCheckedOutElsewhere; any other error is returned unchanged.
func checkedOutElsewhere(branch string, err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	for _, marker := range []string{"is already used by worktree at '", "is already checked out at '"} {
		if i := strings.Index(msg, marker); i >= 0 {
			rest := msg[i+len(marker):]
			if j := strings.Index(rest, "'"); j >= 0 {
				return &ErrCheckedOutElsewhere{Branch: branch, Path: rest[:j]}
			}
		}
	}
	return err
}

// CheckoutOutcome says what CheckoutRemote did to the local branch.
type CheckoutOutcome string

const (
	CheckoutCreated       CheckoutOutcome = "created"       // no local branch: one tracking the remote was made
	CheckoutSwitched      CheckoutOutcome = "switched"      // the local branch already had the remote's commit
	CheckoutFastForwarded CheckoutOutcome = "fastForwarded" // the local branch was behind and moved up to the remote
	CheckoutDiverged      CheckoutOutcome = "diverged"      // the local branch has its own commits and was left alone
)

// CheckoutRemote switches to the local branch for remote/name, creating it
// when missing. A local branch that is only behind is fast-forwarded to the
// remote, so the checkout lands on the commit the user picked; one with
// commits of its own is checked out untouched and reported as diverged.
func CheckoutRemote(ctx context.Context, dir, remote, name string) (CheckoutOutcome, error) {
	if err := checkRef(remote); err != nil {
		return "", err
	}
	if err := checkRef(name); err != nil {
		return "", err
	}
	upstream := remote + "/" + name
	local, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", "refs/heads/"+name)
	if err != nil {
		if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "switch", "-c", name, "--track", upstream); err != nil {
			return "", err
		}
		return CheckoutCreated, nil
	}
	outcome := CheckoutSwitched
	if target, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", "refs/remotes/"+upstream); err == nil {
		outcome = relate(ctx, dir, strings.TrimSpace(local), strings.TrimSpace(target))
	}
	// Switch first, then fast-forward with a merge: moving the ref before the
	// switch would drag a branch another worktree has checked out.
	if current, _ := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "symbolic-ref", "--quiet", "--short", "HEAD"); strings.TrimSpace(current) != name {
		if err := Checkout(ctx, dir, name); err != nil {
			return "", err
		}
	}
	if outcome == CheckoutFastForwarded {
		if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "merge", "--ff-only", "--quiet", upstream); err != nil {
			return "", fmt.Errorf("switched to %s but could not fast-forward it to %s: %w", name, upstream, err)
		}
	}
	return outcome, nil
}

// relate tells whether moving a local branch from local to target is a
// no-op, a fast-forward, or impossible without losing local commits.
func relate(ctx context.Context, dir, local, target string) CheckoutOutcome {
	isAncestor := func(a, b string) bool {
		_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "merge-base", "--is-ancestor", a, b)
		return err == nil
	}
	switch {
	case local == target, isAncestor(target, local):
		return CheckoutSwitched
	case isAncestor(local, target):
		return CheckoutFastForwarded
	default:
		return CheckoutDiverged
	}
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
	// A cancel stops git mid-merge or mid-rebase the same way a real
	// conflict would, but it is not a conflict: report it as the ordinary
	// error it is (the toast says "git command cancelled") and leave the
	// repository exactly as git left it, for the conflict banner to show.
	if errors.Is(err, gitcmd.ErrCancelled) {
		return Result{}, err
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

// BranchCounts is how far a local branch and its upstream have diverged, as
// the last fetch left them. Unlike Counts it reports a missing upstream as
// an error, so a caller can tell "up to date" from "nothing to compare".
func BranchCounts(ctx context.Context, dir, branch string) (AheadBehind, error) {
	if err := checkRef(branch); err != nil {
		return AheadBehind{}, err
	}
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-list", "--left-right", "--count", branch+"..."+branch+"@{upstream}")
	if err != nil {
		return AheadBehind{}, err
	}
	var counts AheadBehind
	if _, err := fmt.Sscan(out, &counts.Ahead, &counts.Behind); err != nil {
		return AheadBehind{}, fmt.Errorf("ops: unexpected rev-list output %q", out)
	}
	return counts, nil
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
