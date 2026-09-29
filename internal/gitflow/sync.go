package gitflow

import (
	"context"
	"fmt"
	"strings"

	"git-ui/internal/ops"
)

// DivergedError: the branch has commits its upstream lacks and the
// upstream has commits it lacks; only a pull can reconcile them.
type DivergedError struct{ Branch, Upstream string }

func (e *DivergedError) Error() string {
	return fmt.Sprintf("%s has diverged from %s; pull it first", e.Branch, e.Upstream)
}

// upstream is branch's upstream and how far the two have moved apart;
// full is "" when branch has no upstream.
type upstream struct {
	full, short   string
	ahead, behind int
}

func upstreamOf(ctx context.Context, dir, branch string) (upstream, error) {
	full, err := git(ctx, dir, "rev-parse", "--symbolic-full-name", branch+"@{upstream}")
	if err != nil || full == "" {
		return upstream{}, nil
	}
	short, err := git(ctx, dir, "rev-parse", "--abbrev-ref", branch+"@{upstream}")
	if err != nil {
		return upstream{}, err
	}
	counts, err := git(ctx, dir, "rev-list", "--left-right", "--count", branch+"..."+full)
	if err != nil {
		return upstream{}, err
	}
	u := upstream{full: full, short: short}
	if _, err := fmt.Sscanf(counts, "%d\t%d", &u.ahead, &u.behind); err != nil {
		return upstream{}, fmt.Errorf("reading %s…%s: %q", branch, short, counts)
	}
	return u, nil
}

// checkBranch fails with DivergedError when branch can only be reconciled
// with its upstream by a pull; it changes nothing.
func checkBranch(ctx context.Context, dir, branch string) error {
	u, err := upstreamOf(ctx, dir, branch)
	if err != nil {
		return err
	}
	if u.ahead > 0 && u.behind > 0 {
		return &DivergedError{Branch: branch, Upstream: u.short}
	}
	return nil
}

// syncBranch fast-forwards branch to its upstream when it is only behind.
// No upstream, or nothing to take, does nothing.
func syncBranch(ctx context.Context, dir, branch string) (string, error) {
	u, err := upstreamOf(ctx, dir, branch)
	if err != nil || u.full == "" || u.behind == 0 {
		return "", err
	}
	if u.ahead > 0 {
		return "", &DivergedError{Branch: branch, Upstream: u.short}
	}
	if currentBranch(ctx, dir) == branch {
		_, err = git(ctx, dir, "merge", "--ff-only", u.full)
	} else {
		_, err = git(ctx, dir, "fetch", ".", u.full+":refs/heads/"+branch)
	}
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s fast-forwarded to %s", branch, u.short), nil
}

// fetchNotes fetches every remote; a failure (offline, auth) is not fatal
// — the operation goes on with the local branches and says so.
func fetchNotes(ctx context.Context, dir string) []string {
	if remotes, err := git(ctx, dir, "remote"); err != nil || remotes == "" {
		return nil
	}
	if err := ops.Fetch(ctx, dir); err != nil {
		msg, _, _ := strings.Cut(err.Error(), "\n")
		return []string{"Fetch failed, used local branches: " + msg}
	}
	return nil
}
