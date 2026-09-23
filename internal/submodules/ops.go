package submodules

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"git-ui/internal/gitcmd"
)

// ErrDirty is returned by Update or UpdateAll when checking a submodule out
// to its recorded commit would overwrite local changes. Path is "" for
// UpdateAll, which does not know in advance which submodule git will refuse.
type ErrDirty struct{ Path string }

func (e *ErrDirty) Error() string {
	return fmt.Sprintf("%s has local changes that updating would overwrite. Commit or stash them inside the submodule first.", e.Path)
}

// Init initialises and checks out the submodule at path (relative to dir,
// slash-separated) at its recorded commit, cloning it first if needed.
func Init(ctx context.Context, dir, path string) error {
	_, err := gitcmd.Run(ctx, dir, gitcmd.NetworkTimeout,
		"--literal-pathspecs", "submodule", "update", "--init", "--", path)
	return err
}

// Update checks the submodule at path back out to the commit dir's index
// records. It returns *ErrDirty when local changes would be overwritten.
func Update(ctx context.Context, dir, path string) error {
	_, err := gitcmd.Run(ctx, dir, gitcmd.NetworkTimeout,
		"--literal-pathspecs", "submodule", "update", "--", path)
	return asDirty(err, path)
}

// Sync writes .gitmodules' current URL for path into the submodule's own
// remote.origin.url, so a URL change takes effect on the next fetch.
func Sync(ctx context.Context, dir, path string) error {
	_, err := gitcmd.Run(ctx, dir, gitcmd.NetworkTimeout,
		"--literal-pathspecs", "submodule", "sync", "--", path)
	return err
}

// InitAll initialises and checks out every submodule under dir, recursively.
func InitAll(ctx context.Context, dir string) error {
	_, err := gitcmd.Run(ctx, dir, gitcmd.NetworkTimeout,
		"--literal-pathspecs", "submodule", "update", "--init", "--recursive")
	return err
}

// UpdateAll checks every submodule under dir back out to its recorded
// commit, recursively. It returns *ErrDirty (with an empty Path — git does
// not say which submodule it refused) when local changes would be
// overwritten.
func UpdateAll(ctx context.Context, dir string) error {
	_, err := gitcmd.Run(ctx, dir, gitcmd.NetworkTimeout,
		"--literal-pathspecs", "submodule", "update", "--recursive")
	return asDirty(err, "")
}

// asDirty maps a git error whose stderr reports an overwrite refusal to
// *ErrDirty, leaving any other error untouched.
func asDirty(err error, path string) error {
	if err == nil {
		return nil
	}
	var gerr *gitcmd.Error
	if errors.As(err, &gerr) && strings.Contains(gerr.Stderr, "would be overwritten") {
		return &ErrDirty{Path: path}
	}
	return err
}
