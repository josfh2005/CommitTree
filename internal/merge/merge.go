package merge

import (
	"context"
	"errors"
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
	Rebased        // a rebase replayed or fast-forwarded the branch
	Picked         // a cherry-pick made its commit
	NothingToApply // a cherry-pick whose changes the branch already has
)

// Result is what Start produces; Conflicts is set only when Conflicted.
type Result struct {
	Outcome   Outcome  `json:"outcome"`
	Conflicts []string `json:"conflicts"`
}

// ErrInvalidRef guards against a ref that git would read as an option.
var ErrInvalidRef = errors.New("merge: invalid ref")

// ErrMergeInProgress reports a merge that was never concluded; git refuses to
// start another one, and its conflicts are not this merge's.
var ErrMergeInProgress = errors.New("merge: a merge is already in progress")

// Start merges branch into the current one. It always creates a merge commit
// (--no-ff) so an integrated branch stays visible in the graph, and asks for
// zdiff3 markers so conflicts carry the common ancestor. The -c is per
// command and leaves the user's own config alone.
func Start(ctx context.Context, dir, branch string) (Result, error) {
	if err := checkRef(branch); err != nil {
		return Result{}, err
	}
	if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", "MERGE_HEAD"); err == nil {
		return Result{}, ErrMergeInProgress
	}
	before, err := head(ctx, dir)
	if err != nil {
		return Result{}, err
	}
	_, err = gitcmd.Run(ctx, dir, gitcmd.ReadTimeout,
		"-c", "merge.conflictStyle=zdiff3", "merge", "--no-ff", "--no-edit", branch)
	if err == nil {
		after, err := head(ctx, dir)
		if err != nil {
			return Result{}, err
		}
		if after == before {
			return Result{Outcome: UpToDate}, nil
		}
		return Result{Outcome: Merged}, nil
	}
	// git exits non-zero both for conflicts and for real failures (a dirty
	// worktree, an unknown ref). A merge that stopped on conflicts leaves
	// MERGE_HEAD; without it, unmerged paths belong to something else (a
	// cherry-pick, a rebase) and git refused this merge outright.
	if _, headErr := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", "MERGE_HEAD"); headErr != nil {
		return Result{}, err
	}
	if conflicts, listErr := Unmerged(ctx, dir); listErr == nil && len(conflicts) > 0 {
		return Result{Outcome: Conflicted, Conflicts: conflicts}, nil
	}
	return Result{}, err
}

// Continue moves the conflict resolution in progress one step forward: a
// merge closes with git's own generated message, exactly as Commit did; a
// rebase, a cherry-pick, a revert or an `am` run their own --continue and
// may leave the next commit's conflicts behind — the caller re-reads Status
// to see. A stash conflict has no "continue" step; this is a no-op so a
// stray call from the UI never errors.
func Continue(ctx context.Context, dir string) error {
	st, err := Status(ctx, dir)
	if err != nil {
		return err
	}
	if st.Kind == KindMerge {
		return Commit(ctx, dir)
	}
	cmd, ok := sequencer[st.Kind]
	if !ok {
		return nil // KindStash, or nothing in progress
	}
	// Fingerprint the commit being resolved before the attempt: it is how
	// a real failure (the conflict is still unresolved, a hook rejected it)
	// is told apart below from --continue succeeding but immediately
	// stopping again on the next commit's conflicts.
	before := Fingerprint(ctx, dir)
	// GIT_EDITOR through the environment, not -c core.editor: git resolves
	// GIT_EDITOR first, so a value inherited from the user's shell would
	// beat core.editor and open a real editor the app can never close.
	// noEditor also covers hooks, which is why this uses HookTimeout.
	_, err = gitcmd.RunEnv(ctx, dir, gitcmd.HookTimeout, noEditor, cmd, "--continue")
	if err == nil {
		return nil
	}
	// A cancel can land just as the sequencer has advanced onto the next
	// commit's conflicts — the fingerprint check below would then read that
	// as success. It is not: report the cancel as the ordinary error it is
	// and leave the sequencer exactly where git left it.
	if errors.Is(err, gitcmd.ErrCancelled) {
		return err
	}
	// git exits non-zero both when --continue itself fails outright — the
	// conflict it was asked to continue past is still unresolved, or a real
	// hook failure — and when it succeeds in moving past the resolved
	// commit but immediately stops the sequencer on the next commit's
	// conflicts. Only the second is not a failure of this call, and the two
	// are told apart by whether the operation actually advanced: a real
	// failure leaves the same commit fingerprinted as before the attempt.
	if after := Fingerprint(ctx, dir); after != "" && after != before {
		return nil
	}
	return err
}

// sequencer maps a Kind to the git subcommand that continues or aborts it.
// KindMerge is not here: its continue is Commit, and its abort is
// `merge --abort`, both handled by name below. KindStash is not here
// either — it has no git-level step at all.
var sequencer = map[Kind]string{
	KindRebase:     "rebase",
	KindCherryPick: "cherry-pick",
	KindRevert:     "revert",
	KindAM:         "am",
}

// noEditor stops any --continue from opening an editor the app cannot close.
var noEditor = []string{"GIT_EDITOR=true"}

// Abort undoes the conflict resolution in progress: every kind but a stash
// has a real git-level abort, and each must get its own — `rebase --abort`
// on a `git am` (which also lives in .git/rebase-apply) throws away the
// mailbox. A stash conflict has nothing to undo but the files themselves,
// which Discard already handles, so it is a no-op rather than an error.
func Abort(ctx context.Context, dir string) error {
	st, err := Status(ctx, dir)
	if err != nil {
		return err
	}
	if st.Kind == KindMerge {
		_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "merge", "--abort")
		return err
	}
	cmd, ok := sequencer[st.Kind]
	if !ok {
		return nil // KindStash, or nothing in progress
	}
	_, err = gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, cmd, "--abort")
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

// head returns the current commit, or "" in a repository with no commits yet.
func head(ctx context.Context, dir string) (string, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil {
		var gerr *gitcmd.Error
		if errors.As(err, &gerr) && gerr.ExitCode == 1 {
			return "", nil // no commits yet
		}
		return "", err
	}
	return strings.TrimSpace(out), nil
}
