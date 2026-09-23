// Package worktree reads and changes the working tree: what has changed,
// what is staged, and the commit that closes it. Conflicts are the merge
// package's business; this one only reports that a merge is in progress.
package worktree

import (
	"context"
	"strings"

	"git-ui/internal/gitcmd"
)

// FileStatus is one changed path. Status is git's letter for it: M modified,
// A added, D deleted, R renamed, T type-changed, ? untracked.
type FileStatus struct {
	Path    string `json:"path"`
	OldPath string `json:"oldPath,omitempty"`
	Status  string `json:"status"`
	// Submodule and the Sub* flags come from porcelain v2's submodule field
	// (S<c><m><u>): the path is a gitlink, its checked-out commit moved away
	// from what the index records, its working tree has modified content,
	// and it has untracked content, respectively.
	Submodule    bool `json:"submodule,omitempty"`
	SubCommit    bool `json:"subCommit,omitempty"`
	SubModified  bool `json:"subModified,omitempty"`
	SubUntracked bool `json:"subUntracked,omitempty"`
}

// State is what the Changes view shows. A file that is both staged and
// modified appears in Staged and in Unstaged, which is what git means.
type State struct {
	Staged    []FileStatus `json:"staged"`
	Unstaged  []FileStatus `json:"unstaged"`
	Untracked []FileStatus `json:"untracked"`
	Merging   bool         `json:"merging"`
}

// Status reads the working tree with porcelain v2, which reports the staged
// and unstaged state of every path in one call, names a rename's source, and
// with -z survives spaces, newlines and non-ASCII in paths.
func Status(ctx context.Context, dir string) (State, error) {
	st := State{Staged: []FileStatus{}, Unstaged: []FileStatus{}, Untracked: []FileStatus{}}
	// rev-parse --quiet exits non-zero when MERGE_HEAD is absent, which is the
	// ordinary "not merging" case rather than a failure. This is checked
	// independently of the porcelain records below: staging the resolution of
	// the last conflict turns its "u" record into an ordinary "1" one, but the
	// merge stays open — only a commit closes it (see merge.Status, the same
	// pattern) — so Merging must not depend on any "u" record still existing.
	if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", "MERGE_HEAD"); err == nil {
		st.Merging = true
	}
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout,
		"status", "--porcelain=v2", "-z", "--untracked-files=all")
	if err != nil {
		return State{}, err
	}
	fields := strings.Split(out, "\x00")
	for i := 0; i < len(fields); i++ {
		rec := fields[i]
		if rec == "" {
			continue
		}
		switch rec[0] {
		case '1': // ordinary change: "1 XY sub ... <path>"
			x, y, path, sub := parseChange(rec)
			st.add(x, y, path, "", sub)
		case '2': // rename or copy: "2 XY sub ... <path>" then the source in the next field
			x, y, path, sub := parseChange(rec)
			old := ""
			if i+1 < len(fields) {
				i++
				old = fields[i]
			}
			st.add(x, y, path, old, sub)
		case 'u': // unmerged: belt-and-braces agreement with the MERGE_HEAD check above
			st.Merging = true
		case '?':
			st.Untracked = append(st.Untracked, FileStatus{Path: strings.TrimPrefix(rec, "? "), Status: "?"})
		}
	}
	return st, nil
}

// parseChange pulls the two status letters, the submodule field and the path
// out of a v2 entry. The path is the ninth space-separated field for an
// ordinary change and the tenth for a rename, but it may itself contain
// spaces, so it is taken as everything after the known count of fields.
func parseChange(rec string) (x, y byte, path, sub string) {
	parts := strings.SplitN(rec, " ", 9)
	if len(parts) < 9 || len(parts[1]) != 2 {
		return '.', '.', "", ""
	}
	x, y = parts[1][0], parts[1][1]
	sub = parts[2]
	path = parts[8]
	if rec[0] == '2' {
		// A rename entry has one more field (the similarity score) before the path.
		if fields := strings.SplitN(path, " ", 2); len(fields) == 2 {
			path = fields[1]
		}
	}
	return x, y, path, sub
}

// add files one entry into the staged and unstaged lists. '.' means that side
// is unchanged; git reports both in one entry. sub is the four-character
// submodule field from porcelain v2 (S<c><m><u>, or "N..." for a plain file).
func (s *State) add(x, y byte, path, old, sub string) {
	if path == "" {
		return
	}
	isSub := len(sub) == 4 && sub[0] == 'S'
	var subCommit, subModified, subUntracked bool
	if isSub {
		subCommit, subModified, subUntracked = sub[1] == 'C', sub[2] == 'M', sub[3] == 'U'
	}
	if x != '.' {
		s.Staged = append(s.Staged, FileStatus{
			Path: path, OldPath: old, Status: string(x),
			Submodule: isSub, SubCommit: subCommit, SubModified: subModified, SubUntracked: subUntracked,
		})
	}
	if y != '.' {
		// OldPath is carried onto the unstaged side too: an unstaged rename
		// (git mv, then Unstage) still needs its source to Discard back to —
		// without it, Discard can only restore the new path from HEAD, which
		// HEAD never had, and the file is deleted instead of un-renamed.
		// Stage ignores OldPath, so re-staging an unstaged rename still
		// produces an add plus a delete; only Discard reads it.
		s.Unstaged = append(s.Unstaged, FileStatus{
			Path: path, OldPath: old, Status: string(y),
			Submodule: isSub, SubCommit: subCommit, SubModified: subModified, SubUntracked: subUntracked,
		})
	}
}
