package merge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"git-ui/internal/gitcmd"
)

// ErrNoSuchRegion means the file has no region with that id any more —
// resolved meanwhile (by the AI, or another click), or never there.
var ErrNoSuchRegion = errors.New("merge: no such conflict region")

// RegionText is what a region becomes when one side is taken whole
// ("ours", "theirs") or both are kept, ours first ("both").
func RegionText(h Hunk, choice string) (string, error) {
	switch choice {
	case "ours":
		return h.Ours, nil
	case "theirs":
		return h.Theirs, nil
	case "both":
		return h.Ours + h.Theirs, nil
	}
	return "", fmt.Errorf("merge: unknown choice %q", choice)
}

// Region reads path and returns its region id.
func Region(dir, path, id string) (Hunk, error) {
	data, err := os.ReadFile(filepath.Join(dir, path))
	if err != nil {
		return Hunk{}, err
	}
	hunks, err := Parse(string(data))
	if err != nil {
		return Hunk{}, err
	}
	i := slices.IndexFunc(hunks, func(h Hunk) bool { return h.ID == id })
	if i < 0 {
		return Hunk{}, fmt.Errorf("%w: %s in %s", ErrNoSuchRegion, id, path)
	}
	return hunks[i], nil
}

// ResolveRegion replaces region id of path, one of the operation's text
// conflicts, with content, and returns how many regions the file has left.
// The write is atomic and keeps the file's mode; a stale id, content with
// markers, or a path that is not a text conflict writes nothing. The AI
// resolver's resolve_hunk and the Merge view's buttons both come here.
func ResolveRegion(ctx context.Context, dir, path, id, content string) (int, error) {
	st, err := Status(ctx, dir)
	if err != nil {
		return 0, err
	}
	if err := settled(st, st.Conflicts, path, "a text conflict"); err != nil {
		return 0, err
	}
	full := filepath.Join(dir, path)
	info, err := os.Lstat(full)
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("%w: %q is not a regular file", ErrNotInMerge, path)
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return 0, err
	}
	hunks, err := Parse(string(data))
	if err != nil {
		return 0, err
	}
	i := slices.IndexFunc(hunks, func(h Hunk) bool { return h.ID == id })
	if i < 0 {
		return len(hunks), fmt.Errorf("%w: %s in %s", ErrNoSuchRegion, id, path)
	}
	out, err := Splice(string(data), i, content)
	if err != nil {
		return len(hunks), err
	}
	if err := writeAtomic(full, info.Mode().Perm(), out); err != nil {
		return len(hunks), err
	}
	left, _ := Parse(out)
	return len(left), nil
}

// writeAtomic replaces full with content through a temp file in the same
// directory, so a crash never leaves half a file.
func writeAtomic(full string, perm os.FileMode, content string) error {
	tmp, err := os.CreateTemp(filepath.Dir(full), ".git-ui-merge-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, werr := tmp.WriteString(content)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, perm); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, full); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// Restartable are the operation's files Restart can put back as git first
// wrote them: those still in conflict, and settled ones git kept a
// resolve-undo record for (it keeps one when a conflicted file is staged).
// A file the merge settled cleanly has none, so it is never offered.
func Restartable(ctx context.Context, dir string) ([]string, error) {
	st, err := Status(ctx, dir)
	if err != nil {
		return nil, err
	}
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "ls-files", "-z", "--resolve-undo")
	if err != nil {
		return nil, err
	}
	// Records are "<mode> <object> <stage>\t<path>", one per stage. Only a
	// file that had both sides (stages 2 and 3) can be merged again: a
	// modify/delete that was taken has one, and checkout -m refuses it.
	stages := map[string]map[string]bool{}
	for _, rec := range strings.Split(out, "\x00") {
		meta, path, ok := strings.Cut(rec, "\t")
		if !ok {
			continue
		}
		f := strings.Fields(meta)
		if len(f) != 3 {
			continue
		}
		if stages[path] == nil {
			stages[path] = map[string]bool{}
		}
		stages[path][f[2]] = true
	}
	undo := map[string]bool{}
	for path, st := range stages {
		undo[path] = st["2"] && st["3"]
	}
	can := slices.Clone(st.Conflicts)
	for _, p := range append(slices.Clone(st.Staged), st.Unstaged...) {
		if undo[p] && !slices.Contains(can, p) {
			can = append(can, p)
		}
	}
	return can, nil
}

// Restart puts path back as the operation left it, markers included, and
// unstaged — discarding what was resolved in it, by hand or by the AI.
func Restart(ctx context.Context, dir, path string) error {
	can, err := Restartable(ctx, dir)
	if err != nil {
		return err
	}
	if !slices.Contains(can, path) {
		return fmt.Errorf("%w: %q was not in conflict", ErrNotInMerge, path)
	}
	_, err = gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "--literal-pathspecs", "checkout", "-m", "--", path)
	return err
}
