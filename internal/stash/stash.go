// Package stash lists and acts on the stash.
package stash

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"git-ui/internal/gitcmd"
)

var ErrNothingToStash = errors.New("stash: nothing to stash")

// Entry is one stash, newest first — the order the stash reflog already
// gives.
type Entry struct {
	Index   int    `json:"index"`
	Message string `json:"message"`
	Branch  string `json:"branch"`
	Hash    string `json:"hash"`
}

// List reports every stash. An empty stash is an empty slice, not an error.
//
// It reads the stash reflog rather than `git stash list`: `stash list`
// ignores --format, --pretty and -z entirely (verified against git 2.54 —
// `git stash list --format=%gd%x00%s%x00%H` prints plain
// "stash@{0}: On main: wip", with no NULs and no hash), so parsing its
// output for a hash is impossible. `reflog show` on refs/stash takes the
// same format placeholders `log` does and gives all three fields. A
// repository that has never had a stash has no refs/stash at all and makes
// `reflog show` fail with exit 128, so the ref is checked first rather than
// guessing at an exit code that also covers real failures.
func List(ctx context.Context, dir string) ([]Entry, error) {
	if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", "refs/stash"); err != nil {
		return []Entry{}, nil
	}
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "reflog", "show", "--format=%gd%x00%s%x00%H", "refs/stash")
	if err != nil {
		return nil, err
	}
	entries := []Entry{}
	out = strings.TrimRight(out, "\n")
	if out == "" {
		return entries, nil
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(line, "\x00")
		if len(fields) != 3 {
			continue
		}
		idx, ok := parseIndex(fields[0])
		if !ok {
			continue
		}
		branch, message := parseSubject(fields[1])
		entries = append(entries, Entry{Index: idx, Message: message, Branch: branch, Hash: fields[2]})
	}
	return entries, nil
}

// Push stashes the worktree. With nothing to stash git prints "No local
// changes to save" and exits 0 — a silent no-op the UI would show as a
// success with no stash to show for it, so that case becomes
// ErrNothingToStash here. (The message is stable English because gitcmd.Run
// pins LC_ALL=C.) The frontend also disables the button, mirroring Commit's
// disabled-when-nothing-staged rule; this is the backstop for the race
// between the two.
func Push(ctx context.Context, dir, message string, includeUntracked bool) error {
	args := []string{"stash", "push", "-m", message}
	if includeUntracked {
		args = append(args, "--include-untracked")
	}
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, args...)
	if err != nil {
		return err
	}
	if strings.Contains(out, "No local changes to save") {
		return ErrNothingToStash
	}
	return nil
}

func Apply(ctx context.Context, dir string, index int) error {
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "stash", "apply", ref(index))
	return err
}

func Pop(ctx context.Context, dir string, index int) error {
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "stash", "pop", ref(index))
	return err
}

func Drop(ctx context.Context, dir string, index int) error {
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "stash", "drop", ref(index))
	return err
}

func Diff(ctx context.Context, dir string, index int) (string, error) {
	return gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "stash", "show", "-p", ref(index))
}

func ref(index int) string { return fmt.Sprintf("stash@{%d}", index) }

// parseIndex pulls N out of "stash@{N}".
func parseIndex(gd string) (int, bool) {
	inner := strings.TrimSuffix(strings.TrimPrefix(gd, "stash@{"), "}")
	n, err := strconv.Atoi(inner)
	return n, err == nil
}

// parseSubject splits git's own stash subject into the branch it was taken
// from and a message: "WIP on <branch>: <hash> <original subject>" when
// stash chose the message itself, or "On <branch>: <message>" when -m gave
// one — Push above always gives one, but List must also read stashes a
// terminal created.
func parseSubject(subject string) (branch, message string) {
	for _, prefix := range []string{"WIP on ", "On "} {
		if !strings.HasPrefix(subject, prefix) {
			continue
		}
		rest := strings.TrimPrefix(subject, prefix)
		branch, message = rest, rest
		if i := strings.Index(rest, ": "); i >= 0 {
			branch, message = rest[:i], rest[i+2:]
		}
		return branch, message
	}
	return "", subject
}
