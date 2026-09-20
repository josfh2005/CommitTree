// Package tasks implements one-shot AI actions that don't need tools.
package tasks

import (
	"context"
	"fmt"
	"strings"

	"git-ui/internal/ai"
	"git-ui/internal/ai/tools"
	"git-ui/internal/gitlog"
)

const (
	OllamaDiffBudget = 6000
)

// ExplainContext describes a commit for the model, with the diff against its
// first parent cut to budget bytes.
func ExplainContext(ctx context.Context, dir, hash string, budget int) (string, error) {
	d, err := gitlog.GetDetails(ctx, dir, hash)
	if err != nil {
		return "", err
	}
	parent := ""
	if len(d.Parents) > 0 {
		parent = d.Parents[0]
	}
	diff, err := gitlog.Diff(ctx, dir, parent, d.Hash, nil)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Commit %s by %s on %s\n\nSubject: %s\n", d.Short, d.Author, d.Date.Format("2006-01-02"), d.Subject)
	if d.Body != "" {
		fmt.Fprintf(&b, "\nBody:\n%s\n", d.Body)
	}
	b.WriteString("\nFiles:\n")
	for _, f := range d.Files {
		if f.OldPath != "" {
			fmt.Fprintf(&b, "%s %s -> %s\n", f.Status, f.OldPath, f.Path)
		} else {
			fmt.Fprintf(&b, "%s %s\n", f.Status, f.Path)
		}
	}
	b.WriteString("\nDiff:\n")
	b.WriteString(tools.Truncate(diff, budget))
	return b.String(), nil
}

func Explain(ctx context.Context, r ai.Responder, instructions, dir, hash string, budget int) (<-chan ai.Chunk, error) {
	prompt, err := ExplainContext(ctx, dir, hash, budget)
	if err != nil {
		return nil, err
	}
	return r.Respond(ctx, instructions, prompt)
}
