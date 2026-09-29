package worktree

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"git-ui/internal/gitcmd"
)

// Action is what a hunk or line selection does.
type Action string

const (
	ActionStage   Action = "stage"   // unstaged → index
	ActionUnstage Action = "unstage" // index → back out of the index
	ActionDiscard Action = "discard" // unstaged → thrown away from the working tree
)

var (
	// These are shown to the user as they are, in an error toast.
	ErrDiffChanged  = errors.New("The file changed since it was shown — reloaded")
	ErrUndoStale    = errors.New("Can't undo: the file changed since the discard")
	ErrNotPatchable = errors.New("Only a modified text file can be changed by hunk or line")
	ErrWrongSection = errors.New("Staged changes can only be unstaged; unstaged ones can be staged or discarded")
)

// DiffHash identifies the exact diff the user was shown.
func DiffHash(diff string) string {
	sum := sha256.Sum256([]byte(diff))
	return hex.EncodeToString(sum[:])
}

// Patchable reports whether path's diff in one section can be split by hunk
// or line: a plain text modification only. An added, deleted, renamed,
// copied or type-changed path, a submodule and a binary file stay whole-file.
func Patchable(st State, path string, staged bool, diff string) bool {
	list := st.Unstaged
	if staged {
		list = st.Staged
	}
	i := slices.IndexFunc(list, func(f FileStatus) bool { return f.Path == path })
	if i < 0 {
		return false
	}
	f := list[i]
	if f.Status != "M" || f.Submodule || f.OldPath != "" {
		return false
	}
	if strings.HasPrefix(diff, "Binary files ") || strings.Contains(diff, "\nBinary files ") || strings.Contains(diff, "\nGIT binary patch") {
		return false
	}
	return strings.Contains(diff, "\n@@ ")
}

// ApplySelection stages, unstages or discards the selected hunks and lines
// of path. It rebuilds the patch from the file's full diff, refusing if that
// diff is no longer the one the user saw (hash), and returns the applied
// patch so a discard can be undone.
func ApplySelection(ctx context.Context, dir, path string, staged bool, hash string, sel Selection, action Action) (string, error) {
	var args []string
	switch action {
	case ActionStage:
		args = []string{"--cached"}
	case ActionUnstage:
		args = []string{"--cached", "-R"}
	case ActionDiscard:
		args = []string{"-R"}
	default:
		return "", fmt.Errorf("worktree: unknown action %q", action)
	}
	if (action == ActionUnstage) != staged {
		return "", ErrWrongSection
	}
	diff, st, err := FileDiffAndStatus(ctx, dir, path, staged)
	if err != nil {
		return "", err
	}
	if !Patchable(st, path, staged, diff) {
		return "", ErrNotPatchable
	}
	if DiffHash(diff) != hash {
		return "", ErrDiffChanged
	}
	parsed, err := ParseDiff(diff)
	if err != nil {
		return "", err
	}
	patch, err := BuildPatch(parsed, sel, action != ActionStage)
	if err != nil {
		return "", err
	}
	if err := applyPatch(ctx, dir, patch, args...); err != nil {
		return "", err
	}
	return patch, nil
}

// Reapply undoes a discard by applying its patch forward to the working
// tree again. git apply is atomic: a patch that no longer fits changes
// nothing, and git's refusal (exit 1) is reported as ErrUndoStale; any other
// failure (a timeout, a cancel, git missing) is returned as it is.
func Reapply(ctx context.Context, dir, patch string) error {
	err := applyPatch(ctx, dir, patch)
	var gerr *gitcmd.Error
	if errors.As(err, &gerr) && gerr.ExitCode == 1 && !errors.Is(err, gitcmd.ErrTimeout) && !errors.Is(err, gitcmd.ErrCancelled) {
		return ErrUndoStale
	}
	return err
}

// applyPatch feeds patch to git apply through a temporary file (gitcmd has
// no stdin). Like git add and git reset in stage.go, this local write runs
// under ReadTimeout: it touches one file, never hooks or the network.
func applyPatch(ctx context.Context, dir, patch string, extra ...string) error {
	f, err := os.CreateTemp("", "committree-*.patch")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(patch); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	args := append([]string{"apply", "--recount", "--whitespace=nowarn"}, extra...)
	_, err = gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, append(args, f.Name())...)
	return err
}
