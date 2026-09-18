package merge

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"git-ui/internal/gitcmd"
)

// Outcome says how a merge ended.
type Outcome int

const (
	Merged Outcome = iota
	Conflicted
	UpToDate
)

// Result is what Start produces; Conflicts is set only when Conflicted.
type Result struct {
	Outcome   Outcome  `json:"outcome"`
	Conflicts []string `json:"conflicts"`
}

// ErrInvalidRef guards against a ref that git would read as an option.
var ErrInvalidRef = fmt.Errorf("merge: invalid ref")

// Start merges branch into the current one. It always creates a merge commit
// (--no-ff) so an integrated branch stays visible in the graph, and asks for
// zdiff3 markers so conflicts carry the common ancestor. The -c is per
// command and leaves the user's own config alone.
func Start(ctx context.Context, dir, branch string) (Result, error) {
	if err := checkRef(branch); err != nil {
		return Result{}, err
	}
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout,
		"-c", "merge.conflictStyle=zdiff3", "merge", "--no-ff", "--no-edit", branch)
	if err == nil {
		if strings.Contains(out, "Already up to date") {
			return Result{Outcome: UpToDate}, nil
		}
		return Result{Outcome: Merged}, nil
	}
	// git exits non-zero both for conflicts and for real failures (a dirty
	// worktree, an unknown ref). Unmerged paths are what tells them apart.
	if conflicts, listErr := Unmerged(ctx, dir); listErr == nil && len(conflicts) > 0 {
		return Result{Outcome: Conflicted, Conflicts: conflicts}, nil
	}
	return Result{}, err
}

func Abort(ctx context.Context, dir string) error {
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "merge", "--abort")
	return err
}

// Commit closes a merge with git's own generated message.
func Commit(ctx context.Context, dir string) error {
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "commit", "--no-edit")
	return err
}

// Unmerged lists the repository's conflicted paths, sorted.
func Unmerged(ctx context.Context, dir string) ([]string, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "diff", "--name-only", "--diff-filter=U", "-z")
	if err != nil {
		return nil, err
	}
	paths := []string{}
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

// checkRef rejects refs git would read as options. internal/ops has the same
// guard; a four-line check is worth repeating to keep the packages apart.
func checkRef(ref string) error {
	if ref == "" || strings.HasPrefix(ref, "-") {
		return fmt.Errorf("%w: %q", ErrInvalidRef, ref)
	}
	return nil
}
