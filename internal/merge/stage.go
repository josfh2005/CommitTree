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
	if err := settled(st, st.Unstaged, path, "an unstaged file"); err != nil {
		return err
	}
	// A deleted file has nothing to check, and a symlink is staged as a link,
	// never followed. A directory would stage whatever is under it.
	full := filepath.Join(dir, path)
	info, err := os.Lstat(full)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	case info.IsDir():
		return fmt.Errorf("%w: %q is a directory", ErrNotInMerge, path)
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
	if err := settled(st, st.Staged, path, "a staged file"); err != nil {
		return err
	}
	inHead, err := fileInHead(ctx, dir, path)
	if err != nil {
		return err
	}
	if !inHead {
		// The index holds the only copy of a new file missing from the
		// worktree; dropping the entry would lose it.
		if _, err := os.Lstat(filepath.Join(dir, path)); err != nil {
			return fmt.Errorf("merge: %s has no copy in the working tree; unstaging it would lose it", path)
		}
	}
	if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "--literal-pathspecs", "restore", "--staged", "--", path); err != nil {
		return err
	}
	if !inHead {
		_, err = gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "--literal-pathspecs", "add", "--intent-to-add", "--", path)
		return err
	}
	return nil
}

// settled checks that path is in list, and that it is not also a directory
// of this merge: a literal pathspec still matches everything under it, so
// "d" would reach "d/x" too.
func settled(st State, list []string, path, what string) error {
	if !slices.Contains(list, path) {
		return fmt.Errorf("%w: %q is not %s", ErrNotInMerge, path, what)
	}
	for _, l := range [][]string{st.Conflicts, st.Manual, st.Staged, st.Unstaged} {
		for _, p := range l {
			if strings.HasPrefix(p, path+"/") {
				return fmt.Errorf("%w: %q is also a directory in this merge; settle it in a terminal", ErrNotInMerge, path)
			}
		}
	}
	return nil
}

// fileInHead reports whether HEAD has path itself as a file or link. ls-tree
// also prints a directory of that name, which does not count.
func fileInHead(ctx context.Context, dir, path string) (bool, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "--literal-pathspecs", "ls-tree", "-z", "HEAD", "--", path)
	if err != nil {
		return false, err
	}
	for _, rec := range strings.Split(out, "\x00") {
		meta, name, ok := strings.Cut(rec, "\t")
		if fields := strings.Fields(meta); ok && name == path && len(fields) == 3 && fields[1] != "tree" {
			return true, nil
		}
	}
	return false, nil
}

// changedPaths lists the paths a `git diff --name-only -z` form reports,
// minus the unmerged ones, which Status lists as conflicts instead.
func changedPaths(ctx context.Context, dir string, unmerged map[string]unmerged, args ...string) ([]string, error) {
	// --no-renames: a rename listed as its new name alone would hide the
	// deletion of the old one, whatever the user's diff.renames says.
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, append([]string{"diff", "--name-only", "--no-renames", "-z"}, args...)...)
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
