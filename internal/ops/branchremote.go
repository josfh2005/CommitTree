package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"git-ui/internal/gitcmd"
	"git-ui/internal/refs"
)

// The branch context menu's Pull works on a branch's upstream; these
// are why one cannot (docs/spec/05-remote-and-stash.md).
var (
	ErrNoBranch      = errors.New("ops: no such local branch")
	ErrNoUpstream    = errors.New("ops: the branch has no upstream")
	ErrUpstreamLocal = errors.New("ops: the branch tracks a local branch")
)

// FetchRemote fetches one remote and prunes what it no longer has, the
// per-remote form of Fetch for the branch context menu.
func FetchRemote(ctx context.Context, dir, remote string) error {
	if err := checkRef(remote); err != nil {
		return err
	}
	_, err := gitcmd.Run(ctx, dir, gitcmd.NetworkTimeout, "fetch", "--prune", "--", remote)
	return err
}

// branchUpstream is a local branch's upstream as git config records it;
// remote is "" when there is none and "." when it is a local branch.
type branchUpstream struct {
	current           bool
	remote, remoteRef string
}

// pushFormat is the for-each-ref format upstreamOf reads: name, HEAD marker,
// upstream remote, upstream ref and ahead/behind.
const pushFormat = "%(refname:strip=2)%00%(HEAD)%00%(upstream:remotename)%00%(upstream:remoteref)%00%(upstream:track,nobracket)"

func upstreamOf(ctx context.Context, dir, branch string) (branchUpstream, error) {
	if err := checkRef(branch); err != nil {
		return branchUpstream{}, err
	}
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "for-each-ref", "--format="+pushFormat, "refs/heads/"+branch)
	if err != nil {
		return branchUpstream{}, err
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Split(line, "\x00")
		if len(f) == 5 && f[0] == branch {
			return branchUpstream{current: f[1] == "*", remote: f[2], remoteRef: f[3]}, nil
		}
	}
	return branchUpstream{}, fmt.Errorf("%w: %s", ErrNoBranch, branch)
}

// FastForwardBranch moves a local branch that is not checked out up to its
// upstream, fetching it first, without touching the working tree:
// `git fetch <remote> <ref>:refs/heads/<branch>`, which git refuses unless
// it is a fast-forward (and for a branch another worktree has checked out;
// its message is passed on). It reports whether the branch moved: a branch
// already at, or only ahead of, its upstream has nothing to pull. The
// current branch is pulled instead.
func FastForwardBranch(ctx context.Context, dir, branch string) (bool, error) {
	up, err := upstreamOf(ctx, dir, branch)
	if err != nil {
		return false, err
	}
	// No upstream yet: track the remote branch of the same name, when there
	// is one, and pull from it (a diverged one is refused below as usual).
	if up.remote == "" && !up.current {
		if twin := refs.Twin(ctx, dir, branch); twin != "" {
			if err := refs.SetUpstream(ctx, dir, branch, twin); err != nil {
				return false, err
			}
			if up, err = upstreamOf(ctx, dir, branch); err != nil {
				return false, err
			}
		}
	}
	switch {
	case up.remote == ".":
		return false, fmt.Errorf("%w: %s", ErrUpstreamLocal, branch)
	case up.remote == "" || up.remoteRef == "":
		return false, fmt.Errorf("%w: %s", ErrNoUpstream, branch)
	case up.current:
		return false, fmt.Errorf("%s is the current branch — pull it instead", branch)
	}
	before, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "refs/heads/"+branch)
	if err != nil {
		return false, err
	}
	_, err = gitcmd.Run(ctx, dir, gitcmd.NetworkTimeout, "fetch", "--", up.remote, up.remoteRef+":refs/heads/"+branch)
	var gerr *gitcmd.Error
	if errors.As(err, &gerr) && strings.Contains(gerr.Stderr, "non-fast-forward") {
		// Rejected because the branch is not an ancestor of the upstream: it
		// has diverged, unless it is merely ahead (the upstream, which git
		// leaves in FETCH_HEAD even on a rejection, is already in it).
		if _, ancErr := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "merge-base", "--is-ancestor", "FETCH_HEAD", "refs/heads/"+branch); ancErr == nil {
			return false, nil
		}
		return false, fmt.Errorf("%s has diverged — check it out to pull", branch)
	}
	if err != nil {
		return false, err
	}
	after, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "refs/heads/"+branch)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(after) != strings.TrimSpace(before), nil
}
