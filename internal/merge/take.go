package merge

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"git-ui/internal/gitcmd"
)

// Side is which branch's version of a file to take.
type Side string

const (
	Ours   Side = "ours"
	Theirs Side = "theirs"
)

// ErrSubmodule refuses a submodule conflict: checking out one side doesn't
// settle which commit the submodule should point at.
var ErrSubmodule = errors.New("merge: a submodule conflict must be settled in a terminal")

// Take settles one of the merge's Manual files by taking one side's version
// whole and staging it. When that side deleted the file, taking it deletes
// the file. The result is staged, so Unstage undoes it.
func Take(ctx context.Context, dir, path string, side Side) error {
	stage := map[Side]int{Ours: 2, Theirs: 3}[side]
	if stage == 0 {
		return fmt.Errorf("merge: unknown side %q", side)
	}
	st, err := Status(ctx, dir)
	if err != nil {
		return err
	}
	if !slices.Contains(st.Manual, path) {
		return fmt.Errorf("%w: %q is not a file to settle by hand", ErrNotInMerge, path)
	}
	entries, err := unmergedEntries(ctx, dir)
	if err != nil {
		return err
	}
	e := entries[path]
	if slices.Contains(e.modes, "160000") {
		return ErrSubmodule
	}
	// --literal-pathspecs: path is a filename git printed, never a pattern.
	if !e.stages[stage] {
		_, err = gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "--literal-pathspecs", "rm", "-q", "--", path)
		return err
	}
	if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "--literal-pathspecs", "checkout", "--"+string(side), "--", path); err != nil {
		return err
	}
	_, err = gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "--literal-pathspecs", "add", "--", path)
	return err
}
