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
// came from and where the view still lists it. If HEAD has no copy and the
// worktree has none either, the index entry is the only copy left, so
// unstaging is refused instead of losing the file. A rename is restored by
// both its old and new path together, so the index doesn't split the rename
// into a deletion at the old path plus an untracked file at the new one.
func Unstage(ctx context.Context, dir, path string) error {
	st, err := Status(ctx, dir)
	if err != nil {
		return err
	}
	if err := listed(st, path, paths(st.Staged)); err != nil {
		return err
	}
	entry, _ := findStaged(st, path)
	if entry.OldPath == "" {
		inHead, err := fileInHead(ctx, dir, path)
		if err != nil {
			return err
		}
		if !inHead {
			if _, err := os.Lstat(filepath.Join(dir, path)); err != nil {
				return fmt.Errorf("worktree: %s has no copy in the working tree; unstaging it would lose it", path)
			}
		}
		_, err = gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "--literal-pathspecs", "restore", "--staged", "--", path)
		return err
	}
	if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout,
		"--literal-pathspecs", "restore", "--staged", "--", entry.OldPath, path); err != nil {
		return err
	}
	// Restoring the new path from HEAD (which does not have it) drops it from
	// the index entirely, leaving an untracked file where git could otherwise
	// report the whole rename as one unstaged R entry. Re-adding it as
	// intent-to-add restores that pairing without touching its content.
	inHead, err := fileInHead(ctx, dir, path)
	if err != nil {
		return err
	}
	if !inHead {
		_, err = gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "--literal-pathspecs", "add", "--intent-to-add", "--", path)
	}
	return err
}

// Discard throws a path's changes away. For a tracked path it restores from
// HEAD, taking the staged change with it; for an untracked one there is
// nothing to restore to, so the file is deleted — which the caller confirms
// first, because git cannot undo it. A rename is restored by both its old and
// new path together: restoring the new path alone leaves it with no HEAD
// entry, so git would delete it rather than bring back the old path's content.
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
	args := []string{"--literal-pathspecs", "restore", "--source=HEAD", "--staged", "--worktree", "--"}
	if entry, ok := findStaged(st, path); ok && entry.OldPath != "" {
		args = append(args, entry.OldPath, path)
	} else {
		args = append(args, path)
	}
	_, err = gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, args...)
	return err
}

// findStaged looks up path's entry in the staged list, so its OldPath (set
// only for a rename) is available to callers without a second Status call.
func findStaged(st State, path string) (FileStatus, bool) {
	for _, f := range st.Staged {
		if f.Path == path {
			return f, true
		}
	}
	return FileStatus{}, false
}

// fileInHead reports whether HEAD has path itself as a file or link. ls-tree
// also prints a directory of that name, which does not count. Mirrors
// merge.fileInHead — do not reimplement this differently.
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
