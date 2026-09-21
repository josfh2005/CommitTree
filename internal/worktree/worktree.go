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
		case '1': // ordinary change: "1 XY ... <path>"
			x, y, path := parseChange(rec)
			st.add(x, y, path, "")
		case '2': // rename or copy: "2 XY ... <path>" then the source in the next field
			x, y, path := parseChange(rec)
			old := ""
			if i+1 < len(fields) {
				i++
				old = fields[i]
			}
			st.add(x, y, path, old)
		case 'u': // unmerged: the merge view owns this repository
			st.Merging = true
		case '?':
			st.Untracked = append(st.Untracked, FileStatus{Path: strings.TrimPrefix(rec, "? "), Status: "?"})
		}
	}
	return st, nil
}

// parseChange pulls the two status letters and the path out of a v2 entry.
// The path is the ninth space-separated field for an ordinary change and the
// tenth for a rename, but it may itself contain spaces, so it is taken as
// everything after the known count of fields.
func parseChange(rec string) (x, y byte, path string) {
	parts := strings.SplitN(rec, " ", 9)
	if len(parts) < 9 || len(parts[1]) != 2 {
		return '.', '.', ""
	}
	x, y = parts[1][0], parts[1][1]
	path = parts[8]
	if rec[0] == '2' {
		// A rename entry has one more field (the similarity score) before the path.
		if sub := strings.SplitN(path, " ", 2); len(sub) == 2 {
			path = sub[1]
		}
	}
	return x, y, path
}

// add files one entry into the staged and unstaged lists. '.' means that side
// is unchanged; git reports both in one entry.
func (s *State) add(x, y byte, path, old string) {
	if path == "" {
		return
	}
	if x != '.' {
		s.Staged = append(s.Staged, FileStatus{Path: path, OldPath: old, Status: string(x)})
	}
	if y != '.' {
		// The unstaged side of a rename is a change to the new path; the old
		// path only exists staged.
		s.Unstaged = append(s.Unstaged, FileStatus{Path: path, Status: string(y)})
	}
}
