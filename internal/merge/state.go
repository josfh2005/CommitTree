package merge

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"git-ui/internal/gitcmd"
	"git-ui/internal/refs"
)

// State is what the UI needs to show a merge in progress. Conflicts are the
// unmerged text files both sides changed — the agent's to resolve and stage,
// whether or not markers remain. Manual are the ones only a human can settle:
// delete/modify, added on one side, symlinks, submodules, binaries, and
// anything in the worktree that is not a regular file.
type State struct {
	Merging   bool     `json:"merging"`
	From      string   `json:"from"`
	Into      string   `json:"into"`
	Conflicts []string `json:"conflicts"`
	Manual    []string `json:"manual"`
	// Staged and Unstaged are the merge's settled files: in the index for
	// the merge commit, or changed in the worktree but not yet added.
	Staged   []string `json:"staged"`
	Unstaged []string `json:"unstaged"`
}

// Status reports whether dir is mid-merge and what is still unresolved. A
// repository that is not merging yields the zero State and no error.
func Status(ctx context.Context, dir string) (State, error) {
	st := State{Conflicts: []string{}, Manual: []string{}, Staged: []string{}, Unstaged: []string{}}
	// rev-parse --quiet exits non-zero when MERGE_HEAD is absent, which is
	// the ordinary "not merging" case rather than a failure.
	if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", "MERGE_HEAD"); err != nil {
		return st, nil
	}
	st.Merging = true
	st.Into = refs.CurrentLabel(ctx, dir)
	st.From = mergeFrom(ctx, dir)

	entries, err := unmergedEntries(ctx, dir)
	if err != nil {
		return State{}, err
	}
	paths := make([]string, 0, len(entries))
	for path := range entries {
		paths = append(paths, path)
	}
	manualByAttrs, err := attrsCallForManual(ctx, dir, paths)
	if err != nil {
		return State{}, err
	}
	for path, e := range entries {
		if e.needsHuman() || !isText(filepath.Join(dir, path)) || manualByAttrs[path] {
			st.Manual = append(st.Manual, path)
			continue
		}
		st.Conflicts = append(st.Conflicts, path)
	}
	sort.Strings(st.Conflicts)
	sort.Strings(st.Manual)
	if st.Staged, err = changedPaths(ctx, dir, entries, "--cached", "HEAD"); err != nil {
		return State{}, err
	}
	// Unstaged is limited to what the merge brings in: a merge may start with
	// unrelated uncommitted work, which is not the merge's to stage.
	theirs, err := changedPaths(ctx, dir, nil, "HEAD...MERGE_HEAD")
	if err != nil {
		// Unrelated histories have no merge base; everything differing
		// between the two sides is then the merge's.
		if theirs, err = changedPaths(ctx, dir, nil, "HEAD", "MERGE_HEAD"); err != nil {
			return State{}, err
		}
	}
	dirty, err := changedPaths(ctx, dir, entries)
	if err != nil {
		return State{}, err
	}
	for _, p := range dirty {
		if slices.Contains(theirs, p) {
			st.Unstaged = append(st.Unstaged, p)
		}
	}
	return st, nil
}

// unmerged is what git's index says about one unmerged path: which of the
// base (1), ours (2) and theirs (3) stages exist, and every mode seen.
type unmerged struct {
	stages [4]bool
	modes  []string
}

// needsHuman reports a conflict the index alone shows the agent can't settle:
// a side is missing (delete/modify, or added on one side only), or an entry
// is a symlink or a submodule rather than a file.
func (u unmerged) needsHuman() bool {
	if !u.stages[2] || !u.stages[3] {
		return true
	}
	for _, m := range u.modes {
		if m == "120000" || m == "160000" {
			return true
		}
	}
	return false
}

// unmergedEntries groups `git ls-files -u` by path. Each NUL-terminated
// record is "<mode> <sha> <stage>\t<path>".
func unmergedEntries(ctx context.Context, dir string) (map[string]unmerged, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "ls-files", "-u", "-z")
	if err != nil {
		return nil, err
	}
	entries := map[string]unmerged{}
	for _, rec := range strings.Split(out, "\x00") {
		meta, path, ok := strings.Cut(rec, "\t")
		if !ok {
			continue
		}
		fields := strings.Fields(meta)
		if len(fields) != 3 {
			continue
		}
		e := entries[path]
		e.modes = append(e.modes, fields[0])
		if n := fields[2]; len(n) == 1 && n[0] >= '1' && n[0] <= '3' {
			e.stages[n[0]-'0'] = true
		}
		entries[path] = e
	}
	return entries, nil
}

// attrsCallForManual asks git for the merge and conflict-marker-size
// attributes of every candidate path and reports, per path, whether any of
// them mean our marker parser does not apply. It unions the answer over
// three sources: the worktree's own .gitattributes (source ""), and HEAD's
// and MERGE_HEAD's (source "HEAD"/"MERGE_HEAD") — the attributes each side
// actually had when git wrote the conflicted file, which is what decided
// whether it wrote markers. The worktree's post-merge .gitattributes can
// differ from both — the incoming branch may have changed or removed the
// very rule that was in effect — so checking it alone is not enough. A path
// is reported true (Manual) as soon as any one of the three sources calls
// for it; that is fail-safe by construction, since an attribute any side
// ever had wins. An empty paths slice makes no git calls.
func attrsCallForManual(ctx context.Context, dir string, paths []string) (map[string]bool, error) {
	result := map[string]bool{}
	if len(paths) == 0 {
		return result, nil
	}
	for _, source := range []string{"", "HEAD", "MERGE_HEAD"} {
		args := []string{"check-attr"}
		if source != "" {
			args = append(args, "--source", source)
		}
		args = append(args, "-z", "merge", "conflict-marker-size", "--")
		args = append(args, paths...)
		out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, args...)
		if err != nil {
			return nil, err
		}
		for path, attrs := range parseCheckAttrZ(out) {
			if needsHumanForAttrs(attrs) {
				result[path] = true
			}
		}
	}
	return result, nil
}

// parseCheckAttrZ parses the -z output of `git check-attr`: a flat sequence
// of NUL-terminated fields in triples of path, attribute, value. A trailing
// empty field from the final terminator is ignored.
func parseCheckAttrZ(raw string) map[string]map[string]string {
	fields := strings.Split(raw, "\x00")
	if n := len(fields); n > 0 && fields[n-1] == "" {
		fields = fields[:n-1]
	}
	result := map[string]map[string]string{}
	for i := 0; i+2 < len(fields); i += 3 {
		path, attr, value := fields[i], fields[i+1], fields[i+2]
		if result[path] == nil {
			result[path] = map[string]string{}
		}
		result[path][attr] = value
	}
	return result
}

// needsHumanForAttrs reports whether a path's merge/conflict-marker-size
// attributes mean our marker parser does not apply, so an otherwise-text
// conflict there cannot be trusted as resolved just because it has no
// markers our parser recognises.
func needsHumanForAttrs(attrs map[string]string) bool {
	switch attrs["merge"] {
	case "", "unspecified", "set", "text":
	default:
		return true
	}
	switch attrs["conflict-marker-size"] {
	case "", "unspecified", "7":
	default:
		return true
	}
	return false
}

// binaryProbe is how much of a file git itself inspects for a NUL byte when
// deciding whether it is binary.
const binaryProbe = 8000

// isText reports whether the worktree path is a regular file whose first
// bytes hold no NUL. It never follows a symlink and never opens anything but
// a regular file, so a link or a FIFO planted by a merge can't make it block
// or read outside the repository, and it reads at most binaryProbe bytes.
func isText(full string) bool {
	info, err := os.Lstat(full)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	f, err := os.Open(full)
	if err != nil {
		return false
	}
	defer f.Close()
	var buf [binaryProbe]byte
	n, err := io.ReadFull(f, buf[:])
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return false
	}
	return !bytes.Contains(buf[:n], []byte{0})
}

// mergeFrom names what is being merged in: the quoted branch, tag or remote
// branch from the message git prepared, falling back to MERGE_HEAD's short
// hash when the message carries no quoted name.
func mergeFrom(ctx context.Context, dir string) string {
	gitDir, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--absolute-git-dir")
	if err == nil {
		data, err := os.ReadFile(filepath.Join(strings.TrimSpace(gitDir), "MERGE_MSG"))
		if err == nil {
			first := strings.SplitN(string(data), "\n", 2)[0]
			if a, b := strings.Index(first, "'"), strings.LastIndex(first, "'"); a >= 0 && b > a {
				return first[a+1 : b]
			}
		}
	}
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--short", "MERGE_HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}
