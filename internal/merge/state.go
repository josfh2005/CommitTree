package merge

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"git-ui/internal/gitcmd"
	"git-ui/internal/refs"
)

// State is what the UI needs to show a merge in progress. Conflicts are the
// unmerged files carrying markers the agent can splice; Manual are the ones
// with none — binaries, delete/modify — which only a human can settle.
type State struct {
	Merging   bool     `json:"merging"`
	From      string   `json:"from"`
	Into      string   `json:"into"`
	Conflicts []string `json:"conflicts"`
	Manual    []string `json:"manual"`
}

// Status reports whether dir is mid-merge and what is still unresolved. A
// repository that is not merging yields the zero State and no error.
func Status(ctx context.Context, dir string) (State, error) {
	st := State{Conflicts: []string{}, Manual: []string{}}
	// rev-parse --quiet exits non-zero when MERGE_HEAD is absent, which is
	// the ordinary "not merging" case rather than a failure.
	if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", "MERGE_HEAD"); err != nil {
		return State{Conflicts: []string{}, Manual: []string{}}, nil
	}
	st.Merging = true
	st.Into = refs.CurrentLabel(ctx, dir)
	st.From = mergeFrom(ctx, dir)

	unmerged, err := Unmerged(ctx, dir)
	if err != nil {
		return State{}, err
	}
	for _, path := range unmerged {
		data, err := os.ReadFile(filepath.Join(dir, path))
		if err != nil || !HasMarkers(string(data)) {
			st.Manual = append(st.Manual, path)
			continue
		}
		st.Conflicts = append(st.Conflicts, path)
	}
	return st, nil
}

// mergeFrom reads the branch name out of the message git prepared for the
// merge ("Merge branch 'feature'"), returning "" when it can't be found.
func mergeFrom(ctx context.Context, dir string) string {
	gitDir, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(strings.TrimSpace(gitDir), "MERGE_MSG"))
	if err != nil {
		return ""
	}
	first := strings.SplitN(string(data), "\n", 2)[0]
	open, close := strings.Index(first, "'"), strings.LastIndex(first, "'")
	if open >= 0 && close > open {
		return first[open+1 : close]
	}
	return ""
}
