package worktrees

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"git-ui/internal/gitcmd"
	"git-ui/internal/worktree"
)

// canonical resolves symlinks (macOS's /var vs /private/var above all) so a
// path compares equal to git's own output however it was spelled.
func canonical(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return filepath.Clean(path)
}

// RemovalInfo is what the "Remove worktree…" confirmation shows: the
// worktree's branch (detached when Branch is ""), how many uncommitted
// changes it has, whether it is locked, and whether its branch is merged
// into the main working tree's HEAD — what `git branch -d` would accept.
type RemovalInfo struct {
	Branch   string `json:"branch"`
	Detached bool   `json:"detached"`
	Changes  int    `json:"changes"`
	Locked   bool   `json:"locked"`
	Merged   bool   `json:"merged"`
}

// Info reports path's removal state, resolved from mainDir. Worktree
// metadata is shared across a repository's worktrees, so it could equally
// be read from path itself; mainDir is used because callers already have it
// at hand — the same directory Remove requires (see its own comment) for
// the removal this reports on.
func Info(ctx context.Context, mainDir, path string) (RemovalInfo, error) {
	list, err := List(ctx, mainDir)
	if err != nil {
		return RemovalInfo{}, err
	}
	var found *Worktree
	target := canonical(path)
	for i := range list {
		if canonical(list[i].Path) == target {
			found = &list[i]
			break
		}
	}
	if found == nil {
		return RemovalInfo{}, fmt.Errorf("%q is not a worktree of this repository", path)
	}

	info := RemovalInfo{Branch: found.Branch, Detached: found.Detached, Locked: found.Locked}

	st, err := worktree.Status(ctx, path)
	if err != nil {
		return RemovalInfo{}, err
	}
	changed := map[string]bool{}
	for _, list := range [][]worktree.FileStatus{st.Staged, st.Unstaged, st.Untracked} {
		for _, f := range list {
			changed[f.Path] = true
		}
	}
	info.Changes = len(changed)

	if !found.Detached && found.Branch != "" {
		info.Merged = branchMerged(ctx, mainDir, found.Branch)
	}
	return info, nil
}

// branchMerged reports whether name is merged into mainDir's current HEAD —
// exactly what `git branch -d` (without --force) requires to succeed. A
// failure to tell (e.g. the branch was deleted concurrently) reports false
// rather than erroring, since this only feeds a confirmation dialog's
// default checkbox state.
func branchMerged(ctx context.Context, mainDir, name string) bool {
	out, err := gitcmd.Run(ctx, mainDir, gitcmd.ReadTimeout, "branch", "--list", "--merged", "HEAD", "--format=%(refname:short)", name)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == name {
			return true
		}
	}
	return false
}

// Remove runs `git worktree remove [--force] <path>` from mainDir — the
// main repository's directory, never the worktree's own, since git refuses
// to remove a worktree that is the process's current directory. Without
// force it refuses a worktree with uncommitted changes or one that is
// locked; force overrides uncommitted changes but not a lock — a locked
// worktree must be unlocked (`git worktree unlock`) first, which this never
// does on its own.
func Remove(ctx context.Context, mainDir, path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	_, err := gitcmd.Run(ctx, mainDir, gitcmd.HookTimeout, args...)
	return err
}
