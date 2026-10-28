package ops

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"git-ui/internal/gitcmd"
)

// PushSpec is one branch of a PushRequest. Remote is the branch name on the
// remote; "" means the same as Local. Track makes Local follow it (-u).
type PushSpec struct {
	Local  string `json:"local"`
	Remote string `json:"remote"`
	Track  bool   `json:"track"`
}

// PushRequest is what the Push dialog sends: the branches to push to one
// remote, whether to push all tags too, and whether the branches may
// overwrite the remote's (with a lease, never a bare force).
type PushRequest struct {
	Remote   string     `json:"remote"`
	Branches []PushSpec `json:"branches"`
	Tags     bool       `json:"tags"`
	Force    bool       `json:"force"`
}

func (s PushSpec) remoteBranch() string {
	if s.Remote != "" {
		return s.Remote
	}
	return s.Local
}

// PushBranches pushes the chosen branches to req.Remote (docs/spec/05-remote-and-stash.md,
// Push dialog): one result per branch in request order, then one "Tags" row
// when req.Tags. Not atomic: each branch succeeds or fails on its own. Track
// sets the upstream with -u; an untracked branch keeps whatever upstream it
// already has. Force uses --force-with-lease against the remote-tracking ref,
// so a remote that moved since the last fetch rejects the branch instead of
// losing commits; tags are never forced. A bad request (unknown remote,
// nothing to push, a Local that is not a local branch) fails before any push.
func PushBranches(ctx context.Context, dir string, req PushRequest) ([]BranchPushResult, error) {
	if req.Remote == "" {
		return nil, errors.New("choose a remote to push to")
	}
	if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "remote", "get-url", req.Remote); err != nil {
		return nil, fmt.Errorf("unknown remote %q", req.Remote)
	}
	if len(req.Branches) == 0 && !req.Tags {
		return nil, errors.New("nothing to push: choose a branch or tags")
	}
	seen := map[string]bool{}
	for _, s := range req.Branches {
		for _, name := range []string{s.Local, s.remoteBranch()} {
			if !validBranchName(ctx, dir, name) {
				return nil, fmt.Errorf("invalid branch name %q", name)
			}
		}
		if rb := s.remoteBranch(); seen[rb] {
			return nil, fmt.Errorf("two branches push to %q", rb)
		} else {
			seen[rb] = true
		}
	}
	for _, s := range req.Branches {
		if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", "refs/heads/"+s.Local); err != nil {
			return nil, fmt.Errorf("%q is not a local branch", s.Local)
		}
	}

	results := make([]BranchPushResult, len(req.Branches), len(req.Branches)+1)
	var tracked, untracked []int
	for i, s := range req.Branches {
		results[i] = BranchPushResult{Branch: s.Local, Target: req.Remote + "/" + s.remoteBranch()}
		if s.Track {
			tracked = append(tracked, i)
		} else {
			untracked = append(untracked, i)
		}
	}
	// One OK prompts for credentials at most once: when an invocation ends
	// in a cancelled or failed prompt, the rest are not started and fail
	// with the same reason.
	var stop error
	for _, g := range []struct {
		idx []int
		set bool
	}{{tracked, true}, {untracked, false}} {
		if stop != nil {
			failRows(g.idx, results, stop)
			continue
		}
		stop = pushSpecs(ctx, dir, req, g.idx, g.set, results)
	}
	if req.Tags {
		if stop != nil {
			results = append(results, BranchPushResult{Branch: "Tags", Target: req.Remote, Status: PushFailed, Reason: pushErrorText(stop), Tags: true})
		} else {
			results = append(results, pushTags(ctx, dir, req.Remote))
		}
	}
	return results, nil
}

// validBranchName reports whether name is a branch name git accepts as it
// stands (--branch would also expand "@{-1}" to a name; that is not one).
func validBranchName(ctx context.Context, dir, name string) bool {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "check-ref-format", "--branch", name)
	return err == nil && strings.TrimSpace(out) == name
}

func failRows(idx []int, results []BranchPushResult, err error) {
	for _, i := range idx {
		results[i].Status, results[i].Reason = PushFailed, pushErrorText(err)
	}
}

// endsInPromptStop reports whether err is a credentials prompt that was
// cancelled or refused, or a push cancelled outright: a reason not to try
// the next invocation.
func endsInPromptStop(ctx context.Context, err error) bool {
	var pe *gitcmd.PromptError
	return errors.As(err, &pe) || errors.Is(err, gitcmd.ErrCancelled) || ctx.Err() != nil
}

// pushSpecs pushes the specs at idx in one `git push --porcelain`, with -u
// when setUpstream, and fills in their results. It returns the error that
// should stop the invocations after it (see endsInPromptStop), nil otherwise.
func pushSpecs(ctx context.Context, dir string, req PushRequest, idx []int, setUpstream bool, results []BranchPushResult) error {
	if len(idx) == 0 {
		return nil
	}
	args := []string{"push", "--porcelain"}
	if setUpstream {
		args = append(args, "-u")
	}
	if req.Force {
		for _, i := range idx {
			rb := req.Branches[i].remoteBranch()
			// The lease expects the remote branch where we last saw it; no
			// remote-tracking ref means it must not exist.
			expected, _ := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", "refs/remotes/"+req.Remote+"/"+rb)
			args = append(args, "--force-with-lease=refs/heads/"+rb+":"+strings.TrimSpace(expected))
		}
	}
	args = append(args, "--", req.Remote)
	for _, i := range idx {
		s := req.Branches[i]
		args = append(args, "refs/heads/"+s.Local+":refs/heads/"+s.remoteBranch())
	}
	out, runErr := gitcmd.Run(ctx, dir, gitcmd.NetworkTimeout, args...)
	lines := parsePorcelain(out)
	for _, i := range idx {
		s := req.Branches[i]
		line, ok := lines["refs/heads/"+s.Local]
		switch {
		case ok:
			results[i].Status, results[i].Reason = classifyPushLine(line, s.Local)
		case len(lines) == 0 && runErr != nil:
			results[i].Status, results[i].Reason = PushFailed, pushErrorText(runErr)
		default:
			results[i].Status, results[i].Reason = PushFailed, "No result from git"
		}
	}
	if len(lines) == 0 && runErr != nil && endsInPromptStop(ctx, runErr) {
		return runErr
	}
	return nil
}

// pushTags pushes every local tag to remote and folds git's per-tag lines
// into one row: rejected if any tag was, else pushed if any went, else up to
// date.
func pushTags(ctx context.Context, dir, remote string) BranchPushResult {
	res := BranchPushResult{Branch: "Tags", Target: remote, Tags: true}
	out, runErr := gitcmd.Run(ctx, dir, gitcmd.NetworkTimeout, "push", "--porcelain", "--tags", "--", remote)
	lines := parsePorcelain(out)
	if len(lines) == 0 {
		if runErr != nil {
			res.Status, res.Reason = PushFailed, pushErrorText(runErr)
		} else {
			res.Status = PushUpToDate
		}
		return res
	}
	refs := make([]string, 0, len(lines))
	for ref := range lines {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	res.Status = PushUpToDate
	for _, ref := range refs {
		l := lines[ref]
		switch l.flag {
		case '=':
		case '!':
			if res.Status != PushRejected {
				res.Status, res.Reason = PushRejected, l.summary
			}
		default:
			if res.Status == PushUpToDate {
				res.Status = PushPushed
			}
		}
	}
	return res
}
