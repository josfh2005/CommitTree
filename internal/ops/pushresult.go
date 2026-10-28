package ops

import (
	"errors"
	"strings"

	"git-ui/internal/gitcmd"
)

// PushStatus is how one branch of a push ended.
type PushStatus string

const (
	PushPushed   PushStatus = "pushed"
	PushUpToDate PushStatus = "upToDate"
	PushRejected PushStatus = "rejected"
	PushFailed   PushStatus = "failed"
)

// BranchPushResult is one branch's outcome; Target is <remote>/<branch>.
// Tags marks the single row PushBranches adds for "Push all tags".
type BranchPushResult struct {
	Branch string     `json:"branch"`
	Target string     `json:"target"`
	Status PushStatus `json:"status"`
	Reason string     `json:"reason,omitempty"`
	Tags   bool       `json:"tags,omitempty"`
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
	var pe *gitcmd.PromptError
	if errors.As(err, &pe) {
		return pe.Msg
	}
	if errors.Is(err, gitcmd.ErrCancelled) {
		return "Cancelled"
	}
	var gerr *gitcmd.Error
	if errors.As(err, &gerr) && strings.TrimSpace(gerr.Stderr) != "" {
		return strings.TrimSpace(gerr.Stderr)
	}
	return err.Error()
}
