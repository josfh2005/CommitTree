package merge

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"git-ui/internal/gitcmd"
)

// ErrNotInMerge refuses a path that is not one git listed for the merge in
// progress. Paths are compared as exact strings with ones git printed, so
// pathspec magic such as ":(glob)*" can never match one.
var ErrNotInMerge = errors.New("merge: not a file of this merge")

// Stage adds one of the merge's unstaged files to the index. It refuses a file
// that still has conflict markers, as the agent's stage_file does.
func Stage(ctx context.Context, dir, path string) error {
	st, err := Status(ctx, dir)
	if err != nil {
		return err
	}
	if !slices.Contains(st.Unstaged, path) {
		return fmt.Errorf("%w: %q is not an unstaged file", ErrNotInMerge, path)
	}
	// A deleted file has nothing to check, and a symlink is staged as a link,
	// never followed.
	full := filepath.Join(dir, path)
	info, err := os.Lstat(full)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	case info.Mode().IsRegular():
		data, err := os.ReadFile(full)
		if err != nil {
			return err
		}
		if HasMarkers(string(data)) {
			return fmt.Errorf("%w: %s", ErrMarkersLeft, path)
		}
	}
	_, err = gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "--literal-pathspecs", "add", "--", path)
	return err
}

// Unstage takes one of the merge's staged files out of the index and keeps
// its content in the worktree. A file HEAD doesn't have would become
// untracked, and so vanish from the merge view and from the merge commit
// unseen; it is re-added as intent-to-add so it stays listed as unstaged.
func Unstage(ctx context.Context, dir, path string) error {
	st, err := Status(ctx, dir)
	if err != nil {
		return err
	}
	if !slices.Contains(st.Staged, path) {
		return fmt.Errorf("%w: %q is not a staged file", ErrNotInMerge, path)
	}
	inHead, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "--literal-pathspecs", "ls-tree", "--name-only", "-z", "HEAD", "--", path)
	if err != nil {
		return err
	}
	if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "--literal-pathspecs", "restore", "--staged", "--", path); err != nil {
		return err
	}
	if inHead == "" {
		if _, err := os.Lstat(filepath.Join(dir, path)); err == nil {
			_, err = gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "--literal-pathspecs", "add", "--intent-to-add", "--", path)
			return err
		}
	}
	return nil
}

// changedPaths lists the paths a `git diff --name-only -z` form reports,
// minus the unmerged ones, which Status lists as conflicts instead.
func changedPaths(ctx context.Context, dir string, unmerged map[string]unmerged, args ...string) ([]string, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, append([]string{"diff", "--name-only", "-z"}, args...)...)
	if err != nil {
		return nil, err
	}
	paths := []string{}
	for _, p := range strings.Split(out, "\x00") {
		if _, conflicted := unmerged[p]; p != "" && !conflicted {
			paths = append(paths, p)
		}
	}
	slices.Sort(paths)
	return paths, nil
}
