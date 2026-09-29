# Stage, unstage and discard by hunk and line — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** From a file's diff in the Changes view, stage, unstage or discard one hunk or a hand-picked set of lines, with Undo for discards.

**Architecture:** Go parses the file's full diff and builds a partial patch from hunk and line indices the renderer sends, after checking a hash of the diff the user saw; `git apply` (with `--cached` and/or `-R`) does the change. The frontend parses the visible diff into rows, handles click/Shift/Cmd line selection in a new `DiffLines.svelte`, and keeps the last discard's patch (in Go, per repository) for an Undo toast.

**Tech Stack:** Go 1.26 + git CLI via `internal/gitcmd`, Wails v2.16 bindings, Svelte 5 (legacy `export let` / `$:` syntax, as the rest of the app), TypeScript, Vitest.

**Spec:** `docs/superpowers/specs/2026-09-29-hunk-line-actions-design.md`

## Global Constraints

- Only a plain text modification (status `M`, not a submodule, no `OldPath`, not binary) gets hunk or line actions; everything else stays file-level.
- Unstaged section: Stage and Discard. Staged section: Unstage only.
- Discard by hunk or line has no confirmation; it shows a toast that does not auto-dismiss, "Discarded 1 hunk in `<file>`" / "Discarded N lines in `<file>`", with an **Undo** action. File-level Discard keeps its confirmation.
- Stale diff message, verbatim: `The file changed since it was shown — reloaded`.
- Failed undo message, verbatim: `Can't undo: the file changed since the discard`.
- Go always rebuilds the patch from the **full** diff, never from the truncated text the pane shows; the renderer never sends patch text.
- Every write goes through `App.writeWorktree` (write lock + `worktree:changed` event).
- Commits: no `Co-Authored-By` line (owner's rule). Conventional prefixes as in the log (`feat(worktree):`, `feat(ui):`, `docs(spec):`).
- A behaviour change updates `docs/spec/03-working-tree.md` in the same commit.
- Wails bindings: after changing an `App` method, run `wails generate module`, then keep only the changes for the methods this plan touches (`git checkout -p frontend/wailsjs` to drop the unrelated `Menu()`/`keys`/`menu` noise that generation always adds).
- Node 22 for frontend commands: prefix with `source ~/.nvm/nvm.sh && nvm use 22 >/dev/null &&`.

## Review Focus

1. **A user's git diff configuration** (`diff.noprefix=true`, `diff.external`, a `textconv` driver in `.gitattributes`) must not break hunk actions — covered by a test in Task 2.
2. **Content lines that look like headers** — an added line `++ counter` shows in the diff as `+++ counter` and a removed `-- note` as `--- note` — must be treated as changes, not headers, by both parsers — covered in Task 1 (Go) and Task 4 (TS).
3. **A last line without a trailing newline**: a selection that would leave a no-newline line in the middle of the result must be refused with a clear error and change nothing; a whole-hunk action on such a file must work — covered in Task 1 and Task 2.
4. **CRLF files**: staging a hunk of a file with `\r\n` line endings must keep the `\r` bytes — covered in Task 2.
5. **Mouse behaviour in the pane**: Shift+click must select a range of lines instead of extending the native text selection, and a drag to copy text must not toggle a line — no unit test (component); checked in Task 5's manual pass.

---

## File map

| File | Responsibility |
|---|---|
| `internal/worktree/patch.go` (new) | Parse one file's unified diff; build a partial patch from a selection (forward or reverse). Pure, no git. |
| `internal/worktree/patch_test.go` (new) | Table tests for the above. |
| `internal/worktree/hunks.go` (new) | `DiffHash`, `Patchable`, `ApplySelection`, `Reapply`: validation, hash check, `git apply`. |
| `internal/worktree/hunks_test.go` (new) | Integration tests with `testrepo`. |
| `internal/worktree/filediff.go` | Diff flags that neutralise user diff configuration. |
| `internal/app/worktree.go` | `WorktreeDiff`, `GetWorktreeDiff` new shape, `ApplyHunkSelection`, `UndoDiscard`. |
| `internal/app/app.go` | `discards sync.Map` field on `App`. |
| `internal/app/worktree_test.go` | Adapt to `WorktreeDiff`; new tests. |
| `frontend/wailsjs/go/app/App.{d.ts,js}`, `frontend/wailsjs/go/models.ts` | Regenerated bindings (only the touched methods/types). |
| `frontend/src/lib/types.ts` | `WorktreeDiff`, `HunkPick`, `HunkAction`. |
| `frontend/src/lib/hunks.ts` (new) + `hunks.test.ts` (new) | Rows, actionable hunks, selection logic, picks, summary text. |
| `frontend/src/lib/api.ts` | `getWorktreeDiff` new type, `applyHunkSelection`, `undoDiscard`. |
| `frontend/src/lib/actions.ts` | `applyHunkSelection` (busy, errors, Undo toast), `undoDiscard`. |
| `frontend/src/components/DiffLines.svelte` (new) | Diff rows, hunk buttons, selection bar, selection highlight. |
| `frontend/src/components/ChangesView.svelte` | Keep a `WorktreeDiff`; render `DiffLines`. |
| `docs/spec/03-working-tree.md` | Behaviour description. |

---

### Task 1: Diff parser and partial patch builder

**Files:**
- Create: `internal/worktree/patch.go`
- Test: `internal/worktree/patch_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type LineKind int` with `LineContext`, `LineAdd`, `LineDel`
  - `type DiffLine struct { Kind LineKind; Text string; NoNewline bool }`
  - `type Hunk struct { OldStart, NewStart int; Section string; Lines []DiffLine }`
  - `type ParsedDiff struct { Header []string; Hunks []Hunk }`
  - `type HunkPick struct { Hunk int \`json:"hunk"\`; Lines []int \`json:"lines"\` }` (empty `Lines` = whole hunk)
  - `type Selection []HunkPick`
  - `var ErrEmptySelection`, `var ErrSplitsLastLine`
  - `func ParseDiff(text string) (ParsedDiff, error)`
  - `func BuildPatch(d ParsedDiff, sel Selection, reverse bool) (string, error)`

- [ ] **Step 1: Write the failing tests**

Create `internal/worktree/patch_test.go`:

```go
package worktree_test

import (
	"errors"
	"strings"
	"testing"

	"git-ui/internal/worktree"
)

const header = "diff --git a/f.txt b/f.txt\nindex 1111111..2222222 100644\n--- a/f.txt\n+++ b/f.txt\n"

// sample has two hunks. Hunk 0 body: 0 " one", 1 "-two", 2 "+TWO",
// 3 "+two-and-a-half", 4 " three". Hunk 1 body: 0 " ten", 1 "-eleven", 2 " twelve".
const sample = header +
	"@@ -1,3 +1,4 @@ func main()\n one\n-two\n+TWO\n+two-and-a-half\n three\n" +
	"@@ -10,3 +11,2 @@\n ten\n-eleven\n twelve\n"

func parse(t *testing.T, text string) worktree.ParsedDiff {
	t.Helper()
	d, err := worktree.ParseDiff(text)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestParseDiff(t *testing.T) {
	d := parse(t, sample)
	if len(d.Header) != 4 || len(d.Hunks) != 2 {
		t.Fatalf("header %d lines, %d hunks; want 4 and 2", len(d.Header), len(d.Hunks))
	}
	h := d.Hunks[1]
	if h.OldStart != 10 || h.NewStart != 11 || len(h.Lines) != 3 || h.Lines[1].Kind != worktree.LineDel || h.Lines[1].Text != "eleven" {
		t.Fatalf("hunk 1 = %+v", h)
	}
	if d.Hunks[0].Section != " func main()" {
		t.Errorf("section = %q", d.Hunks[0].Section)
	}
}

// Review Focus 2: an added "++ x" shows as "+++ x" and a removed "-- y" as
// "--- y"; inside a hunk they are changes, not file headers.
func TestParseDiffKeepsHeaderLookalikesInsideAHunk(t *testing.T) {
	d := parse(t, header+"@@ -1,2 +1,2 @@\n--- y\n+++ x\n keep\n")
	lines := d.Hunks[0].Lines
	if len(d.Header) != 4 || len(lines) != 3 || lines[0].Kind != worktree.LineDel || lines[0].Text != "-- y" || lines[1].Kind != worktree.LineAdd || lines[1].Text != "++ x" {
		t.Fatalf("header = %q, lines = %+v", d.Header, lines)
	}
}

func TestParseDiffMarksNoNewline(t *testing.T) {
	d := parse(t, header+"@@ -1,2 +1,2 @@\n a\n-b\n\\ No newline at end of file\n+c\n\\ No newline at end of file\n")
	lines := d.Hunks[0].Lines
	if len(lines) != 3 || !lines[1].NoNewline || !lines[2].NoNewline || lines[0].NoNewline {
		t.Fatalf("lines = %+v", lines)
	}
}

func TestBuildPatch(t *testing.T) {
	tests := []struct {
		name    string
		diff    string
		sel     worktree.Selection
		reverse bool
		want    string
	}{
		{
			name: "whole second hunk, forward",
			diff: sample, sel: worktree.Selection{{Hunk: 1}},
			want: header + "@@ -10,3 +10,2 @@\n ten\n-eleven\n twelve\n",
		},
		{
			name: "forward: an unselected - becomes context, an unselected + is dropped",
			diff: sample, sel: worktree.Selection{{Hunk: 0, Lines: []int{2}}},
			want: header + "@@ -1,3 +1,4 @@ func main()\n one\n two\n+TWO\n three\n",
		},
		{
			name: "forward: the next hunk's new start follows the lines this patch removes",
			diff: sample, sel: worktree.Selection{{Hunk: 1}, {Hunk: 0, Lines: []int{1}}},
			want: header + "@@ -1,3 +1,2 @@ func main()\n one\n-two\n three\n@@ -10,3 +9,2 @@\n ten\n-eleven\n twelve\n",
		},
		{
			name: "reverse: an unselected + becomes context, an unselected - is dropped",
			diff: sample, sel: worktree.Selection{{Hunk: 0, Lines: []int{2}}}, reverse: true,
			want: header + "@@ -1,3 +1,4 @@ func main()\n one\n+TWO\n two-and-a-half\n three\n",
		},
		{
			name: "reverse: the next hunk's old start follows the lines this patch restores",
			diff: sample, sel: worktree.Selection{{Hunk: 0, Lines: []int{1}}, {Hunk: 1}}, reverse: true,
			want: header + "@@ -1,5 +1,4 @@ func main()\n one\n-two\n TWO\n two-and-a-half\n three\n@@ -12,3 +11,2 @@\n ten\n-eleven\n twelve\n",
		},
		{
			name: "a side left empty uses the line-before convention",
			diff: header + "@@ -1 +1 @@\n-a\n+b\n", sel: worktree.Selection{{Hunk: 0, Lines: []int{0}}},
			want: header + "@@ -1,1 +0,0 @@\n-a\n",
		},
		{
			name: "no-newline markers survive a whole hunk",
			diff: header + "@@ -1,2 +1,2 @@\n a\n-b\n\\ No newline at end of file\n+c\n\\ No newline at end of file\n",
			sel:  worktree.Selection{{Hunk: 0}},
			want: header + "@@ -1,2 +1,2 @@\n a\n-b\n\\ No newline at end of file\n+c\n\\ No newline at end of file\n",
		},
		{
			name: "removing only the last line without a newline",
			diff: header + "@@ -1,2 +1,2 @@\n a\n-b\n\\ No newline at end of file\n+c\n\\ No newline at end of file\n",
			sel:  worktree.Selection{{Hunk: 0, Lines: []int{1}}},
			want: header + "@@ -1,2 +1,1 @@\n a\n-b\n\\ No newline at end of file\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := worktree.BuildPatch(parse(t, tt.diff), tt.sel, tt.reverse)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("patch =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

func TestBuildPatchRefuses(t *testing.T) {
	noNewline := parse(t, header+"@@ -1,2 +1,2 @@\n a\n-b\n\\ No newline at end of file\n+c\n\\ No newline at end of file\n")
	tests := []struct {
		name string
		d    worktree.ParsedDiff
		sel  worktree.Selection
		want error
	}{
		{"nothing selected", parse(t, sample), nil, worktree.ErrEmptySelection},
		{"a context line", parse(t, sample), worktree.Selection{{Hunk: 0, Lines: []int{0}}}, nil},
		{"a hunk that does not exist", parse(t, sample), worktree.Selection{{Hunk: 2}}, nil},
		{"a line out of range", parse(t, sample), worktree.Selection{{Hunk: 1, Lines: []int{3}}}, nil},
		// Review Focus 3: keeping "b" (no newline) as context before "+c" is not a valid file.
		{"splitting a last line without a newline", noNewline, worktree.Selection{{Hunk: 0, Lines: []int{2}}}, worktree.ErrSplitsLastLine},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := worktree.BuildPatch(tt.d, tt.sel, false)
			if err == nil {
				t.Fatal("want an error")
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if strings.Contains(err.Error(), "%!") {
				t.Errorf("badly formatted error %q", err)
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/worktree/ -run 'TestParseDiff|TestBuildPatch'`
Expected: FAIL to build, `undefined: worktree.ParseDiff` (and the other new names).

- [ ] **Step 3: Implement `patch.go`**

Create `internal/worktree/patch.go`:

```go
package worktree

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// LineKind is what one body line of a hunk does.
type LineKind int

const (
	LineContext LineKind = iota
	LineAdd
	LineDel
)

// DiffLine is one body line of a hunk, without its leading ' ', '+' or '-'.
// NoNewline marks a line followed by "\ No newline at end of file".
type DiffLine struct {
	Kind      LineKind
	Text      string
	NoNewline bool
}

// Hunk is one "@@" block. Section is what git printed after the second "@@"
// (the enclosing function), kept so a rebuilt header reads the same.
type Hunk struct {
	OldStart, NewStart int
	Section            string
	Lines              []DiffLine
}

// ParsedDiff is one file's unified diff: the lines before the first hunk
// ("diff --git", "index", "---", "+++") and its hunks.
type ParsedDiff struct {
	Header []string
	Hunks  []Hunk
}

// HunkPick selects lines of one hunk by their index among its body lines
// (context lines count too); no Lines means the whole hunk.
type HunkPick struct {
	Hunk  int   `json:"hunk"`
	Lines []int `json:"lines"`
}

// Selection is what the diff pane sends: the picked hunks, in any order.
type Selection []HunkPick

var (
	ErrEmptySelection = errors.New("worktree: nothing selected")
	// ErrSplitsLastLine refuses a selection that would keep a last line with
	// no trailing newline in the middle of the result, which no file can be.
	ErrSplitsLastLine = errors.New("worktree: this selection splits the file's last line, which has no newline; act on the whole hunk")
)

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@(.*)$`)

// ParseDiff splits one file's diff, as FileDiff returns it, into its header
// and hunks. Inside a hunk every line is read by its first character, so an
// added "++ x" (shown "+++ x") is a change, not a header.
func ParseDiff(text string) (ParsedDiff, error) {
	var d ParsedDiff
	if text == "" {
		return d, nil
	}
	for _, l := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		if m := hunkHeader.FindStringSubmatch(l); m != nil {
			oldStart, _ := strconv.Atoi(m[1])
			newStart, _ := strconv.Atoi(m[2])
			d.Hunks = append(d.Hunks, Hunk{OldStart: oldStart, NewStart: newStart, Section: m[3]})
			continue
		}
		if len(d.Hunks) == 0 {
			d.Header = append(d.Header, l)
			continue
		}
		h := &d.Hunks[len(d.Hunks)-1]
		switch {
		case strings.HasPrefix(l, `\`):
			if n := len(h.Lines); n > 0 {
				h.Lines[n-1].NoNewline = true
			}
		case strings.HasPrefix(l, "+"):
			h.Lines = append(h.Lines, DiffLine{Kind: LineAdd, Text: l[1:]})
		case strings.HasPrefix(l, "-"):
			h.Lines = append(h.Lines, DiffLine{Kind: LineDel, Text: l[1:]})
		case strings.HasPrefix(l, " "), l == "":
			h.Lines = append(h.Lines, DiffLine{Kind: LineContext, Text: strings.TrimPrefix(l, " ")})
		default:
			return ParsedDiff{}, fmt.Errorf("worktree: unexpected diff line %q", l)
		}
	}
	return d, nil
}

// BuildPatch keeps only the selected changes of d. Unselected lines must stay
// as they are in the content the patch is applied to: forward (staging, onto
// the index, which has the - lines) an unselected - becomes context and an
// unselected + is dropped; reverse (unstaging and discarding, applied with -R
// onto content that has the + lines) it is the other way round. Header counts
// are recomputed, and the start of the side that moves is shifted by the net
// change of the hunks before it.
func BuildPatch(d ParsedDiff, sel Selection, reverse bool) (string, error) {
	picked := map[int]map[int]bool{} // hunk → selected body lines; a nil map is the whole hunk
	for _, p := range sel {
		if p.Hunk < 0 || p.Hunk >= len(d.Hunks) {
			return "", fmt.Errorf("worktree: no hunk %d", p.Hunk)
		}
		if len(p.Lines) == 0 {
			picked[p.Hunk] = nil
			continue
		}
		lines, seen := picked[p.Hunk]
		if seen && lines == nil {
			continue // already taken whole
		}
		if lines == nil {
			lines = map[int]bool{}
			picked[p.Hunk] = lines
		}
		for _, i := range p.Lines {
			if i < 0 || i >= len(d.Hunks[p.Hunk].Lines) || d.Hunks[p.Hunk].Lines[i].Kind == LineContext {
				return "", fmt.Errorf("worktree: line %d of hunk %d is not a change", i, p.Hunk)
			}
			lines[i] = true
		}
	}

	var b strings.Builder
	for _, l := range d.Header {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	shift, changed := 0, false
	for hi, h := range d.Hunks {
		lines, ok := picked[hi]
		if !ok {
			continue
		}
		var body []DiffLine
		oldCount, newCount, changes := 0, 0, 0
		for li, l := range h.Lines {
			kind := l.Kind
			if kind != LineContext && lines != nil && !lines[li] {
				if (kind == LineDel) != reverse {
					kind = LineContext
				} else {
					continue
				}
			}
			if kind != LineContext {
				changes++
			}
			if kind != LineAdd {
				oldCount++
			}
			if kind != LineDel {
				newCount++
			}
			body = append(body, DiffLine{Kind: kind, Text: l.Text, NoNewline: l.NoNewline})
		}
		if changes == 0 {
			continue
		}
		for _, l := range body[:len(body)-1] {
			if l.NoNewline && l.Kind == LineContext {
				return "", ErrSplitsLastLine
			}
		}
		changed = true

		// The anchor is the side the patch is applied to, which keeps its
		// original start; the other side lands shifted by earlier hunks.
		anchor, anchorCount, otherCount := h.OldStart, oldCount, newCount
		if reverse {
			anchor, anchorCount, otherCount = h.NewStart, newCount, oldCount
		}
		first := anchor
		if anchorCount == 0 {
			first++ // an empty side's start is the line before it
		}
		other := first + shift
		if otherCount == 0 {
			other--
		}
		shift += otherCount - anchorCount
		oldStart, newStart := anchor, other
		if reverse {
			oldStart, newStart = other, anchor
		}

		fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@%s\n", oldStart, oldCount, newStart, newCount, h.Section)
		for _, l := range body {
			b.WriteByte(" +-"[l.Kind])
			b.WriteString(l.Text)
			b.WriteByte('\n')
			if l.NoNewline {
				b.WriteString("\\ No newline at end of file\n")
			}
		}
	}
	if !changed {
		return "", ErrEmptySelection
	}
	return b.String(), nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/worktree/ -run 'TestParseDiff|TestBuildPatch' -v`
Expected: PASS, every subtest listed.

- [ ] **Step 5: Commit**

```bash
git add internal/worktree/patch.go internal/worktree/patch_test.go
git commit -m "feat(worktree): parse a file diff and build a partial patch from a selection"
```

---

### Task 2: Apply a selection with git, and undo a discard

**Files:**
- Modify: `internal/worktree/filediff.go` (the tracked-file `args` at the end of `FileDiff`)
- Create: `internal/worktree/hunks.go`
- Test: `internal/worktree/hunks_test.go`

**Interfaces:**
- Consumes (Task 1): `ParseDiff`, `BuildPatch`, `Selection`, `HunkPick`, `ErrSplitsLastLine`. Existing: `Status(ctx, dir) (State, error)`, `FileDiff(ctx, dir, path, staged) (string, error)`, `State{Staged, Unstaged, Untracked []FileStatus}`, `FileStatus{Path, OldPath, Status, Submodule}`, `gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, args...) (string, error)`.
- Produces:
  - `type Action string` with `ActionStage = "stage"`, `ActionUnstage = "unstage"`, `ActionDiscard = "discard"`
  - `var ErrDiffChanged, ErrNotPatchable, ErrWrongSection, ErrUndoStale`
  - `func DiffHash(diff string) string`
  - `func Patchable(st State, path string, staged bool, diff string) bool`
  - `func ApplySelection(ctx context.Context, dir, path string, staged bool, hash string, sel Selection, action Action) (string, error)` — returns the applied patch
  - `func Reapply(ctx context.Context, dir, patch string) error`

- [ ] **Step 1: Write the failing tests**

Create `internal/worktree/hunks_test.go`:

```go
package worktree_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"git-ui/internal/testrepo"
	"git-ui/internal/worktree"
)

// numbered is "line 1\n" … "line n\n", with some lines replaced.
func numbered(n int, change map[int]string) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		line := fmt.Sprintf("line %d", i)
		if c, ok := change[i]; ok {
			line = c
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// twoHunks commits f.txt (12 numbered lines) and changes line 2 to
// "TWO\nTWO-B" and line 11 to "ELEVEN": the unstaged diff has two hunks.
// Hunk 0 body: 0 " line 1", 1 "-line 2", 2 "+TWO", 3 "+TWO-B", 4-6 context.
func twoHunks(t *testing.T) *testrepo.Repo {
	t.Helper()
	r := testrepo.New(t)
	r.WriteFile("f.txt", numbered(12, nil))
	r.Git("add", "f.txt")
	r.Git("commit", "-q", "-m", "f")
	r.WriteFile("f.txt", numbered(12, map[int]string{2: "TWO\nTWO-B", 11: "ELEVEN"}))
	return r
}

func diffOf(t *testing.T, r *testrepo.Repo, staged bool) string {
	t.Helper()
	d, err := worktree.FileDiff(ctx, r.Dir, "f.txt", staged)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func apply(t *testing.T, r *testrepo.Repo, staged bool, sel worktree.Selection, action worktree.Action) string {
	t.Helper()
	patch, err := worktree.ApplySelection(ctx, r.Dir, "f.txt", staged, worktree.DiffHash(diffOf(t, r, staged)), sel, action)
	if err != nil {
		t.Fatal(err)
	}
	return patch
}

func TestApplySelectionStagesOneHunk(t *testing.T) {
	r := twoHunks(t)
	apply(t, r, false, worktree.Selection{{Hunk: 0}}, worktree.ActionStage)

	cached, unstaged := r.Git("diff", "--cached"), r.Git("diff")
	if !strings.Contains(cached, "+TWO") || strings.Contains(cached, "+ELEVEN") {
		t.Errorf("staged diff = %s", cached)
	}
	if strings.Contains(unstaged, "+TWO") || !strings.Contains(unstaged, "+ELEVEN") {
		t.Errorf("unstaged diff = %s", unstaged)
	}
}

func TestApplySelectionStagesSomeLines(t *testing.T) {
	r := twoHunks(t)
	apply(t, r, false, worktree.Selection{{Hunk: 0, Lines: []int{2}}}, worktree.ActionStage)

	if got, want := r.Git("show", ":f.txt"), strings.TrimSuffix(numbered(12, map[int]string{2: "line 2\nTWO"}), "\n"); got != want {
		t.Errorf("index =\n%s\nwant\n%s", got, want)
	}
	if got := read(t, r.Dir, "f.txt"); got != numbered(12, map[int]string{2: "TWO\nTWO-B", 11: "ELEVEN"}) {
		t.Errorf("working tree changed:\n%s", got)
	}
}

func TestApplySelectionUnstagesOneHunk(t *testing.T) {
	r := twoHunks(t)
	r.Git("add", "f.txt")
	apply(t, r, true, worktree.Selection{{Hunk: 1}}, worktree.ActionUnstage)

	cached := r.Git("diff", "--cached")
	if !strings.Contains(cached, "+TWO") || strings.Contains(cached, "+ELEVEN") {
		t.Errorf("staged diff = %s", cached)
	}
	if !strings.Contains(read(t, r.Dir, "f.txt"), "ELEVEN") {
		t.Error("unstage touched the working tree")
	}
}

func TestApplySelectionDiscardsSomeLines(t *testing.T) {
	r := twoHunks(t)
	patch := apply(t, r, false, worktree.Selection{{Hunk: 0, Lines: []int{3}}}, worktree.ActionDiscard)

	if got, want := read(t, r.Dir, "f.txt"), numbered(12, map[int]string{2: "TWO", 11: "ELEVEN"}); got != want {
		t.Errorf("file =\n%s\nwant\n%s", got, want)
	}
	if cached := r.Git("diff", "--cached"); cached != "" {
		t.Errorf("discard touched the index: %s", cached)
	}
	if !strings.Contains(patch, "+TWO-B") {
		t.Errorf("returned patch = %s", patch)
	}
}

func TestApplySelectionOnAPartiallyStagedFile(t *testing.T) {
	r := twoHunks(t)
	r.Git("add", "f.txt")
	r.WriteFile("f.txt", numbered(12, map[int]string{2: "TWO\nTWO-B", 6: "SIX", 11: "ELEVEN"}))
	apply(t, r, false, worktree.Selection{{Hunk: 0}}, worktree.ActionDiscard)

	if got, want := read(t, r.Dir, "f.txt"), numbered(12, map[int]string{2: "TWO\nTWO-B", 11: "ELEVEN"}); got != want {
		t.Errorf("file =\n%s\nwant the staged content\n%s", got, want)
	}
	if cached := r.Git("diff", "--cached"); !strings.Contains(cached, "+ELEVEN") {
		t.Errorf("staged part lost: %s", cached)
	}
}

func TestApplySelectionRefusesAStaleDiff(t *testing.T) {
	r := twoHunks(t)
	hash := worktree.DiffHash(diffOf(t, r, false))
	r.WriteFile("f.txt", numbered(12, map[int]string{2: "OTHER"}))

	_, err := worktree.ApplySelection(ctx, r.Dir, "f.txt", false, hash, worktree.Selection{{Hunk: 0}}, worktree.ActionStage)
	if !errors.Is(err, worktree.ErrDiffChanged) {
		t.Fatalf("err = %v, want ErrDiffChanged", err)
	}
	if cached := r.Git("diff", "--cached"); cached != "" {
		t.Errorf("something was staged: %s", cached)
	}
}

func TestReapplyUndoesADiscard(t *testing.T) {
	r := twoHunks(t)
	changed := read(t, r.Dir, "f.txt")
	patch := apply(t, r, false, worktree.Selection{{Hunk: 0}}, worktree.ActionDiscard)

	if err := worktree.Reapply(ctx, r.Dir, patch); err != nil {
		t.Fatal(err)
	}
	if got := read(t, r.Dir, "f.txt"); got != changed {
		t.Errorf("after undo =\n%s\nwant\n%s", got, changed)
	}
}

func TestReapplyRefusesWhenTheLinesChangedAgain(t *testing.T) {
	r := twoHunks(t)
	patch := apply(t, r, false, worktree.Selection{{Hunk: 0}}, worktree.ActionDiscard)
	r.WriteFile("f.txt", numbered(12, map[int]string{1: "ONE", 2: "OTHER", 3: "THREE", 11: "ELEVEN"}))
	before := read(t, r.Dir, "f.txt")

	if err := worktree.Reapply(ctx, r.Dir, patch); !errors.Is(err, worktree.ErrUndoStale) {
		t.Fatalf("err = %v, want ErrUndoStale", err)
	}
	if read(t, r.Dir, "f.txt") != before {
		t.Error("a failed undo changed the file")
	}
}

func TestApplySelectionRefusesWholeFileOnlyPaths(t *testing.T) {
	r := twoHunks(t)
	r.WriteFile("new.txt", "new\n")
	for _, tc := range []struct {
		path   string
		staged bool
		action worktree.Action
		want   error
	}{
		{"new.txt", false, worktree.ActionStage, worktree.ErrNotPatchable},
		{"f.txt", false, worktree.ActionUnstage, worktree.ErrWrongSection},
		{"f.txt", true, worktree.ActionStage, worktree.ErrWrongSection},
		{"f.txt", true, worktree.ActionDiscard, worktree.ErrWrongSection},
	} {
		d, _ := worktree.FileDiff(ctx, r.Dir, tc.path, tc.staged)
		_, err := worktree.ApplySelection(ctx, r.Dir, tc.path, tc.staged, worktree.DiffHash(d), worktree.Selection{{Hunk: 0}}, tc.action)
		if !errors.Is(err, tc.want) {
			t.Errorf("%s staged=%v %s: err = %v, want %v", tc.path, tc.staged, tc.action, err, tc.want)
		}
	}
	if _, err := worktree.ApplySelection(ctx, r.Dir, "nope.txt", false, "", worktree.Selection{{Hunk: 0}}, worktree.ActionStage); err == nil {
		t.Error("an unlisted path was accepted")
	}
}

func TestApplySelectionRefusesARename(t *testing.T) {
	r := twoHunks(t)
	r.Git("checkout", "--", "f.txt")
	r.Git("mv", "f.txt", "g.txt")
	d, err := worktree.FileDiff(ctx, r.Dir, "g.txt", true)
	if err != nil {
		t.Fatal(err)
	}
	_, err = worktree.ApplySelection(ctx, r.Dir, "g.txt", true, worktree.DiffHash(d), worktree.Selection{{Hunk: 0}}, worktree.ActionUnstage)
	if !errors.Is(err, worktree.ErrNotPatchable) {
		t.Fatalf("err = %v, want ErrNotPatchable", err)
	}
}

// Review Focus 1: a user's diff settings must not change what gets applied.
func TestApplySelectionIgnoresTheUsersDiffConfig(t *testing.T) {
	r := twoHunks(t)
	r.Git("config", "diff.noprefix", "true")
	r.Git("config", "diff.mnemonicPrefix", "true")
	r.Git("config", "diff.upper.textconv", "tr a-z A-Z")
	r.WriteFile(".gitattributes", "f.txt diff=upper\n")
	apply(t, r, false, worktree.Selection{{Hunk: 1}}, worktree.ActionStage)

	if cached := r.Git("diff", "--cached", "--no-textconv"); !strings.Contains(cached, "+ELEVEN") || strings.Contains(cached, "+TWO") {
		t.Errorf("staged diff = %s", cached)
	}
}

// Review Focus 3: a file whose last line has no newline.
func TestApplySelectionOnALastLineWithoutNewline(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile("f.txt", "a\nb")
	r.Git("add", "f.txt")
	r.Git("commit", "-q", "-m", "f")
	r.WriteFile("f.txt", "a\nc")
	apply(t, r, false, worktree.Selection{{Hunk: 0}}, worktree.ActionDiscard)
	if got := read(t, r.Dir, "f.txt"); got != "a\nb" {
		t.Errorf("file = %q, want %q", got, "a\nb")
	}
}

// Review Focus 4: CRLF line endings are kept byte for byte.
func TestApplySelectionKeepsCRLF(t *testing.T) {
	r := testrepo.New(t)
	r.Git("config", "core.autocrlf", "false")
	r.WriteFile("f.txt", "one\r\ntwo\r\nthree\r\n")
	r.Git("add", "f.txt")
	r.Git("commit", "-q", "-m", "f")
	r.WriteFile("f.txt", "one\r\nTWO\r\nthree\r\n")
	apply(t, r, false, worktree.Selection{{Hunk: 0}}, worktree.ActionStage)

	if got := r.Git("cat-file", "-p", ":f.txt"); got != "one\r\nTWO\r\nthree" {
		t.Errorf("index blob = %q", got)
	}
}
```

Notes: `testrepo.Repo.Git` returns `strings.TrimSpace` of git's output, hence the missing final `\r\n` in the CRLF comparison and the `TrimSuffix` in `TestApplySelectionStagesSomeLines`. `read(t, dir, name)` and `ctx` already exist in this test package (`stage_test.go`, `worktree_test.go`). `testrepo` isolates its own git commands from the global config, but `worktree` functions run with the developer's config — which is exactly why Review Focus 1 is tested with repository-local settings.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/worktree/ -run 'TestApplySelection|TestReapply'`
Expected: FAIL to build, `undefined: worktree.ApplySelection` (and the other new names).

- [ ] **Step 3: Neutralise the user's diff configuration in `FileDiff`**

In `internal/worktree/filediff.go`, replace the tracked-file arguments at the end of `FileDiff`:

```go
	args := []string{"--literal-pathspecs", "diff", "--submodule=log"}
```

with:

```go
	// The pane's text is also what hunk and line actions rebuild a patch
	// from, so no user setting may change its shape: prefixes stay a/ b/,
	// and external diff drivers, textconv and colour are off.
	args := []string{
		"-c", "diff.noprefix=false", "-c", "diff.mnemonicPrefix=false",
		"--literal-pathspecs", "diff", "--no-ext-diff", "--no-textconv", "--no-color", "--submodule=log",
	}
```

- [ ] **Step 4: Implement `hunks.go`**

Create `internal/worktree/hunks.go`:

```go
package worktree

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"git-ui/internal/gitcmd"
)

// Action is what a hunk or line selection does.
type Action string

const (
	ActionStage   Action = "stage"   // unstaged → index
	ActionUnstage Action = "unstage" // index → back out of the index
	ActionDiscard Action = "discard" // unstaged → thrown away from the working tree
)

var (
	// ErrDiffChanged and ErrUndoStale are shown to the user as they are.
	ErrDiffChanged  = errors.New("The file changed since it was shown — reloaded")
	ErrUndoStale    = errors.New("Can't undo: the file changed since the discard")
	ErrNotPatchable = errors.New("worktree: only a modified text file can be changed by hunk or line")
	ErrWrongSection = errors.New("worktree: staged changes can only be unstaged; unstaged ones can be staged or discarded")
)

// DiffHash identifies the exact diff the user was shown.
func DiffHash(diff string) string {
	sum := sha256.Sum256([]byte(diff))
	return hex.EncodeToString(sum[:])
}

// Patchable reports whether path's diff in one section can be split by hunk
// or line: a plain text modification only. An added, deleted, renamed,
// copied or type-changed path, a submodule and a binary file stay whole-file.
func Patchable(st State, path string, staged bool, diff string) bool {
	list := st.Unstaged
	if staged {
		list = st.Staged
	}
	i := slices.IndexFunc(list, func(f FileStatus) bool { return f.Path == path })
	if i < 0 {
		return false
	}
	f := list[i]
	if f.Status != "M" || f.Submodule || f.OldPath != "" {
		return false
	}
	if strings.HasPrefix(diff, "Binary files ") || strings.Contains(diff, "\nBinary files ") || strings.Contains(diff, "\nGIT binary patch") {
		return false
	}
	return strings.Contains(diff, "\n@@ ")
}

// ApplySelection stages, unstages or discards the selected hunks and lines
// of path. It rebuilds the patch from the file's full diff, refusing if that
// diff is no longer the one the user saw (hash), and returns the applied
// patch so a discard can be undone.
func ApplySelection(ctx context.Context, dir, path string, staged bool, hash string, sel Selection, action Action) (string, error) {
	var args []string
	switch action {
	case ActionStage:
		args = []string{"--cached"}
	case ActionUnstage:
		args = []string{"--cached", "-R"}
	case ActionDiscard:
		args = []string{"-R"}
	default:
		return "", fmt.Errorf("worktree: unknown action %q", action)
	}
	if (action == ActionUnstage) != staged {
		return "", ErrWrongSection
	}
	diff, err := FileDiff(ctx, dir, path, staged)
	if err != nil {
		return "", err
	}
	st, err := Status(ctx, dir)
	if err != nil {
		return "", err
	}
	if !Patchable(st, path, staged, diff) {
		return "", ErrNotPatchable
	}
	if DiffHash(diff) != hash {
		return "", ErrDiffChanged
	}
	parsed, err := ParseDiff(diff)
	if err != nil {
		return "", err
	}
	patch, err := BuildPatch(parsed, sel, action != ActionStage)
	if err != nil {
		return "", err
	}
	if err := applyPatch(ctx, dir, patch, args...); err != nil {
		return "", err
	}
	return patch, nil
}

// Reapply undoes a discard by applying its patch forward to the working
// tree again. git apply is atomic: a patch that no longer fits changes nothing.
func Reapply(ctx context.Context, dir, patch string) error {
	if err := applyPatch(ctx, dir, patch); err != nil {
		return ErrUndoStale
	}
	return nil
}

// applyPatch feeds patch to git apply through a temporary file (gitcmd has
// no stdin).
func applyPatch(ctx context.Context, dir, patch string, extra ...string) error {
	f, err := os.CreateTemp("", "committree-*.patch")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(patch); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	args := append([]string{"apply", "--recount", "--whitespace=nowarn"}, extra...)
	_, err = gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, append(args, f.Name())...)
	return err
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/worktree/ -v -run 'TestApplySelection|TestReapply|TestParseDiff|TestBuildPatch'`
Expected: PASS.

If `TestApplySelectionKeepsCRLF` fails, do not loosen `git apply` (no `--ignore-whitespace`): check that `ParseDiff` keeps the `\r` at the end of `Text` (it only splits on `\n`) and that nothing trims the patch before it is written.

- [ ] **Step 6: Run the whole worktree package**

Run: `go test ./internal/worktree/`
Expected: `ok` (the `FileDiff` flag change must not break the existing diff tests).

- [ ] **Step 7: Commit**

```bash
git add internal/worktree/filediff.go internal/worktree/hunks.go internal/worktree/hunks_test.go
git commit -m "feat(worktree): stage, unstage and discard a hunk or line selection with git apply"
```

---

### Task 3: App methods and bindings

**Files:**
- Modify: `internal/app/app.go` (the `App` struct, next to `owedDrops sync.Map`)
- Modify: `internal/app/worktree.go:88-102` (`GetWorktreeDiff`) and add two methods after `DiscardFile`
- Modify: `internal/app/worktree_test.go` (callers of `GetWorktreeDiff`, new tests)
- Modify: `frontend/wailsjs/go/app/App.d.ts`, `frontend/wailsjs/go/app/App.js`, `frontend/wailsjs/go/models.ts` (generated)

**Interfaces:**
- Consumes (Task 2): `worktree.FileDiff`, `worktree.Status`, `worktree.DiffHash`, `worktree.Patchable`, `worktree.ApplySelection`, `worktree.Reapply`, `worktree.Selection`, `worktree.Action`, `worktree.ActionDiscard`. Existing: `a.dir(id)`, `a.writeWorktree(id, fn)`, `tools.Truncate`, `worktreeDiffCap`, test helpers `newMergeApp(t) (*App, *testrepo.Repo, string)` and `newAIMergeApp(t, url) (*App, *testrepo.Repo, string, *events)`.
- Produces:
  - `type WorktreeDiff struct { Text string \`json:"text"\`; Hash string \`json:"hash"\`; Truncated bool \`json:"truncated"\`; Patchable bool \`json:"patchable"\` }`
  - `func (a *App) GetWorktreeDiff(id, path string, staged bool) (WorktreeDiff, error)`
  - `func (a *App) ApplyHunkSelection(id, path string, staged bool, hash string, sel worktree.Selection, action string) error`
  - `func (a *App) UndoDiscard(id string) error`
  - TS bindings: `GetWorktreeDiff(...): Promise<app.WorktreeDiff>`, `ApplyHunkSelection(arg1:string, arg2:string, arg3:boolean, arg4:string, arg5:Array<worktree.HunkPick>, arg6:string): Promise<void>`, `UndoDiscard(arg1:string): Promise<void>`

- [ ] **Step 1: Adapt the existing tests and write the new ones**

In `internal/app/worktree_test.go`, `GetWorktreeDiff` now returns a `WorktreeDiff`. In every existing test that calls it (`TestGetWorktreeDiffRefusesAnUnlistedPath`, `TestGetWorktreeDiffOfAVanishedUntrackedFileErrors`, `TestGetWorktreeDiffTruncatesALargeDiff`, `TestGetWorktreeDiffOfASubmodulePointerMove`), keep the variable names the assertions use by reading `.Text` right away. For example:

```go
	d, err := a.GetWorktreeDiff(id, "huge.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	out := d.Text
```

and for the one-line form inside an `if`:

```go
	if d, err := a.GetWorktreeDiff(id, "a.txt", false); err == nil {
		t.Errorf("want an error for a path that vanished before the diff, got out = %q", d.Text)
	}
```

Then append to `internal/app/worktree_test.go`:

```go
// A plain modification is patchable and carries the full diff's hash; an
// untracked file is not patchable; a truncated diff says so.
func TestGetWorktreeDiffFlags(t *testing.T) {
	a, r, id := newMergeApp(t)
	r.WriteFile("greeting.txt", "hey\n")
	d, err := a.GetWorktreeDiff(id, "greeting.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Patchable || d.Truncated || d.Hash != worktree.DiffHash(d.Text) {
		t.Errorf("modified file: %+v", d)
	}

	r.WriteFile("new.txt", "new\n")
	if d, err := a.GetWorktreeDiff(id, "new.txt", false); err != nil || d.Patchable {
		t.Errorf("untracked file: %+v, %v", d, err)
	}

	r.WriteFile("greeting.txt", strings.Repeat("y", worktreeDiffCap+1024)+"\n")
	d, err = a.GetWorktreeDiff(id, "greeting.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	full, _ := worktree.FileDiff(context.Background(), r.Dir, "greeting.txt", false)
	if !d.Truncated || d.Hash != worktree.DiffHash(full) {
		t.Errorf("truncated diff: truncated=%v, hash of the full diff=%v", d.Truncated, d.Hash == worktree.DiffHash(full))
	}
}

func TestApplyHunkSelectionDiscardAndUndo(t *testing.T) {
	a, r, id := newMergeApp(t)
	r.WriteFile("greeting.txt", "hey\n")
	d, err := a.GetWorktreeDiff(id, "greeting.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ApplyHunkSelection(id, "greeting.txt", false, d.Hash, worktree.Selection{{Hunk: 0}}, "discard"); err != nil {
		t.Fatal(err)
	}
	if got := r.Git("show", "HEAD:greeting.txt"); got != "hi" {
		t.Fatalf("HEAD content = %q", got)
	}
	data, _ := os.ReadFile(filepath.Join(r.Dir, "greeting.txt"))
	if string(data) != "hi\n" {
		t.Fatalf("after discard = %q, want the committed content", data)
	}

	if err := a.UndoDiscard(id); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(r.Dir, "greeting.txt"))
	if string(data) != "hey\n" {
		t.Fatalf("after undo = %q", data)
	}
	if err := a.UndoDiscard(id); err == nil {
		t.Error("a second undo succeeded; there is nothing left to undo")
	}
}

func TestApplyHunkSelectionStageKeepsNothingToUndo(t *testing.T) {
	a, r, id := newMergeApp(t)
	r.WriteFile("greeting.txt", "hey\n")
	d, _ := a.GetWorktreeDiff(id, "greeting.txt", false)
	if err := a.ApplyHunkSelection(id, "greeting.txt", false, d.Hash, worktree.Selection{{Hunk: 0}}, "stage"); err != nil {
		t.Fatal(err)
	}
	if cached := r.Git("diff", "--cached"); !strings.Contains(cached, "+hey") {
		t.Fatalf("staged diff = %s", cached)
	}
	if err := a.UndoDiscard(id); err == nil {
		t.Error("undo after a stage succeeded")
	}
}
```

Check the imports at the top of `worktree_test.go` include `context`, `os`, `path/filepath`, `strings` and `git-ui/internal/worktree` (they already do).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/app/ -run 'TestGetWorktreeDiff|TestApplyHunkSelection'`
Expected: FAIL to build (`d.Text undefined`, `a.ApplyHunkSelection undefined`).

- [ ] **Step 3: Implement**

In `internal/app/app.go`, in the `App` struct right after `owedDrops sync.Map ...`, add:

```go
	discards sync.Map // repo ID → the last hunk/line discard's patch, for Undo
```

In `internal/app/worktree.go`, replace `GetWorktreeDiff` with:

```go
// WorktreeDiff is one changed file's diff as the pane shows it. Hash is the
// SHA-256 of the full (untruncated) diff; the pane sends it back with a hunk
// or line action so Go can refuse a diff that changed since. Patchable says
// whether the file can be acted on by hunk or line at all.
type WorktreeDiff struct {
	Text      string `json:"text"`
	Hash      string `json:"hash"`
	Truncated bool   `json:"truncated"`
	Patchable bool   `json:"patchable"`
}

// GetWorktreeDiff returns what one changed file shows in the pane: the staged
// diff against HEAD, or the unstaged diff against the index, capped. Only a
// path the status just listed can be read (see worktree.FileDiff).
func (a *App) GetWorktreeDiff(id, path string, staged bool) (WorktreeDiff, error) {
	dir, err := a.dir(id)
	if err != nil {
		return WorktreeDiff{}, err
	}
	out, err := worktree.FileDiff(a.ctx, dir, path, staged)
	if err != nil {
		return WorktreeDiff{Text: out}, err
	}
	st, err := worktree.Status(a.ctx, dir)
	if err != nil {
		return WorktreeDiff{}, err
	}
	text := tools.Truncate(out, worktreeDiffCap)
	return WorktreeDiff{
		Text:      text,
		Hash:      worktree.DiffHash(out),
		Truncated: text != out,
		Patchable: worktree.Patchable(st, path, staged, out),
	}, nil
}
```

After `DiscardFile`, add:

```go
// ApplyHunkSelection stages, unstages or discards part of one file (see
// worktree.ApplySelection). A discard's patch is kept as the repository's
// last discard, for UndoDiscard.
func (a *App) ApplyHunkSelection(id, path string, staged bool, hash string, sel worktree.Selection, action string) error {
	return a.writeWorktree(id, func(ctx context.Context, dir string) error {
		patch, err := worktree.ApplySelection(ctx, dir, path, staged, hash, sel, worktree.Action(action))
		if err != nil {
			return err
		}
		if worktree.Action(action) == worktree.ActionDiscard {
			a.discards.Store(id, patch)
		}
		return nil
	})
}

// UndoDiscard puts the last hunk/line discard back. It is kept when the undo
// fails (the lines changed since), so it can be tried again.
func (a *App) UndoDiscard(id string) error {
	return a.writeWorktree(id, func(ctx context.Context, dir string) error {
		patch, ok := a.discards.Load(id)
		if !ok {
			return errors.New("nothing to undo")
		}
		if err := worktree.Reapply(ctx, dir, patch.(string)); err != nil {
			return err
		}
		a.discards.Delete(id)
		return nil
	})
}
```

(`errors` is already imported in `worktree.go`; add it if the compiler says otherwise.)

- [ ] **Step 4: Run the Go tests**

Run: `go test ./internal/app/ ./internal/worktree/`
Expected: `ok` for both.

- [ ] **Step 5: Regenerate the bindings**

Run: `source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && ~/go/bin/wails generate module`
Then drop the unrelated noise: `git checkout -p frontend/wailsjs` and answer `y` (discard) for the `Menu` function, `keys` and `menu` namespaces, and `n` (keep) for `GetWorktreeDiff`, `ApplyHunkSelection`, `UndoDiscard`, `app.WorktreeDiff` and `worktree.HunkPick`.
Check: `git diff frontend/wailsjs` shows only those five.

- [ ] **Step 6: Commit**

The frontend does not compile until Task 4 adapts `api.ts`; that is expected between these two commits.

```bash
git add internal/app/app.go internal/app/worktree.go internal/app/worktree_test.go frontend/wailsjs
git commit -m "feat(app): hunk and line actions, discard undo, and diff hash/flags for the pane"
```

---

### Task 4: Frontend logic — rows, selection, API and actions

**Files:**
- Modify: `frontend/src/lib/types.ts` (append)
- Create: `frontend/src/lib/hunks.ts`
- Test: `frontend/src/lib/hunks.test.ts`
- Modify: `frontend/src/lib/api.ts:91` (`getWorktreeDiff`) and add two entries
- Modify: `frontend/src/lib/actions.ts` (imports; new functions after `checkoutCommit`)
- Modify: `frontend/src/components/ChangesView.svelte` (only what is needed to compile with the new `getWorktreeDiff` type; the real UI change is Task 5)

**Interfaces:**
- Consumes (Task 3): `Go.GetWorktreeDiff`, `Go.ApplyHunkSelection`, `Go.UndoDiscard`. Existing: `call<T>()` in `api.ts`, `run(label, fn)` and `toast(message, kind, action?)` (a toast with an `action` does not auto-dismiss).
- Produces:
  - types: `WorktreeDiff { text; hash; truncated; patchable }`, `HunkPick { hunk: number; lines: number[] }`, `HunkAction = 'stage' | 'unstage' | 'discard'`
  - `hunks.ts`: `RowKind`, `DiffRow { text; kind; hunk; line }`, `diffRows(text, truncated): { rows: DiffRow[]; actionable: Set<number> }`, `rowKey(row)`, `selectable(row, actionable)`, `LineSelection { keys: Set<string>; anchor: number | null }`, `emptySelection()`, `clickSelect(rows, actionable, sel, index, { shift, toggle })`, `toPicks(keys): HunkPick[]`, `pickSummary(picks): string`
  - `api.applyHunkSelection(id, path, staged, hash, picks, action)`, `api.undoDiscard(id)`
  - `actions.applyHunkSelection(id, path, staged, hash, picks, action, what)`, `actions.undoDiscard(id)`

- [ ] **Step 1: Write the failing tests**

Create `frontend/src/lib/hunks.test.ts`:

```ts
import { describe, expect, it } from 'vitest'
import { clickSelect, diffRows, emptySelection, pickSummary, rowKey, toPicks } from './hunks'

const header = 'diff --git a/f.txt b/f.txt\nindex 1..2 100644\n--- a/f.txt\n+++ b/f.txt\n'
const text = header + '@@ -1,3 +1,3 @@\n one\n-two\n+TWO\n\\ No newline at end of file\n@@ -10,2 +10,2 @@\n-ten\n+TEN\n'

describe('diffRows', () => {
  it('numbers body lines per hunk the way Go does, skipping notes', () => {
    const { rows, actionable } = diffRows(text, false)
    expect(rows.map((r) => [r.kind, r.hunk, r.line])).toEqual([
      ['meta', -1, -1], ['meta', -1, -1], ['meta', -1, -1], ['meta', -1, -1],
      ['hunk', 0, -1], ['context', 0, 0], ['del', 0, 1], ['add', 0, 2], ['note', 0, -1],
      ['hunk', 1, -1], ['del', 1, 0], ['add', 1, 1],
    ])
    expect([...actionable]).toEqual([0, 1])
  })

  it('reads header look-alikes inside a hunk as changes', () => {
    const { rows } = diffRows(header + '@@ -1 +1 @@\n--- y\n+++ x\n', false)
    expect(rows.slice(5).map((r) => r.kind)).toEqual(['del', 'add'])
  })

  it('leaves the last hunk of a truncated diff without actions', () => {
    const { rows, actionable } = diffRows(text + '[truncated]', true)
    expect([...actionable]).toEqual([0])
    expect(rows[rows.length - 1].kind).toBe('note')
  })
})

describe('clickSelect', () => {
  const { rows, actionable } = diffRows(text, false)
  const idx = (hunk: number, line: number) => rows.findIndex((r) => r.hunk === hunk && r.line === line)
  const plain = { shift: false, toggle: false }

  it('selects one change line and ignores context and header rows', () => {
    const sel = clickSelect(rows, actionable, emptySelection(), idx(0, 1), plain)
    expect([...sel.keys]).toEqual(['0:1'])
    expect(clickSelect(rows, actionable, sel, idx(0, 0), plain)).toBe(sel)
    expect(clickSelect(rows, actionable, sel, 4, plain)).toBe(sel)
  })

  it('selects a range with Shift, across hunks, skipping what is not a change', () => {
    let sel = clickSelect(rows, actionable, emptySelection(), idx(0, 2), plain)
    sel = clickSelect(rows, actionable, sel, idx(1, 1), { shift: true, toggle: false })
    expect([...sel.keys].sort()).toEqual(['0:2', '1:0', '1:1'])
  })

  it('toggles single lines with Cmd or Ctrl', () => {
    let sel = clickSelect(rows, actionable, emptySelection(), idx(0, 1), plain)
    sel = clickSelect(rows, actionable, sel, idx(1, 0), { shift: false, toggle: true })
    expect([...sel.keys].sort()).toEqual(['0:1', '1:0'])
    sel = clickSelect(rows, actionable, sel, idx(0, 1), { shift: false, toggle: true })
    expect([...sel.keys]).toEqual(['1:0'])
  })

  it('does not select lines of a hunk without actions', () => {
    const cut = diffRows(text, true)
    const last = cut.rows.findIndex((r) => r.hunk === 1 && r.kind === 'del')
    expect(clickSelect(cut.rows, cut.actionable, emptySelection(), last, plain).keys.size).toBe(0)
  })

  it('keys rows by hunk and line', () => {
    expect(rowKey(rows[idx(1, 1)])).toBe('1:1')
  })
})

describe('toPicks and pickSummary', () => {
  it('groups selected lines by hunk, sorted', () => {
    expect(toPicks(new Set(['1:1', '0:2', '1:0']))).toEqual([{ hunk: 0, lines: [2] }, { hunk: 1, lines: [0, 1] }])
  })

  it('describes what an action touched', () => {
    expect(pickSummary([{ hunk: 0, lines: [] }])).toBe('1 hunk')
    expect(pickSummary([{ hunk: 0, lines: [] }, { hunk: 1, lines: [] }])).toBe('2 hunks')
    expect(pickSummary([{ hunk: 0, lines: [1] }])).toBe('1 line')
    expect(pickSummary([{ hunk: 0, lines: [1] }, { hunk: 1, lines: [0, 1] }])).toBe('3 lines')
  })
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run src/lib/hunks.test.ts`
Expected: FAIL, `Failed to resolve import "./hunks"`.

- [ ] **Step 3: Add the types**

Append to `frontend/src/lib/types.ts`:

```ts
/** One changed file's diff for the pane (app.WorktreeDiff). `hash` identifies
 *  the full diff and goes back with a hunk or line action. */
export interface WorktreeDiff {
  text: string
  hash: string
  truncated: boolean
  patchable: boolean
}

/** Lines of one hunk, by index among its body lines; empty = the whole hunk. */
export interface HunkPick {
  hunk: number
  lines: number[]
}

export type HunkAction = 'stage' | 'unstage' | 'discard'
```

- [ ] **Step 4: Implement `hunks.ts`**

Create `frontend/src/lib/hunks.ts`:

```ts
import type { HunkPick } from './types'

export type RowKind = 'meta' | 'hunk' | 'context' | 'add' | 'del' | 'note'

/** One row of the diff pane. `line` is the index among the hunk's body lines
 *  (context, + and -), numbered exactly as worktree.ParseDiff does in Go;
 *  -1 for headers, @@ rows and notes. `hunk` is -1 before the first @@. */
export interface DiffRow {
  text: string
  kind: RowKind
  hunk: number
  line: number
}

/**
 * diffRows splits the pane's diff text into rows. Inside a hunk a row is read
 * by its first character only, so an added "++ x" (shown "+++ x") is a change.
 * Every hunk takes actions except the last one of a truncated diff, which may
 * be cut short.
 */
export function diffRows(text: string, truncated: boolean): { rows: DiffRow[]; actionable: Set<number> } {
  const lines = text.split('\n')
  if (lines.length && lines[lines.length - 1] === '') lines.pop()
  const rows: DiffRow[] = []
  let hunk = -1
  let line = -1
  for (const t of lines) {
    if (t.startsWith('@@ ')) {
      hunk++
      line = -1
      rows.push({ text: t, kind: 'hunk', hunk, line: -1 })
    } else if (hunk < 0) {
      rows.push({ text: t, kind: 'meta', hunk, line: -1 })
    } else if (t.startsWith('\\') || (truncated && t === '[truncated]')) {
      rows.push({ text: t, kind: 'note', hunk, line: -1 })
    } else {
      const kind: RowKind = t.startsWith('+') ? 'add' : t.startsWith('-') ? 'del' : 'context'
      rows.push({ text: t, kind, hunk, line: ++line })
    }
  }
  const actionable = new Set<number>()
  for (let h = 0; h <= (truncated ? hunk - 1 : hunk); h++) actionable.add(h)
  return { rows, actionable }
}

export const rowKey = (row: DiffRow) => `${row.hunk}:${row.line}`

export function selectable(row: DiffRow | undefined, actionable: Set<number>): boolean {
  return !!row && (row.kind === 'add' || row.kind === 'del') && actionable.has(row.hunk)
}

export interface LineSelection {
  keys: Set<string>
  /** Row index of the last plain or toggle click, where a Shift range starts. */
  anchor: number | null
}

export const emptySelection = (): LineSelection => ({ keys: new Set(), anchor: null })

/**
 * clickSelect applies a click on rows[index]: a plain click selects that line
 * alone, Shift selects every change line between the anchor and it (replacing
 * the selection), toggle (Cmd or Ctrl) adds or removes it. A click on a row
 * that is not a selectable change returns `sel` unchanged.
 */
export function clickSelect(
  rows: DiffRow[],
  actionable: Set<number>,
  sel: LineSelection,
  index: number,
  mods: { shift: boolean; toggle: boolean },
): LineSelection {
  const row = rows[index]
  if (!selectable(row, actionable)) return sel
  if (mods.shift && sel.anchor !== null) {
    const [from, to] = sel.anchor < index ? [sel.anchor, index] : [index, sel.anchor]
    const keys = new Set<string>()
    for (let i = from; i <= to; i++) if (selectable(rows[i], actionable)) keys.add(rowKey(rows[i]))
    return { keys, anchor: sel.anchor }
  }
  if (mods.toggle) {
    const keys = new Set(sel.keys)
    const key = rowKey(row)
    if (keys.has(key)) keys.delete(key)
    else keys.add(key)
    return { keys, anchor: index }
  }
  return { keys: new Set([rowKey(row)]), anchor: index }
}

/** toPicks turns selected row keys into what Go takes, grouped by hunk and sorted. */
export function toPicks(keys: Set<string>): HunkPick[] {
  const byHunk = new Map<number, number[]>()
  for (const key of keys) {
    const [hunk, line] = key.split(':').map(Number)
    byHunk.set(hunk, [...(byHunk.get(hunk) ?? []), line])
  }
  return [...byHunk.entries()]
    .sort((a, b) => a[0] - b[0])
    .map(([hunk, lines]) => ({ hunk, lines: lines.sort((a, b) => a - b) }))
}

/** pickSummary is the "1 hunk" / "3 lines" part of the discard toast. */
export function pickSummary(picks: HunkPick[]): string {
  if (picks.every((p) => p.lines.length === 0)) return picks.length === 1 ? '1 hunk' : `${picks.length} hunks`
  const n = picks.reduce((sum, p) => sum + p.lines.length, 0)
  return n === 1 ? '1 line' : `${n} lines`
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run src/lib/hunks.test.ts`
Expected: PASS (10 tests).

- [ ] **Step 6: API and actions**

In `frontend/src/lib/api.ts`, replace the `getWorktreeDiff` line with:

```ts
  getWorktreeDiff: (id: string, path: string, staged: boolean) => call<WorktreeDiff>(Go.GetWorktreeDiff(id, path, staged)),
  applyHunkSelection: (id: string, path: string, staged: boolean, hash: string, picks: HunkPick[], action: HunkAction) =>
    call<void>(Go.ApplyHunkSelection(id, path, staged, hash, picks, action)),
  undoDiscard: (id: string) => call<void>(Go.UndoDiscard(id)),
```

and add `HunkAction, HunkPick, WorktreeDiff` to the `import type { … } from './types'` line of `api.ts`.

In `frontend/src/lib/actions.ts`, add `HunkAction, HunkPick` to the `import type { … } from './types'` line, and after `checkoutCommit` add:

```ts
const HUNK_BUSY: Record<HunkAction, string> = { stage: 'Staging…', unstage: 'Unstaging…', discard: 'Discarding…' }

/** applyHunkSelection stages, unstages or discards part of one file. `what`
 *  ("1 hunk", "3 lines") names it in the discard toast, whose Undo puts it back. */
export async function applyHunkSelection(id: string, path: string, staged: boolean, hash: string, picks: HunkPick[], action: HunkAction, what: string) {
  const ok = await run(HUNK_BUSY[action], () => api.applyHunkSelection(id, path, staged, hash, picks, action))
  if (ok && action === 'discard') {
    toast(`Discarded ${what} in ${path.split('/').pop()}`, 'info', { label: 'Undo', run: () => undoDiscard(id) })
  }
}

export async function undoDiscard(id: string) {
  await run('Restoring…', () => api.undoDiscard(id))
}
```

- [ ] **Step 7: Keep `ChangesView` compiling**

In `frontend/src/components/ChangesView.svelte`, the two `api.getWorktreeDiff` calls now return a `WorktreeDiff`. In `open()` and `refresh()`, change `text = diff` to `text = diff.text`. (Task 5 replaces this properly.)

- [ ] **Step 8: Run all frontend tests and the type check**

Run: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run && npm run check`
Expected: all test files pass; `0 ERRORS` (the two existing a11y warnings in `BlameView.svelte` and `ContextMenu.svelte` are known).

- [ ] **Step 9: Commit**

```bash
git add frontend/src/lib/types.ts frontend/src/lib/hunks.ts frontend/src/lib/hunks.test.ts frontend/src/lib/api.ts frontend/src/lib/actions.ts frontend/src/components/ChangesView.svelte
git commit -m "feat(ui): diff rows, line selection and hunk/line actions with an Undo toast"
```

---

### Task 5: The diff pane — buttons, selection, spec and manual pass

**Files:**
- Create: `frontend/src/components/DiffLines.svelte`
- Modify: `frontend/src/components/ChangesView.svelte` (state, template, styles)
- Modify: `docs/spec/03-working-tree.md`

**Interfaces:**
- Consumes (Task 4): `diffRows`, `clickSelect`, `emptySelection`, `pickSummary`, `rowKey`, `selectable`, `toPicks`, `LineSelection`, `applyHunkSelection`, types `WorktreeDiff`, `HunkAction`, `HunkPick`. Existing: `busy` store, CSS variables `--accent`, `--faint`, `--border`, `--bg`, `--surface`, `--text`, `--danger`, `--add-bg`, `--del-bg`.
- Produces: `<DiffLines repoId path staged diff />`.

- [ ] **Step 1: Create `DiffLines.svelte`**

```svelte
<script lang="ts">
  import { applyHunkSelection } from '../lib/actions'
  import { clickSelect, diffRows, emptySelection, pickSummary, rowKey, selectable, toPicks, type LineSelection } from '../lib/hunks'
  import { busy } from '../lib/stores'
  import type { HunkAction, HunkPick, WorktreeDiff } from '../lib/types'

  export let repoId: string
  export let path: string
  export let staged: boolean
  export let diff: WorktreeDiff

  let sel: LineSelection = emptySelection()

  $: parsed = diffRows(diff.text, diff.truncated)
  $: actionable = diff.patchable ? parsed.actionable : new Set<number>()
  // A new diff (a reload after any change, or another file) drops the selection.
  $: diff, (sel = emptySelection())
  $: actions = (staged ? [['unstage', 'Unstage']] : [['stage', 'Stage'], ['discard', 'Discard']]) as [HunkAction, string][]

  function click(event: MouseEvent, index: number) {
    // A drag that selected text to copy is not a line click.
    if (!event.shiftKey && window.getSelection()?.toString()) return
    sel = clickSelect(parsed.rows, actionable, sel, index, { shift: event.shiftKey, toggle: event.metaKey || event.ctrlKey })
  }

  // Without this, Shift+click would extend the native text selection instead.
  function mousedown(event: MouseEvent, index: number) {
    if (event.shiftKey && selectable(parsed.rows[index], actionable)) event.preventDefault()
  }

  function act(action: HunkAction, picks: HunkPick[]) {
    sel = emptySelection()
    applyHunkSelection(repoId, path, staged, diff.hash, picks, action, pickSummary(picks))
  }

  function keydown(event: KeyboardEvent) {
    if (event.key === 'Escape' && sel.keys.size) sel = emptySelection()
  }
</script>

<svelte:window on:keydown={keydown} />

{#if sel.keys.size}
  <div class="selbar">
    <span class="count">{sel.keys.size === 1 ? '1 line' : `${sel.keys.size} lines`} selected</span>
    {#each actions as [action, label]}
      <button class:danger={action === 'discard'} disabled={!!$busy} on:click={() => act(action, toPicks(sel.keys))}>{label} lines</button>
    {/each}
    <button class="clear" title="Clear selection (Esc)" aria-label="Clear selection" on:click={() => (sel = emptySelection())}>×</button>
  </div>
{/if}
{#each parsed.rows as row, i}
  {#if row.kind === 'hunk'}
    <div class="line hunk">
      <span class="ellipsis">{row.text}</span>
      {#if actionable.has(row.hunk)}
        <span class="hunk-actions">
          {#each actions as [action, label]}
            <button class:danger={action === 'discard'} disabled={!!$busy} on:click={() => act(action, [{ hunk: row.hunk, lines: [] }])}>{label} hunk</button>
          {/each}
        </span>
      {/if}
    </div>
  {:else}
    <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
    <div
      class="line {row.kind}"
      class:selectable={selectable(row, actionable)}
      class:selected={sel.keys.has(rowKey(row))}
      on:mousedown={(e) => mousedown(e, i)}
      on:click={(e) => click(e, i)}
    >{row.text || ' '}</div>
  {/if}
{/each}

<style>
  .line { padding: 0 12px; white-space: pre; line-height: 18px; border-left: 3px solid transparent; }
  .add { background: var(--add-bg); }
  .del { background: var(--del-bg); }
  .meta, .note { color: var(--faint); }
  .hunk { color: var(--accent); display: flex; align-items: center; justify-content: space-between; gap: 8px; min-height: 22px; }
  .selectable { cursor: pointer; }
  .selected { border-left-color: var(--accent); filter: saturate(1.6) brightness(0.95); }
  .hunk-actions { display: flex; gap: 4px; flex: none; }
  .selbar {
    position: sticky; top: 0; z-index: 1; display: flex; align-items: center; gap: 6px;
    padding: 4px 12px; background: var(--surface); border-bottom: 1px solid var(--border);
  }
  .count { color: var(--faint); margin-right: auto; }
  button {
    font: inherit; font-size: 11px; line-height: 16px; padding: 1px 6px; cursor: pointer;
    color: var(--text); background: var(--bg); border: 1px solid var(--border); border-radius: 4px;
  }
  button:disabled { opacity: 0.5; cursor: default; }
  button.danger { color: var(--danger); }
  .clear { border: 0; background: none; font-size: 14px; }
</style>
```

- [ ] **Step 2: Use it in `ChangesView.svelte`**

1. Imports: add `import DiffLines from './DiffLines.svelte'` and add `WorktreeDiff` to the `import type { FileStatus } from '../lib/types'` line. Remove the now-unused `import { lineClass } from '../lib/diff'`.
2. State: replace `let text = ''` with `let diff: WorktreeDiff | null = null`.
3. Submodule parsing: replace `submoduleDiffFor(text, selectedFile)` with `submoduleDiffFor(diff?.text ?? '', selectedFile)`.
4. In `open()`: replace `text = ''` with `diff = null`, and
   ```ts
      const diff = await api.getWorktreeDiff(repoId, file.path, isStaged(file))
      if (current !== request) return
      text = diff.text
   ```
   with
   ```ts
      const next = await api.getWorktreeDiff(repoId, file.path, isStaged(file))
      if (current !== request) return
      diff = next
   ```
5. In `refresh()`: the same replacement (`const next = await api.getWorktreeDiff(repoId, path, staged)` … `diff = next`).
6. Template: replace
   ```svelte
      {:else}
        {#each text.split('\n') as line}
          <div class="line {lineClass(line)}">{line || ' '}</div>
        {/each}
      {/if}
   ```
   with
   ```svelte
      {:else if diff && selection}
        <DiffLines {repoId} path={selection.path} staged={selection.section === 'Staged'} {diff} />
      {/if}
   ```
7. Styles: delete the `.line`, `.add`, `.del`, `.hunk` and `.meta` rules from `ChangesView.svelte` (they moved to `DiffLines.svelte`); keep `.content`, `.error`, `.empty`. Change `.content`'s `padding: 8px 0` to `padding: 0 0 8px` so the sticky selection bar sits flush at the top.

- [ ] **Step 3: Type check and tests**

Run: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npm run check && npx vitest run`
Expected: `0 ERRORS`, same two known warnings; all tests pass.

- [ ] **Step 4: Update the behaviour spec**

In `docs/spec/03-working-tree.md`, after the paragraph that starts "Diff lines are coloured by their leading character", add:

```markdown
### Acting on part of a file

A file whose change is a plain text modification (status `M`: not added,
deleted, renamed, copied, type-changed, binary or a submodule) can be acted
on by hunk or by line from its diff. Every other file keeps only the
file-level actions.

- Each hunk header carries **Stage hunk** and **Discard hunk** for a file in
  Unstaged, or **Unstage hunk** for a file in Staged. Discarding from Staged
  is only offered for the whole file.
- A click on a `+` or `-` line selects it; Shift+click selects the change
  lines between the last clicked line and this one; Cmd+click (Ctrl+click)
  adds or removes one line. Context and header lines cannot be selected. A
  drag still selects text for copying. Esc clears the selection.
- With lines selected, a bar at the top of the diff shows the count and
  **Stage lines** / **Discard lines** (Unstaged) or **Unstage lines**
  (Staged). Staging some lines of a hunk leaves the others unstaged, and so
  on for the other two actions.
- The last hunk of a truncated diff has no actions, since it may be cut short.
- The action is refused, with "The file changed since it was shown —
  reloaded", when the file's diff changed after it was shown (an edit in an
  editor or a terminal); the diff then reloads and nothing is changed.
- A hunk or line discard is not confirmed. It shows "Discarded 1 hunk in
  `<file>`" (or "N lines") with **Undo**, which stays until dismissed. Undo
  puts the discarded lines back; only the last discard of the repository can
  be undone, and only while the app is open. If those lines changed since,
  Undo reports "Can't undo: the file changed since the discard" and changes
  nothing.
- All buttons are disabled while another write is running.
```

- [ ] **Step 5: Build, reopen and do the manual pass**

Run: `make build && git checkout -- frontend/wailsjs && (pkill -x CommitTree; sleep 1; open build/bin/CommitTree.app)`
(`make build` regenerates the bindings with the `Menu()` noise; the checkout drops it again, since the real binding changes are already committed.)

Prepare a demo repository:

```bash
rm -rf /tmp/git-ui-hunks-demo && mkdir -p /tmp/git-ui-hunks-demo && cd /tmp/git-ui-hunks-demo && git init -q -b main && seq 1 30 | sed 's/^/line /' > f.txt && git add f.txt && git -c user.name=Demo -c user.email=demo@example.com commit -qm base && sed -i '' -e 's/^line 3$/THREE/' -e 's/^line 20$/TWENTY\nTWENTY-B/' f.txt
```

In the app, add `/tmp/git-ui-hunks-demo`, open Changes, select `f.txt`, and check:
- [ ] Both hunk headers show Stage hunk / Discard hunk; staging one moves it to Staged, whose header shows only Unstage hunk.
- [ ] Clicking `+TWENTY-B` selects it; the bar says "1 line selected"; Stage lines stages only that line.
- [ ] **Review Focus 5:** Shift+click selects a range of change lines (no native text highlight appears); a mouse drag over text still highlights it for copying and does not select lines; Cmd+click toggles one line; Esc clears.
- [ ] Discard hunk shows the toast with Undo; Undo brings the lines back.
- [ ] Edit `f.txt` in a terminal after the diff is shown, then click Stage hunk: the toast reads "The file changed since it was shown — reloaded" and the diff refreshes.
- [ ] An untracked file shows no hunk buttons and no line selection.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/DiffLines.svelte frontend/src/components/ChangesView.svelte docs/spec/03-working-tree.md
git commit -m "feat(ui): hunk buttons and line selection in the Changes diff"
```
