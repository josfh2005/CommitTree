package tasks

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"git-ui/internal/ai/tools"
	"git-ui/internal/gitcmd"
	"git-ui/internal/gitlog"
)

// MaxExplainCommits caps how many of the commits behind a range of lines are
// described to the model, newest first.
const MaxExplainCommits = 5

// ExplainLinesContext describes lines start..end of path at rev ("" = the
// working tree) for the model: the lines, who last changed them, and for each
// of those commits its message and its diff of the file. Diffs share what is
// left of budget after the lines and blame.
func ExplainLinesContext(ctx context.Context, dir, rev, path string, start, end, budget int) (string, error) {
	b, err := gitlog.GetBlame(ctx, dir, rev, path, gitlog.BlameOptions{Start: start, End: end})
	if err != nil {
		return "", err
	}
	at := "working tree"
	if rev != "" {
		at = rev
		if len(at) > 7 {
			at = at[:7]
		}
	}
	var out strings.Builder
	fmt.Fprintf(&out, "File: %s (%s), lines %d-%d\n\nLines:\n", path, at, start, end)
	var text strings.Builder
	for i, line := range b.Lines {
		fmt.Fprintf(&text, "%d: %s\n", b.StartLine+i, line)
	}
	out.WriteString(tools.Truncate(text.String(), budget/3))
	fmt.Fprintf(&out, "\nBlame:\n%s\n", tools.FormatBlame(b.Blocks))

	// Distinct commits, newest first; uncommitted lines are described by the
	// working-tree diff instead.
	seen := map[string]bool{}
	var commits []gitlog.BlameBlock
	uncommitted := false
	for _, blk := range b.Blocks {
		if blk.Uncommitted {
			uncommitted = true
			continue
		}
		if !seen[blk.Hash] {
			seen[blk.Hash] = true
			commits = append(commits, blk)
		}
	}
	sort.SliceStable(commits, func(i, j int) bool { return commits[i].Date.After(commits[j].Date) })
	if len(commits) > MaxExplainCommits {
		commits = commits[:MaxExplainCommits]
	}

	parts := len(commits)
	if uncommitted {
		parts++
	}
	share := budget - out.Len()
	if parts > 0 {
		share /= parts
	}
	if share < 200 {
		share = 200
	}

	if uncommitted {
		diff, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "diff", "--no-color", "HEAD", "--", path)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&out, "\nUncommitted changes:\n%s\n", tools.Truncate(diff, share))
	}
	for _, blk := range commits {
		d, err := gitlog.GetDetails(ctx, dir, blk.Hash)
		if err != nil {
			return "", err
		}
		parent := ""
		if len(d.Parents) > 0 {
			parent = d.Parents[0]
		}
		diff, err := gitlog.Diff(ctx, dir, parent, d.Hash, []string{blk.Filename})
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&out, "\nCommit %s by %s on %s\nSubject: %s\n", d.Short, d.Author, d.Date.Format("2006-01-02"), d.Subject)
		if d.Body != "" {
			fmt.Fprintf(&out, "Body:\n%s\n", d.Body)
		}
		fmt.Fprintf(&out, "Diff of %s:\n%s\n", blk.Filename, tools.Truncate(diff, share))
	}
	return out.String(), nil
}
