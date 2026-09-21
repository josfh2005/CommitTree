package worktree

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"git-ui/internal/gitcmd"
)

// ErrNothingStaged refuses a commit with an empty index, which git would
// reject anyway, with a message about the working tree instead of the index.
var ErrNothingStaged = errors.New("worktree: nothing is staged")

// CommitInfo is what the commit box needs to decide what it can offer.
type CommitInfo struct {
	StagedCount int    `json:"stagedCount"`
	CanAmend    bool   `json:"canAmend"`    // false in a repository with no commits
	LastMessage string `json:"lastMessage"` // the message Amend starts from
	Pushed      bool   `json:"pushed"`      // the commit Amend would rewrite is on the upstream
	Upstream    string `json:"upstream"`
}

// Commit closes the staged changes. An amend rewrites the last commit, and is
// allowed with nothing staged because rewriting only the message is the
// common case.
func Commit(ctx context.Context, dir, message string, amend bool) error {
	if strings.TrimSpace(message) == "" {
		return errors.New("worktree: the commit message is empty")
	}
	if !amend {
		st, err := Status(ctx, dir)
		if err != nil {
			return err
		}
		if len(st.Staged) == 0 {
			return ErrNothingStaged
		}
	}
	// The message goes through a file: on a command line a long body with
	// quotes, newlines or a leading dash is at the mercy of quoting and of
	// ARG_MAX.
	path, cleanup, err := messageFile(ctx, dir, message)
	if err != nil {
		return err
	}
	defer cleanup()

	args := []string{"commit", "-F", path}
	if amend {
		args = append(args, "--amend")
	}
	_, err = gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, args...)
	return err
}

// messageFile writes the message inside the repository's own git directory,
// so it never lands in the working tree and never shows up as untracked.
func messageFile(ctx context.Context, dir, message string) (string, func(), error) {
	gitDir, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", func() {}, err
	}
	f, err := os.CreateTemp(strings.TrimSpace(gitDir), "git-ui-commit-*")
	if err != nil {
		return "", func() {}, err
	}
	name := f.Name()
	cleanup := func() { os.Remove(name) }
	if _, err := f.WriteString(message); err != nil {
		f.Close()
		cleanup()
		return "", func() {}, err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return name, cleanup, nil
}

// Preview reports what the commit box may offer: how much is staged, whether
// there is a commit to amend, its message, and whether amending it would
// rewrite something the upstream already has.
func Preview(ctx context.Context, dir string) (CommitInfo, error) {
	st, err := Status(ctx, dir)
	if err != nil {
		return CommitInfo{}, err
	}
	info := CommitInfo{StagedCount: len(st.Staged)}

	if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", "HEAD"); err != nil {
		return info, nil // no commits yet: nothing to amend
	}
	info.CanAmend = true
	if msg, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "log", "-1", "--pretty=%B"); err == nil {
		info.LastMessage = strings.TrimRight(msg, "\n")
	}

	// The symbolic @{upstream}, not an abbreviated remote-tracking name a
	// local branch could shadow — see ops.ResetPreview for the same fix.
	up, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil {
		return info, nil // no upstream: nothing can have been pushed
	}
	info.Upstream = strings.TrimSpace(up)
	// HEAD is "pushed" when it has no commits the upstream lacks.
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-list", "--count", "HEAD", "^@{upstream}", "--")
	if err != nil {
		return info, nil
	}
	ahead, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return info, fmt.Errorf("worktree: count ahead: %w", err)
	}
	info.Pushed = ahead == 0
	return info, nil
}
