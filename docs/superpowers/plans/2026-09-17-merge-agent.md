# Branch Merge with an AI Conflict Agent — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Merge a branch into the current one from the sidebar, show the conflicted state in the app, and let an AI agent resolve conflicts hunk by hunk, stopping before the commit.

**Architecture:** A new `internal/merge` package owns driving git (`Start`/`Status`/`Abort`/`Commit`) and, separately, pure text surgery over conflict markers (`Parse`/`Splice`). A new `internal/ai/mergetools` package exposes that surgery to the agent as four tools, so the model can only ever replace a marker block — never rewrite a file. `internal/app` binds the operations for Wails and runs the agent through the existing per-repo chat slot. The frontend gains a merge store, a "Merge X into Y" item in the branch context menu, and a `MergeView` that replaces commit details while the repo is merging.

**Tech Stack:** Go 1.26 (stdlib + `git` CLI through `internal/gitcmd`), Wails v2.16, Svelte 5 in legacy syntax, Vitest, `go test`.

**Spec:** `docs/superpowers/specs/2026-09-17-merge-agent-design.md`

## Global Constraints

- Go 1.26. No new Go dependencies; everything goes through `internal/gitcmd`.
- Svelte 5 with **legacy syntax only**: `export let`, `$:`, `on:click`. Never runes, never `new Component`.
- Never hardcode colors in components. Use the CSS tokens in `frontend/src/theme.css` (`--border`, `--muted`, `--faint`, `--surface`, `--hover`, `--active`, `--accent`, `--danger`, `--ok`, `--add-bg`, `--del-bg`). If a needed color has no token, add one for both light and dark mode.
- Git commands: `gitcmd.ReadTimeout` for local operations, `gitcmd.NetworkTimeout` only for network ones. Merging is local — always `ReadTimeout`.
- Every git command that takes a user-supplied ref validates it first (non-empty, not starting with `-`).
- **Never add `Co-Authored-By` lines to commits.**
- Frontend commands run under Node 22: `source ~/.nvm/nvm.sh && nvm use 22` first.
- Verification before any commit: `go vet ./...`, `go test ./...`, and for frontend changes `npm test` plus `npm run check` (0 errors; 3 pre-existing a11y warnings are expected).

---

### Task 1: Conflict marker parsing

**Files:**
- Create: `internal/merge/conflict.go`
- Test: `internal/merge/conflict_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `merge.ContextLines` (const, 6); `merge.Hunk{Index int; Ours, Theirs, Base, Before, After string}` with unexported `start, end int`; `merge.Parse(content string) ([]Hunk, error)`; `merge.HasMarkers(s string) bool`; `merge.ErrBadConflict` (sentinel).

- [ ] **Step 1: Write the failing test**

```go
package merge

import (
	"strings"
	"testing"
)

const threeWay = `package main

import "fmt"

func main() {
<<<<<<< HEAD
	fmt.Println("ours")
||||||| base
	fmt.Println("base")
=======
	fmt.Println("theirs")
>>>>>>> feature
}
`

func TestParseThreeWayHunk(t *testing.T) {
	hunks, err := Parse(threeWay)
	if err != nil {
		t.Fatal(err)
	}
	if len(hunks) != 1 {
		t.Fatalf("got %d hunks, want 1", len(hunks))
	}
	h := hunks[0]
	if h.Ours != "\tfmt.Println(\"ours\")\n" {
		t.Errorf("ours = %q", h.Ours)
	}
	if h.Base != "\tfmt.Println(\"base\")\n" {
		t.Errorf("base = %q", h.Base)
	}
	if h.Theirs != "\tfmt.Println(\"theirs\")\n" {
		t.Errorf("theirs = %q", h.Theirs)
	}
	if !strings.HasSuffix(h.Before, "func main() {\n") {
		t.Errorf("before = %q", h.Before)
	}
	if h.After != "}\n" {
		t.Errorf("after = %q", h.After)
	}
}

func TestParseTwoWayHunkHasNoBase(t *testing.T) {
	hunks, err := Parse("a\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> other\nb\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(hunks) != 1 || hunks[0].Base != "" {
		t.Fatalf("got %d hunks, base %q", len(hunks), hunks[0].Base)
	}
}

func TestParseSeveralHunksAreIndexed(t *testing.T) {
	content := "<<<<<<< HEAD\n1\n=======\n2\n>>>>>>> x\nmiddle\n<<<<<<< HEAD\n3\n=======\n4\n>>>>>>> x\n"
	hunks, err := Parse(content)
	if err != nil {
		t.Fatal(err)
	}
	if len(hunks) != 2 || hunks[0].Index != 0 || hunks[1].Index != 1 {
		t.Fatalf("got %d hunks with indexes %d,%d", len(hunks), hunks[0].Index, hunks[1].Index)
	}
	if hunks[0].Ours != "1\n" || hunks[1].Theirs != "4\n" {
		t.Errorf("wrong sides: %q %q", hunks[0].Ours, hunks[1].Theirs)
	}
}

// A markdown setext underline is a run of "=" longer or shorter than the
// seven-character marker, and must not be mistaken for the separator.
func TestParseIgnoresMarkdownUnderline(t *testing.T) {
	content := "<<<<<<< HEAD\nTitle\n========\nbody\n=======\ntheirs\n>>>>>>> x\n"
	hunks, err := Parse(content)
	if err != nil {
		t.Fatal(err)
	}
	if hunks[0].Ours != "Title\n========\nbody\n" {
		t.Errorf("ours = %q", hunks[0].Ours)
	}
}

func TestParseCRLF(t *testing.T) {
	hunks, err := Parse("<<<<<<< HEAD\r\nours\r\n=======\r\ntheirs\r\n>>>>>>> x\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if hunks[0].Ours != "ours\r\n" {
		t.Errorf("ours = %q", hunks[0].Ours)
	}
}

func TestParseUnterminatedIsAnError(t *testing.T) {
	if _, err := Parse("<<<<<<< HEAD\nours\n=======\ntheirs\n"); err == nil {
		t.Fatal("want an error for an unterminated conflict")
	}
}

func TestParseCleanFileHasNoHunks(t *testing.T) {
	hunks, err := Parse("nothing to see\n")
	if err != nil || len(hunks) != 0 {
		t.Fatalf("hunks = %v, err = %v", hunks, err)
	}
}

func TestHasMarkers(t *testing.T) {
	if !HasMarkers("a\n<<<<<<< HEAD\n") {
		t.Error("want true for a file with an opening marker")
	}
	if HasMarkers("a\n========\nb\n") {
		t.Error("want false for a markdown underline")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/merge/`
Expected: FAIL — the package does not exist yet (`no Go files` / undefined: `Parse`).

- [ ] **Step 3: Write the implementation**

```go
// Package merge runs git merges and edits the conflict markers they leave
// behind. The marker surgery (Parse, Splice) is pure text handling with no
// git involved, so it can be tested exhaustively.
package merge

import (
	"errors"
	"fmt"
	"strings"
)

// ContextLines is how many lines above and below a conflict are shown with it.
const ContextLines = 6

// ErrBadConflict reports a file whose markers don't nest the way git writes
// them; refusing is safer than guessing where the sides end.
var ErrBadConflict = errors.New("merge: malformed conflict markers")

// Hunk is one conflicted region of a file. Base is empty when the file was
// written in the two-way style, which carries no common ancestor.
type Hunk struct {
	Index  int
	Ours   string
	Theirs string
	Base   string
	Before string
	After  string

	start, end int // line indices of the marker block; end is exclusive
}

// Parse returns the conflicted regions of content, in file order.
func Parse(content string) ([]Hunk, error) {
	lines := splitLines(content)
	hunks := []Hunk{}
	for i := 0; i < len(lines); i++ {
		if !marker(lines[i], "<<<<<<<") {
			continue
		}
		h := Hunk{Index: len(hunks), start: i}
		var ours, base, theirs []string
		side := &ours
		closed := false
		for i++; i < len(lines) && !closed; i++ {
			line := lines[i]
			switch {
			case marker(line, "<<<<<<<"):
				return nil, fmt.Errorf("%w: conflict reopened at line %d", ErrBadConflict, i+1)
			case marker(line, "|||||||") && side == &ours:
				side = &base
			case marker(line, "=======") && side != &theirs:
				side = &theirs
			case marker(line, ">>>>>>>") && side == &theirs:
				h.end = i + 1
				closed = true
			default:
				*side = append(*side, line)
			}
		}
		if !closed {
			return nil, fmt.Errorf("%w: unterminated conflict at line %d", ErrBadConflict, h.start+1)
		}
		i = h.end - 1
		h.Ours, h.Base, h.Theirs = strings.Join(ours, ""), strings.Join(base, ""), strings.Join(theirs, "")
		h.Before = strings.Join(lines[max(0, h.start-ContextLines):h.start], "")
		hunks = append(hunks, h)
	}
	// After is filled second: it stops at the next conflict, which isn't
	// known until the whole file has been walked.
	for i := range hunks {
		limit := len(lines)
		if i+1 < len(hunks) {
			limit = hunks[i+1].start
		}
		hunks[i].After = strings.Join(lines[hunks[i].end:min(limit, hunks[i].end+ContextLines)], "")
	}
	return hunks, nil
}

// HasMarkers reports whether s still contains conflict markers.
func HasMarkers(s string) bool {
	for _, line := range splitLines(s) {
		if marker(line, "<<<<<<<") || marker(line, "=======") || marker(line, ">>>>>>>") {
			return true
		}
	}
	return false
}

// marker reports whether line is a conflict marker of the given kind: the
// seven-character sign followed by a space or the end of the line. Markdown
// underlines ("========") are longer, so they don't match.
func marker(line, sign string) bool {
	if !strings.HasPrefix(line, sign) {
		return false
	}
	rest := strings.TrimSuffix(strings.TrimSuffix(line[len(sign):], "\n"), "\r")
	return rest == "" || strings.HasPrefix(rest, " ")
}

// splitLines keeps each line's own terminator, so joining the result
// reproduces the input byte for byte.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.SplitAfter(s, "\n")
	if n := len(lines); lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/merge/ -v`
Expected: PASS, all eight tests.

- [ ] **Step 5: Commit**

```bash
git add internal/merge/conflict.go internal/merge/conflict_test.go
git commit -m "feat(merge): parse conflict markers into hunks"
```

---

### Task 2: Splicing a resolution back into a file

**Files:**
- Modify: `internal/merge/conflict.go`
- Test: `internal/merge/conflict_test.go`

**Interfaces:**
- Consumes: `Parse`, `HasMarkers`, `splitLines`, `Hunk.start/.end` from Task 1.
- Produces: `merge.Splice(content string, index int, resolved string) (string, error)`; `merge.ErrNoSuchHunk`, `merge.ErrMarkersLeft`.

- [ ] **Step 1: Write the failing test**

```go
func TestSpliceReplacesOnlyTheMarkerBlock(t *testing.T) {
	out, err := Splice(threeWay, 0, "\tfmt.Println(\"both\")\n")
	if err != nil {
		t.Fatal(err)
	}
	want := `package main

import "fmt"

func main() {
	fmt.Println("both")
}
`
	if out != want {
		t.Errorf("got:\n%q\nwant:\n%q", out, want)
	}
}

// The guarantee the whole design rests on: bytes outside the replaced block
// are untouched.
func TestSpliceLeavesTheRestByteIdentical(t *testing.T) {
	content := "head\r\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> x\ntail\twith\ttabs\n"
	out, err := Splice(content, 0, "fixed\n")
	if err != nil {
		t.Fatal(err)
	}
	if out != "head\r\nfixed\ntail\twith\ttabs\n" {
		t.Errorf("got %q", out)
	}
}

func TestSpliceSecondHunkUsesFreshIndexes(t *testing.T) {
	content := "<<<<<<< HEAD\n1\n=======\n2\n>>>>>>> x\nmiddle\n<<<<<<< HEAD\n3\n=======\n4\n>>>>>>> x\n"
	once, err := Splice(content, 0, "one\n")
	if err != nil {
		t.Fatal(err)
	}
	// After resolving hunk 0 the remaining conflict is hunk 0 again.
	twice, err := Splice(once, 0, "two\n")
	if err != nil {
		t.Fatal(err)
	}
	if twice != "one\nmiddle\ntwo\n" {
		t.Errorf("got %q", twice)
	}
}

func TestSpliceAddsTheMissingNewline(t *testing.T) {
	out, err := Splice("<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> x\nafter\n", 0, "no newline")
	if err != nil {
		t.Fatal(err)
	}
	if out != "no newline\nafter\n" {
		t.Errorf("got %q", out)
	}
}

func TestSpliceKeepsAFileWithNoTrailingNewline(t *testing.T) {
	out, err := Splice("<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> x", 0, "end")
	if err != nil {
		t.Fatal(err)
	}
	if out != "end" {
		t.Errorf("got %q", out)
	}
}

func TestSpliceEmptyResolutionDropsTheBlock(t *testing.T) {
	out, err := Splice("a\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> x\nb\n", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if out != "a\nb\n" {
		t.Errorf("got %q", out)
	}
}

func TestSpliceRejectsAResolutionWithMarkers(t *testing.T) {
	_, err := Splice(threeWay, 0, "<<<<<<< HEAD\nstill conflicted\n")
	if !errors.Is(err, ErrMarkersLeft) {
		t.Fatalf("err = %v, want ErrMarkersLeft", err)
	}
}

func TestSpliceRejectsAnOutOfRangeHunk(t *testing.T) {
	if _, err := Splice(threeWay, 3, "x\n"); !errors.Is(err, ErrNoSuchHunk) {
		t.Fatalf("err = %v, want ErrNoSuchHunk", err)
	}
}
```

Add `"errors"` to the test file's imports.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/merge/`
Expected: FAIL — `undefined: Splice`.

- [ ] **Step 3: Write the implementation**

Append to `internal/merge/conflict.go`:

```go
var (
	// ErrNoSuchHunk means the file no longer has a conflict at that index —
	// usually because it was already resolved.
	ErrNoSuchHunk = errors.New("merge: no such conflict")
	// ErrMarkersLeft means a proposed resolution still contains markers,
	// which would leave the file conflicted after staging.
	ErrMarkersLeft = errors.New("merge: the resolution still contains conflict markers")
)

// Splice replaces the marker block of hunk index with resolved and returns
// the new content. Every other byte of content is preserved, including line
// endings and a missing final newline. It re-parses content on each call, so
// resolving hunks one at a time needs no offset bookkeeping from the caller.
func Splice(content string, index int, resolved string) (string, error) {
	hunks, err := Parse(content)
	if err != nil {
		return "", err
	}
	if index < 0 || index >= len(hunks) {
		return "", fmt.Errorf("%w: asked for %d, the file has %d", ErrNoSuchHunk, index, len(hunks))
	}
	if HasMarkers(resolved) {
		return "", ErrMarkersLeft
	}
	lines := splitLines(content)
	h := hunks[index]
	tail := strings.Join(lines[h.end:], "")
	body := resolved
	// The block replaced whole lines, so the replacement ends a line too —
	// unless it sits at the end of a file that never had a final newline.
	if body != "" && !strings.HasSuffix(body, "\n") && (tail != "" || strings.HasSuffix(content, "\n")) {
		body += "\n"
	}
	return strings.Join(lines[:h.start], "") + body + tail, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/merge/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/merge/conflict.go internal/merge/conflict_test.go
git commit -m "feat(merge): splice a resolution into one conflict block"
```

---

### Task 3: Driving git — start, abort and commit a merge

**Files:**
- Create: `internal/merge/merge.go`
- Test: `internal/merge/merge_test.go`

**Interfaces:**
- Consumes: `gitcmd.Run`, `gitcmd.ReadTimeout`, `testrepo.New`.
- Produces: `merge.Outcome` with `merge.Merged`, `merge.Conflicted`, `merge.UpToDate`; `merge.Result{Outcome Outcome; Conflicts []string}`; `merge.Start(ctx, dir, branch) (Result, error)`; `merge.Abort(ctx, dir) error`; `merge.Commit(ctx, dir) error`; `merge.Unmerged(ctx, dir) ([]string, error)`; `merge.ErrInvalidRef`.

- [ ] **Step 1: Write the failing test**

```go
package merge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git-ui/internal/testrepo"
)

// conflicting builds a repo where main and feature both changed greeting.txt.
func conflicting(t *testing.T) *testrepo.Repo {
	t.Helper()
	r := testrepo.New(t)
	r.WriteFile("greeting.txt", "hello\n")
	r.Git("add", "greeting.txt")
	r.Git("commit", "-q", "-m", "add greeting")
	r.Git("switch", "-q", "-c", "feature")
	r.WriteFile("greeting.txt", "hola\n")
	r.Git("commit", "-q", "-am", "greet in spanish")
	r.Git("switch", "-q", "main")
	r.WriteFile("greeting.txt", "hi\n")
	r.Git("commit", "-q", "-am", "greet informally")
	return r
}

func TestStartCleanMerge(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("switch", "-q", "-c", "feature")
	r.Commit("on feature")
	r.Git("switch", "-q", "main")

	got, err := Start(context.Background(), r.Dir, "feature")
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != Merged {
		t.Fatalf("outcome = %v, want Merged", got.Outcome)
	}
	// --no-ff means a merge commit even when a fast-forward was possible.
	parents := r.Git("rev-list", "--parents", "-n", "1", "HEAD")
	if len(strings.Fields(parents)) != 3 {
		t.Errorf("want a merge commit with two parents, got %q", parents)
	}
}

func TestStartConflicted(t *testing.T) {
	r := conflicting(t)
	got, err := Start(context.Background(), r.Dir, "feature")
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != Conflicted {
		t.Fatalf("outcome = %v, want Conflicted", got.Outcome)
	}
	if len(got.Conflicts) != 1 || got.Conflicts[0] != "greeting.txt" {
		t.Fatalf("conflicts = %v", got.Conflicts)
	}
	// zdiff3 was requested, so the markers carry the common ancestor.
	data, err := os.ReadFile(filepath.Join(r.Dir, "greeting.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "|||||||") {
		t.Errorf("want a zdiff3 base section, got:\n%s", data)
	}
}

func TestStartAlreadyUpToDate(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("branch", "feature")

	got, err := Start(context.Background(), r.Dir, "feature")
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != UpToDate {
		t.Fatalf("outcome = %v, want UpToDate", got.Outcome)
	}
}

func TestStartRejectsAnOptionLikeRef(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	if _, err := Start(context.Background(), r.Dir, "--exec=rm -rf /"); err == nil {
		t.Fatal("want an error for a ref starting with a dash")
	}
}

func TestAbortRestoresTheBranch(t *testing.T) {
	r := conflicting(t)
	before := r.Git("rev-parse", "HEAD")
	if _, err := Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	if err := Abort(context.Background(), r.Dir); err != nil {
		t.Fatal(err)
	}
	if got := r.Git("rev-parse", "HEAD"); got != before {
		t.Errorf("HEAD = %s, want %s", got, before)
	}
	if data, _ := os.ReadFile(filepath.Join(r.Dir, "greeting.txt")); string(data) != "hi\n" {
		t.Errorf("greeting.txt = %q, want the pre-merge content", data)
	}
}

func TestCommitClosesTheMerge(t *testing.T) {
	r := conflicting(t)
	if _, err := Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	r.WriteFile("greeting.txt", "hi there\n")
	r.Git("add", "greeting.txt")
	if err := Commit(context.Background(), r.Dir); err != nil {
		t.Fatal(err)
	}
	parents := r.Git("rev-list", "--parents", "-n", "1", "HEAD")
	if len(strings.Fields(parents)) != 3 {
		t.Errorf("want two parents, got %q", parents)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/merge/`
Expected: FAIL — `undefined: Start`, `undefined: Merged`.

- [ ] **Step 3: Write the implementation**

```go
package merge

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"git-ui/internal/gitcmd"
)

// Outcome says how a merge ended.
type Outcome int

const (
	Merged Outcome = iota
	Conflicted
	UpToDate
)

// Result is what Start produces; Conflicts is set only when Conflicted.
type Result struct {
	Outcome   Outcome  `json:"outcome"`
	Conflicts []string `json:"conflicts"`
}

// ErrInvalidRef guards against a ref that git would read as an option.
var ErrInvalidRef = fmt.Errorf("merge: invalid ref")

// Start merges branch into the current one. It always creates a merge commit
// (--no-ff) so an integrated branch stays visible in the graph, and asks for
// zdiff3 markers so conflicts carry the common ancestor. The -c is per
// command and leaves the user's own config alone.
func Start(ctx context.Context, dir, branch string) (Result, error) {
	if err := checkRef(branch); err != nil {
		return Result{}, err
	}
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout,
		"-c", "merge.conflictStyle=zdiff3", "merge", "--no-ff", "--no-edit", branch)
	if err == nil {
		if strings.Contains(out, "Already up to date") {
			return Result{Outcome: UpToDate}, nil
		}
		return Result{Outcome: Merged}, nil
	}
	// git exits non-zero both for conflicts and for real failures (a dirty
	// worktree, an unknown ref). Unmerged paths are what tells them apart.
	if conflicts, listErr := Unmerged(ctx, dir); listErr == nil && len(conflicts) > 0 {
		return Result{Outcome: Conflicted, Conflicts: conflicts}, nil
	}
	return Result{}, err
}

func Abort(ctx context.Context, dir string) error {
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "merge", "--abort")
	return err
}

// Commit closes a merge with git's own generated message.
func Commit(ctx context.Context, dir string) error {
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "commit", "--no-edit")
	return err
}

// Unmerged lists the repository's conflicted paths, sorted.
func Unmerged(ctx context.Context, dir string) ([]string, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "diff", "--name-only", "--diff-filter=U", "-z")
	if err != nil {
		return nil, err
	}
	paths := []string{}
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

// checkRef rejects refs git would read as options. internal/ops has the same
// guard; a four-line check is worth repeating to keep the packages apart.
func checkRef(ref string) error {
	if ref == "" || strings.HasPrefix(ref, "-") {
		return fmt.Errorf("%w: %q", ErrInvalidRef, ref)
	}
	return nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/merge/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/merge/merge.go internal/merge/merge_test.go
git commit -m "feat(merge): start, abort and commit a no-ff merge"
```

---

### Task 4: Reading the merge state

**Files:**
- Create: `internal/merge/state.go`
- Test: `internal/merge/state_test.go`

**Interfaces:**
- Consumes: `Unmerged`, `HasMarkers`, `gitcmd.Run`, `refs.CurrentLabel`.
- Produces: `merge.State{Merging bool; From, Into string; Conflicts, Manual []string}`; `merge.Status(ctx, dir) (State, error)`.

- [ ] **Step 1: Write the failing test**

```go
package merge

import (
	"context"
	"testing"

	"git-ui/internal/testrepo"
)

func TestStatusWhenNotMerging(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")

	st, err := Status(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Merging {
		t.Fatalf("merging = true, want false: %+v", st)
	}
}

func TestStatusDuringAConflict(t *testing.T) {
	r := conflicting(t)
	if _, err := Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}

	st, err := Status(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Merging {
		t.Fatal("merging = false, want true")
	}
	if st.Into != "main" || st.From != "feature" {
		t.Errorf("merging %q into %q, want feature into main", st.From, st.Into)
	}
	if len(st.Conflicts) != 1 || st.Conflicts[0] != "greeting.txt" {
		t.Errorf("conflicts = %v", st.Conflicts)
	}
	if len(st.Manual) != 0 {
		t.Errorf("manual = %v, want none", st.Manual)
	}
}

// A binary conflict has no markers to splice, so it belongs in Manual.
func TestStatusPutsAMarkerlessConflictInManual(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile("logo.bin", "\x00\x01base\n")
	r.Git("add", "logo.bin")
	r.Git("commit", "-q", "-m", "add logo")
	r.Git("switch", "-q", "-c", "feature")
	r.WriteFile("logo.bin", "\x00\x01theirs\n")
	r.Git("commit", "-q", "-am", "their logo")
	r.Git("switch", "-q", "main")
	r.WriteFile("logo.bin", "\x00\x01ours\n")
	r.Git("commit", "-q", "-am", "our logo")

	if _, err := Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	st, err := Status(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Manual) != 1 || st.Manual[0] != "logo.bin" {
		t.Fatalf("manual = %v, conflicts = %v", st.Manual, st.Conflicts)
	}
}

func TestStatusAfterStagingTheResolution(t *testing.T) {
	r := conflicting(t)
	if _, err := Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	r.WriteFile("greeting.txt", "hi there\n")
	r.Git("add", "greeting.txt")

	st, err := Status(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Merging {
		t.Error("merging = false: the merge is still open until it is committed")
	}
	if len(st.Conflicts) != 0 {
		t.Errorf("conflicts = %v, want none left", st.Conflicts)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/merge/`
Expected: FAIL — `undefined: Status`.

- [ ] **Step 3: Write the implementation**

```go
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
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/merge/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/merge/state.go internal/merge/state_test.go
git commit -m "feat(merge): report the in-progress merge state"
```

---

### Task 5: The agent's merge tools

**Files:**
- Create: `internal/ai/mergetools/mergetools.go`
- Test: `internal/ai/mergetools/mergetools_test.go`

**Interfaces:**
- Consumes: `merge.Status`, `merge.Parse`, `merge.Splice`, `merge.HasMarkers`, `merge.ContextLines`; `ai.ToolSpec`, `ai.ToolCall`; `tools.Truncate`.
- Produces: `mergetools.Specs() []ai.ToolSpec`; `mergetools.Run(ctx, dir string, call ai.ToolCall) (result string, changed bool)`.

**Note for the implementer:** every tool returns a plain string the model reads, including errors — a refusal is how the model learns to retry, so never return a Go error out of `Run`. `changed` is true only when the working tree was modified, and the caller uses it to refresh the UI.

- [ ] **Step 1: Write the failing test**

```go
package mergetools_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git-ui/internal/ai"
	"git-ui/internal/ai/mergetools"
	"git-ui/internal/merge"
	"git-ui/internal/testrepo"
)

func conflicted(t *testing.T) *testrepo.Repo {
	t.Helper()
	r := testrepo.New(t)
	r.WriteFile("greeting.txt", "hello\n")
	r.Git("add", "greeting.txt")
	r.Git("commit", "-q", "-m", "add greeting")
	r.Git("switch", "-q", "-c", "feature")
	r.WriteFile("greeting.txt", "hola\n")
	r.Git("commit", "-q", "-am", "spanish")
	r.Git("switch", "-q", "main")
	r.WriteFile("greeting.txt", "hi\n")
	r.Git("commit", "-q", "-am", "informal")
	if _, err := merge.Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	return r
}

func call(name string, args map[string]any) ai.ToolCall {
	return ai.ToolCall{ID: "c1", Name: name, Args: args}
}

func TestSpecsCoverTheFourTools(t *testing.T) {
	names := map[string]bool{}
	for _, s := range mergetools.Specs() {
		names[s.Name] = true
	}
	for _, want := range []string{"list_conflicts", "read_conflict", "resolve_hunk", "stage_file"} {
		if !names[want] {
			t.Errorf("missing tool %q", want)
		}
	}
}

func TestListConflicts(t *testing.T) {
	r := conflicted(t)
	out, changed := mergetools.Run(context.Background(), r.Dir, call("list_conflicts", nil))
	if changed {
		t.Error("changed = true, but listing writes nothing")
	}
	if !strings.Contains(out, "greeting.txt") || !strings.Contains(out, "1 conflict") {
		t.Errorf("out = %q", out)
	}
}

func TestReadConflictShowsAllThreeSides(t *testing.T) {
	r := conflicted(t)
	out, _ := mergetools.Run(context.Background(), r.Dir, call("read_conflict", map[string]any{"path": "greeting.txt", "hunk": float64(0)}))
	for _, want := range []string{"hi", "hola", "hello"} {
		if !strings.Contains(out, want) {
			t.Errorf("out = %q, want it to contain %q", out, want)
		}
	}
}

func TestResolveHunkWritesTheFile(t *testing.T) {
	r := conflicted(t)
	out, changed := mergetools.Run(context.Background(), r.Dir,
		call("resolve_hunk", map[string]any{"path": "greeting.txt", "hunk": float64(0), "resolved": "hi / hola\n"}))
	if !changed {
		t.Error("changed = false, want true")
	}
	if !strings.Contains(out, "0 conflict") {
		t.Errorf("out = %q, want it to report none left", out)
	}
	data, err := os.ReadFile(filepath.Join(r.Dir, "greeting.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hi / hola\n" {
		t.Errorf("file = %q", data)
	}
}

func TestResolveHunkRefusesMarkers(t *testing.T) {
	r := conflicted(t)
	out, changed := mergetools.Run(context.Background(), r.Dir,
		call("resolve_hunk", map[string]any{"path": "greeting.txt", "hunk": float64(0), "resolved": "<<<<<<< HEAD\nhi\n"}))
	if changed {
		t.Error("changed = true, but nothing should have been written")
	}
	if !strings.Contains(strings.ToLower(out), "marker") {
		t.Errorf("out = %q, want the refusal to mention markers", out)
	}
}

// The agent must not reach outside the repository, nor touch files that
// aren't part of this merge.
func TestToolsRefuseAPathThatIsNotConflicted(t *testing.T) {
	r := conflicted(t)
	r.WriteFile("untouched.txt", "keep me\n")
	for _, path := range []string{"untouched.txt", "../escape.txt", "/etc/hosts"} {
		out, changed := mergetools.Run(context.Background(), r.Dir,
			call("resolve_hunk", map[string]any{"path": path, "hunk": float64(0), "resolved": "x\n"}))
		if changed {
			t.Fatalf("%s: changed = true", path)
		}
		if !strings.Contains(out, "not a conflicted file") {
			t.Errorf("%s: out = %q", path, out)
		}
	}
	if data, _ := os.ReadFile(filepath.Join(r.Dir, "untouched.txt")); string(data) != "keep me\n" {
		t.Errorf("untouched.txt was modified: %q", data)
	}
}

func TestStageFileRefusesWhileMarkersRemain(t *testing.T) {
	r := conflicted(t)
	out, _ := mergetools.Run(context.Background(), r.Dir, call("stage_file", map[string]any{"path": "greeting.txt"}))
	if !strings.Contains(strings.ToLower(out), "marker") {
		t.Errorf("out = %q", out)
	}
}

func TestStageFileAfterResolving(t *testing.T) {
	r := conflicted(t)
	mergetools.Run(context.Background(), r.Dir,
		call("resolve_hunk", map[string]any{"path": "greeting.txt", "hunk": float64(0), "resolved": "hi / hola\n"}))
	out, changed := mergetools.Run(context.Background(), r.Dir, call("stage_file", map[string]any{"path": "greeting.txt"}))
	if !changed {
		t.Error("changed = false, want true")
	}
	if !strings.Contains(out, "Staged") {
		t.Errorf("out = %q", out)
	}
	st, err := merge.Status(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Conflicts) != 0 {
		t.Errorf("conflicts = %v, want none", st.Conflicts)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/ai/mergetools/`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Write the implementation**

```go
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

// open validates the requested path against the merge's conflicted files and
// returns its hunks. A non-empty msg means the call was refused and the
// caller must return it unchanged.
func open(ctx context.Context, dir string, args map[string]any) (path string, hunks []merge.Hunk, msg string) {
	path, _ = args["path"].(string)
	st, err := merge.Status(ctx, dir)
	if err != nil {
		return "", nil, "Could not read the merge state: " + err.Error()
	}
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
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/ai/mergetools/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ai/mergetools/
git commit -m "feat(ai): hunk-level merge tools for the conflict agent"
```

---

### Task 6: A per-run step limit for the agent

**Files:**
- Modify: `internal/ai/agent/agent.go` (the `Run` struct and the loop in `Execute`)
- Test: `internal/ai/agent/agent_test.go`

**Interfaces:**
- Consumes: the existing `agent.Run` and `agent.Execute`.
- Produces: `agent.Run.MaxSteps int` — 0 keeps the default `agent.MaxSteps` (8).

- [ ] **Step 1: Write the failing test**

Read the existing `agent_test.go` first and reuse its fake provider rather than writing a new one. Add:

```go
func TestExecuteRespectsAPerRunStepLimit(t *testing.T) {
	// A provider that always asks for another tool call, so only the step
	// limit can end the run.
	calls := 0
	provider := &fakeProvider{reply: func(ai.Request) []ai.Chunk {
		calls++
		return []ai.Chunk{{ToolCalls: []ai.ToolCall{{ID: "c", Name: "list_refs", Args: map[string]any{}}}}, {Done: true}}
	}}
	run := agent.Run{
		RepoID: "r1", RunID: "run1", Provider: provider, Model: "m", MaxSteps: 2,
		Tools:   []ai.ToolSpec{{Name: "list_refs"}},
		RunTool: func(context.Context, ai.ToolCall) string { return "ok" },
		Emit:    func(string, any) {},
	}
	if _, err := agent.Execute(context.Background(), run, []ai.Message{{Role: ai.RoleUser, Content: "go"}}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("provider called %d times, want 2 (MaxSteps)", calls)
	}
}

func TestExecuteDefaultsToTheGlobalStepLimit(t *testing.T) {
	calls := 0
	provider := &fakeProvider{reply: func(ai.Request) []ai.Chunk {
		calls++
		return []ai.Chunk{{ToolCalls: []ai.ToolCall{{ID: "c", Name: "list_refs", Args: map[string]any{}}}}, {Done: true}}
	}}
	run := agent.Run{
		RepoID: "r1", RunID: "run1", Provider: provider, Model: "m", // MaxSteps left at 0
		Tools:   []ai.ToolSpec{{Name: "list_refs"}},
		RunTool: func(context.Context, ai.ToolCall) string { return "ok" },
		Emit:    func(string, any) {},
	}
	if _, err := agent.Execute(context.Background(), run, []ai.Message{{Role: ai.RoleUser, Content: "go"}}); err != nil {
		t.Fatal(err)
	}
	if calls != agent.MaxSteps {
		t.Fatalf("provider called %d times, want the default %d", calls, agent.MaxSteps)
	}
}
```

If the existing fake provider has a different shape, adapt these two tests to it — do not change the existing fake.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/ai/agent/`
Expected: FAIL — `unknown field MaxSteps in struct literal`.

- [ ] **Step 3: Write the implementation**

In the `Run` struct, after `Tools`:

```go
	// MaxSteps caps the model/tool rounds for this run; 0 uses MaxSteps.
	// Resolving a merge takes many more rounds than answering a question.
	MaxSteps int
```

At the top of `Execute`, replace the loop header:

```go
	steps := r.MaxSteps
	if steps <= 0 {
		steps = MaxSteps
	}
	msgs := append([]ai.Message(nil), history...)
	for step := 0; step < steps; step++ {
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/ai/agent/ -v`
Expected: PASS, including every pre-existing test.

- [ ] **Step 5: Commit**

```bash
git add internal/ai/agent/
git commit -m "feat(ai): let a run set its own step limit"
```

---

### Task 7: The resolve-conflicts prompt

**Files:**
- Create: `internal/ai/prompts/defaults/resolve-conflicts.md`
- Modify: `internal/ai/prompts/prompts.go` (add the name constant)
- Test: `internal/ai/prompts/prompts_test.go`

**Interfaces:**
- Consumes: `prompts.Names()`, `prompts.Store.Get`.
- Produces: `prompts.ResolveConflicts = "resolve-conflicts"`.

- [ ] **Step 1: Write the failing test**

```go
func TestResolveConflictsPromptIsAvailable(t *testing.T) {
	found := false
	for _, name := range prompts.Names() {
		if name == prompts.ResolveConflicts {
			found = true
		}
	}
	if !found {
		t.Fatalf("Names() = %v, want it to include %q", prompts.Names(), prompts.ResolveConflicts)
	}

	s := prompts.New(t.TempDir())
	text, err := s.Get(prompts.ResolveConflicts, prompts.Vars{Repo: "acme", Branch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "acme") {
		t.Errorf("prompt did not expand {{repo}}: %q", text)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/ai/prompts/`
Expected: FAIL — `undefined: prompts.ResolveConflicts`.

- [ ] **Step 3: Write the implementation**

In `prompts.go`, beside the existing names:

```go
	ResolveConflicts = "resolve-conflicts"
```

Create `internal/ai/prompts/defaults/resolve-conflicts.md`:

```markdown
You are resolving the merge conflicts in the git repository {{repo}}, on
branch {{branch}}. Today is {{date}}.

Work on your own until every conflict you can settle is settled. Do not ask
the user questions — they are watching and will review your work before
anything is committed.

How to work:

1. Call `list_conflicts` to see what is left.
2. For each file, call `read_conflict` for one region at a time.
3. Decide what the code should be, then call `resolve_hunk` with just that
   region's final content.
4. When a file has no conflicts left, call `stage_file`.
5. Repeat until `list_conflicts` reports nothing you can resolve.

How to decide:

- The common ancestor tells you what each side changed. Prefer a resolution
  that keeps the intent of both changes over picking a side wholesale.
- Only pick one side when the two changes genuinely contradict each other.
- Write nothing that was in neither side. Never invent a function, an import
  or a value to make the two fit.
- If you cannot tell which resolution is correct, leave that region alone and
  say so in your final message. An unresolved conflict is a normal outcome;
  a wrong resolution is not.
- Files reported as having no markers are for the user to settle. Skip them.

You may use the read-only tools (`file_history`, `show_commit`,
`diff_commit_file`) to see why each side made its change.

Finish with a short report: what you resolved and why, then what you left
alone and what the user needs to decide. Keep it to a few lines per file.
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/ai/prompts/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ai/prompts/
git commit -m "feat(ai): prompt for the merge conflict agent"
```

---

### Task 8: Merge operations on the app API

**Files:**
- Create: `internal/app/merge.go`
- Test: `internal/app/merge_test.go`

**Interfaces:**
- Consumes: `App.write`, `App.dir`, `App.store`, `merge.*` from Tasks 3–4.
- Produces: `App.MergeBranch(id, branch string) (merge.Result, error)`; `App.GetMergeState(id string) (merge.State, error)`; `App.AbortMerge(id string) error`; `App.CommitMerge(id string) error`; `App.GetConflictFile(id, path string) (ConflictFile, error)`; `app.ConflictFile{Path string; Resolved bool; Text string}`.

- [ ] **Step 1: Write the failing test**

```go
package app_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git-ui/internal/app"
	"git-ui/internal/merge"
	"git-ui/internal/testrepo"
)

// conflictingApp returns an App with one repo mid-conflict, and the repo id.
func conflictingApp(t *testing.T) (*app.App, *testrepo.Repo, string) {
	t.Helper()
	r := testrepo.New(t)
	r.WriteFile("greeting.txt", "hello\n")
	r.Git("add", "greeting.txt")
	r.Git("commit", "-q", "-m", "add greeting")
	r.Git("switch", "-q", "-c", "feature")
	r.WriteFile("greeting.txt", "hola\n")
	r.Git("commit", "-q", "-am", "spanish")
	r.Git("switch", "-q", "main")
	r.WriteFile("greeting.txt", "hi\n")
	r.Git("commit", "-q", "-am", "informal")

	a, id := newAppWithRepo(t, r.Dir) // existing helper in app_test.go
	return a, r, id
}

func TestMergeBranchReportsConflicts(t *testing.T) {
	a, _, id := conflictingApp(t)
	got, err := a.MergeBranch(id, "feature")
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != merge.Conflicted || len(got.Conflicts) != 1 {
		t.Fatalf("result = %+v", got)
	}
}

func TestGetMergeStateDuringAMerge(t *testing.T) {
	a, _, id := conflictingApp(t)
	if _, err := a.MergeBranch(id, "feature"); err != nil {
		t.Fatal(err)
	}
	st, err := a.GetMergeState(id)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Merging || st.From != "feature" || st.Into != "main" {
		t.Fatalf("state = %+v", st)
	}
}

func TestGetConflictFileShowsMarkersThenTheDiff(t *testing.T) {
	a, r, id := conflictingApp(t)
	if _, err := a.MergeBranch(id, "feature"); err != nil {
		t.Fatal(err)
	}

	f, err := a.GetConflictFile(id, "greeting.txt")
	if err != nil {
		t.Fatal(err)
	}
	if f.Resolved || !strings.Contains(f.Text, "<<<<<<<") {
		t.Fatalf("while conflicted: resolved = %v, text = %q", f.Resolved, f.Text)
	}

	r.WriteFile("greeting.txt", "hi / hola\n")
	r.Git("add", "greeting.txt")
	f, err = a.GetConflictFile(id, "greeting.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !f.Resolved || !strings.Contains(f.Text, "+hi / hola") {
		t.Fatalf("after staging: resolved = %v, text = %q", f.Resolved, f.Text)
	}
}

func TestGetConflictFileRefusesAPathOutsideTheMerge(t *testing.T) {
	a, _, id := conflictingApp(t)
	if _, err := a.MergeBranch(id, "feature"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.GetConflictFile(id, "../escape.txt"); err == nil {
		t.Fatal("want an error for a path that is not part of the merge")
	}
}

func TestAbortMergeRestoresTheRepo(t *testing.T) {
	a, r, id := conflictingApp(t)
	before := r.Git("rev-parse", "HEAD")
	if _, err := a.MergeBranch(id, "feature"); err != nil {
		t.Fatal(err)
	}
	if err := a.AbortMerge(id); err != nil {
		t.Fatal(err)
	}
	if got := r.Git("rev-parse", "HEAD"); got != before {
		t.Errorf("HEAD moved: %s != %s", got, before)
	}
}

func TestCommitMergeCreatesTheMergeCommit(t *testing.T) {
	a, r, id := conflictingApp(t)
	if _, err := a.MergeBranch(id, "feature"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.Dir, "greeting.txt"), []byte("hi / hola\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.Git("add", "greeting.txt")
	if err := a.CommitMerge(id); err != nil {
		t.Fatal(err)
	}
	if n := len(strings.Fields(r.Git("rev-list", "--parents", "-n", "1", "HEAD"))); n != 3 {
		t.Errorf("want a merge commit with two parents, got %d fields", n)
	}
	st, err := a.GetMergeState(id)
	if err != nil || st.Merging {
		t.Errorf("state = %+v, err = %v", st, err)
	}
}
```

Check `internal/app/app_test.go` for the existing helper that builds an `App` around a repo directory and use its real name; if it has none, add one named `newAppWithRepo(t *testing.T, dir string) (*app.App, string)` in this new test file.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/app/`
Expected: FAIL — `a.MergeBranch undefined`.

- [ ] **Step 3: Write the implementation**

```go
package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"git-ui/internal/gitcmd"
	"git-ui/internal/merge"
)

// ConflictFile is one file of a merge as the UI shows it: the raw content
// with markers while it is conflicted, and the staged diff once it is not.
type ConflictFile struct {
	Path     string `json:"path"`
	Resolved bool   `json:"resolved"`
	Text     string `json:"text"`
}

// MergeBranch merges branch into the repository's current branch. A
// conflicted merge is left in place for the user or the agent to resolve.
func (a *App) MergeBranch(id, branch string) (merge.Result, error) {
	var result merge.Result
	err := a.write(id, func(ctx context.Context, dir string) error {
		var err error
		result, err = merge.Start(ctx, dir, branch)
		return err
	})
	return result, err
}

func (a *App) GetMergeState(id string) (merge.State, error) {
	dir, err := a.dir(id)
	if err != nil {
		return merge.State{}, err
	}
	return merge.Status(a.ctx, dir)
}

func (a *App) AbortMerge(id string) error {
	return a.write(id, func(ctx context.Context, dir string) error { return merge.Abort(ctx, dir) })
}

func (a *App) CommitMerge(id string) error {
	return a.write(id, func(ctx context.Context, dir string) error { return merge.Commit(ctx, dir) })
}

// GetConflictFile returns what the merge view shows for one file. Only files
// belonging to the merge in progress can be read, so a path from the
// renderer can't be used to read the disk.
func (a *App) GetConflictFile(id, path string) (ConflictFile, error) {
	dir, err := a.dir(id)
	if err != nil {
		return ConflictFile{}, err
	}
	st, err := merge.Status(a.ctx, dir)
	if err != nil {
		return ConflictFile{}, err
	}
	conflicted, known := false, false
	for _, p := range st.Conflicts {
		if p == path {
			conflicted, known = true, true
		}
	}
	for _, p := range st.Manual {
		if p == path {
			known = true
		}
	}
	if !known {
		// Not unmerged any more: it was resolved and staged during this
		// merge, so show the staged diff. Anything git doesn't know is
		// refused below.
		out, err := gitcmd.Run(a.ctx, dir, gitcmd.ReadTimeout, "diff", "--cached", "--", path)
		if err != nil {
			return ConflictFile{}, err
		}
		if out == "" {
			return ConflictFile{}, fmt.Errorf("%q is not part of this merge", path)
		}
		return ConflictFile{Path: path, Resolved: true, Text: out}, nil
	}
	if !conflicted {
		return ConflictFile{Path: path, Text: "This file has no conflict markers to edit here. Resolve it in your editor."}, nil
	}
	data, err := os.ReadFile(filepath.Join(dir, path))
	if err != nil {
		return ConflictFile{}, err
	}
	return ConflictFile{Path: path, Text: string(data)}, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/app/ -run Merge -v && go test ./internal/app/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/app/merge.go internal/app/merge_test.go
git commit -m "feat(app): merge, abort, commit and read a conflicted file"
```

---

### Task 9: Running the conflict agent

**Files:**
- Modify: `internal/app/merge.go`
- Test: `internal/app/merge_test.go`

**Interfaces:**
- Consumes: `a.ai` (`aiState.runs`, `deps.Chats`, `deps.Prompts`, `emit`), `agent.Execute`, `mergetools.*`, `tools.*`, `prompts.ResolveConflicts`, `merge.Status`.
- Produces: `App.ResolveConflicts(repoID, runID string) error`; the `merge:changed` event with `app.MergeChangedEvent{RepoID string}`.

**Note for the implementer:** `SendChat` in `internal/app/ai.go` is the reference implementation for taking the chat slot, saving history and emitting start/done/error. Read it completely before writing this, and follow the same order — in particular, release the slot before emitting done or error.

- [ ] **Step 1: Write the failing test**

```go
func TestResolveConflictsStreamsIntoTheConversation(t *testing.T) {
	a, _, id := conflictingApp(t)
	if _, err := a.MergeBranch(id, "feature"); err != nil {
		t.Fatal(err)
	}
	// Reuse the test provider from ai_test.go: it answers with a fixed
	// assistant message and no tool calls.
	events := captureEvents(a) // existing helper in ai_test.go

	if err := a.ResolveConflicts(id, "run1"); err != nil {
		t.Fatal(err)
	}
	waitForEvent(t, events, "chat:done")

	messages, err := a.GetChat(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) < 2 || messages[0].Role != ai.RoleUser {
		t.Fatalf("messages = %+v", messages)
	}
	if !strings.Contains(messages[0].Content, "feature") || !strings.Contains(messages[0].Content, "main") {
		t.Errorf("the question should name both branches: %q", messages[0].Content)
	}
}

func TestResolveConflictsRefusesWhenNotMerging(t *testing.T) {
	a, _, id := conflictingApp(t)
	err := a.ResolveConflicts(id, "run1")
	if err == nil {
		t.Fatal("want an error when the repository is not merging")
	}
}

func TestResolveConflictsIsBusyWhileChatting(t *testing.T) {
	a, _, id := conflictingApp(t)
	if _, err := a.MergeBranch(id, "feature"); err != nil {
		t.Fatal(err)
	}
	if err := a.SendChat(id, "hola", "run1"); err != nil {
		t.Fatal(err)
	}
	if err := a.ResolveConflicts(id, "run2"); !errors.Is(err, app.ErrChatBusy) {
		t.Fatalf("err = %v, want ErrChatBusy", err)
	}
}
```

Adapt the helper names (`captureEvents`, `waitForEvent`, the fake provider) to what `internal/app/ai_test.go` already defines — read it first and reuse, don't duplicate.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/app/ -run ResolveConflicts`
Expected: FAIL — `a.ResolveConflicts undefined`.

- [ ] **Step 3: Write the implementation**

Add to `internal/app/merge.go`:

```go
// EventMergeChanged tells the frontend the working tree moved during a merge,
// so the merge view can refresh while the agent works.
const EventMergeChanged = "merge:changed"

type MergeChangedEvent struct {
	RepoID string `json:"repoID"`
}

// ResolveConflicts runs the conflict agent over the merge in progress. It
// shares the repository's chat slot with SendChat and ExplainInChat, so a
// resolve run and a chat can never interleave, and it stops before
// committing: staging is as far as the agent goes.
func (a *App) ResolveConflicts(repoID, runID string) error {
	if a.ai == nil {
		return ErrAIDisabled
	}
	if runID == "" {
		return errors.New("run id is required")
	}
	repo, ok := a.store.Get(repoID)
	if !ok {
		return fmt.Errorf("unknown repository %q", repoID)
	}
	st, err := merge.Status(a.ctx, repo.Path)
	if err != nil {
		return err
	}
	if !st.Merging {
		return errors.New("this repository is not merging")
	}
	cfg, err := a.aiSettings()
	if err != nil {
		return err
	}

	a.ai.mu.Lock()
	if _, busy := a.ai.runs[repoID]; busy {
		a.ai.mu.Unlock()
		return ErrChatBusy
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.ai.runs[repoID] = cancel
	a.ai.mu.Unlock()
	finish := func() {
		a.ai.mu.Lock()
		delete(a.ai.runs, repoID)
		a.ai.mu.Unlock()
		cancel()
	}

	text := fmt.Sprintf("Resolve the conflicts from merging %s into %s", st.From, st.Into)
	history, err := a.ai.deps.Chats.Load(repoID)
	if err == nil {
		history = append(history, ai.Message{Role: ai.RoleUser, Content: text})
		err = a.ai.deps.Chats.Save(repoID, history)
	}
	var system string
	if err == nil {
		system, err = a.ai.deps.Prompts.Get(prompts.ResolveConflicts, prompts.Vars{
			Repo: repo.Name, Path: repo.Path, Branch: st.Into, Date: time.Now().Format("2006-01-02"),
		})
	}
	if err != nil {
		finish()
		return err
	}

	a.emit(agent.EventStart, agent.StartEvent{RepoID: repoID, RunID: runID, Text: text})

	go func() {
		run := agent.Run{
			RepoID: repoID, RunID: runID,
			Provider: ollama.New(cfg.OllamaURL), Model: cfg.ChatModel, System: system,
			Tools:    append(mergetools.Specs(), tools.Specs()...),
			MaxSteps: MergeMaxSteps,
			RunTool: func(ctx context.Context, call ai.ToolCall) string {
				if isMergeTool(call.Name) {
					out, changed := mergetools.Run(ctx, repo.Path, call)
					if changed {
						a.emit(EventMergeChanged, MergeChangedEvent{RepoID: repoID})
					}
					return out
				}
				return tools.Run(ctx, repo.Path, call)
			},
			Emit: a.emit,
		}
		updated, runErr := agent.Execute(ctx, run, history)
		saveErr := a.ai.deps.Chats.Save(repoID, updated)
		finish()
		a.emit(EventMergeChanged, MergeChangedEvent{RepoID: repoID})
		switch {
		case runErr != nil && !errors.Is(runErr, context.Canceled):
			a.emit(agent.EventError, agent.ErrorEvent{RepoID: repoID, RunID: runID, Message: runErr.Error(), Code: chatErrorCode(runErr)})
		case saveErr != nil:
			a.emit(agent.EventError, agent.ErrorEvent{RepoID: repoID, RunID: runID, Message: saveErr.Error(), Code: "other"})
		default:
			a.emit(agent.EventDone, agent.DoneEvent{RepoID: repoID, RunID: runID})
		}
	}()
	return nil
}

// MergeMaxSteps is generous because each conflicted file costs several tool
// rounds; history trimming keeps the context bounded regardless.
const MergeMaxSteps = 30

func isMergeTool(name string) bool {
	for _, spec := range mergetools.Specs() {
		if spec.Name == name {
			return true
		}
	}
	return false
}
```

Add the needed imports to `internal/app/merge.go`: `errors`, `time`, `git-ui/internal/ai`, `git-ui/internal/ai/agent`, `git-ui/internal/ai/mergetools`, `git-ui/internal/ai/ollama`, `git-ui/internal/ai/prompts`, `git-ui/internal/ai/tools`.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/app/ -v && go vet ./...`
Expected: PASS, vet clean.

- [ ] **Step 5: Commit**

```bash
git add internal/app/merge.go internal/app/merge_test.go
git commit -m "feat(ai): run the conflict agent from the app API"
```

---

### Task 10: Frontend plumbing — types, api, store and actions

**Files:**
- Modify: `frontend/src/lib/types.ts`, `frontend/src/lib/api.ts`, `frontend/src/lib/stores.ts`, `frontend/src/lib/actions.ts`
- Test: `frontend/src/lib/merge.test.ts` (create), `frontend/src/lib/merge.ts` (create)

**Interfaces:**
- Consumes: the Wails bindings regenerated by the build (`MergeBranch`, `GetMergeState`, `AbortMerge`, `CommitMerge`, `ResolveConflicts`, `GetConflictFile`).
- Produces: `MergeState`, `MergeResult`, `ConflictFile` types; `api.mergeBranch|getMergeState|abortMerge|commitMerge|resolveConflicts|getConflictFile`; the `mergeState` store and `loadMergeState()`; `mergeFiles(state, started)` in `lib/merge.ts`; `mergeBranch|abortMerge|commitMerge|resolveConflicts` actions.

**Note for the implementer:** run `make build` (or `wails dev`) once after Task 9 so `frontend/wailsjs/go/app/App.d.ts` gains the new methods, otherwise `npm run check` fails on the imports. The Makefile restores `frontend/wailsjs/runtime` afterwards — leave that alone and never commit changes under `frontend/wailsjs/runtime`.

- [ ] **Step 1: Write the failing test**

Create `frontend/src/lib/merge.test.ts`:

```ts
import { describe, expect, it } from 'vitest'
import { mergeFiles } from './merge'
import type { MergeState } from './types'

const state = (over: Partial<MergeState> = {}): MergeState => ({
  merging: true,
  from: 'feature',
  into: 'main',
  conflicts: [],
  manual: [],
  ...over,
})

describe('mergeFiles', () => {
  it('splits the files the merge started with into resolved and pending', () => {
    const rows = mergeFiles(state({ conflicts: ['b.ts'] }), ['a.ts', 'b.ts'])
    expect(rows).toEqual([
      { path: 'b.ts', status: 'conflict' },
      { path: 'a.ts', status: 'resolved' },
    ])
  })

  it('lists files with no markers as manual, after the conflicts', () => {
    const rows = mergeFiles(state({ conflicts: ['b.ts'], manual: ['logo.png'] }), ['b.ts', 'logo.png'])
    expect(rows).toEqual([
      { path: 'b.ts', status: 'conflict' },
      { path: 'logo.png', status: 'manual' },
    ])
  })

  // After a restart there is no record of how the merge began, so only what
  // git still reports can be shown.
  it('works without the starting list', () => {
    expect(mergeFiles(state({ conflicts: ['b.ts'] }), [])).toEqual([{ path: 'b.ts', status: 'conflict' }])
  })

  it('is empty when nothing is merging', () => {
    expect(mergeFiles(state({ merging: false }), ['a.ts'])).toEqual([])
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd frontend && npx vitest run src/lib/merge.test.ts`
Expected: FAIL — cannot resolve `./merge`.

- [ ] **Step 3: Write the implementation**

Create `frontend/src/lib/merge.ts`:

```ts
import type { MergeState } from './types'

export type MergeFileStatus = 'conflict' | 'manual' | 'resolved'

export interface MergeFile {
  path: string
  status: MergeFileStatus
}

/**
 * mergeFiles lists the merge's files for the panel: what is still conflicted,
 * what needs a human, and what has already been resolved. `started` is the
 * conflict list from when the merge began, which is the only way to know a
 * file was resolved during it; after a restart it is empty and only the
 * outstanding files are shown.
 */
export function mergeFiles(state: MergeState, started: string[]): MergeFile[] {
  if (!state.merging) return []
  const rows: MergeFile[] = state.conflicts.map((path) => ({ path, status: 'conflict' as const }))
  rows.push(...state.manual.map((path) => ({ path, status: 'manual' as const })))
  const outstanding = new Set([...state.conflicts, ...state.manual])
  rows.push(...started.filter((path) => !outstanding.has(path)).map((path) => ({ path, status: 'resolved' as const })))
  return rows
}
```

Add to `frontend/src/lib/types.ts`:

```ts
export interface MergeState {
  merging: boolean
  from: string
  into: string
  conflicts: string[]
  manual: string[]
}

export const MERGED = 0
export const CONFLICTED = 1
export const UP_TO_DATE = 2

export interface MergeResult {
  outcome: number
  conflicts: string[]
}

export interface ConflictFile {
  path: string
  resolved: boolean
  text: string
}

export interface MergeChangedEvent { repoID: string }
```

Add to `frontend/src/lib/api.ts`, after the ref operations:

```ts
  mergeBranch: (id: string, branch: string) => call<MergeResult>(Go.MergeBranch(id, branch)),
  getMergeState: (id: string) => call<MergeState>(Go.GetMergeState(id)),
  abortMerge: (id: string) => call<void>(Go.AbortMerge(id)),
  commitMerge: (id: string) => call<void>(Go.CommitMerge(id)),
  resolveConflicts: (repoID: string, runID: string) => call<void>(Go.ResolveConflicts(repoID, runID)),
  getConflictFile: (id: string, path: string) => call<ConflictFile>(Go.GetConflictFile(id, path)),
```

and extend its import of `./types` with `ConflictFile, MergeResult, MergeState`.

Add to `frontend/src/lib/stores.ts`:

```ts
export const mergeState = writable<MergeState | null>(null)
/** Conflicts the current merge started with, so resolved files stay listed. */
export const mergeStarted = writable<string[]>([])

export async function loadMergeState() {
  const repo = get(selectedRepo)
  if (!repo || repo.missing) {
    mergeState.set(null)
    return
  }
  try {
    const state = await api.getMergeState(repo.id)
    mergeState.set(state)
    if (!state.merging) mergeStarted.set([])
  } catch {
    mergeState.set(null)
  }
}
```

Call it from `refreshRepo` and `selectRepo`:

```ts
export async function refreshRepo() {
  await loadRepos()
  await loadRefs()
  await loadMergeState()
  logVersion.update((v) => v + 1)
}
```

and in `selectRepo`, after `loadRefs()`, add `loadMergeState()`. Import `MergeState` in the type import.

Add to `frontend/src/lib/actions.ts`:

```ts
export async function mergeBranch(id: string, branch: Branch, into: string) {
  const label = branch.remote ? `${branch.remote}/${branch.name}` : branch.name
  const ok = await confirmDialog({
    title: 'Merge branch',
    message: `Merge ${label} into ${into}? A merge commit is always created.`,
    confirmLabel: 'Merge',
  })
  if (!ok) return
  busy.set('Merging…')
  try {
    const result = await api.mergeBranch(id, label)
    if (result.outcome === UP_TO_DATE) toast(`${into} is already up to date with ${label}.`, 'info')
    else if (result.outcome === CONFLICTED) mergeStarted.set(result.conflicts)
  } catch (e) {
    toast(errorMessage(e), 'error')
  } finally {
    busy.set('')
    await refreshRepo()
  }
}

export async function abortMerge(id: string) {
  const ok = await confirmDialog({
    title: 'Abort merge',
    message: 'Throw away every resolution from this merge and go back to where the branch was?',
    confirmLabel: 'Abort merge',
    danger: true,
  })
  if (ok) await run('Aborting merge…', () => api.abortMerge(id))
}

export const commitMerge = (id: string) => run('Committing merge…', () => api.commitMerge(id))

export async function resolveConflicts(id: string) {
  chatOpen.set(true)
  try {
    await api.resolveConflicts(id, crypto.randomUUID())
  } catch (e) {
    toast(errorMessage(e), 'error')
  }
}
```

Extend its imports: `chatOpen, mergeStarted` from `./stores`, and `CONFLICTED, UP_TO_DATE` from `./types`. Check `toast`'s allowed kinds in `lib/ui.ts` and use an existing one if `'info'` is not among them.

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd frontend && npx vitest run && npm run check`
Expected: PASS; check reports 0 errors.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib frontend/wailsjs/go
git commit -m "feat(ui): merge state, api and actions"
```

---

### Task 11: The merge in the UI

**Files:**
- Create: `frontend/src/components/MergeView.svelte`
- Modify: `frontend/src/components/RepoRefs.svelte`, `frontend/src/components/LogView.svelte`, `frontend/src/components/Sidebar.svelte`
- Test: manual verification (steps below), plus the existing suites staying green.

**Interfaces:**
- Consumes: `mergeFiles`, `mergeState`, `mergeStarted`, `loadMergeState`, the four actions from Task 10, `api.getConflictFile`, `EventMergeChanged` (`'merge:changed'`).
- Produces: no new exports.

**Note for the implementer:** `CommitDetails.svelte` is the model for `MergeView.svelte` — same two-column grid, same file-row and diff-line classes. Copy its structure and styles rather than inventing new ones, and reuse its `lineClass` for the staged diffs.

- [ ] **Step 1: Add the branch context-menu item**

In `RepoRefs.svelte`, import `mergeBranch` from `../lib/actions` and `mergeState` from `../lib/stores`, then add to `branchMenu`, right after "Check out":

```svelte
      {
        label: `Merge ${branchLabel(b)} into ${$refs?.head ?? ''}`,
        action: () => mergeBranch(repoId, b, $refs?.head ?? ''),
        disabled: b.current || !!$busy || !!$refs?.detached || !!$mergeState?.merging,
      },
```

- [ ] **Step 2: Write MergeView.svelte**

```svelte
<script lang="ts">
  import { EventsOn } from '../../wailsjs/runtime/runtime'
  import Icon from './Icon.svelte'
  import { abortMerge, commitMerge, resolveConflicts } from '../lib/actions'
  import { mergeFiles, type MergeFile } from '../lib/merge'
  import { busy, loadMergeState, mergeStarted, mergeState } from '../lib/stores'
  import { errorMessage } from '../lib/ui'
  import { onDestroy } from 'svelte'

  export let repoId: string

  let selected = ''
  let text = ''
  let resolved = false
  let error = ''
  let request = 0

  const off = EventsOn('merge:changed', () => loadMergeState())
  onDestroy(off)

  $: files = mergeFiles($mergeState ?? { merging: false, from: '', into: '', conflicts: [], manual: [] }, $mergeStarted)
  $: pending = ($mergeState?.conflicts.length ?? 0) + ($mergeState?.manual.length ?? 0)
  // Keep a selection valid as the agent resolves files underneath it.
  $: if (files.length && !files.some((f) => f.path === selected)) open(files[0])

  async function open(file: MergeFile) {
    const current = ++request
    selected = file.path
    text = ''
    error = ''
    try {
      const f = await api.getConflictFile(repoId, file.path)
      if (current !== request) return
      text = f.text
      resolved = f.resolved
    } catch (e) {
      if (current === request) error = errorMessage(e)
    }
  }

  function lineClass(line: string): string {
    if (!resolved) {
      if (line.startsWith('<<<<<<<') || line.startsWith('>>>>>>>') || line.startsWith('|||||||') || line.startsWith('=======')) return 'marker'
      return ''
    }
    if (line.startsWith('+++') || line.startsWith('---') || line.startsWith('diff ') || line.startsWith('index ')) return 'meta'
    if (line.startsWith('@@')) return 'hunk'
    if (line.startsWith('+')) return 'add'
    if (line.startsWith('-')) return 'del'
    return ''
  }
</script>

<script context="module" lang="ts">
  import { api } from '../lib/api'
</script>

<div class="merge">
  <header>
    <span class="title">Merging <strong>{$mergeState?.from}</strong> into <strong>{$mergeState?.into}</strong></span>
    <span class="count">{pending} left</span>
    <span class="spacer"></span>
    <button class="btn" disabled={!!$busy || pending === 0} on:click={() => resolveConflicts(repoId)}>
      <Icon name="sparkle" size={14} /> Resolve with AI
    </button>
    <button class="btn" disabled={!!$busy} on:click={() => abortMerge(repoId)}>Abort merge</button>
    <button class="btn primary" disabled={!!$busy || pending > 0} on:click={() => commitMerge(repoId)}>Commit merge</button>
  </header>

  <div class="body">
    <div class="files">
      {#each files as f (f.path)}
        <button class="row-item file" class:active={selected === f.path} on:click={() => open(f)}>
          <span class="status s-{f.status}">
            {#if f.status === 'resolved'}<Icon name="check" size={12} />{:else if f.status === 'manual'}!{:else}·{/if}
          </span>
          <span class="ellipsis">{f.path}</span>
        </button>
      {:else}
        <div class="none">Nothing left to resolve.</div>
      {/each}
    </div>
    <div class="content mono">
      {#if error}
        <div class="error">{error}</div>
      {:else}
        {#each text.split('\n') as line}
          <div class="line {lineClass(line)}">{line || ' '}</div>
        {/each}
      {/if}
    </div>
  </div>
</div>

<style>
  .merge { display: flex; flex-direction: column; height: 100%; }
  header { display: flex; align-items: center; gap: 8px; padding: 6px 10px; border-bottom: 1px solid var(--border); flex: none; }
  .title { font-size: 13px; }
  .count { font-size: 12px; color: var(--muted); }
  .spacer { flex: 1; }
  .body { display: grid; grid-template-columns: minmax(260px, 36%) 1fr; flex: 1; min-height: 0; }
  .files { overflow-y: auto; padding: 6px 8px; border-right: 1px solid var(--border); }
  .file { height: 24px; font-size: 12px; }
  .status { width: 14px; flex: none; font-family: var(--mono); font-weight: 600; color: var(--muted); }
  .s-resolved { color: var(--ok); }
  .s-manual { color: var(--danger); }
  .none { padding: 4px 10px; color: var(--faint); font-size: 12px; }
  .content { overflow: auto; padding: 8px 0; user-select: text; }
  .line { padding: 0 12px; white-space: pre; line-height: 18px; }
  .marker { background: var(--hover); color: var(--muted); font-weight: 600; }
  .add { background: var(--add-bg); }
  .del { background: var(--del-bg); }
  .hunk { color: var(--accent); }
  .meta { color: var(--faint); }
  .error { padding: 12px; color: var(--danger); white-space: pre-wrap; }
</style>
```

Move `import { api } from '../lib/api'` into the main `<script>` with the other imports and delete the `context="module"` block — it is written separately above only to keep the import visible; a module-context import is wrong here.

- [ ] **Step 3: Show it instead of the commit details**

In `LogView.svelte`, import `mergeState` from `../lib/stores` and `MergeView from './MergeView.svelte'`, then wrap the bottom panel so `MergeView` takes over while merging:

```svelte
{#if $mergeState?.merging}
  <MergeView {repoId} />
{:else if $selectedHash}
  <CommitDetails {repoId} hash={$selectedHash} />
{/if}
```

Match the existing markup — read `LogView.svelte` first and keep its splitter and container structure intact.

In `Sidebar.svelte`, show a badge on a repo that is mid-merge, beside the existing `missing` badge, using `$mergeState` for the selected repo only:

```svelte
{#if active && $mergeState?.merging}<span class="badge">merging</span>{/if}
```

- [ ] **Step 4: Verify**

```bash
go vet ./... && go test ./... && cd frontend && npx vitest run && npm run check
```
Expected: vet clean, all Go tests pass, all frontend tests pass, check reports 0 errors and the 3 known a11y warnings.

Then `make build`, open the app, and walk through it manually in a scratch repository with a real conflict:

1. Right-click a branch → the item reads "Merge <branch> into <current>".
2. Merging a branch with no conflict creates a merge commit visible in the graph.
3. Merging a conflicting branch leaves the app in the merge view, listing the conflicted files, with "Commit merge" disabled.
4. "Resolve with AI" opens the chat, streams the agent's work, and the file list updates as files are resolved.
5. "Abort merge" returns the repo to its previous state and the log reappears.
6. Resolving everything enables "Commit merge", and committing produces a merge commit and restores the normal view.

- [ ] **Step 5: Commit**

```bash
git add frontend/src
git commit -m "feat(ui): merge view with the AI conflict agent"
```

---

## Self-Review

**Spec coverage.** Start/Abort/Commit and `--no-ff` with zdiff3 (Task 3); `State` and its marker-based classification (Task 4); `Parse`/`Splice` and their guarantees (Tasks 1–2); the four tools and the containment argument (Task 5); `MaxSteps` and `merge:changed` (Tasks 6 and 9); the prompt (Task 7); the six app methods (Tasks 8–9); the branch menu item, `MergeView` replacing commit details, and the sidebar badge (Tasks 10–11). The spec's error-handling section is covered by: dirty worktree and unknown ref → the `*gitcmd.Error` returned by `Start` and surfaced by `mergeBranch`'s toast; already-up-to-date → the `UP_TO_DATE` toast; Ollama errors → `chatErrorCode`, already in place; step limit → `agent`'s existing `StepLimitNote`; tool refusals → the refusal strings tested in Task 5; a user editing the file mid-run → `Splice`'s fresh parse, tested in Task 2.

**Placeholders.** None: every code step carries the code, and the one place that says "adapt to the existing helper" (Tasks 6 and 9, for `ai_test.go`'s fake provider) names the file to read and forbids duplicating it.

**Type consistency.** `merge.Hunk`, `merge.State`, `merge.Result` and `app.ConflictFile` are used with the same field names throughout. `mergetools.Run` returns `(string, bool)` in Task 5 and is consumed that way in Task 9. `mergeFiles(state, started)` is defined in Task 10 and called with the same two arguments in Task 11. The `MergeState` TypeScript interface matches the Go JSON tags field for field.

**Known gap, deliberate.** Task 11 has no automated test — the repo has no Svelte component testing, and adding a component test harness for one view is out of scope here. Its logic lives in `mergeFiles`, which is tested; the rest is markup, covered by the manual walkthrough in Step 4.
