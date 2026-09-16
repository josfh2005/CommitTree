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

func CreateBranch(ctx context.Context, dir, name, target string, checkout bool) error {
	if err := validateBranch(ctx, dir, name); err != nil {
		return err
	}
	if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "branch", name, target); err != nil {
		return err
	}
	if checkout {
		_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "switch", name)
		return err
	}
	return nil
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
