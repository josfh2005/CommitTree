// Package mergetools gives the conflict agent its tools. Every write goes
// through resolve_hunk or stage_file, and both refuse any path that is not a
// conflicted file of the merge, rebase or cherry-pick in progress, so the
// agent cannot reach code the operation never touched.
package mergetools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
				"resolved": str("The final content for that region, exactly as it will be written to the file: every line of both sides you keep, in order. What you describe in your reply is not applied; only this is. Leave out the lines shown before and after the region: they stay in the file."),
			}, "path", "hunk", "resolved"),
		},
		{
			Name:        "stage_file",
			Description: "Mark a file as resolved once it has no conflicts left. Fails while any marker remains.",
			Parameters:  object(map[string]any{"path": str("File path.")}, "path"),
		},
	}
}

// Sides describes the two sides of a conflict for the model. A rebase swaps
// git's ours and theirs relative to a merge, so the words come from the
// caller, who knows which operation is in progress. The zero value keeps
// the merge wording.
type Sides struct{ Ours, Theirs string }

// Run executes one tool call and reports whether it changed the working tree.
// Errors come back as text for the model to read and retry, never as a Go
// error.
func Run(ctx context.Context, dir string, call ai.ToolCall, sides Sides) (string, bool) {
	switch call.Name {
	case "list_conflicts":
		return tools.Truncate(listConflicts(ctx, dir), MaxResult), false
	case "read_conflict":
		return tools.Truncate(readConflict(ctx, dir, call.Args, sides), MaxResult), false
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
		return "Nothing is being merged, rebased or cherry-picked."
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
		if n == 0 {
			fmt.Fprintf(&b, "%s — 0 conflict(s) left; call stage_file\n", path)
			continue
		}
		fmt.Fprintf(&b, "%s — %d conflict(s)\n", path, n)
	}
	for _, path := range st.Manual {
		fmt.Fprintf(&b, "%s — no conflict markers (binary or add/delete); leave it for the user\n", path)
	}
	return b.String()
}

func readConflict(ctx context.Context, dir string, args map[string]any, sides Sides) string {
	path, hunks, msg := open(ctx, dir, args)
	if msg != "" {
		return msg
	}
	index := argInt(args, "hunk")
	if index < 0 || index >= len(hunks) {
		return fmt.Sprintf("There is no region %d. %s", index, regionsLeft(path, len(hunks)))
	}
	h := hunks[index]
	ours, theirs := sides.Ours, sides.Theirs
	if ours == "" {
		ours = "our side (the branch you are merging into)"
	}
	if theirs == "" {
		theirs = "their side (the branch being merged)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s, conflict %d of %d\n\n", path, index, len(hunks))
	fmt.Fprintf(&b, "--- lines before ---\n%s\n", h.Before)
	switch {
	case h.HasBase && h.Base == "":
		// The strongest hint there is, and the easiest to miss when the
		// section is simply left out: nothing was there, both sides added.
		b.WriteString("--- common ancestor ---\n(empty: neither side had lines here before; both ADDED lines at this place. Nothing was replaced, so keep both sides' lines unless they duplicate each other.)\n")
	case h.HasBase:
		fmt.Fprintf(&b, "--- common ancestor ---\n%s\n", h.Base)
	}
	fmt.Fprintf(&b, "--- %s ---\n%s\n", ours, h.Ours)
	fmt.Fprintf(&b, "--- %s ---\n%s\n", theirs, h.Theirs)
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
	index := argInt(args, "hunk")
	if hunks, err := merge.Parse(string(data)); err == nil && index >= 0 && index < len(hunks) {
		if msg := checkResolution(hunks[index], resolved); msg != "" {
			return msg, false
		}
	}
	out, err := merge.Splice(string(data), index, resolved)
	if errors.Is(err, merge.ErrNoSuchHunk) {
		hunks, _ := merge.Parse(string(data))
		return fmt.Sprintf("There is no region %d. %s", index, regionsLeft(path, len(hunks))), false
	}
	if err != nil {
		return "Could not apply the resolution: " + err.Error(), false
	}
	info, err := os.Stat(full)
	if err != nil {
		return "Could not read the file mode of " + path + ": " + err.Error(), false
	}
	tmp, err := os.CreateTemp(filepath.Dir(full), ".git-ui-merge-*")
	if err != nil {
		return "Could not write " + path + ": " + err.Error(), false
	}
	name := tmp.Name()
	_, writeErr := tmp.WriteString(out)
	closeErr := tmp.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(name)
		return "Could not write " + path + ": " + errors.Join(writeErr, closeErr).Error(), false
	}
	if err := os.Chmod(name, info.Mode().Perm()); err != nil {
		os.Remove(name)
		return "Could not set the file mode of " + path + ": " + err.Error(), false
	}
	if err := os.Rename(name, full); err != nil {
		os.Remove(name)
		return "Could not replace " + path + ": " + err.Error(), false
	}
	left, err := merge.Parse(out)
	if err != nil {
		return "Wrote " + path + ", but it no longer parses: " + err.Error(), true
	}
	return "Applied. " + regionsLeft(path, len(left)), true
}

// checkResolution catches the two slips small models make most, before
// anything is written: repeating the context lines read_conflict showed
// around the region (they stay in the file, so they would appear twice),
// and brackets that no longer balance the way both sides agree they should.
// It returns why the resolution was refused, or "".
func checkResolution(h merge.Hunk, resolved string) string {
	r := textLines(resolved)
	ours, theirs := textLines(h.Ours), textLines(h.Theirs)
	before, after := textLines(h.Before), textLines(h.After)
	for k := min(len(before), len(r)); k > 0; k-- {
		echo := r[:k]
		if hasText(echo) && slices.Equal(echo, before[len(before)-k:]) && !hasPrefix(ours, echo) && !hasPrefix(theirs, echo) {
			return fmt.Sprintf("Not applied: your resolution starts with the %d line(s) shown under \"lines before\" (%q…). Those stay in the file, so they would appear twice. Send only what replaces the region.", k, echo[0])
		}
	}
	for k := min(len(after), len(r)); k > 0; k-- {
		echo := r[len(r)-k:]
		if hasText(echo) && slices.Equal(echo, after[:k]) && !hasSuffix(ours, echo) && !hasSuffix(theirs, echo) {
			return fmt.Sprintf("Not applied: your resolution ends with the %d line(s) shown under \"lines after\" (%q…). Those stay in the file, so they would appear twice. Send only what replaces the region.", k, echo[0])
		}
	}
	if d := bracketDelta(h.Ours); d == bracketDelta(h.Theirs) && bracketDelta(resolved) != d {
		return fmt.Sprintf("Not applied: both sides leave brackets unbalanced by %d here, your resolution by %d — a bracket was dropped or repeated (often a closing } that is already under \"lines after\"). Send the region again.", d, bracketDelta(resolved))
	}
	return ""
}

// textLines splits s into lines without their endings or trailing blanks.
func textLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t\r")
	}
	return lines
}

func hasText(lines []string) bool {
	return slices.ContainsFunc(lines, func(l string) bool { return strings.TrimSpace(l) != "" })
}

func hasPrefix(lines, p []string) bool { return len(lines) >= len(p) && slices.Equal(lines[:len(p)], p) }

func hasSuffix(lines, s []string) bool {
	return len(lines) >= len(s) && slices.Equal(lines[len(lines)-len(s):], s)
}

// bracketDelta is how many more brackets s opens than it closes.
func bracketDelta(s string) int {
	d := 0
	for _, c := range s {
		switch c {
		case '(', '[', '{':
			d++
		case ')', ']', '}':
			d--
		}
	}
	return d
}

// regionsLeft tells the model where a file's remaining regions are. Regions
// renumber after every resolve, which models miss: having resolved region 0
// of two, they ask for region 1, which no longer exists.
func regionsLeft(path string, n int) string {
	if n == 0 {
		return path + " has no conflicts left; call stage_file."
	}
	return fmt.Sprintf("%s has %d conflict(s) left, numbered 0 to %d; regions renumber after each resolve, so the next one is region 0.", path, n, n-1)
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
	// git reads a bare path as a pattern; a file named "*.txt" would stage every
	// .txt file. --literal-pathspecs makes it name exactly one file.
	if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "--literal-pathspecs", "add", "--", path); err != nil {
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
	// Conflicts only: a Manual path (delete/modify, binary, symlink) is a
	// human's to settle, and accepting it here would let stage_file settle it.
	conflicted := false
	for _, p := range st.Conflicts {
		if p == path {
			conflicted = true
			break
		}
	}
	if !conflicted {
		return "", nil, fmt.Sprintf("%q is not a conflicted file in this merge. Call list_conflicts to see which files are.", path)
	}
	full := filepath.Join(dir, path)
	info, err := os.Lstat(full)
	if err != nil {
		return "", nil, "Could not read " + path + ": " + err.Error()
	}
	// Status already files a symlink under Manual; this is defence in depth.
	if info.Mode()&os.ModeSymlink != 0 {
		return "", nil, path + " is a symbolic link; resolve it by hand."
	}
	data, err := os.ReadFile(full)
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
