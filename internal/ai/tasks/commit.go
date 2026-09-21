package tasks

import (
	"context"
	"fmt"
	"strings"

	"git-ui/internal/ai/tools"
	"git-ui/internal/gitcmd"
	"git-ui/internal/worktree"
)

// CommitContext describes the staged changes for the model: the file list and
// the staged diff, cut to budget bytes. The unstaged changes are deliberately
// absent — the message must describe what is being committed.
func CommitContext(ctx context.Context, dir string, budget int) (string, error) {
	st, err := worktree.Status(ctx, dir)
	if err != nil {
		return "", err
	}
	diff, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "diff", "--cached")
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("Staged files:\n")
	for _, f := range st.Staged {
		if f.OldPath != "" {
			fmt.Fprintf(&b, "%s %s -> %s\n", f.Status, f.OldPath, f.Path)
			continue
		}
		fmt.Fprintf(&b, "%s %s\n", f.Status, f.Path)
	}
	cut := tools.Truncate(diff, budget)
	b.WriteString("\nStaged diff:\n")
	if len(cut) < len(diff) {
		b.WriteString("(truncated: the diff is longer than the budget)\n")
	}
	b.WriteString(cut)
	return b.String(), nil
}
