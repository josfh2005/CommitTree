package worktree

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"git-ui/internal/gitcmd"
)

// FileDiff returns one changed file's diff: the staged diff against HEAD, or
// the unstaged diff against the index (an untracked file's whole content as
// an addition). Only a path Status just listed can be read, so a path from
// the renderer or a model cannot be used to read the disk. The diff is not
// capped; callers cut it to their own budget.
func FileDiff(ctx context.Context, dir, path string, staged bool) (string, error) {
	st, err := Status(ctx, dir)
	if err != nil {
		return "", err
	}
	known := []string{}
	for _, list := range [][]FileStatus{st.Staged, st.Unstaged, st.Untracked} {
		for _, f := range list {
			known = append(known, f.Path)
		}
	}
	if !slices.Contains(known, path) {
		return "", fmt.Errorf("%q is not a changed file of this repository", path)
	}
	// An untracked file has no HEAD or index side to diff against; --no-index
	// against /dev/null is what shows its whole content as an addition. That
	// mode behaves like the plain diff(1) command it emulates: it exits 1
	// merely because the two sides differ, not because anything went wrong -
	// but git overloads that same exit code for "could not access the path",
	// which is exactly what a stale path produces if the file vanished
	// between the Status read above and this call. Exit 1 can't tell the two
	// apart on its own, so the path is checked on disk first; only once it's
	// confirmed to exist is exit 1 read as "they differ".
	if !staged && slices.ContainsFunc(st.Untracked, func(f FileStatus) bool { return f.Path == path }) {
		if _, statErr := os.Lstat(filepath.Join(dir, path)); statErr != nil {
			return "", statErr
		}
		out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "--literal-pathspecs", "diff", "--no-index", "--", "/dev/null", path)
		var gerr *gitcmd.Error
		if errors.As(err, &gerr) && gerr.ExitCode == 1 {
			return out, nil
		}
		return out, err
	}
	args := []string{"--literal-pathspecs", "diff", "--submodule=log"}
	if staged {
		args = append(args, "--cached")
	}
	args = append(args, "--", path)
	return gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, args...)
}
