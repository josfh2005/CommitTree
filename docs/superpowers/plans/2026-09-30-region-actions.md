# Region actions (part A) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Settle each conflict region from the Merge view — Take a side, Both, or Edit — and restart a file, through the same write path the AI resolver uses.

**Architecture:** `internal/merge` gains region ids, `RegionText`, `ResolveRegion`, `Restartable` and `Restart`; the AI's `resolve_hunk` writes through `ResolveRegion`. `internal/app` exposes `ResolveMergeRegion` and `RestartConflictFile` under the repository write lock and returns each conflict file's region spans. The Svelte Merge view draws a button row above each region from those spans.

**Tech Stack:** Go 1.2x (Wails v2 backend), Svelte 5 + TypeScript, vitest, `git` CLI via `internal/gitcmd`.

**Spec:** `docs/superpowers/specs/2026-09-30-region-actions-design.md`

## Global Constraints

- One parser and one write path: nothing in the frontend parses conflict markers or writes files.
- Every write goes through `App.writeMerge` (repository write lock + `merge:changed` event).
- Refused while an AI run holds the repository (`ErrChatBusy`), and disabled in the UI then.
- Region ids stay exactly as the AI already sees them: first 8 hex chars of SHA-1 over `ours + "\x00" + base + "\x00" + theirs`, repeats suffixed `-2`, `-3`.
- Side names in the UI come from `takeLabels` (`frontend/src/lib/merge.ts`); never hard-code "ours"/"theirs".
- Behaviour changes update `docs/spec/04-conflicts.md` in the same commit (repo rule).
- Commits: conventional style, no `Co-Authored-By` lines (user rule).
- Frontend commands need node 22: `export PATH=~/.nvm/versions/node/v22.23.1/bin:$PATH`.

## Review Focus

1. A region resolved by the AI (or another click) while the user looks at the stale view → the click must write nothing and reload, never hit a different region. Test: Task 1 `TestResolveRegionStaleIDWritesNothing`, Task 3 app test.
2. Restart on a file that was already staged → it comes back with its markers, unstaged; Restart on a cleanly merged staged file (never conflicted) → refused, never silently reverted. Test: Task 2.
3. A file whose last line has no newline, and CRLF files → resolving keeps every other byte. Test: Task 1 `TestResolveRegionKeepsBytesAround`.
4. Two identical regions in one file → each has its own id and resolving one leaves the other. Test: Task 1 `TestRegionIDsTellRepeatsApart`.
5. A click while the AI resolver runs → refused by the backend even if the button was not disabled in time. Test: Task 3 `TestResolveMergeRegionRefusedWhileAIRuns`.

---

### Task 1: Region ids, RegionText and ResolveRegion in `internal/merge`; the AI writes through it

**Files:**
- Modify: `internal/merge/conflict.go` (Hunk, Parse)
- Create: `internal/merge/region.go`
- Create: `internal/merge/region_test.go`
- Modify: `internal/ai/mergetools/mergetools.go` (readConflict, resolveHunk; delete `regionIDs`)
- Test: `internal/ai/mergetools/mergetools_test.go` (existing tests must pass unchanged)

**Interfaces:**
- Produces:
  - `merge.Hunk` fields `ID string`, `Start int`, `End int` (exported; `End` exclusive; replace the unexported `start`/`end` everywhere in the package).
  - `var merge.ErrNoSuchRegion error`
  - `func merge.RegionText(h Hunk, choice string) (string, error)` — `"ours"|"theirs"|"both"`.
  - `func merge.Region(dir, path, id string) (Hunk, error)` — reads and parses the file, finds id.
  - `func merge.ResolveRegion(ctx context.Context, dir, path, id, content string) (left int, err error)`

- [ ] **Step 1: Write the failing tests** — `internal/merge/region_test.go`:

```go
package merge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git-ui/internal/testrepo"
)

// textConflict merges feature into main with name conflicting (diff3).
func textConflict(t *testing.T, name, base, ours, theirs string) *testrepo.Repo {
	t.Helper()
	r := testrepo.New(t)
	r.Git("config", "merge.conflictStyle", "diff3")
	r.WriteFile(name, base)
	r.Git("add", name)
	r.Git("commit", "-q", "-m", "base")
	r.Git("switch", "-q", "-c", "feature")
	r.WriteFile(name, theirs)
	r.Git("commit", "-q", "-am", "theirs")
	r.Git("switch", "-q", "main")
	r.WriteFile(name, ours)
	r.Git("commit", "-q", "-am", "ours")
	if _, err := Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	return r
}

const twoRegions = "a\nb\nc\nd\ne\nf\ng\nh\n"

func twoRegionConflict(t *testing.T) *testrepo.Repo {
	return textConflict(t, "f.txt", twoRegions,
		strings.NewReplacer("b\n", "B-ours\n", "g\n", "G-ours\n").Replace(twoRegions),
		strings.NewReplacer("b\n", "B-theirs\n", "g\n", "G-theirs\n").Replace(twoRegions))
}

func regions(t *testing.T, dir string) []Hunk {
	t.Helper()
	hs, err := Parse(readFile(t, dir, "f.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return hs
}

func TestParseGivesStableIDsAndSpans(t *testing.T) {
	r := twoRegionConflict(t)
	hs := regions(t, r.Dir)
	if len(hs) != 2 || hs[0].ID == "" || hs[0].ID == hs[1].ID {
		t.Fatalf("hunks = %+v", hs)
	}
	lines := strings.SplitAfter(readFile(t, r.Dir, "f.txt"), "\n")
	if !strings.HasPrefix(lines[hs[0].Start], "<<<<<<<") || !strings.HasPrefix(lines[hs[0].End-1], ">>>>>>>") {
		t.Fatalf("span %d..%d does not cover the markers", hs[0].Start, hs[0].End)
	}
	second := hs[1].ID
	if _, err := ResolveRegion(context.Background(), r.Dir, "f.txt", hs[0].ID, "B\n"); err != nil {
		t.Fatal(err)
	}
	if after := regions(t, r.Dir); len(after) != 1 || after[0].ID != second {
		t.Fatalf("the other region's id changed: %+v", after)
	}
}

func TestRegionIDsTellRepeatsApart(t *testing.T) {
	hs, err := Parse("<<<<<<< a\nx\n=======\ny\n>>>>>>> b\nmid\n<<<<<<< a\nx\n=======\ny\n>>>>>>> b\n")
	if err != nil {
		t.Fatal(err)
	}
	if hs[1].ID != hs[0].ID+"-2" {
		t.Fatalf("ids = %q, %q", hs[0].ID, hs[1].ID)
	}
}

func TestRegionText(t *testing.T) {
	h := Hunk{Ours: "o\n", Theirs: "t\n", Base: "b\n"}
	for choice, want := range map[string]string{"ours": "o\n", "theirs": "t\n", "both": "o\nt\n"} {
		if got, err := RegionText(h, choice); err != nil || got != want {
			t.Errorf("%s: %q, %v", choice, got, err)
		}
	}
	if _, err := RegionText(h, "base"); err == nil {
		t.Error("unknown choice accepted")
	}
}

func TestResolveRegionStaleIDWritesNothing(t *testing.T) {
	r := twoRegionConflict(t)
	id := regions(t, r.Dir)[0].ID
	if _, err := ResolveRegion(context.Background(), r.Dir, "f.txt", id, "B\n"); err != nil {
		t.Fatal(err)
	}
	before := readFile(t, r.Dir, "f.txt")
	_, err := ResolveRegion(context.Background(), r.Dir, "f.txt", id, "B again\n")
	if !errors.Is(err, ErrNoSuchRegion) {
		t.Fatalf("err = %v", err)
	}
	if readFile(t, r.Dir, "f.txt") != before {
		t.Fatal("a stale id changed the file")
	}
}

func TestResolveRegionReportsWhatIsLeftAndKeepsTheMode(t *testing.T) {
	r := twoRegionConflict(t)
	full := filepath.Join(r.Dir, "f.txt")
	if err := os.Chmod(full, 0o755); err != nil {
		t.Fatal(err)
	}
	hs := regions(t, r.Dir)
	left, err := ResolveRegion(context.Background(), r.Dir, "f.txt", hs[0].ID, "B\n")
	if err != nil || left != 1 {
		t.Fatalf("left %d, err %v", left, err)
	}
	left, err = ResolveRegion(context.Background(), r.Dir, "f.txt", hs[1].ID, "G\n")
	if err != nil || left != 0 {
		t.Fatalf("left %d, err %v", left, err)
	}
	if got := readFile(t, r.Dir, "f.txt"); got != "a\nB\nc\nd\ne\nf\nG\nh\n" {
		t.Fatalf("file = %q", got)
	}
	if info, _ := os.Stat(full); info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v", info.Mode())
	}
}

func TestResolveRegionKeepsBytesAround(t *testing.T) {
	r := textConflict(t, "crlf.txt", "a\r\nb\r\nc", "a\r\nB1\r\nc", "a\r\nB2\r\nc")
	hs, err := Parse(readFile(t, r.Dir, "crlf.txt"))
	if err != nil || len(hs) != 1 {
		t.Fatalf("%v %+v", err, hs)
	}
	if _, err := ResolveRegion(context.Background(), r.Dir, "crlf.txt", hs[0].ID, "B\r\n"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, r.Dir, "crlf.txt"); got != "a\r\nB\r\nc" {
		t.Fatalf("file = %q", got)
	}
}

func TestResolveRegionRefusesWhatIsNotATextConflict(t *testing.T) {
	r := twoRegionConflict(t)
	id := regions(t, r.Dir)[0].ID
	if _, err := ResolveRegion(context.Background(), r.Dir, "other.txt", id, "x\n"); !errors.Is(err, ErrNotInMerge) {
		t.Errorf("unknown path: %v", err)
	}
	if _, err := ResolveRegion(context.Background(), r.Dir, "f.txt", id, "<<<<<<< x\n"); !errors.Is(err, ErrMarkersLeft) {
		t.Errorf("markers: %v", err)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/merge/ -run 'Region|StableIDs'`
Expected: build failure — `ResolveRegion`, `RegionText`, `ErrNoSuchRegion`, `Hunk.ID`, `Hunk.Start` undefined.

- [ ] **Step 3: Implement**

In `internal/merge/conflict.go`: rename the unexported fields `start, end` to exported `Start, End` (update every use in the package: Parse, Splice, After computation), add `ID string` to `Hunk` with a doc line, and at the end of `Parse`, before `return hunks, nil`, fill ids:

```go
	seen := map[string]int{}
	for i := range hunks {
		h := &hunks[i]
		sum := sha1.Sum([]byte(h.Ours + "\x00" + h.Base + "\x00" + h.Theirs))
		id := hex.EncodeToString(sum[:4])
		seen[id]++
		if n := seen[id]; n > 1 {
			id = fmt.Sprintf("%s-%d", id, n)
		}
		h.ID = id
	}
```

(imports `crypto/sha1`, `encoding/hex`). Doc on `ID`: "names the region by its content, so the name survives the renumbering every resolve causes; the AI resolver and the Merge view's buttons both use it."

Create `internal/merge/region.go`:

```go
package merge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// ErrNoSuchRegion means the file has no region with that id any more —
// resolved meanwhile (by the AI, or another click), or never there.
var ErrNoSuchRegion = errors.New("merge: no such conflict region")

// RegionText is what a region becomes when one side is taken whole
// ("ours", "theirs") or both are kept, ours first ("both").
func RegionText(h Hunk, choice string) (string, error) {
	switch choice {
	case "ours":
		return h.Ours, nil
	case "theirs":
		return h.Theirs, nil
	case "both":
		return h.Ours + h.Theirs, nil
	}
	return "", fmt.Errorf("merge: unknown choice %q", choice)
}

// Region reads path and returns its region id.
func Region(dir, path, id string) (Hunk, error) {
	data, err := os.ReadFile(filepath.Join(dir, path))
	if err != nil {
		return Hunk{}, err
	}
	hunks, err := Parse(string(data))
	if err != nil {
		return Hunk{}, err
	}
	i := slices.IndexFunc(hunks, func(h Hunk) bool { return h.ID == id })
	if i < 0 {
		return Hunk{}, fmt.Errorf("%w: %s in %s", ErrNoSuchRegion, id, path)
	}
	return hunks[i], nil
}

// ResolveRegion replaces region id of path, one of the operation's text
// conflicts, with content, and returns how many regions the file has left.
// The write is atomic and keeps the file's mode; a stale id, content with
// markers, or a path that is not a text conflict writes nothing. The AI
// resolver's resolve_hunk and the Merge view's buttons both come here.
func ResolveRegion(ctx context.Context, dir, path, id, content string) (int, error) {
	st, err := Status(ctx, dir)
	if err != nil {
		return 0, err
	}
	if err := settled(st, st.Conflicts, path, "a text conflict"); err != nil {
		return 0, err
	}
	full := filepath.Join(dir, path)
	info, err := os.Lstat(full)
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("%w: %q is not a regular file", ErrNotInMerge, path)
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return 0, err
	}
	hunks, err := Parse(string(data))
	if err != nil {
		return 0, err
	}
	i := slices.IndexFunc(hunks, func(h Hunk) bool { return h.ID == id })
	if i < 0 {
		return len(hunks), fmt.Errorf("%w: %s in %s", ErrNoSuchRegion, id, path)
	}
	out, err := Splice(string(data), i, content)
	if err != nil {
		return len(hunks), err
	}
	if err := writeAtomic(full, info.Mode().Perm(), out); err != nil {
		return len(hunks), err
	}
	left, _ := Parse(out)
	return len(left), nil
}

// writeAtomic replaces full with content through a temp file in the same
// directory, so a crash never leaves half a file.
func writeAtomic(full string, perm os.FileMode, content string) error {
	tmp, err := os.CreateTemp(filepath.Dir(full), ".git-ui-merge-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, werr := tmp.WriteString(content)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, perm); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, full); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
```

In `internal/ai/mergetools/mergetools.go`:
- `readConflict`: header uses `h.ID` instead of `regionIDs(hunks)[index]`.
- `resolveHunk`: replace everything from reading the file to the end of the function with:

```go
	resolved, _ := args["resolved"].(string)
	data, err := os.ReadFile(filepath.Join(dir, path))
	if err != nil {
		return "Could not read " + path + ": " + err.Error(), false
	}
	hunks, _ := merge.Parse(string(data))
	id, _ := args["region"].(string)
	if id == "" {
		index := argInt(args, "hunk")
		if index < 0 || index >= len(hunks) {
			return fmt.Sprintf("There is no region %d. %s", index, regionsLeft(path, len(hunks))), false
		}
		id = hunks[index].ID
	}
	i := slices.IndexFunc(hunks, func(h merge.Hunk) bool { return h.ID == id })
	if i < 0 {
		return fmt.Sprintf("Not applied: %s has no region %s (already resolved, or never there). Call read_conflict for the current regions.", path, id), false
	}
	if msg := checkResolution(hunks[i], resolved); msg != "" {
		return msg, false
	}
	left, err := merge.ResolveRegion(ctx, dir, path, id, resolved)
	if err != nil {
		return "Could not apply the resolution: " + err.Error(), false
	}
	return "Applied. " + regionsLeft(path, left), true
```

- Delete `regionIDs` and the now-unused imports (`crypto/sha1`, `encoding/hex`, and `errors` if unused).

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/merge/ ./internal/ai/... && go vet ./internal/merge/ ./internal/ai/...`
Expected: all `ok` (the mergetools tests, including `TestResolvingSeveralRegionsInOneTurnByID` and the context/bracket guard tests, pass unchanged).

- [ ] **Step 5: Commit**

```bash
git add internal/merge internal/ai/mergetools
git commit -m "refactor(merge): region ids, RegionText and ResolveRegion in the merge package; the AI resolver writes through them"
```

---

### Task 2: Restartable and Restart in `internal/merge`

**Files:**
- Modify: `internal/merge/region.go`
- Test: `internal/merge/region_test.go`

**Interfaces:**
- Consumes: `textConflict`, `twoRegionConflict`, `regions` (Task 1 test helpers).
- Produces:
  - `func merge.Restartable(ctx context.Context, dir string) ([]string, error)`
  - `func merge.Restart(ctx context.Context, dir, path string) error`

- [ ] **Step 1: Write the failing tests** (append to `region_test.go`):

```go
// A resolved and staged file comes back with its markers, unstaged; a file
// the merge settled cleanly is never offered and is refused.
func TestRestartBringsBackAStagedFile(t *testing.T) {
	r := twoRegionConflict(t)
	ctx := context.Background()
	original := readFile(t, r.Dir, "f.txt")
	for _, h := range regions(t, r.Dir) {
		if _, err := ResolveRegion(ctx, r.Dir, "f.txt", h.ID, "done\n"); err != nil {
			t.Fatal(err)
		}
	}
	if err := Stage(ctx, r.Dir, "f.txt"); err != nil {
		t.Fatal(err)
	}
	can, err := Restartable(ctx, r.Dir)
	if err != nil || !slices.Contains(can, "f.txt") {
		t.Fatalf("restartable = %v, %v", can, err)
	}
	if err := Restart(ctx, r.Dir, "f.txt"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, r.Dir, "f.txt"); got != original {
		t.Fatalf("file = %q, want the original conflict %q", got, original)
	}
	if st := status(t, r.Dir); !slices.Contains(st.Conflicts, "f.txt") {
		t.Fatalf("not conflicted again: %+v", st)
	}
}

func TestRestartRefusesAFileThatWasNeverConflicted(t *testing.T) {
	r := twoRegionConflict(t)
	if err := Restart(context.Background(), r.Dir, "not-in-the-merge.txt"); !errors.Is(err, ErrNotInMerge) {
		t.Fatalf("err = %v", err)
	}
}
```

(add `"slices"` to the test imports; `status` is the existing helper in `stage_test.go`.)

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/merge/ -run Restart`
Expected: build failure — `Restartable`, `Restart` undefined.

- [ ] **Step 3: Implement** (append to `region.go`; add `"strings"` and `"git-ui/internal/gitcmd"` imports):

```go
// Restartable are the operation's files Restart can put back as git first
// wrote them: those still in conflict, and settled ones git kept a
// resolve-undo record for (it keeps one when a conflicted file is staged).
// A file the merge settled cleanly has none, so it is never offered.
func Restartable(ctx context.Context, dir string) ([]string, error) {
	st, err := Status(ctx, dir)
	if err != nil {
		return nil, err
	}
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "ls-files", "-z", "--resolve-undo")
	if err != nil {
		return nil, err
	}
	undo := map[string]bool{}
	for _, rec := range strings.Split(out, "\x00") {
		if _, path, ok := strings.Cut(rec, "\t"); ok {
			undo[path] = true
		}
	}
	can := slices.Clone(st.Conflicts)
	for _, p := range append(slices.Clone(st.Staged), st.Unstaged...) {
		if undo[p] && !slices.Contains(can, p) {
			can = append(can, p)
		}
	}
	return can, nil
}

// Restart puts path back as the operation left it, markers included, and
// unstaged — discarding what was resolved in it, by hand or by the AI.
func Restart(ctx context.Context, dir, path string) error {
	can, err := Restartable(ctx, dir)
	if err != nil {
		return err
	}
	if !slices.Contains(can, path) {
		return fmt.Errorf("%w: %q was not in conflict", ErrNotInMerge, path)
	}
	_, err = gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "--literal-pathspecs", "checkout", "-m", "--", path)
	return err
}
```

If `TestRestartBringsBackAStagedFile` fails because `checkout -m` does not recreate a staged file, run `git update-index --unresolve -- <path>` (same flags) before the checkout, and keep the test as is.

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/merge/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/merge
git commit -m "feat(merge): restart a conflicted file, even once staged, as git first wrote it"
```

---

### Task 3: App methods, region spans in ConflictFile, bindings

**Files:**
- Modify: `internal/app/merge.go` (ConflictFile, GetConflictFile, new methods)
- Modify: `internal/app/ai.go` (helper `aiBusy`)
- Test: `internal/app/merge_test.go`
- Modify: `frontend/wailsjs/go/app/App.d.ts`, `frontend/wailsjs/go/app/App.js`, `frontend/wailsjs/go/models.ts`

**Interfaces:**
- Consumes: `merge.Region`, `merge.RegionText`, `merge.ResolveRegion`, `merge.Restartable`, `merge.Restart`, `merge.Stage`, `Hunk.ID/Start/End`.
- Produces (Go, bound to the frontend):
  - `type Region struct { ID string \`json:"id"\`; Start int \`json:"start"\`; End int \`json:"end"\` }`
  - `ConflictFile` gains `Regions []Region \`json:"regions"\`` and `Restartable bool \`json:"restartable"\``
  - `type RegionResult struct { Left int \`json:"left"\`; Staged bool \`json:"staged"\` }`
  - `func (a *App) ResolveMergeRegion(id, path, region, choice, text string) (RegionResult, error)` — choice `"ours"|"theirs"|"both"|"text"`
  - `func (a *App) RestartConflictFile(id, path string) error`

- [ ] **Step 1: Write the failing tests** (append to `internal/app/merge_test.go`; `newMergeApp` leaves `greeting.txt` conflicted after `MergeBranch(id, "feature")`):

```go
func TestResolveMergeRegionStagesTheLastRegion(t *testing.T) {
	a, r, id := newMergeApp(t)
	if _, err := a.MergeBranch(id, "feature"); err != nil {
		t.Fatal(err)
	}
	f, err := a.GetConflictFile(id, "greeting.txt")
	if err != nil || len(f.Regions) != 1 || !f.Restartable {
		t.Fatalf("file = %+v, %v", f, err)
	}
	lines := strings.SplitAfter(f.Text, "\n")
	if !strings.HasPrefix(lines[f.Regions[0].Start], "<<<<<<<") {
		t.Fatalf("region starts at %q", lines[f.Regions[0].Start])
	}
	res, err := a.ResolveMergeRegion(id, "greeting.txt", f.Regions[0].ID, "both", "")
	if err != nil || res.Left != 0 || !res.Staged {
		t.Fatalf("res = %+v, %v", res, err)
	}
	st, _ := merge.Status(context.Background(), r.Dir)
	if !slices.Contains(st.Staged, "greeting.txt") {
		t.Fatalf("not staged: %+v", st)
	}
	if err := a.RestartConflictFile(id, "greeting.txt"); err != nil {
		t.Fatal(err)
	}
	if st, _ := merge.Status(context.Background(), r.Dir); !slices.Contains(st.Conflicts, "greeting.txt") {
		t.Fatalf("not conflicted after restart: %+v", st)
	}
}

func TestResolveMergeRegionWithEditedText(t *testing.T) {
	a, r, id := newMergeApp(t)
	if _, err := a.MergeBranch(id, "feature"); err != nil {
		t.Fatal(err)
	}
	f, _ := a.GetConflictFile(id, "greeting.txt")
	if _, err := a.ResolveMergeRegion(id, "greeting.txt", f.Regions[0].ID, "text", "hello there\n"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(r.Dir, "greeting.txt")); string(data) != "hello there\n" {
		t.Fatalf("file = %q", data)
	}
}

func TestResolveMergeRegionRefusedWhileAIRuns(t *testing.T) {
	a, _, id, _ := newAIMergeApp(t, "http://127.0.0.1:0")
	if _, err := a.MergeBranch(id, "feature"); err != nil {
		t.Fatal(err)
	}
	f, _ := a.GetConflictFile(id, "greeting.txt")
	a.ai.mu.Lock()
	a.ai.runs[id] = func() {}
	a.ai.mu.Unlock()
	if _, err := a.ResolveMergeRegion(id, "greeting.txt", f.Regions[0].ID, "ours", ""); !errors.Is(err, ErrChatBusy) {
		t.Fatalf("err = %v", err)
	}
	if err := a.RestartConflictFile(id, "greeting.txt"); !errors.Is(err, ErrChatBusy) {
		t.Fatalf("restart err = %v", err)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/app/ -run 'ResolveMergeRegion'`
Expected: build failure — `Regions`, `ResolveMergeRegion`, `RestartConflictFile` undefined.

- [ ] **Step 3: Implement**

`internal/app/ai.go`, next to `StopChat`:

```go
// aiBusy reports whether an AI run holds repoID; the Merge view's writes
// wait for it rather than edit the files under it.
func (a *App) aiBusy(repoID string) bool {
	if a.ai == nil {
		return false
	}
	a.ai.mu.Lock()
	defer a.ai.mu.Unlock()
	_, busy := a.ai.runs[repoID]
	return busy
}
```

`internal/app/merge.go`:

```go
// Region is where one conflict region sits in ConflictFile.Text: lines
// Start (its <<<<<<< line) to End (after its >>>>>>> line), 0-based.
type Region struct {
	ID    string `json:"id"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

type ConflictFile struct {
	Path     string   `json:"path"`
	Resolved bool     `json:"resolved"`
	Text     string   `json:"text"`
	Regions  []Region `json:"regions"`
	// Restartable: Restart file can put it back as git first wrote it.
	Restartable bool `json:"restartable"`
}

// RegionResult is what resolving one region left: the file's regions still
// to settle, and whether it was staged because none were.
type RegionResult struct {
	Left   int  `json:"left"`
	Staged bool `json:"staged"`
}
```

In `GetConflictFile`: compute once, after `st` is read, `can, _ := merge.Restartable(a.ctx, dir)` and set `Restartable: slices.Contains(can, path)` on every returned `ConflictFile`; initialise `Regions: []Region{}` on every return. In the Conflicts branch, after reading `data`:

```go
			f := ConflictFile{Path: path, Text: string(data), Regions: []Region{}, Restartable: slices.Contains(can, path)}
			if hunks, err := merge.Parse(f.Text); err == nil {
				for _, h := range hunks {
					f.Regions = append(f.Regions, Region{ID: h.ID, Start: h.Start, End: h.End})
				}
			}
			return f, nil
```

Change the Manual branch's text to: `"This file has no conflict markers to edit here. Take one side with the buttons above, or resolve it in your editor."`

New methods (after `TakeMergeSide`):

```go
// ResolveMergeRegion settles one conflict region of path: one side whole
// ("ours", "theirs"), both ours first ("both"), or the user's own text
// ("text"). It stages the file once no regions are left.
func (a *App) ResolveMergeRegion(id, path, region, choice, text string) (RegionResult, error) {
	if a.aiBusy(id) {
		return RegionResult{}, fmt.Errorf("%w; wait for it or stop it first", ErrChatBusy)
	}
	var res RegionResult
	err := a.writeMerge(id, func(ctx context.Context, dir string) error {
		content := text
		if choice != "text" {
			h, err := merge.Region(dir, path, region)
			if err != nil {
				return err
			}
			if content, err = merge.RegionText(h, choice); err != nil {
				return err
			}
		}
		left, err := merge.ResolveRegion(ctx, dir, path, region, content)
		if err != nil {
			return err
		}
		res.Left = left
		if left == 0 {
			if err := merge.Stage(ctx, dir, path); err != nil {
				return err
			}
			res.Staged = true
		}
		return nil
	})
	return res, err
}

// RestartConflictFile puts path back as the operation left it, markers
// included, discarding what was resolved in it.
func (a *App) RestartConflictFile(id, path string) error {
	if a.aiBusy(id) {
		return fmt.Errorf("%w; wait for it or stop it first", ErrChatBusy)
	}
	return a.writeMerge(id, func(ctx context.Context, dir string) error { return merge.Restart(ctx, dir, path) })
}
```

Bindings (Wails regenerates these on `make dev`; write them by hand so the frontend type-checks now):
- `App.d.ts`: `export function ResolveMergeRegion(arg1:string,arg2:string,arg3:string,arg4:string,arg5:string):Promise<app.RegionResult>;` and `export function RestartConflictFile(arg1:string,arg2:string):Promise<void>;`
- `App.js`: the two matching `window['go']['app']['App'][...]` wrappers.
- `models.ts`, in `namespace app`: add classes `Region` (id, start, end) and `RegionResult` (left, staged) following the existing class pattern, and add `regions: Region[]` (converted with `this.convertValues(source["regions"], Region)`) and `restartable: boolean` to `ConflictFile`.

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/app/ && go vet ./internal/app/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/app frontend/wailsjs
git commit -m "feat(merge): resolve one conflict region or restart a file from the app; conflict files carry their region spans"
```

---

### Task 4: Frontend logic — types, api, actions, layout helper, AI-running store

**Files:**
- Modify: `frontend/src/lib/types.ts`, `frontend/src/lib/api.ts`, `frontend/src/lib/actions.ts`, `frontend/src/lib/stores.ts`, `frontend/src/lib/merge.ts`
- Modify: `frontend/src/components/ChatPanel.svelte` (one reactive line)
- Test: `frontend/src/lib/merge.test.ts`

**Interfaces:**
- Consumes: Task 3's Go methods through `Go.ResolveMergeRegion`, `Go.RestartConflictFile`.
- Produces:
  - `types.ts`: `interface Region { id: string; start: number; end: number }`, `interface RegionResult { left: number; staged: boolean }`, `ConflictFile.regions: Region[]`, `ConflictFile.restartable: boolean`; `type RegionChoice = 'ours' | 'theirs' | 'both' | 'text'`.
  - `api.resolveMergeRegion(id, path, region, choice: RegionChoice, text: string): Promise<RegionResult>`, `api.restartConflictFile(id, path): Promise<void>`.
  - `merge.ts`: `type LinePart = 'marker' | 'ours' | 'base' | 'theirs' | null`; `interface LineInfo { part: LinePart; region: Region | null; starts: boolean }`; `function layoutLines(lines: string[], regions: Region[]): LineInfo[]`; `function regionSides(lines: string[], info: LineInfo[], id: string): { ours: string; theirs: string }`.
  - `actions.ts`: `resolveMergeRegion(id, path, region, choice, text, reload: () => void): Promise<void>`, `restartConflictFile(id, path): Promise<void>`.
  - `stores.ts`: `export const chatRunRepo = writable('')` — the repository id with an AI run in progress, `''` when none.

- [ ] **Step 1: Write the failing tests** (append to `frontend/src/lib/merge.test.ts`, importing `layoutLines, regionSides`):

```ts
describe('layoutLines', () => {
  const text = 'a\n<<<<<<< HEAD\nours1\nours2\n||||||| base\nbase1\n=======\ntheirs1\n>>>>>>> feature\nz'
  const lines = text.split('\n')
  const regions = [{ id: 'r1', start: 1, end: 9 }]

  it('marks each line with the side it belongs to and where regions start', () => {
    const info = layoutLines(lines, regions)
    expect(info.map((l) => l.part)).toEqual([null, 'marker', 'ours', 'ours', 'marker', 'base', 'marker', 'theirs', 'marker', null])
    expect(info.map((l) => l.starts)).toEqual([false, true, false, false, false, false, false, false, false, false])
    expect(info[3].region?.id).toBe('r1')
    expect(info[0].region).toBeNull()
  })

  it('gives each side of a region as text, for Edit…', () => {
    const info = layoutLines(lines, regions)
    expect(regionSides(lines, info, 'r1')).toEqual({ ours: 'ours1\nours2\n', theirs: 'theirs1\n' })
  })

  it('handles a two-way region with no ancestor', () => {
    const two = ['<<<<<<< HEAD', 'o', '=======', 't', '>>>>>>> x']
    expect(layoutLines(two, [{ id: 'r', start: 0, end: 5 }]).map((l) => l.part)).toEqual(['marker', 'ours', 'marker', 'theirs', 'marker'])
  })
})
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd frontend && npx vitest run src/lib/merge.test.ts`
Expected: FAIL — `layoutLines` is not exported.

- [ ] **Step 3: Implement**

`merge.ts` (import `Region` from `./types`):

```ts
export type LinePart = 'marker' | 'ours' | 'base' | 'theirs' | null

export interface LineInfo {
  part: LinePart
  region: Region | null
  /** true on a region's <<<<<<< line, where its buttons go */
  starts: boolean
}

/** layoutLines says, for each line of a conflict file, which region and
 *  which side of it the line belongs to. The backend gives the spans; this
 *  only walks the markers inside them. */
export function layoutLines(lines: string[], regions: Region[]): LineInfo[] {
  const info: LineInfo[] = lines.map(() => ({ part: null, region: null, starts: false }))
  for (const r of regions) {
    let side: LinePart = 'ours'
    for (let i = r.start; i < Math.min(r.end, lines.length); i++) {
      const l = lines[i]
      let part: LinePart = side
      if (i === r.start || i === r.end - 1) part = 'marker'
      else if (side === 'ours' && l.startsWith('|||||||')) { part = 'marker'; side = 'base' }
      else if (side !== 'theirs' && l.startsWith('=======')) { part = 'marker'; side = 'theirs' }
      info[i] = { part, region: r, starts: i === r.start }
    }
  }
  return info
}

/** regionSides is a region's ours and theirs text, each line ending in \n. */
export function regionSides(lines: string[], info: LineInfo[], id: string): { ours: string; theirs: string } {
  const side = (part: LinePart) => lines.filter((_, i) => info[i].region?.id === id && info[i].part === part).map((l) => l + '\n').join('')
  return { ours: side('ours'), theirs: side('theirs') }
}
```

`types.ts`: add `Region`, `RegionResult`, `RegionChoice`; extend `ConflictFile` with `regions: Region[]` and `restartable: boolean`.

`api.ts`, next to `takeMergeSide`:

```ts
  resolveMergeRegion: (id: string, path: string, region: string, choice: RegionChoice, text: string) => call<RegionResult>(Go.ResolveMergeRegion(id, path, region, choice, text)),
  restartConflictFile: (id: string, path: string) => call<void>(Go.RestartConflictFile(id, path)),
```

`actions.ts`, after `takeMergeSide`:

```ts
/** resolveMergeRegion settles one region from the Merge view. A region the
 *  AI or another click resolved meanwhile writes nothing; the view reloads. */
export async function resolveMergeRegion(id: string, path: string, region: string, choice: RegionChoice, text: string, reload: () => void) {
  busy.set('Resolving…')
  try {
    const res = await api.resolveMergeRegion(id, path, region, choice, text)
    if (res.staged) toast(`${path} resolved and staged`)
  } catch (e) {
    const message = errorMessage(e)
    toast(message.includes('no such conflict region') ? 'That region changed; reloaded.' : message, 'error')
    reload()
  } finally {
    busy.set('')
    await refreshRepo()
  }
}

export async function restartConflictFile(id: string, path: string) {
  const ok = await confirmDialog({
    title: 'Restart file',
    message: `Put ${path} back as the merge left it, with its conflict markers? What was resolved in it, by hand or by the AI, is lost.`,
    confirmLabel: 'Restart file',
    danger: true,
  })
  if (ok) await run('Restarting…', () => api.restartConflictFile(id, path))
}
```

`stores.ts`: `/** The repository an AI run is working on, '' when none (set by ChatPanel). */ export const chatRunRepo = writable('')`.

`ChatPanel.svelte`, after `$: running = …`: `$: chatRunRepo.set(running ? state.repoID : '')` (import `chatRunRepo` from `../lib/stores`).

- [ ] **Step 4: Run to verify they pass**

Run: `cd frontend && npx vitest run && npx svelte-check --threshold error`
Expected: all PASS, `0 ERRORS`.

- [ ] **Step 5: Commit**

```bash
git add frontend/src
git commit -m "feat(merge): frontend plumbing for region actions — api, actions, line layout, AI-running store"
```

---

### Task 5: Merge view UI, spec, manual check

**Files:**
- Modify: `frontend/src/components/MergeView.svelte`
- Modify: `docs/spec/04-conflicts.md`

**Interfaces:**
- Consumes: `layoutLines`, `regionSides`, `takeLabels`, `resolveMergeRegion`, `restartConflictFile`, `takeMergeSide`, `chatRunRepo`, `ConflictFile.regions/restartable`.

- [ ] **Step 1: State and handlers** — in the `<script>`:

```ts
  import { resolveMergeRegion, restartConflictFile } from '../lib/actions'   // add to the existing import
  import { layoutLines, regionSides } from '../lib/merge'                     // add to the existing import
  import { chatRunRepo } from '../lib/stores'                                 // add to the existing import

  let regions: Region[] = []
  let restartable = false
  // What a hovered Take button would keep, to dim the rest of its region.
  let hover: { id: string; keep: 'ours' | 'theirs' } | null = null
  // The region being edited by hand, and its text box's content.
  let editing: { id: string; value: string } | null = null

  $: lines = text.split('\n')
  $: layout = layoutLines(lines, regions)
  $: locked = !!$busy || $chatRunRepo === repoId
  $: sides = takeLabels($mergeState)
  $: current = files.find((f) => selection && f.path === selection.path)

  function dimmed(i: number): boolean {
    const l = layout[i]
    if (!hover || l.region?.id !== hover.id) return false
    return l.part !== hover.keep
  }

  function take(regionId: string, choice: 'ours' | 'theirs' | 'both') {
    if (!selection) return
    hover = null
    resolveMergeRegion(repoId, selection.path, regionId, choice, '', refresh)
  }

  function startEdit(regionId: string) {
    const s = regionSides(lines, layout, regionId)
    editing = { id: regionId, value: s.ours + s.theirs }
  }

  function applyEdit() {
    if (!selection || !editing) return
    const { id, value } = editing
    editing = null
    resolveMergeRegion(repoId, selection.path, id, 'text', value, refresh)
  }

  function editKeys(e: KeyboardEvent) {
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) { e.preventDefault(); applyEdit() }
    if (e.key === 'Escape') { e.preventDefault(); editing = null }
  }
```

In `open()` and `refresh()`, next to `text = f.text; resolved = f.resolved`, add `regions = f.regions ?? []; restartable = f.restartable; editing = null`; in `open()` also reset `regions = []; restartable = false` where it resets `text = ''`.

- [ ] **Step 2: Markup** — replace the `{#each text.split('\n') as line}` block inside `.content` with:

```svelte
        {#if selection && (restartable || current?.status === 'manual')}
          <div class="file-actions">
            {#if current?.status === 'manual'}
              <button class="btn" disabled={locked} on:click={() => selection && takeMergeSide(repoId, selection.path, 'ours', sides.ours)}>Take {sides.ours}</button>
              <button class="btn" disabled={locked} on:click={() => selection && takeMergeSide(repoId, selection.path, 'theirs', sides.theirs)}>Take {sides.theirs}</button>
            {/if}
            <span class="spacer"></span>
            {#if restartable}
              <button class="btn" disabled={locked} title="Put the file back as the merge left it, with its conflict markers" on:click={() => selection && restartConflictFile(repoId, selection.path)}>Restart file</button>
            {/if}
          </div>
        {/if}
        {#each lines as line, i}
          {@const l = layout[i]}
          {#if l.starts && l.region}
            {@const id = l.region.id}
            {#if editing?.id === id}
              <div class="region-edit">
                <textarea class="mono" rows={Math.max(3, editing.value.split('\n').length)} bind:value={editing.value} on:keydown={editKeys}></textarea>
                <div class="region-bar">
                  <button class="btn primary" on:click={applyEdit}>Apply</button>
                  <button class="btn" on:click={() => (editing = null)}>Cancel</button>
                </div>
              </div>
            {:else}
              <div class="region-bar">
                <button class="btn" disabled={locked} on:mouseenter={() => (hover = { id, keep: 'ours' })} on:mouseleave={() => (hover = null)} on:click={() => take(id, 'ours')}>Take {sides.ours}</button>
                <button class="btn" disabled={locked} on:mouseenter={() => (hover = { id, keep: 'theirs' })} on:mouseleave={() => (hover = null)} on:click={() => take(id, 'theirs')}>Take {sides.theirs}</button>
                <button class="btn" disabled={locked} on:click={() => take(id, 'both')}>Both</button>
                <button class="btn" disabled={locked} on:click={() => startEdit(id)}>Edit…</button>
              </div>
            {/if}
          {/if}
          {#if !(editing && l.region?.id === editing.id)}
            <div class="line {lineClass(line, resolved)}" class:dim={dimmed(i)}>{line || ' '}</div>
          {/if}
        {/each}
```

Styles:

```css
  .file-actions { display: flex; gap: 6px; padding: 0 12px 8px; }
  .file-actions .spacer { flex: 1; }
  .region-bar { display: flex; gap: 6px; padding: 4px 12px; font-family: var(--font-ui, inherit); }
  .region-bar .btn { font-size: 12px; padding: 2px 8px; }
  .region-edit { padding: 4px 12px; }
  .region-edit textarea { width: 100%; box-sizing: border-box; font-size: 12px; line-height: 18px; padding: 6px 8px; border: 1px solid var(--border); border-radius: 6px; background: var(--bg); color: var(--text); resize: vertical; }
  .line.dim { opacity: 0.35; }
```

- [ ] **Step 3: Type-check and test**

Run: `cd frontend && npx svelte-check --threshold error && npx vitest run`
Expected: `0 ERRORS`, all PASS.

- [ ] **Step 4: Spec** — in `docs/spec/04-conflicts.md`, in the section describing the Merge view's file pane, add:

```markdown
Above each conflict region of a text file the pane shows a row of buttons
named after the two sides — "Take develop", "Take feature/checkout" (the
names the rest of the view uses for this operation) — plus "Both" (ours,
then theirs) and "Edit…". Hovering a Take button dims the lines it would
drop. Edit… turns the region into a text box holding both sides, applied
with Apply or ⌘↵ and dropped with Cancel or Esc. Each writes only that
region, found by its content id: if the region changed meanwhile (the AI,
another click), nothing is written and the file reloads. When a file's
last region is settled it is staged ("… resolved and staged").

"Restart file" puts a file of the operation that was in conflict — still
conflicted, or staged since — back as git first wrote it, markers
included and unstaged, after a confirmation; it is the undo for anything
resolved in it, by hand or by the AI. A file the merge settled cleanly
has no Restart.

A file without markers shows its "Take <side>" choices as buttons at the
top of the pane (the right-click menu still offers them). All of these are
disabled while another operation runs or an AI run is working on the
repository, and the app refuses them then as well.
```

- [ ] **Step 5: Manual check** — rebuild and use the conflict lab:

```bash
make dev
scripts/conflict-lab/setup.sh
```

In CommitTree: open `conflict-lab`, merge `feature/checkout` into `develop`. With buttons only: README → Edit… (drop the duplicate line); HTML, imports, tax.py → Both; `list()` and `format.ts` → Edit…; methods → Both; settings flags → Both; timeout → Take develop; discount → Take develop; old-report.ts → Take develop. Then `python3 scripts/conflict-lab/check.py --model buttons`. Expected: scenarios 1–6, 8, 10 PASS; 7 and 9 PARTIAL (a side was taken on purpose); 11 FAIL (settled on purpose). Try Restart file on a staged file: it returns with markers. Try a button while "Resolve with AI" runs: disabled.

- [ ] **Step 6: Commit**

```bash
git add frontend/src docs/spec/04-conflicts.md
git commit -m "feat(merge): Take a side, Both or Edit each conflict region; Restart file; visible Take for files without markers"
```
