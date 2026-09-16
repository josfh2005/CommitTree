// Package ops runs checkout, fetch and pull.
package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"git-ui/internal/gitcmd"
)

var (
	ErrNotFastForward = errors.New("cannot fast-forward: local and remote history have diverged")
	ErrInvalidRef     = errors.New("invalid ref")
)

func Checkout(ctx context.Context, dir, branch string) error {
	if err := checkRef(branch); err != nil {
		return err
	}
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "switch", branch)
	return err
}

func CheckoutRemote(ctx context.Context, dir, remote, name string) error {
	if err := checkRef(remote); err != nil {
		return err
	}
	if err := checkRef(name); err != nil {
		return err
	}
	if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", "refs/heads/"+name); err == nil {
		return Checkout(ctx, dir, name)
	}
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "switch", "-c", name, "--track", remote+"/"+name)
	return err
}

func CheckoutDetached(ctx context.Context, dir, hash string) error {
	if err := checkRef(hash); err != nil {
		return err
	}
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "switch", "--detach", hash)
	return err
}

func Fetch(ctx context.Context, dir string) error {
	_, err := gitcmd.Run(ctx, dir, gitcmd.NetworkTimeout, "fetch", "--all", "--prune")
	return err
}

func Pull(ctx context.Context, dir string) error {
	_, err := gitcmd.Run(ctx, dir, gitcmd.NetworkTimeout, "pull", "--ff-only")
	var gerr *gitcmd.Error
	if errors.As(err, &gerr) && strings.Contains(strings.ToLower(gerr.Stderr), "not possible to fast-forward") {
		return fmt.Errorf("%w\n%s", ErrNotFastForward, strings.TrimSpace(gerr.Stderr))
	}
	return err
}

func checkRef(ref string) error {
	if ref == "" || strings.HasPrefix(ref, "-") {
		return fmt.Errorf("%w: %q", ErrInvalidRef, ref)
	}
	return nil
}
