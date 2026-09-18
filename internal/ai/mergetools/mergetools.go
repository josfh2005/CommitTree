// Package mergetools gives the conflict agent its tools. Every write goes
// through resolve_hunk or stage_file, and both refuse any path that is not a
// conflicted file of the merge in progress, so the agent cannot reach code
// the merge never touched.
package mergetools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"git-ui/internal/ai"
	"git-ui/internal/ai/tools"
	"git-ui/internal/gitcmd"
	"git-ui/internal/merge"
)

// MaxResult caps a tool's output so one huge file can't crowd out the
// conversation.
const MaxResult = 8000

func Specs() []ai.ToolSpec {
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	num := func(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }
	object := func(props map[string]any, required ...string) map[string]any {
		return map[string]any{"type": "object", "properties": props, "required": required}
	}
	return []ai.ToolSpec{
		{
			Name:        "list_conflicts",
			Description: "List the files still in conflict, with how many conflicting regions each one has.",
			Parameters:  object(map[string]any{}),
		},
		{
			Name:        "read_conflict",
			Description: "Show one conflicting region: the common ancestor, our side, their side, and the lines around it.",
			Parameters: object(map[string]any{
				"path": str("File path, exactly as list_conflicts reported it."),
				"hunk": num("Which conflicting region, counting from 0."),
			}, "path", "hunk"),
		},
		{
			Name:        "resolve_hunk",
			Description: "Replace one conflicting region with the resolved code. Send only the lines that belong in place of the region, with no conflict markers.",
			Parameters: object(map[string]any{
				"path":     str("File path."),
				"hunk":     num("Which conflicting region, counting from 0. Regions renumber as you resolve them, so re-read the file after each change."),
				"resolved": str("The final content for that region."),
			}, "path", "hunk", "resolved"),
		},
		{
			Name:        "stage_file",
			Description: "Mark a file as resolved once it has no conflicts left. Fails while any marker remains.",
			Parameters:  object(map[string]any{"path": str("File path.")}, "path"),
		},
	}
}

// Run executes one tool call and reports whether it changed the working tree.
// Errors come back as text for the model to read and retry, never as a Go
// error.
func Run(ctx context.Context, dir string, call ai.ToolCall) (string, bool) {
	switch call.Name {
	case "list_conflicts":
		return tools.Truncate(listConflicts(ctx, dir), MaxResult), false
	case "read_conflict":
		return tools.Truncate(readConflict(ctx, dir, call.Args), MaxResult), false
	case "resolve_hunk":
		return resolveHunk(ctx, dir, call.Args)
	case "stage_file":
		return stageFile(ctx, dir, call.Args)
	}
	return fmt.Sprintf("Unknown tool %q.", call.Name), false
}

func listConflicts(ctx context.Context, dir string) string {
	st, err := merge.Status(ctx, dir)
	if err != nil {
		return "Could not read the merge state: " + err.Error()
	}
	if !st.Merging {
		return "This repository is not merging."
	}
	var b strings.Builder
	if len(st.Conflicts) == 0 {
		b.WriteString("No conflicts left to resolve.\n")
	}
	for _, path := range st.Conflicts {
		n := 0
		if data, err := os.ReadFile(filepath.Join(dir, path)); err == nil {
			if hunks, err := merge.Parse(string(data)); err == nil {
				n = len(hunks)
			}
		}
		fmt.Fprintf(&b, "%s — %d conflict(s)\n", path, n)
	}
	for _, path := range st.Manual {
		fmt.Fprintf(&b, "%s — no conflict markers (binary or add/delete); leave it for the user\n", path)
	}
	return b.String()
}

func readConflict(ctx context.Context, dir string, args map[string]any) string {
	path, hunks, msg := open(ctx, dir, args)
	if msg != "" {
		return msg
	}
	index := argInt(args, "hunk")
	if index < 0 || index >= len(hunks) {
		return fmt.Sprintf("%s has %d conflict(s); there is no region %d.", path, len(hunks), index)
	}
	h := hunks[index]
	var b strings.Builder
	fmt.Fprintf(&b, "%s, conflict %d of %d\n\n", path, index, len(hunks))
	fmt.Fprintf(&b, "--- lines before ---\n%s\n", h.Before)
	if h.Base != "" {
		fmt.Fprintf(&b, "--- common ancestor ---\n%s\n", h.Base)
	}
	fmt.Fprintf(&b, "--- our side (the branch you are merging into) ---\n%s\n", h.Ours)
	fmt.Fprintf(&b, "--- their side (the branch being merged) ---\n%s\n", h.Theirs)
	fmt.Fprintf(&b, "--- lines after ---\n%s", h.After)
	return b.String()
}

func resolveHunk(ctx context.Context, dir string, args map[string]any) (string, bool) {
	path, _, msg := open(ctx, dir, args)
	if msg != "" {
		return msg, false
	}
	full := filepath.Join(dir, path)
	data, err := os.ReadFile(full)
	if err != nil {
		return "Could not read " + path + ": " + err.Error(), false
	}
	resolved, _ := args["resolved"].(string)
	out, err := merge.Splice(string(data), argInt(args, "hunk"), resolved)
	if err != nil {
		return "Could not apply the resolution: " + err.Error(), false
	}
	info, err := os.Stat(full)
	if err != nil {
		return "Could not read the file mode of " + path + ": " + err.Error(), false
	}
	if err := os.WriteFile(full, []byte(out), info.Mode().Perm()); err != nil {
		return "Could not write " + path + ": " + err.Error(), false
	}
	left, err := merge.Parse(out)
	if err != nil {
		return "Wrote " + path + ", but it no longer parses: " + err.Error(), true
	}
	return fmt.Sprintf("Applied. %s now has %d conflict(s) left.", path, len(left)), true
}

func stageFile(ctx context.Context, dir string, args map[string]any) (string, bool) {
	path, _, msg := open(ctx, dir, args)
	if msg != "" {
		return msg, false
	}
	data, err := os.ReadFile(filepath.Join(dir, path))
	if err != nil {
		return "Could not read " + path + ": " + err.Error(), false
	}
	if merge.HasMarkers(string(data)) {
		return path + " still contains conflict markers; resolve every region before staging it.", false
	}
	if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "add", "--", path); err != nil {
		return "Could not stage " + path + ": " + err.Error(), false
	}
	return "Staged " + path + ".", true
}

// open validates the requested path against the merge's unmerged files and
// returns its hunks. A non-empty msg means the call was refused and the
// caller must return it unchanged.
func open(ctx context.Context, dir string, args map[string]any) (path string, hunks []merge.Hunk, msg string) {
	path, _ = args["path"].(string)
	st, err := merge.Status(ctx, dir)
	if err != nil {
		return "", nil, "Could not read the merge state: " + err.Error()
	}
	conflicted := false
	for _, p := range append(append([]string{}, st.Conflicts...), st.Manual...) {
		if p == path {
			conflicted = true
			break
		}
	}
	if !conflicted {
		return "", nil, fmt.Sprintf("%q is not a conflicted file in this merge. Call list_conflicts to see which files are.", path)
	}
	data, err := os.ReadFile(filepath.Join(dir, path))
	if err != nil {
		return "", nil, "Could not read " + path + ": " + err.Error()
	}
	hunks, err = merge.Parse(string(data))
	if err != nil {
		return "", nil, "Could not parse the conflicts in " + path + ": " + err.Error()
	}
	return path, hunks, ""
}

// argInt reads a JSON number argument, which arrives as float64.
func argInt(args map[string]any, key string) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return -1
}
