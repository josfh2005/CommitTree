package refs

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"git-ui/internal/gitcmd"
)

var (
	ErrNotMerged     = errors.New("branch is not fully merged")
	ErrCurrentBranch = errors.New("cannot delete the checked-out branch")
	ErrInvalidName   = errors.New("invalid ref name")
)

// CreateBranch creates name at target (and switches to it with checkout).
// --no-track keeps git's own branch.autoSetupMerge from choosing an
// upstream; TrackTwin then decides — the remote branch of the same name,
// when the two haven't diverged — and the result says what it did.
func CreateBranch(ctx context.Context, dir, name, target string, checkout bool) (Tracking, error) {
	if err := validateBranch(ctx, dir, name); err != nil {
		return Tracking{}, err
	}
	if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "branch", "--no-track", name, target); err != nil {
		return Tracking{}, err
	}
	tracking, err := TrackTwin(ctx, dir, name)
	if err != nil {
		return Tracking{}, err
	}
	if checkout {
		if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "switch", name); err != nil {
			return tracking, err
		}
	}
	return tracking, nil
}

func DeleteBranch(ctx context.Context, dir, name string, force bool) error {
	if name == "" || strings.HasPrefix(name, "-") {
		return fmt.Errorf("%w: %q", ErrInvalidName, name)
	}
	if cur, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "symbolic-ref", "-q", "--short", "HEAD"); err == nil && strings.TrimSpace(cur) == name {
		return ErrCurrentBranch
	}
	flag := "-d"
	if force {
		flag = "-D"
	}
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "branch", flag, name)
	var gerr *gitcmd.Error
	if errors.As(err, &gerr) && strings.Contains(gerr.Stderr, "not fully merged") {
		return fmt.Errorf("%w: %s", ErrNotMerged, name)
	}
	return err
}

func DeleteRemoteBranch(ctx context.Context, dir, remote, name string) error {
	if name == "" || strings.HasPrefix(name, "-") || strings.HasPrefix(remote, "-") {
		return fmt.Errorf("%w: %s/%s", ErrInvalidName, remote, name)
	}
	_, err := gitcmd.Run(ctx, dir, gitcmd.NetworkTimeout, "push", remote, "--delete", name)
	return err
}

func CreateTag(ctx context.Context, dir, name, target, message string) error {
	if name == "" || strings.HasPrefix(name, "-") {
		return fmt.Errorf("%w: %q", ErrInvalidName, name)
	}
	if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "check-ref-format", "refs/tags/"+name); err != nil {
		return fmt.Errorf("%w: %q", ErrInvalidName, name)
	}
	args := []string{"tag", name, target}
	if message != "" {
		args = []string{"tag", "-a", name, "-m", message, target}
	}
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, args...)
	return err
}

func DeleteTag(ctx context.Context, dir, name string) error {
	if name == "" || strings.HasPrefix(name, "-") {
		return fmt.Errorf("%w: %q", ErrInvalidName, name)
	}
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "tag", "-d", name)
	return err
}

func validateBranch(ctx context.Context, dir, name string) error {
	if name == "" || strings.HasPrefix(name, "-") {
		return fmt.Errorf("%w: %q", ErrInvalidName, name)
	}
	if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "check-ref-format", "--branch", name); err != nil {
		return fmt.Errorf("%w: %q", ErrInvalidName, name)
	}
	return nil
}
