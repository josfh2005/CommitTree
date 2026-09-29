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

// syncBranch fast-forwards branch to its upstream when it is only behind.
// No upstream, or nothing to take, does nothing.
func syncBranch(ctx context.Context, dir, branch string) (string, error) {
	full, err := git(ctx, dir, "rev-parse", "--symbolic-full-name", branch+"@{upstream}")
	if err != nil || full == "" {
		return "", nil
	}
	short, err := git(ctx, dir, "rev-parse", "--abbrev-ref", branch+"@{upstream}")
	if err != nil {
		return "", err
	}
	counts, err := git(ctx, dir, "rev-list", "--left-right", "--count", branch+"..."+full)
	if err != nil {
		return "", err
	}
	var ahead, behind int
	if _, err := fmt.Sscanf(counts, "%d\t%d", &ahead, &behind); err != nil {
		return "", fmt.Errorf("reading %s…%s: %q", branch, short, counts)
	}
	if behind == 0 {
		return "", nil
	}
	if ahead > 0 {
		return "", &DivergedError{Branch: branch, Upstream: short}
	}
	if currentBranch(ctx, dir) == branch {
		_, err = git(ctx, dir, "merge", "--ff-only", full)
	} else {
		_, err = git(ctx, dir, "fetch", ".", full+":refs/heads/"+branch)
	}
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s fast-forwarded to %s", branch, short), nil
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
