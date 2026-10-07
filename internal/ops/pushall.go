package ops

import (
	"context"
	"errors"
	"sort"
	"strings"

	"git-ui/internal/gitcmd"
	"git-ui/internal/refs"
)

// PushStatus is how one branch of a push of all branches ended.
type PushStatus string

const (
	PushPushed   PushStatus = "pushed"
	PushUpToDate PushStatus = "upToDate"
	PushRejected PushStatus = "rejected"
	PushFailed   PushStatus = "failed"
)

// BranchPushResult is one branch's outcome; Target is <remote>/<branch>.
type BranchPushResult struct {
	Branch string     `json:"branch"`
	Target string     `json:"target"`
	Status PushStatus `json:"status"`
	Reason string     `json:"reason,omitempty"`
}

type pushTarget struct {
	branch, remote, remoteRef string
	setUpstream               bool
}

const pushFormat = "%(refname:strip=2)%00%(HEAD)%00%(upstream:remotename)%00%(upstream:remoteref)%00%(upstream:track,nobracket)"

// pushTargets is what PushAll pushes (docs/spec/05-remote-and-stash.md): the
// current branch first — published to origin when it has no upstream —
// then, by name, every other local branch ahead of a live upstream on a
// remote. A branch tracking another local branch (remote ".") is never
// pushed: that would move the local branch, not publish anything.
func pushTargets(ctx context.Context, dir string) ([]pushTarget, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "for-each-ref", "--format="+pushFormat, "refs/heads")
	if err != nil {
		return nil, err
	}
	var current, others []pushTarget
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Split(line, "\x00")
		if len(f) != 5 {
			continue
		}
		name, head, remote, remoteRef, track := f[0], f[1], f[2], f[3], f[4]
		ahead, _, gone := refs.ParseTrack(track)
		onRemote := remote != "" && remote != "." && remoteRef != ""
		switch {
		case head == "*" && remote == "":
			current = append(current, pushTarget{branch: name, remote: "origin", remoteRef: "refs/heads/" + name, setUpstream: true})
		case head == "*" && onRemote:
			current = append(current, pushTarget{branch: name, remote: remote, remoteRef: remoteRef})
		case head != "*" && onRemote && !gone && ahead > 0:
			others = append(others, pushTarget{branch: name, remote: remote, remoteRef: remoteRef})
		}
	}
	return append(current, others...), nil
}

// PushAll pushes the current branch and every other local branch ahead of
// its upstream, one `git push --porcelain` per remote in name order. Never
// forced and not atomic: each branch succeeds or fails on its own, and a
// failure to reach one remote fails only that remote's branches.
func PushAll(ctx context.Context, dir string) ([]BranchPushResult, error) {
	targets, err := pushTargets(ctx, dir)
	if err != nil {
		return nil, err
	}
	results := make([]BranchPushResult, len(targets))
	groups := map[string][]int{}
	for i, t := range targets {
		results[i] = BranchPushResult{Branch: t.branch, Target: t.remote + "/" + strings.TrimPrefix(t.remoteRef, "refs/heads/")}
		groups[t.remote] = append(groups[t.remote], i)
	}
	remotes := make([]string, 0, len(groups))
	for r := range groups {
		remotes = append(remotes, r)
	}
	sort.Strings(remotes)
	for _, remote := range remotes {
		pushGroup(ctx, dir, remote, groups[remote], targets, results)
	}
	return results, nil
}

// pushGroup pushes the targets at idx, all on remote, and fills in their
// results. -u is only for a current branch being published; on the others
// it re-sets the upstream they already have.
func pushGroup(ctx context.Context, dir, remote string, idx []int, targets []pushTarget, results []BranchPushResult) {
	args := []string{"push", "--porcelain"}
	for _, i := range idx {
		if targets[i].setUpstream {
			args = append(args, "-u")
			break
		}
	}
	args = append(args, "--", remote)
	for _, i := range idx {
		args = append(args, "refs/heads/"+targets[i].branch+":"+targets[i].remoteRef)
	}
	out, runErr := gitcmd.Run(ctx, dir, gitcmd.NetworkTimeout, args...)
	lines := parsePorcelain(out)
	for _, i := range idx {
		line, ok := lines["refs/heads/"+targets[i].branch]
		switch {
		case ok:
			results[i].Status, results[i].Reason = classifyPushLine(line, targets[i].branch)
		case len(lines) == 0 && runErr != nil:
			results[i].Status, results[i].Reason = PushFailed, pushErrorText(runErr)
		default:
			results[i].Status, results[i].Reason = PushFailed, "No result from git"
		}
	}
}

type porcelainLine struct {
	flag    byte
	summary string
}

// parsePorcelain reads `git push --porcelain`: one "<flag>\t<src>:<dst>\t<summary>"
// line per ref between "To <url>" and "Done", keyed by src. A space flag
// that lost its space to trimming reads as a plain fast-forward.
func parsePorcelain(out string) map[string]porcelainLine {
	lines := map[string]porcelainLine{}
	for _, l := range strings.Split(out, "\n") {
		f := strings.SplitN(l, "\t", 3)
		if len(f) != 3 || len(f[0]) > 1 {
			continue
		}
		src, _, ok := strings.Cut(f[1], ":")
		if !ok {
			continue
		}
		flag := byte(' ')
		if f[0] != "" {
			flag = f[0][0]
		}
		lines[src] = porcelainLine{flag: flag, summary: f[2]}
	}
	return lines
}

func classifyPushLine(l porcelainLine, branch string) (PushStatus, string) {
	switch l.flag {
	case ' ', '*', '+':
		return PushPushed, ""
	case '=':
		return PushUpToDate, ""
	case '!':
		if strings.Contains(l.summary, "(non-fast-forward)") || strings.Contains(l.summary, "(fetch first)") {
			return PushRejected, "The remote has commits you don't have — pull " + branch + " first"
		}
		return PushRejected, l.summary
	default:
		return PushFailed, l.summary
	}
}

// pushErrorText is the reason shown for every branch of a remote whose
// push printed no ref line: git's own message, or "Cancelled".
func pushErrorText(err error) string {
	if errors.Is(err, gitcmd.ErrCancelled) {
		return "Cancelled"
	}
	var gerr *gitcmd.Error
	if errors.As(err, &gerr) && strings.TrimSpace(gerr.Stderr) != "" {
		return strings.TrimSpace(gerr.Stderr)
	}
	return err.Error()
}
