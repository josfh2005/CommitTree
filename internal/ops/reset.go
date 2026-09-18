package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"git-ui/internal/gitcmd"
)

// ResetMode is how far git reset goes: soft keeps the undone changes staged,
// mixed keeps them unstaged, hard discards them and any uncommitted work.
type ResetMode string

const (
	ResetSoft  ResetMode = "soft"
	ResetMixed ResetMode = "mixed"
	ResetHard  ResetMode = "hard"
)

var (
	ErrDetached   = errors.New("HEAD is detached: there is no branch to reset")
	ErrMerging    = errors.New("a merge is in progress: finish or abort it before resetting")
	ErrInProgress = errors.New("a rebase, cherry-pick or revert is in progress: finish or abort it before resetting")
)

// inProgress are the files and directories git keeps while a rebase,
// cherry-pick or revert is stopped; a reset would throw that state away.
var inProgress = []string{"CHERRY_PICK_HEAD", "REVERT_HEAD", "sequencer", "rebase-merge", "rebase-apply"}

// fullHash is a whole SHA-1 or SHA-256 object name. Anything shorter could be
// a ref, a range or an option; the log always has the full hash.
var fullHash = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

// Reset moves the current branch to the commit hash.
func Reset(ctx context.Context, dir, hash string, mode ResetMode) error {
	switch mode {
	case ResetSoft, ResetMixed, ResetHard:
	default:
		return fmt.Errorf("unknown reset mode %q", mode)
	}
	if err := checkCommit(ctx, dir, hash); err != nil {
		return err
	}
	if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "symbolic-ref", "-q", "HEAD"); err != nil {
		return ErrDetached
	}
	// A hard reset would silently abort the merge, and a soft one would
	// leave its state behind for the next commit.
	if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", "MERGE_HEAD"); err == nil {
		return ErrMerging
	}
	for _, name := range inProgress {
		out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--git-path", name)
		if err != nil {
			return err
		}
		p := strings.TrimSpace(out)
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		if _, err := os.Lstat(p); err == nil {
			return ErrInProgress
		}
	}
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "reset", "-q", "--"+string(mode), hash, "--")
	return err
}

// ResetInfo is what the confirmation shows before a reset to a commit.
type ResetInfo struct {
	Undone   int    `json:"undone"`   // commits on HEAD that the branch would no longer contain
	Gained   int    `json:"gained"`   // commits the branch would newly contain, when hash is not behind HEAD
	Pushed   int    `json:"pushed"`   // of those, how many the upstream already has
	Upstream string `json:"upstream"` // e.g. "origin/main"; "" when the branch has none
}

// ResetPreview counts what resetting the current branch to hash would undo.
func ResetPreview(ctx context.Context, dir, hash string) (ResetInfo, error) {
	var info ResetInfo
	if err := checkCommit(ctx, dir, hash); err != nil {
		return info, err
	}
	undone, err := count(ctx, dir, "HEAD", "^"+hash)
	if err != nil {
		return info, err
	}
	info.Undone = undone
	if info.Gained, err = count(ctx, dir, hash, "^HEAD"); err != nil {
		return info, err
	}
	up, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil {
		return info, nil // no upstream: nothing can have been pushed
	}
	info.Upstream = strings.TrimSpace(up)
	// ^@{upstream}, not the abbreviated name, which a local branch could share.
	local, err := count(ctx, dir, "HEAD", "^"+hash, "^@{upstream}")
	if err != nil {
		return info, err
	}
	info.Pushed = undone - local
	return info, nil
}

// checkCommit accepts only a full hash that names an existing commit.
func checkCommit(ctx context.Context, dir, hash string) error {
	if !fullHash.MatchString(hash) {
		return fmt.Errorf("%w: %q is not a full commit hash", ErrInvalidRef, hash)
	}
	if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", hash+"^{commit}"); err != nil {
		return fmt.Errorf("%w: %s is not a commit", ErrInvalidRef, hash)
	}
	return nil
}

func count(ctx context.Context, dir string, revs ...string) (int, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, append([]string{"rev-list", "--count"}, append(revs, "--")...)...)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(out))
}
