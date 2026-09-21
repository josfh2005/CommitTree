package worktree

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

// ErrNotInWorktree refuses a path that is not one Status just listed. Paths
// are compared as exact strings against what git printed, so pathspec magic
// such as ":(glob)*" can never match one.
var ErrNotInWorktree = errors.New("worktree: not a changed file of this repository")

// Stage adds one unstaged or untracked path to the index.
func Stage(ctx context.Context, dir, path string) error {
	st, err := Status(ctx, dir)
	if err != nil {
		return err
	}
	if err := listed(st, path, append(paths(st.Unstaged), paths(st.Untracked)...)); err != nil {
		return err
	}
	_, err = gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "--literal-pathspecs", "add", "--", path)
	return err
}

// Unstage takes one staged path out of the index and keeps the worktree as
// it is. A path HEAD does not have becomes untracked again, which is where it
// came from and where the view still lists it.
func Unstage(ctx context.Context, dir, path string) error {
	st, err := Status(ctx, dir)
	if err != nil {
		return err
	}
	if err := listed(st, path, paths(st.Staged)); err != nil {
		return err
	}
	_, err = gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "--literal-pathspecs", "restore", "--staged", "--", path)
	return err
}

// Discard throws a path's changes away. For a tracked path it restores from
// HEAD, taking the staged change with it; for an untracked one there is
// nothing to restore to, so the file is deleted — which the caller confirms
// first, because git cannot undo it.
func Discard(ctx context.Context, dir, path string) error {
	st, err := Status(ctx, dir)
	if err != nil {
		return err
	}
	all := append(append(paths(st.Staged), paths(st.Unstaged)...), paths(st.Untracked)...)
	if err := listed(st, path, all); err != nil {
		return err
	}
	if slices.Contains(paths(st.Untracked), path) {
		return deleteUntracked(dir, path)
	}
	_, err = gitcmd.Run(ctx, dir, gitcmd.ReadTimeout,
		"--literal-pathspecs", "restore", "--source=HEAD", "--staged", "--worktree", "--", path)
	return err
}

// deleteUntracked removes a file the repository never tracked. It refuses
// anything that is not a regular file or a symlink, so a planted directory or
// FIFO cannot make this delete outside the repository or block on open.
func deleteUntracked(dir, path string) error {
	full := filepath.Join(dir, path)
	info, err := os.Lstat(full)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("worktree: %s is not a regular file; remove it in a terminal", path)
	}
	return os.Remove(full)
}

// listed checks that path is one git printed, and that it is not also a
// directory of this repository: a literal pathspec still matches everything
// under it, so "d" would reach "d/inside.txt" too.
func listed(st State, path string, allowed []string) error {
	if !slices.Contains(allowed, path) {
		return fmt.Errorf("%w: %q", ErrNotInWorktree, path)
	}
	for _, list := range [][]string{paths(st.Staged), paths(st.Unstaged), paths(st.Untracked)} {
		for _, p := range list {
			if strings.HasPrefix(p, path+"/") {
				return fmt.Errorf("%w: %q is also a directory here; act on its files", ErrNotInWorktree, path)
			}
		}
	}
	return nil
}

func paths(files []FileStatus) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.Path)
	}
	return out
}
