// Package tools implements the read-only git tools the chat model can call.
package tools

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"git-ui/internal/ai"
	"git-ui/internal/gitlog"
	"git-ui/internal/refs"
)

const (
	MaxOutput      = 8000
	MaxDiffLines   = 300
	MaxBlameBlocks = 200
)

func Specs() []ai.ToolSpec {
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	num := func(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }
	obj := func(props map[string]any, required ...string) map[string]any {
		o := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			o["required"] = required
		}
		return o
	}
	return []ai.ToolSpec{
		{
			Name:        "search_log",
			Description: "Search the commit history. Returns one line per commit: short hash, date, author, subject and refs. All filters are optional.",
			Parameters: obj(map[string]any{
				"text":   str("Case-insensitive text that must appear in the commit message"),
				"author": str("Part of the author name or email"),
				"since":  str("Only commits on or after this date (YYYY-MM-DD)"),
				"until":  str("Only commits on or before this date (YYYY-MM-DD)"),
				"branch": str("Branch, tag or revision to search; all refs when empty"),
				"path":   str("Only commits touching this file or directory"),
				"limit":  num("Maximum commits to return (default 20, max 50)"),
			}),
		},
		{
			Name:        "show_commit",
			Description: "Show a commit's full message, author, date, parents and changed files.",
			Parameters:  obj(map[string]any{"rev": str("Commit hash, branch or tag")}, "rev"),
		},
		{
			Name:        "diff_commit_file",
			Description: "Show the diff of one file in a commit, compared with its first parent.",
			Parameters:  obj(map[string]any{"rev": str("Commit hash, branch or tag"), "path": str("File path as listed by show_commit")}, "rev", "path"),
		},
		{
			Name:        "list_refs",
			Description: "List the current branch, local branches, remote branches and tags.",
			Parameters:  obj(map[string]any{}),
		},
		{
			Name:        "file_history",
			Description: "List the commits that changed a file, newest first.",
			Parameters:  obj(map[string]any{"path": str("File path"), "limit": num("Maximum commits (default 15, max 30)")}, "path"),
		},
		{
			Name:        "blame_file",
			Description: "Show which commit last changed each line of a file (git blame). Returns one line per block of lines: line range, short hash, date, author and commit subject. Without a line range, long files are cut; pass start_line and end_line to look at part of a file.",
			Parameters: obj(map[string]any{
				"path":       str("File path"),
				"rev":        str("Commit hash, branch or tag to blame at (default HEAD)"),
				"start_line": num("First line (1-based), optional"),
				"end_line":   num("Last line, optional"),
			}, "path"),
		},
	}
}

// Run executes a tool call. Problems are returned as text starting with
// "error: " so the model can correct its arguments.
func Run(ctx context.Context, dir string, call ai.ToolCall) string {
	var out string
	var err error
	switch call.Name {
	case "search_log":
		out, err = searchLog(ctx, dir, call.Args)
	case "show_commit":
		out, err = showCommit(ctx, dir, call.Args)
	case "diff_commit_file":
		out, err = diffCommitFile(ctx, dir, call.Args)
	case "list_refs":
		out, err = listRefs(ctx, dir)
	case "file_history":
		out, err = fileHistory(ctx, dir, call.Args)
	case "blame_file":
		out, err = blameFile(ctx, dir, call.Args)
	default:
		err = fmt.Errorf("unknown tool %q", call.Name)
	}
	if err != nil {
		return "error: " + err.Error()
	}
	if out == "" {
		out = "(no results)"
	}
	return Truncate(out, MaxOutput)
}

// Truncate cuts s to at most max bytes on a rune boundary.
func Truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "\n[truncated]"
}

func utf8RuneStart(b byte) bool { return b&0xC0 != 0x80 }

func searchLog(ctx context.Context, dir string, args map[string]any) (string, error) {
	f := gitlog.Filters{
		Text:   argString(args, "text"),
		Author: argString(args, "author"),
		Since:  argString(args, "since"),
		Until:  argString(args, "until"),
		Branch: argString(args, "branch"),
	}
	if strings.HasPrefix(f.Branch, "-") {
		return "", fmt.Errorf("invalid branch %q", f.Branch)
	}
	if p := argString(args, "path"); p != "" {
		f.Paths = []string{p}
	}
	return logLines(ctx, dir, f, argInt(args, "limit", 20, 50))
}

func fileHistory(ctx context.Context, dir string, args map[string]any) (string, error) {
	path := argString(args, "path")
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	return logLines(ctx, dir, gitlog.Filters{Paths: []string{path}}, argInt(args, "limit", 15, 30))
}

func blameFile(ctx context.Context, dir string, args map[string]any) (string, error) {
	path := argString(args, "path")
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	rev := argString(args, "rev")
	if rev == "" {
		rev = "HEAD"
	}
	opts := gitlog.BlameOptions{Start: argInt(args, "start_line", 0, 1<<30), End: argInt(args, "end_line", 0, 1<<30)}
	if opts.Start > 0 && opts.End == 0 {
		opts.End = opts.Start
	}
	b, err := gitlog.GetBlame(ctx, dir, rev, path, opts)
	if err != nil {
		return "", err
	}
	blocks := b.Blocks
	note := ""
	if opts.Start == 0 && len(blocks) > MaxBlameBlocks {
		blocks = blocks[:MaxBlameBlocks]
		note = "\n… truncated; pass start_line/end_line to narrow"
	}
	out := FormatBlame(blocks)
	// Keep the note itself from being crowded out by Run's MaxOutput
	// truncation: if the blocks alone already fill the budget, drop
	// blocks (not the cap) until the note fits.
	for note != "" && len(out)+len(note) > MaxOutput && len(blocks) > 0 {
		blocks = blocks[:len(blocks)-1]
		out = FormatBlame(blocks)
	}
	return out + note, nil
}

// FormatBlame renders one line per block, without the file text, to keep
// tool output and explanation prompts small.
func FormatBlame(blocks []gitlog.BlameBlock) string {
	lines := make([]string, 0, len(blocks))
	for _, blk := range blocks {
		span := fmt.Sprintf("L%d-%d", blk.Start, blk.Start+blk.Count-1)
		if blk.Uncommitted {
			lines = append(lines, span+"  (not committed yet)")
			continue
		}
		lines = append(lines, fmt.Sprintf("%s  %s  %s  %s  %s", span, blk.Short, blk.Date.Format("2006-01-02"), blk.Author, blk.Summary))
	}
	return strings.Join(lines, "\n")
}

func logLines(ctx context.Context, dir string, f gitlog.Filters, limit int) (string, error) {
	commits, err := gitlog.Get(ctx, dir, f, gitlog.OrderTopo, 0, limit)
	if err != nil {
		return "", err
	}
	lines := make([]string, 0, len(commits))
	for _, c := range commits {
		line := fmt.Sprintf("%s %s %s: %s", c.Short, c.Date.Format("2006-01-02"), c.Author, c.Subject)
		var names []string
		for _, r := range c.Refs {
			if r.Kind != gitlog.RefHead {
				names = append(names, r.Name)
			}
		}
		if len(names) > 0 {
			line += " (" + strings.Join(names, ", ") + ")"
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n"), nil
}

func resolve(ctx context.Context, dir string, args map[string]any) (string, error) {
	rev := argString(args, "rev")
	if rev == "" {
		return "", fmt.Errorf("rev is required")
	}
	if strings.HasPrefix(rev, "-") {
		return "", fmt.Errorf("invalid revision %q", rev)
	}
	hash, err := gitlog.ResolveCommit(ctx, dir, rev)
	if err != nil {
		return "", err
	}
	if hash == "" {
		return "", fmt.Errorf("unknown revision %q", rev)
	}
	return hash, nil
}

func showCommit(ctx context.Context, dir string, args map[string]any) (string, error) {
	hash, err := resolve(ctx, dir, args)
	if err != nil {
		return "", err
	}
	d, err := gitlog.GetDetails(ctx, dir, hash)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "commit %s\nauthor: %s <%s>\ndate: %s\n", d.Short, d.Author, d.Email, d.Date.Format("2006-01-02 15:04"))
	if len(d.Parents) > 0 {
		short := make([]string, len(d.Parents))
		for i, p := range d.Parents {
			short[i] = p[:min(7, len(p))]
		}
		fmt.Fprintf(&b, "parents: %s\n", strings.Join(short, " "))
	}
	fmt.Fprintf(&b, "\n%s\n", d.Subject)
	if d.Body != "" {
		fmt.Fprintf(&b, "\n%s\n", d.Body)
	}
	b.WriteString("\nfiles:\n")
	for _, f := range d.Files {
		if f.OldPath != "" {
			fmt.Fprintf(&b, "%s %s -> %s\n", f.Status, f.OldPath, f.Path)
		} else {
			fmt.Fprintf(&b, "%s %s\n", f.Status, f.Path)
		}
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func diffCommitFile(ctx context.Context, dir string, args map[string]any) (string, error) {
	path := argString(args, "path")
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	hash, err := resolve(ctx, dir, args)
	if err != nil {
		return "", err
	}
	d, err := gitlog.GetDetails(ctx, dir, hash)
	if err != nil {
		return "", err
	}
	parent := ""
	if len(d.Parents) > 0 {
		parent = d.Parents[0]
	}
	patch, err := gitlog.Diff(ctx, dir, parent, hash, []string{path})
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimRight(patch, "\n"), "\n")
	if len(lines) > MaxDiffLines {
		lines = append(lines[:MaxDiffLines], fmt.Sprintf("[diff truncated at %d lines]", MaxDiffLines))
	}
	return strings.Join(lines, "\n"), nil
}

func listRefs(ctx context.Context, dir string) (string, error) {
	r, err := refs.List(ctx, dir)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if r.Detached {
		fmt.Fprintf(&b, "Current branch: (detached at %s)\n", r.HeadHash[:min(7, len(r.HeadHash))])
	} else {
		fmt.Fprintf(&b, "Current branch: %s\n", r.Head)
	}
	b.WriteString("Local branches:")
	for _, br := range r.Local {
		b.WriteString(" " + br.Name)
	}
	b.WriteString("\nRemote branches:")
	count := 0
	for _, remote := range r.Remotes {
		for _, br := range remote.Branches {
			if count == 100 {
				break
			}
			b.WriteString(" " + remote.Name + "/" + br.Name)
			count++
		}
	}
	b.WriteString("\nTags:")
	for i, tag := range r.Tags {
		if i == 100 {
			break
		}
		b.WriteString(" " + tag.Name)
	}
	return b.String(), nil
}

func argString(args map[string]any, key string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// argInt reads a JSON number (or numeric string), applying a default and a cap.
func argInt(args map[string]any, key string, def, max int) int {
	n := def
	switch v := args[key].(type) {
	case float64:
		n = int(v)
	case string:
		if parsed, err := strconv.Atoi(v); err == nil {
			n = parsed
		}
	}
	if n <= 0 {
		n = def
	}
	return min(n, max)
}
