# Push, Pull and Stash (Sub-project 2b) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Push, pull (merge/rebase/auto strategy, upstream handling) and full stash support (push/apply/pop/drop/diff), with a rebase-pull or a stash conflict resolved in the same view that already handles a merge conflict.

**Architecture:** `internal/ops` already has `Fetch` and a fast-forward-only `Pull` (unused by any UI); this plan replaces `Pull`'s ff-only refusal with real merge/rebase handling and adds `Push`/`Counts`. `internal/merge`'s `State` gains a `Kind` (`merge`/`rebase`/`cherry-pick`/`revert`/`am`/`stash`) so one view keeps working for all of them; `Stage`/`Unstage`/`Take` are untouched. A new `internal/stash` package and a new `internal/gitsettings` package (the pull-strategy setting, kept apart from `ai/settings`) round out the backend. The frontend gets a small toolbar (Fetch/Pull/Push with ahead/behind badges), a Stash sidebar section mirroring Tags, and `MergeView.svelte` learns to show three different headers/action bars instead of one.

**Tech Stack:** Go 1.26, Wails v2, Svelte 5, vitest.

**Spec:** `docs/superpowers/specs/2026-09-21-push-pull-stash-design.md`

## What the plan audit changed (2026-09-21, before any code)

An audit of the first draft of this plan found 8 blocking defects, most
reproduced against git 2.54 in scratch repositories. All 8 are fixed here;
the fix is described where it belongs, and repeated in one line below so a
reviewer can check them off.

1. **`stash.List` always returned `[]`** — `git stash list` ignores
   `--format`/`--pretty`/`-z`. Task 7 reads `git reflog show … refs/stash`
   instead, guarded by a `rev-parse --verify refs/stash` so a
   never-stashed repository is an empty list, not an error.
2. **`newPlainApp` killed the `internal/app` test binary** — with no
   `WithAI`, `a.emit` falls through to wails `runtime.EventsEmit`, whose
   `getEvents` calls `log.Fatalf` on a context with no `"events"` value.
   Task 6's helper now installs `WithAI(a, AIDeps{Emit: newEvents().emit})`
   (and a temp `gitSettingsPath`, so no test reads the developer's real
   `git.json`).
3. **`-c core.editor=true` does not stop `--continue` opening an editor** —
   `GIT_EDITOR` from the user's shell wins. New **Task 0** adds
   `gitcmd.RunEnv`, and Task 2 passes `GIT_EDITOR=true` in the environment.
4. **`KindStash` swallowed a cherry-pick, a revert and a `git am`** — Task 1
   checks `CHERRY_PICK_HEAD`/`REVERT_HEAD` first and tells `git am` apart
   from a rebase by `.git/rebase-apply/applying`; Task 2 gives each its own
   `--continue`/`--abort`.
5. **Task 4 left the tree non-building for two commits** — `internal/app`'s
   old `Fetch`/`Pull` are now deleted in Task 4, with `ops.Pull`'s arity
   change.
6. **Task 4 Step 1 mandated a compile error** — the unused `merge` import in
   `ops_test.go` is gone; `slices` and `errors` are imported instead.
7. **Task 12 changed only MergeView's labels** — `abortMerge`'s dialog,
   `commitWarning` and the sidebar's Fetch/Pull now follow the kind too, and
   the wording lives in tested pure functions in `merge.ts` (Task 9) rather
   than inline in a component nothing tests.
8. **A stash conflict was a dead end** — Task 12 gives it "Done" (plus
   "Drop stash" when a conflicted Pop still owes one, read from the new
   `App.OwedStashDrop`), and a dismissed stash conflict gives the screen
   back to the Changes view, with "Resolve conflicts" in the toolbar as the
   way in again.

The non-blocking findings are fixed too: one Fetch/Pull instead of two
(Task 11), no dead `gitSettings` exports, `TestPullThroughTheAppLayer` on a
temp settings file, a named branch instead of `onto 5e6df51`, and
`ErrNothingToStash` instead of a silent no-op. `GetStashDiff` stays, ships
unused, and Task 8 says so explicitly.

### The four questions the audit asked the plan to answer

- **A conflicted cherry-pick, revert or `am`** gets its own `Kind`, and
  Continue/Abort run that command's own `--continue`/`--abort`. Detected
  before the markerless stash case; `am` is detected but has no automated
  test (see Task 1) and is verified by hand in Task 13.
- **Pull does not try to tell its own conflict from a pre-existing one**: it
  refuses up front with `ops.ErrResolutionInProgress` when anything is
  already unresolved, so a `Merging` state afterwards is unambiguously its
  own doing.
- **Task 10's two deferrals are resolved in place**: `.icon-btn` is global
  (`theme.css:88`), so the toolbar scopes its badge anchor with
  `:global()`; `SettingsDialog` loads `git` inside its existing reactive
  `load()` and saves on change, as a second settings object independent of
  the AI one.
- **The sidebar's per-row Fetch/Pull buttons go**, replaced by the toolbar
  (which has the badges and the conflict guard they lacked); the repo
  context menu keeps both entries, since it works on a repository that is
  not selected, and both now call the same actions the toolbar does.

## Global Constraints

- `internal/ops.Fetch` and its Wails binding already exist and are unchanged by this plan. `internal/ops.Pull`'s signature changes from `Pull(ctx, dir) error` to `Pull(ctx, dir, strategy) (Result, error)`; `ErrNotFastForward` and `TestPullRefusesDivergedHistory` are removed.
- `internal/merge.State` gains `Kind`, `Step`, `Total`, `Subject`. `Merging` stays `true` for every `Kind` so existing callers (`App.svelte`'s `showChanges`, `worktree.State.Merging`) need no change. `Stage`, `Unstage` and `Take` keep their exact signatures and behaviour — every existing merge test must still pass unmodified.
- `git rebase --continue` (and the `cherry-pick`/`revert`/`am` equivalents) must run with `GIT_EDITOR=true` **in the environment**, not `-c core.editor=true`: a `GIT_EDITOR` inherited from the user's shell beats `core.editor`, so the `-c` form can still open an editor and hang the app. `gitcmd.Run` has no env parameter today — Task 0 adds one.
- `internal/merge.Kind` covers every conflict git can leave behind, not only the three the spec named: `merge`, `rebase`, `cherry-pick`, `revert`, `am`, `stash`. A conflicted cherry-pick looks exactly like a conflicted stash pop to a naive "no MERGE_HEAD, no rebase dir, unmerged entries" test, and `git am` uses `.git/rebase-apply`, which a naive rebase test would abort with `rebase --abort`. Detection order and the per-kind `Continue`/`Abort` subcommand are pinned in Task 1 and Task 2.
- A new package is named `internal/gitsettings` (package `gitsettings`), not `internal/settings` — `internal/ai/settings` already uses the package name `settings`, and a same-named sibling would force an import alias everywhere both are used.
- **This repository has no git remote.** Nothing in this plan may run `git push`/`git pull` against a real remote, and the manual pass in Task 13 builds its own scratch bare repository plus two clones under the scratchpad. Every remote test uses `testrepo.NewBareFrom`/`testrepo.Clone` (both already exist).
- Every mutation still runs under the existing per-repo write lock (`a.write` / `a.writeMerge` / `a.writeWorktree`). Fetch, Push and Pull use `gitcmd.NetworkTimeout` (5 minutes); nothing else changes timeout.
- A conflicted `Pull`, `StashApply` or `StashPop` is not a Go `error` at the app-layer boundary — the same convention `merge.Start` already uses for a conflicted merge. Only a real failure (network, auth, a dirty worktree) is an `error`.
- Go: `go vet ./... && go test ./...` and `gofmt -l internal` clean.
- Frontend: prefix every command with `export PATH=$HOME/.nvm/versions/node/v22.23.1/bin:$PATH` (the default shell node is v14 and fails). `npm run check` must end with 0 ERRORS; the 3 pre-existing a11y warnings in ContextMenu/Splitter/Sidebar are not this plan's to fix.
- Never commit `frontend/wailsjs/runtime`; `make build` restores it.
- Commit messages must NOT contain `Co-Authored-By` lines.

---

### Task 0: `gitcmd` — an environment override for one command

**Files:**
- Modify: `internal/gitcmd/gitcmd.go`
- Modify: `internal/gitcmd/gitcmd_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  ```go
  func RunEnv(ctx context.Context, dir string, timeout time.Duration, env []string, args ...string) (string, error)
  func Run(ctx context.Context, dir string, timeout time.Duration, args ...string) (string, error) // unchanged signature, now a RunEnv(nil, ...) wrapper
  ```

Every `--continue` in Task 2 runs git commands that may open an editor. `-c
core.editor=true` does **not** stop that: git resolves the editor as
`GIT_EDITOR` → `core.editor` → `EDITOR` → `vi`, so a developer with
`GIT_EDITOR` exported (or the app launched from such a shell) still gets a
blocking editor, and `gitcmd.Run` — which builds its own `cmd.Env` from
`os.Environ()` — has no way to override it.

- [ ] **Step 1: Write the failing test**

Append to `internal/gitcmd/gitcmd_test.go` (match the file's existing package
and helper conventions — read it first):

```go
// RunEnv's entries win over the inherited environment, which is what makes
// GIT_EDITOR=true reliable for the --continue calls in internal/merge.
func TestRunEnvOverridesTheInheritedEnvironment(t *testing.T) {
	t.Setenv("GIT_EDITOR", "false")
	r := testrepo.New(t)
	r.Commit("base")

	out, err := gitcmd.RunEnv(context.Background(), r.Dir, gitcmd.ReadTimeout,
		[]string{"GIT_EDITOR=true"}, "var", "GIT_EDITOR")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "true" {
		t.Errorf("GIT_EDITOR = %q, want true — the override did not win", strings.TrimSpace(out))
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/gitcmd/ -run RunEnv`
Expected: FAIL — `gitcmd.RunEnv` undefined.

- [ ] **Step 3: Write the implementation**

In `internal/gitcmd/gitcmd.go`, rename the body of `Run` to `RunEnv` and add
the env parameter; keep `Run` as the one-line wrapper so no existing caller
changes:

```go
// Run executes git with args in dir and returns stdout. Prompts are disabled
// so missing credentials fail instead of hanging, and output is in English so
// callers can match messages.
func Run(ctx context.Context, dir string, timeout time.Duration, args ...string) (string, error) {
	return RunEnv(ctx, dir, timeout, nil, args...)
}

// RunEnv is Run with extra environment entries appended last, so they beat
// anything inherited from the user's shell — GIT_EDITOR=true for a
// --continue that must never open an editor, above all.
func RunEnv(ctx context.Context, dir string, timeout time.Duration, env []string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	cmd.Env = append(cmd.Env, env...)
	// ... the rest of the existing body, unchanged ...
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/gitcmd/ -v`
Expected: PASS for every test in the package.

- [ ] **Step 5: Commit**

```bash
git add internal/gitcmd
git commit -m "feat(gitcmd): RunEnv, for a command that must override the inherited environment"
```

---

### Task 1: Generalize the conflict view — `merge.State.Kind`

**Files:**
- Modify: `internal/merge/state.go`
- Modify: `internal/merge/state_test.go`

**Interfaces:**
- Consumes: nothing new — `unmergedEntries`, `attrsCallForManual`, `changedPaths`, `mergeTouched`, `mergeFrom` (all existing, unchanged).
- Produces:
  ```go
  type Kind string
  const (
      KindMerge      Kind = "merge"
      KindRebase     Kind = "rebase"
      KindCherryPick Kind = "cherry-pick"
      KindRevert     Kind = "revert"
      KindAM         Kind = "am"
      KindStash      Kind = "stash"
  )
  // State gains:
  //   Kind    Kind   `json:"kind"`
  //   Step    int    `json:"step,omitempty"`
  //   Total   int    `json:"total,omitempty"`
  //   Subject string `json:"subject,omitempty"`
  func Status(ctx context.Context, dir string) (State, error) // same signature, wider detection
  ```

- [ ] **Step 1: Write the failing tests**

`internal/merge`'s test files are all `package merge` (the internal test package, not `merge_test`) — every existing test calls `Status`/`Start`/`Abort` unqualified, uses `context.Background()` inline, and `stage_test.go` already defines a shared `status(t, dir) State` helper (`t.Fatal`s on error) that every file in the package can use. Follow that convention, not a qualified `merge.` prefix. Append to `internal/merge/state_test.go`, and add `"slices"` to its imports (not there yet — `state_test.go` currently has none):

```go
// A rebase that conflicts is Kind rebase, with step info from git's own
// rebase-merge bookkeeping, not Kind merge — MERGE_HEAD never exists here.
func TestStatusReportsARebaseInProgress(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("switch", "-q", "-c", "feature")
	r.WriteFile("a.txt", "feature change\n")
	r.Git("commit", "-q", "-am", "feature change")
	r.Git("switch", "-q", "main")
	r.WriteFile("a.txt", "main change\n")
	r.Git("commit", "-q", "-am", "main change")
	r.Git("switch", "-q", "feature")
	r.GitFails("rebase", "main")

	st := status(t, r.Dir)
	if st.Kind != KindRebase || !st.Merging {
		t.Fatalf("state = %+v, want Kind rebase and Merging true", st)
	}
	if st.Step != 1 || st.Total != 1 {
		t.Errorf("step/total = %d/%d, want 1/1", st.Step, st.Total)
	}
	if st.Subject != "feature change" {
		t.Errorf("subject = %q", st.Subject)
	}
	if st.From != "feature" {
		t.Errorf("from = %q, want feature", st.From)
	}
	if !slices.Contains(st.Conflicts, "a.txt") {
		t.Errorf("conflicts = %v, want a.txt", st.Conflicts)
	}
}

// A stash pop that conflicts has no MERGE_HEAD and no rebase directory —
// unmerged entries alone are the signal.
func TestStatusReportsAStashConflict(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile("a.txt", "one\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-m", "base")
	r.WriteFile("a.txt", "stashed change\n")
	r.Git("stash", "push", "-q", "-m", "wip")
	r.WriteFile("a.txt", "conflicting change\n")
	r.Git("commit", "-q", "-am", "conflicting change")
	r.GitFails("stash", "pop")

	st := status(t, r.Dir)
	if st.Kind != KindStash || !st.Merging {
		t.Fatalf("state = %+v, want Kind stash and Merging true", st)
	}
	if !slices.Contains(st.Conflicts, "a.txt") {
		t.Errorf("conflicts = %v, want a.txt", st.Conflicts)
	}
}
```

```go
// A conflicted cherry-pick has unmerged entries and no MERGE_HEAD, exactly
// like a conflicted stash pop — CHERRY_PICK_HEAD is the only thing telling
// them apart, and getting this wrong makes Continue/Abort silent no-ops on
// a repository the user cannot then finish or abort from the app.
func TestStatusTellsACherryPickApartFromAStashConflict(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("switch", "-q", "-c", "feature")
	r.WriteFile("a.txt", "feature change\n")
	r.Git("commit", "-q", "-am", "feature change")
	r.Git("switch", "-q", "main")
	r.WriteFile("a.txt", "main change\n")
	r.Git("commit", "-q", "-am", "main change")
	r.GitFails("cherry-pick", "feature")

	st := status(t, r.Dir)
	if st.Kind != KindCherryPick || !st.Merging {
		t.Fatalf("state = %+v, want Kind cherry-pick and Merging true", st)
	}
	if st.Subject != "feature change" {
		t.Errorf("subject = %q, want the picked commit's subject", st.Subject)
	}
	if !slices.Contains(st.Conflicts, "a.txt") {
		t.Errorf("conflicts = %v, want a.txt", st.Conflicts)
	}
}

// A conflicted revert is the same shape, under REVERT_HEAD.
func TestStatusReportsARevertInProgress(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile("a.txt", "one\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-m", "base")
	r.WriteFile("a.txt", "two\n")
	r.Git("commit", "-q", "-am", "second")
	r.WriteFile("a.txt", "three\n")
	r.Git("commit", "-q", "-am", "third")
	r.GitFails("revert", "--no-edit", "HEAD~1")

	st := status(t, r.Dir)
	if st.Kind != KindRevert || !st.Merging {
		t.Fatalf("state = %+v, want Kind revert and Merging true", st)
	}
}
```

`TestStatusWhenNotMerging` already pins the zero-Kind, non-merging case — no new test needed for it.

There is no automated test for `KindAM`: building a conflicted `git am` needs a
mailbox patch file and is slow and brittle. It is detected by
`.git/rebase-apply/applying` (Step 3) and verified by hand in Task 13 only —
what matters is that it is *not* mistaken for a rebase, because `rebase
--abort` on an `am` is destructive.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/merge/ -run 'Rebase|StashConflict' -v`
Expected: FAIL — `merge.KindRebase`/`merge.KindStash` undefined.

- [ ] **Step 3: Write the implementation**

In `internal/merge/state.go`, add near the top (after the imports, before `State`):

```go
import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"git-ui/internal/gitcmd"
	"git-ui/internal/refs"
)

// Kind says what the repository is in the middle of resolving. Every value
// but KindStash has a git-level marker; KindStash is the leftover case —
// unmerged entries with no marker at all, which only a stash apply/pop
// leaves behind.
type Kind string

const (
	KindMerge      Kind = "merge"
	KindRebase     Kind = "rebase"
	KindCherryPick Kind = "cherry-pick"
	KindRevert     Kind = "revert"
	KindAM         Kind = "am"
	KindStash      Kind = "stash"
)
```

Add `Kind`, `Step`, `Total`, `Subject` to `State`:

```go
type State struct {
	Kind      Kind     `json:"kind"`
	Merging   bool     `json:"merging"`
	From      string   `json:"from"`
	Into      string   `json:"into"`
	Conflicts []string `json:"conflicts"`
	Manual    []string `json:"manual"`
	Staged    []string `json:"staged"`
	Unstaged  []string `json:"unstaged"`
	// Step and Total are set only for KindRebase: "commit 2 of 5". Subject
	// is the commit currently being replayed.
	Step    int    `json:"step,omitempty"`
	Total   int    `json:"total,omitempty"`
	Subject string `json:"subject,omitempty"`
}
```

Replace `Status` (keep everything from `entries, err := unmergedEntries(...)` onward as it already reads, except where noted):

```go
// Status reports whether dir is in the middle of a merge, a rebase, or a
// conflicted stash apply/pop, and what is still unresolved. A repository
// with none of the three yields the zero State and no error.
func Status(ctx context.Context, dir string) (State, error) {
	st := State{Conflicts: []string{}, Manual: []string{}, Staged: []string{}, Unstaged: []string{}}

	entries, err := unmergedEntries(ctx, dir)
	if err != nil {
		return State{}, err
	}

	// Detection order matters, and the cheap-looking "no MERGE_HEAD, no
	// rebase directory, unmerged entries" shortcut is wrong: a conflicted
	// cherry-pick or revert matches it exactly, and `git am` writes to
	// .git/rebase-apply, so treating that directory as a rebase would make
	// Abort run `rebase --abort` on an am. Sequencer heads first, then
	// MERGE_HEAD, then the two rebase directories (am told apart by its own
	// "applying" file), and only then the markerless stash case.
	switch {
	case pickedCommit(ctx, dir, "CHERRY_PICK_HEAD") != "":
		st.Kind = KindCherryPick
		st.Into = refs.CurrentLabel(ctx, dir)
		st.From, st.Subject = sequencerFromInto(ctx, dir, "CHERRY_PICK_HEAD")
	case pickedCommit(ctx, dir, "REVERT_HEAD") != "":
		st.Kind = KindRevert
		st.Into = refs.CurrentLabel(ctx, dir)
		st.From, st.Subject = sequencerFromInto(ctx, dir, "REVERT_HEAD")
	case hasMergeHead(ctx, dir):
		st.Kind = KindMerge
		st.Into = refs.CurrentLabel(ctx, dir)
		st.From = mergeFrom(ctx, dir)
	default:
		if rebaseDir, ok := inRebase(ctx, dir); ok {
			if isApplyingMailbox(rebaseDir) {
				st.Kind = KindAM
				st.Into = refs.CurrentLabel(ctx, dir)
				st.Subject = amSubject(rebaseDir)
			} else {
				st.Kind = KindRebase
				st.From, st.Into = rebaseFromInto(ctx, dir, rebaseDir)
				st.Step, st.Total, st.Subject = rebaseStepInfo(ctx, dir, rebaseDir)
			}
		} else if len(entries) > 0 {
			st.Kind = KindStash
		}
	}
	if st.Kind == "" {
		return st, nil
	}
	st.Merging = true

	paths := make([]string, 0, len(entries))
	for path := range entries {
		paths = append(paths, path)
	}
	manualByAttrs, err := attrsCallForManual(ctx, dir, paths)
	if err != nil {
		return State{}, err
	}
	for path, e := range entries {
		if e.needsHuman() || !isText(filepath.Join(dir, path)) || manualByAttrs[path] {
			st.Manual = append(st.Manual, path)
			continue
		}
		st.Conflicts = append(st.Conflicts, path)
	}
	sort.Strings(st.Conflicts)
	sort.Strings(st.Manual)

	if st.Kind != KindMerge {
		// Only a merge has an "incoming side" (mergeTouched) to filter an
		// unrelated dirty file by; for every other kind every settled path
		// here is the conflict's own.
		if st.Staged, err = changedPaths(ctx, dir, entries, "--cached", "HEAD"); err != nil {
			return State{}, err
		}
		if st.Unstaged, err = changedPaths(ctx, dir, entries); err != nil {
			return State{}, err
		}
		return st, nil
	}

	if st.Staged, err = changedPaths(ctx, dir, entries, "--cached", "HEAD"); err != nil {
		return State{}, err
	}
	touched, err := mergeTouched(ctx, dir)
	if err != nil {
		return State{}, err
	}
	dirty, err := changedPaths(ctx, dir, entries)
	if err != nil {
		return State{}, err
	}
	for _, p := range dirty {
		if touched[p] {
			st.Unstaged = append(st.Unstaged, p)
		}
	}
	return st, nil
}

// pickedCommit returns the full hash CHERRY_PICK_HEAD or REVERT_HEAD points
// at, or "" when that pseudo-ref does not exist.
func pickedCommit(ctx context.Context, dir, ref string) string {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", ref)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// sequencerFromInto labels a cherry-pick or a revert by the commit it is
// replaying: From is its short hash (there is no branch to name), Subject
// its message's first line.
func sequencerFromInto(ctx context.Context, dir, ref string) (from, subject string) {
	if out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "log", "-1", "--format=%h%x00%s", ref); err == nil {
		fields := strings.SplitN(strings.TrimSpace(out), "\x00", 2)
		if len(fields) == 2 {
			return fields[0], fields[1]
		}
	}
	return "", ""
}

// isApplyingMailbox reports whether a rebase-apply directory belongs to
// `git am` rather than to an old-backend rebase: am writes an "applying"
// file there, a rebase does not.
func isApplyingMailbox(rebaseDir string) bool {
	if filepath.Base(rebaseDir) != "rebase-apply" {
		return false
	}
	_, err := os.Stat(filepath.Join(rebaseDir, "applying"))
	return err == nil
}

// amSubject is the first line of the patch being applied, when git left one
// where it can be read.
func amSubject(rebaseDir string) string {
	data, err := os.ReadFile(filepath.Join(rebaseDir, "msg-clean"))
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(data)), "\n")
	return line
}

// hasMergeHead reports whether a merge is in progress.
func hasMergeHead(ctx context.Context, dir string) bool {
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", "MERGE_HEAD")
	return err == nil
}

// inRebase reports whether a rebase is in progress and the absolute path of
// its bookkeeping directory — "rebase-merge" for the merge backend (a plain
// `git rebase` since git 2.26), or "rebase-apply" for the older one.
func inRebase(ctx context.Context, dir string) (string, bool) {
	for _, name := range []string{"rebase-merge", "rebase-apply"} {
		out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--git-path", name)
		if err != nil {
			continue
		}
		p := strings.TrimSpace(out)
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		if info, statErr := os.Stat(p); statErr == nil && info.IsDir() {
			return p, true
		}
	}
	return "", false
}

// rebaseFromInto reads the branch being rebased and the commit it is being
// replayed onto. Both backends write head-name and onto.
func rebaseFromInto(ctx context.Context, dir, rebaseDir string) (from, into string) {
	if data, err := os.ReadFile(filepath.Join(rebaseDir, "head-name")); err == nil {
		from = strings.TrimPrefix(strings.TrimSpace(string(data)), "refs/heads/")
	}
	if data, err := os.ReadFile(filepath.Join(rebaseDir, "onto")); err == nil {
		onto := strings.TrimSpace(string(data))
		// "Rebasing feature onto 5e6df51" tells the user nothing; name the
		// branch that commit is on when there is one, and fall back to the
		// short hash when there isn't (a detached onto, or a commit no
		// branch contains any more).
		if out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "name-rev", "--name-only", "--no-undefined", "--refs=refs/heads/*", onto); err == nil {
			into = strings.TrimSpace(out)
		} else if out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--short", onto); err == nil {
			into = strings.TrimSpace(out)
		}
	}
	return from, into
}

// rebaseStepInfo reads "commit N of M" and the replayed commit's subject.
// Only the merge backend (rebase-merge) writes msgnum/end in a form this
// reads; the older apply backend is left at the zero values rather than
// guessed at.
func rebaseStepInfo(ctx context.Context, dir, rebaseDir string) (step, total int, subject string) {
	if filepath.Base(rebaseDir) != "rebase-merge" {
		return 0, 0, ""
	}
	step = readIntFile(filepath.Join(rebaseDir, "msgnum"))
	total = readIntFile(filepath.Join(rebaseDir, "end"))
	if out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "log", "-1", "--format=%s", "REBASE_HEAD"); err == nil {
		subject = strings.TrimSpace(out)
	}
	return step, total, subject
}

func readIntFile(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return n
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/merge/ -v`
Expected: PASS for every test in the package, including every pre-existing one — `TestStatusReportsAMergeInProgress` and everything Task 1/2 of 2a wrote must be unaffected.

- [ ] **Step 5: Commit**

```bash
git add internal/merge
git commit -m "feat(merge): detect a rebase or a stash conflict, not only a merge"
```

---

### Task 2: `Continue` and a kind-aware `Abort`

**Files:**
- Modify: `internal/merge/merge.go`
- Modify: `internal/merge/merge_test.go`

**Interfaces:**
- Consumes: `Status`, `Kind` from Task 1.
- Produces:
  ```go
  func Continue(ctx context.Context, dir string) error
  func Abort(ctx context.Context, dir string) error // same signature, now kind-aware
  ```

- [ ] **Step 1: Write the failing tests**

Append to `internal/merge/merge_test.go`, which already defines `conflicting(t) *testrepo.Repo` (a repo with an unresolved merge waiting to `Start`) and already imports `context`, `strings` and `testrepo` — this is the same `package merge` convention as Task 1, unqualified names and `context.Background()`:

```go
// Continue on a plain merge behaves exactly as Commit did.
func TestContinueClosesAMergeLikeCommit(t *testing.T) {
	r := conflicting(t)
	if _, err := Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	r.WriteFile("greeting.txt", "hi there\n")
	r.Git("add", "greeting.txt")
	if err := Continue(context.Background(), r.Dir); err != nil {
		t.Fatal(err)
	}
	if st := status(t, r.Dir); st.Merging {
		t.Fatalf("state = %+v, want the merge closed", st)
	}
	parents := r.Git("rev-list", "--parents", "-n", "1", "HEAD")
	if len(strings.Fields(parents)) != 3 {
		t.Errorf("want two parents, got %q", parents)
	}
}

// Continue on a rebase runs rebase --continue; resolving the only conflict
// finishes it and the branch ends up on top of main, not merged into it.
func TestContinueFinishesARebase(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("switch", "-q", "-c", "feature")
	r.WriteFile("a.txt", "feature change\n")
	r.Git("commit", "-q", "-am", "feature change")
	r.Git("switch", "-q", "main")
	r.WriteFile("a.txt", "main change\n")
	r.Git("commit", "-q", "-am", "main change")
	r.Git("switch", "-q", "feature")
	r.GitFails("rebase", "main")

	r.WriteFile("a.txt", "resolved\n")
	r.Git("add", "a.txt")
	if err := Continue(context.Background(), r.Dir); err != nil {
		t.Fatal(err)
	}
	if st := status(t, r.Dir); st.Merging {
		t.Fatalf("state = %+v, want the rebase finished", st)
	}
	if got := r.Git("log", "-1", "--format=%P"); strings.Contains(got, " ") {
		t.Errorf("HEAD has more than one parent: %q, want a rebase, not a merge commit", got)
	}
}

// Abort on a rebase restores the branch to where it was.
func TestAbortStopsARebase(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("switch", "-q", "-c", "feature")
	before := r.Git("rev-parse", "HEAD")
	r.WriteFile("a.txt", "feature change\n")
	r.Git("commit", "-q", "-am", "feature change")
	r.Git("switch", "-q", "main")
	r.WriteFile("a.txt", "main change\n")
	r.Git("commit", "-q", "-am", "main change")
	r.Git("switch", "-q", "feature")
	r.GitFails("rebase", "main")

	if err := Abort(context.Background(), r.Dir); err != nil {
		t.Fatal(err)
	}
	if st := status(t, r.Dir); st.Merging {
		t.Fatalf("state = %+v, want the rebase aborted", st)
	}
	if got := r.Git("rev-parse", "HEAD"); got == before {
		t.Errorf("HEAD = %s, want the feature commit still on top, not the pre-rebase base", got)
	}
}

// A cherry-pick is continued and aborted by its own subcommand. Without
// this, the "no MERGE_HEAD, no rebase dir" shortcut would call it a stash
// conflict and both buttons would be silent no-ops.
func TestContinueAndAbortWorkOnACherryPick(t *testing.T) {
	pick := func(t *testing.T) *testrepo.Repo {
		t.Helper()
		r := testrepo.New(t)
		r.Commit("base")
		r.Git("switch", "-q", "-c", "feature")
		r.WriteFile("a.txt", "feature change\n")
		r.Git("commit", "-q", "-am", "feature change")
		r.Git("switch", "-q", "main")
		r.WriteFile("a.txt", "main change\n")
		r.Git("commit", "-q", "-am", "main change")
		r.GitFails("cherry-pick", "feature")
		return r
	}

	t.Run("continue", func(t *testing.T) {
		r := pick(t)
		r.WriteFile("a.txt", "resolved\n")
		r.Git("add", "a.txt")
		// This is the call that hangs forever if the editor is not
		// overridden through the environment: a cherry-pick --continue
		// opens one for the commit message.
		if err := Continue(context.Background(), r.Dir); err != nil {
			t.Fatal(err)
		}
		if st := status(t, r.Dir); st.Merging {
			t.Fatalf("state = %+v, want the cherry-pick finished", st)
		}
	})

	t.Run("abort", func(t *testing.T) {
		r := pick(t)
		before := r.Git("rev-parse", "HEAD")
		if err := Abort(context.Background(), r.Dir); err != nil {
			t.Fatal(err)
		}
		if st := status(t, r.Dir); st.Merging {
			t.Fatalf("state = %+v, want the cherry-pick aborted", st)
		}
		if got := r.Git("rev-parse", "HEAD"); got != before {
			t.Errorf("HEAD = %s, want %s — abort must not move the branch", got, before)
		}
	})
}

// Continue and Abort on a stash conflict do nothing at the git level — there
// is nothing to continue or abort, only files to resolve or leave.
func TestContinueAndAbortOnAStashConflictAreNoOps(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile("a.txt", "one\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-m", "base")
	r.WriteFile("a.txt", "stashed\n")
	r.Git("stash", "push", "-q", "-m", "wip")
	r.WriteFile("a.txt", "conflicting\n")
	r.Git("commit", "-q", "-am", "conflicting")
	r.GitFails("stash", "pop")

	if err := Continue(context.Background(), r.Dir); err != nil {
		t.Fatal(err)
	}
	if err := Abort(context.Background(), r.Dir); err != nil {
		t.Fatal(err)
	}
	if st := status(t, r.Dir); st.Kind != KindStash {
		t.Fatalf("state = %+v, want the stash conflict untouched", st)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/merge/ -run 'Continue|AbortStopsARebase|CherryPick'`
Expected: FAIL — `merge.Continue` is undefined.

- [ ] **Step 3: Write the implementation**

In `internal/merge/merge.go`, replace `Abort` and add `Continue` next to it:

```go
// Continue moves the conflict resolution in progress one step forward: a
// merge closes with git's own generated message, exactly as Commit did; a
// rebase, a cherry-pick, a revert or an `am` run their own --continue and
// may leave the next commit's conflicts behind — the caller re-reads Status
// to see. A stash conflict has no "continue" step; this is a no-op so a
// stray call from the UI never errors.
func Continue(ctx context.Context, dir string) error {
	st, err := Status(ctx, dir)
	if err != nil {
		return err
	}
	if st.Kind == KindMerge {
		return Commit(ctx, dir)
	}
	cmd, ok := sequencer[st.Kind]
	if !ok {
		return nil // KindStash, or nothing in progress
	}
	// GIT_EDITOR through the environment, not -c core.editor: git resolves
	// GIT_EDITOR first, so a value inherited from the user's shell would
	// beat core.editor and open a real editor the app can never close.
	// noEditor also covers hooks, which is why this uses HookTimeout.
	_, err = gitcmd.RunEnv(ctx, dir, gitcmd.HookTimeout, noEditor, cmd, "--continue")
	return err
}

// sequencer maps a Kind to the git subcommand that continues or aborts it.
// KindMerge is not here: its continue is Commit, and its abort is
// `merge --abort`, both handled by name below. KindStash is not here
// either — it has no git-level step at all.
var sequencer = map[Kind]string{
	KindRebase:     "rebase",
	KindCherryPick: "cherry-pick",
	KindRevert:     "revert",
	KindAM:         "am",
}

// noEditor stops any --continue from opening an editor the app cannot close.
var noEditor = []string{"GIT_EDITOR=true"}

// Abort undoes the conflict resolution in progress: every kind but a stash
// has a real git-level abort, and each must get its own — `rebase --abort`
// on a `git am` (which also lives in .git/rebase-apply) throws away the
// mailbox. A stash conflict has nothing to undo but the files themselves,
// which Discard already handles, so it is a no-op rather than an error.
func Abort(ctx context.Context, dir string) error {
	st, err := Status(ctx, dir)
	if err != nil {
		return err
	}
	if st.Kind == KindMerge {
		_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "merge", "--abort")
		return err
	}
	cmd, ok := sequencer[st.Kind]
	if !ok {
		return nil // KindStash, or nothing in progress
	}
	_, err = gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, cmd, "--abort")
	return err
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/merge/ -v`
Expected: PASS for every test, including the pre-existing `Abort`/`Commit` tests — `Abort`'s behaviour for `KindMerge` is unchanged.

- [ ] **Step 5: Commit**

```bash
git add internal/merge
git commit -m "feat(merge): Continue and a kind-aware Abort for rebase and stash"
```

---

### Task 3: Wire the app layer to `Continue`, and prove the AI resolver stays merge-only

**Files:**
- Modify: `internal/app/merge.go`
- Modify: `internal/app/merge_test.go`

**Interfaces:**
- Consumes: `merge.Continue` from Task 2.
- Produces: no new exported surface — `CommitMerge`'s body changes from `merge.Commit` to `merge.Continue`.

- [ ] **Step 1: Write the failing test**

Append to `internal/app/merge_test.go`:

```go
// ResolveConflicts is scoped to a real merge by its own existing MERGE_HEAD
// check (mergeHead returns "" outside a merge); this pins that it stays
// refused once nothing is merging, now that Status also reports a rebase or
// a stash conflict as Merging true — AbortMerge (kind-aware since Task 2)
// is used here specifically to leave the repository in a clean state
// regardless of whatever newAIMergeApp's own conflicted history started it
// in, without this test needing to know that shape itself.
func TestResolveConflictsRefusesOutsideARealMerge(t *testing.T) {
	a, _, id, _ := newAIMergeApp(t, "http://127.0.0.1:0")
	_ = a.AbortMerge(id)

	if err := a.ResolveConflicts(id, "run1"); err == nil {
		t.Error("want a refusal outside a merge")
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./internal/app/ -run ResolveConflictsRefusesOutsideARealMerge -v`
Expected: PASS already — this step is a regression pin, not a new behaviour; `mergeHead` already returns `""` for anything that is not `KindMerge`. If it fails, stop and re-check Task 1's `hasMergeHead` before touching anything else.

- [ ] **Step 3: Write the implementation**

In `internal/app/merge.go`, change `CommitMerge`:

```go
// CommitMerge stops any agent run on the repository first, as AbortMerge
// does, then advances whatever conflict resolution is in progress: a merge
// commits, a rebase continues (and may leave the next commit's conflicts
// for the view to show), a stash conflict does nothing.
func (a *App) CommitMerge(id string) error {
	a.stopRun(id)
	return a.write(id, func(ctx context.Context, dir string) error { return merge.Continue(ctx, dir) })
}
```

`AbortMerge` is unchanged — `merge.Abort` is already kind-aware from Task 2.

- [ ] **Step 4: Run the whole Go suite**

Run: `go vet ./... && go test ./...`
Expected: everything passes.

- [ ] **Step 5: Commit**

```bash
git add internal/app
git commit -m "feat(app): CommitMerge advances a rebase too, not only a merge"
```

---

### Task 4: `internal/ops` — Push, Counts, and a real Pull

**Files:**
- Modify: `internal/ops/ops.go`
- Modify: `internal/ops/ops_test.go`
- Modify: `internal/app/app.go` (delete the old `Fetch`/`Pull` methods **in this task**)

`ops.Pull`'s arity changes here, and `internal/app/app.go` is its only
caller. Deleting that caller in a later task would leave the tree
non-building for two commits, so the deletion belongs here — Task 6 adds the
new `App.Fetch`/`App.Push`/`App.Pull` in `internal/app/remote.go`. Between
the two commits the Wails bindings still carry the old `Pull`, which nothing
regenerates until Task 6; no Go build or test touches them, so the tree stays
green.

**Interfaces:**
- Consumes: `merge.Status`, `merge.Kind` from Task 1.
- Produces:
  ```go
  const (
      StrategyAuto   = "auto"
      StrategyMerge  = "merge"
      StrategyRebase = "rebase"
  )
  var ErrInvalidStrategy = errors.New("ops: invalid pull strategy")
  var ErrResolutionInProgress = errors.New("ops: finish the conflict in progress first")
  type Outcome int
  const (
      UpToDate Outcome = iota
      Merged
      Rebased
      Conflicted
  )
  type Result struct {
      Outcome   Outcome  `json:"outcome"`
      Conflicts []string `json:"conflicts"`
  }
  type AheadBehind struct {
      Ahead  int `json:"ahead"`
      Behind int `json:"behind"`
  }
  func Push(ctx context.Context, dir string) error
  func Pull(ctx context.Context, dir, strategy string) (Result, error) // replaces the old Pull(ctx, dir) error
  func Counts(ctx context.Context, dir string) (AheadBehind, error)
  ```

- [ ] **Step 1: Write the failing tests**

Replace `TestFetchAndPullFastForward` and delete `TestPullRefusesDivergedHistory` in `internal/ops/ops_test.go`, then append the new tests:

```go
func TestFetchAndPullFastForward(t *testing.T) {
	a, b := clones(t)
	h := b.Commit("from b")
	b.Git("push", "-q", "origin", "main")

	if err := ops.Fetch(ctx, a.Dir); err != nil {
		t.Fatal(err)
	}
	if got := a.Git("rev-parse", "origin/main"); got != h {
		t.Fatalf("origin/main = %s, want %s", got, h)
	}
	result, err := ops.Pull(ctx, a.Dir, ops.StrategyAuto)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != ops.Merged {
		t.Errorf("outcome = %v, want Merged", result.Outcome)
	}
	if got := a.Git("rev-parse", "HEAD"); got != h {
		t.Fatalf("HEAD = %s, want %s", got, h)
	}
}

func TestPullReportsUpToDate(t *testing.T) {
	a, _ := clones(t)
	result, err := ops.Pull(ctx, a.Dir, ops.StrategyAuto)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != ops.UpToDate {
		t.Errorf("outcome = %v, want UpToDate", result.Outcome)
	}
}

func TestPullMergesDivergedHistoryWithMergeStrategy(t *testing.T) {
	a, b := clones(t)
	b.Commit("from b")
	b.Git("push", "-q", "origin", "main")
	a.Commit("local only")

	result, err := ops.Pull(ctx, a.Dir, ops.StrategyMerge)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != ops.Merged {
		t.Fatalf("outcome = %v, want Merged", result.Outcome)
	}
	if got := a.Git("log", "-1", "--format=%P"); !strings.Contains(got, " ") {
		t.Errorf("HEAD parents = %q, want two — a merge commit", got)
	}
}

func TestPullRebasesDivergedHistoryWithRebaseStrategy(t *testing.T) {
	a, b := clones(t)
	b.Commit("from b")
	b.Git("push", "-q", "origin", "main")
	a.Commit("local only")

	result, err := ops.Pull(ctx, a.Dir, ops.StrategyRebase)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != ops.Rebased {
		t.Fatalf("outcome = %v, want Rebased", result.Outcome)
	}
	if got := a.Git("log", "-1", "--format=%P"); strings.Contains(got, " ") {
		t.Errorf("HEAD parents = %q, want one — a rebase, not a merge commit", got)
	}
}

func TestPullConflictLeavesTheRepositoryForTheConflictViewToShow(t *testing.T) {
	a, b := clones(t)
	a.WriteFile("file-1.txt", "a's change\n")
	a.Git("commit", "-q", "-am", "a's change")
	b.WriteFile("file-1.txt", "b's change\n")
	b.Git("commit", "-q", "-am", "b's change")
	b.Git("push", "-q", "origin", "main")

	result, err := ops.Pull(ctx, a.Dir, ops.StrategyMerge)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != ops.Conflicted || !slices.Contains(result.Conflicts, "file-1.txt") {
		t.Fatalf("result = %+v, want Conflicted on file-1.txt", result)
	}
}

func TestPullRefusesWhileAConflictIsUnresolved(t *testing.T) {
	a, b := clones(t)
	a.WriteFile("file-1.txt", "a's change\n")
	a.Git("commit", "-q", "-am", "a's change")
	b.WriteFile("file-1.txt", "b's change\n")
	b.Git("commit", "-q", "-am", "b's change")
	b.Git("push", "-q", "origin", "main")
	if _, err := ops.Pull(ctx, a.Dir, ops.StrategyMerge); err != nil {
		t.Fatal(err)
	}

	// The first pull left a conflict; a second one must refuse rather than
	// report the conflict it did not cause.
	if _, err := ops.Pull(ctx, a.Dir, ops.StrategyMerge); !errors.Is(err, ops.ErrResolutionInProgress) {
		t.Errorf("err = %v, want ErrResolutionInProgress", err)
	}
}

func TestPullRefusesAnUnknownStrategy(t *testing.T) {
	a, _ := clones(t)
	if _, err := ops.Pull(ctx, a.Dir, "sometimes"); !errors.Is(err, ops.ErrInvalidStrategy) {
		t.Errorf("err = %v, want ErrInvalidStrategy", err)
	}
}

func TestPushSetsUpstreamOnFirstPush(t *testing.T) {
	a, _ := clones(t)
	a.Git("switch", "-q", "-c", "topic")
	a.Commit("on topic")

	if err := ops.Push(ctx, a.Dir); err != nil {
		t.Fatal(err)
	}
	if up := a.Git("rev-parse", "--abbrev-ref", "topic@{upstream}"); up != "origin/topic" {
		t.Errorf("upstream = %q, want origin/topic", up)
	}
}

func TestPushToAnExistingUpstream(t *testing.T) {
	a, b := clones(t)
	h := a.Commit("from a")
	if err := ops.Push(ctx, a.Dir); err != nil {
		t.Fatal(err)
	}
	if err := ops.Fetch(ctx, b.Dir); err != nil {
		t.Fatal(err)
	}
	if got := b.Git("rev-parse", "origin/main"); got != h {
		t.Fatalf("origin/main on b = %s, want %s", got, h)
	}
}

func TestCountsAheadAndBehind(t *testing.T) {
	a, b := clones(t)
	a.Commit("local")
	b.Commit("remote")
	b.Git("push", "-q", "origin", "main")
	if err := ops.Fetch(ctx, a.Dir); err != nil {
		t.Fatal(err)
	}

	counts, err := ops.Counts(ctx, a.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Ahead != 1 || counts.Behind != 1 {
		t.Errorf("counts = %+v, want 1 ahead, 1 behind", counts)
	}
}

func TestCountsWithNoUpstream(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	counts, err := ops.Counts(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Ahead != 0 || counts.Behind != 0 {
		t.Errorf("counts = %+v, want zero with no upstream", counts)
	}
}
```

Add `"slices"` and `"errors"` to the test file's imports — `slices.Contains`
and `errors.Is` are used above. Do **not** import `git-ui/internal/merge`
into `ops_test.go`: it is used by `ops.go`, not by these tests, and an unused
import is a compile error.

Also delete `internal/app/app.go`'s `Fetch` and `Pull` methods (lines 266-272)
as part of this task's Step 3, so the tree builds at every commit.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/ops/`
Expected: FAIL — `ops.Push`, `ops.Counts`, `ops.StrategyAuto` etc. undefined; `ops.Pull` called with the wrong arity.

- [ ] **Step 3: Write the implementation**

Replace `internal/ops/ops.go`'s `Fetch`/`Pull` and add the rest:

```go
// Package ops runs checkout, fetch, pull, push and reset.
package ops

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"git-ui/internal/gitcmd"
	"git-ui/internal/merge"
)

var (
	ErrInvalidRef           = errors.New("invalid ref")
	ErrInvalidStrategy      = errors.New("ops: invalid pull strategy")
	ErrResolutionInProgress = errors.New("ops: finish the conflict in progress first")
)

const (
	StrategyAuto   = "auto"
	StrategyMerge  = "merge"
	StrategyRebase = "rebase"
)

// Outcome says how a Pull ended.
type Outcome int

const (
	UpToDate Outcome = iota
	Merged
	Rebased
	Conflicted
)

// Result is what Pull produces; Conflicts is set only when Conflicted.
type Result struct {
	Outcome   Outcome  `json:"outcome"`
	Conflicts []string `json:"conflicts"`
}

// AheadBehind is how far the current branch and its upstream have diverged.
type AheadBehind struct {
	Ahead  int `json:"ahead"`
	Behind int `json:"behind"`
}

func Fetch(ctx context.Context, dir string) error {
	_, err := gitcmd.Run(ctx, dir, gitcmd.NetworkTimeout, "fetch", "--all", "--prune")
	return err
}

// Pull fetches and integrates the upstream. "auto" runs a plain pull, so
// git's own resolved pull.rebase config decides merge or rebase, same as a
// pull typed at a terminal would; "merge"/"rebase" force this call's choice,
// the one-shot -c pattern merge.Start already uses for merge.conflictStyle.
// A conflict is reported as Result, not returned as an error — the same
// distinction merge.Start draws for a conflicted merge.
func Pull(ctx context.Context, dir, strategy string) (Result, error) {
	args := []string{"pull", "--"}
	switch strategy {
	case StrategyAuto:
	case StrategyMerge:
		args = append([]string{"-c", "pull.rebase=false"}, args...)
	case StrategyRebase:
		args = append([]string{"-c", "pull.rebase=true"}, args...)
	default:
		return Result{}, fmt.Errorf("%w: %q", ErrInvalidStrategy, strategy)
	}

	// A repository already mid-merge, mid-rebase or mid-anything cannot be
	// pulled into, and more importantly the conflict-detection below could
	// not tell a conflict this pull caused from one that was already there.
	// Refuse up front instead of guessing afterwards. (The frontend's
	// canSync already disables Pull in that state; this is the backend's own
	// guarantee, for an agent tool call or a race with a finishing merge.)
	if st, stErr := merge.Status(ctx, dir); stErr == nil && st.Merging {
		return Result{}, fmt.Errorf("%w: %s", ErrResolutionInProgress, st.Kind)
	}

	before, err := head(ctx, dir)
	if err != nil {
		return Result{}, err
	}
	_, err = gitcmd.Run(ctx, dir, gitcmd.NetworkTimeout, args...)
	if err == nil {
		after, headErr := head(ctx, dir)
		if headErr != nil {
			return Result{}, headErr
		}
		switch {
		case after == before:
			return Result{Outcome: UpToDate}, nil
		case before == "":
			return Result{Outcome: Merged}, nil // first pull into an empty repository
		default:
			if _, ffErr := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "merge-base", "--is-ancestor", before, after); ffErr == nil {
				return Result{Outcome: Merged}, nil
			}
			return Result{Outcome: Rebased}, nil
		}
	}
	// A conflict leaves the repository mid-merge or mid-rebase. Nothing was
	// in progress before this call (the guard above), so a Merging state
	// here is this pull's own doing. Anything else (network, auth, a dirty
	// worktree) is a real failure and is returned as-is.
	if st, statusErr := merge.Status(ctx, dir); statusErr == nil && st.Merging {
		return Result{Outcome: Conflicted, Conflicts: append(append([]string{}, st.Conflicts...), st.Manual...)}, nil
	}
	return Result{}, err
}

// Push publishes the current branch. One with no upstream yet gets one on
// origin, by convention; an existing upstream is pushed to as-is.
func Push(ctx context.Context, dir string) error {
	if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"); err != nil {
		branch, brErr := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "symbolic-ref", "-q", "--short", "HEAD")
		if brErr != nil {
			return brErr // detached HEAD: nothing to publish
		}
		_, err := gitcmd.Run(ctx, dir, gitcmd.NetworkTimeout, "push", "-u", "origin", "--", strings.TrimSpace(branch))
		return err
	}
	_, err := gitcmd.Run(ctx, dir, gitcmd.NetworkTimeout, "push", "--")
	return err
}

// Counts reports how far the current branch and its upstream have diverged,
// for the toolbar's badge. No upstream — or any other failure reading it —
// yields a zero AheadBehind rather than an error, the same convention
// worktree.Preview uses for its own @{upstream} lookup.
func Counts(ctx context.Context, dir string) (AheadBehind, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-list", "--left-right", "--count", "HEAD...@{upstream}")
	if err != nil {
		return AheadBehind{}, nil
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return AheadBehind{}, nil
	}
	ahead, err1 := strconv.Atoi(fields[0])
	behind, err2 := strconv.Atoi(fields[1])
	if err1 != nil || err2 != nil {
		return AheadBehind{}, nil
	}
	return AheadBehind{Ahead: ahead, Behind: behind}, nil
}

func checkRef(ref string) error {
	if ref == "" || strings.HasPrefix(ref, "-") {
		return fmt.Errorf("%w: %q", ErrInvalidRef, ref)
	}
	return nil
}

// head returns the current commit, or "" in a repository with no commits.
func head(ctx context.Context, dir string) (string, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil {
		var gerr *gitcmd.Error
		if errors.As(err, &gerr) && gerr.ExitCode == 1 {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(out), nil
}
```

Leave `Checkout`, `CheckoutRemote`, `CheckoutDetached` exactly as they are — only the imports, `Fetch`/`Pull`, and everything from `checkRef` onward in this listing changes; `checkRef` itself is unchanged, just repositioned in this listing for clarity.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/ops/ -v`
Expected: PASS for every test in the package.

- [ ] **Step 5: Commit**

```bash
git add internal/ops
git commit -m "feat(ops): push, ahead/behind counts, and a pull that resolves instead of refusing"
```

---

### Task 5: `internal/gitsettings` — the pull-strategy setting

**Files:**
- Create: `internal/gitsettings/gitsettings.go`
- Create: `internal/gitsettings/gitsettings_test.go`

**Interfaces:**
- Consumes: nothing internal to this codebase.
- Produces:
  ```go
  package gitsettings
  type Settings struct {
      PullStrategy string `json:"pullStrategy"`
  }
  const (
      PullAuto   = "auto"
      PullMerge  = "merge"
      PullRebase = "rebase"
  )
  var ErrInvalid = errors.New("gitsettings: invalid settings")
  func Defaults() Settings
  func DefaultPath() (string, error)
  func Load(path string) (Settings, error)
  func Save(path string, s Settings) error
  ```

- [ ] **Step 1: Write the failing test**

Create `internal/gitsettings/gitsettings_test.go`:

```go
package gitsettings_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"git-ui/internal/gitsettings"
)

func TestDefaultsAreAutoStrategy(t *testing.T) {
	if got := gitsettings.Defaults().PullStrategy; got != gitsettings.PullAuto {
		t.Errorf("default = %q, want auto", got)
	}
}

func TestLoadOfAMissingFileReturnsDefaults(t *testing.T) {
	got, err := gitsettings.Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got != gitsettings.Defaults() {
		t.Errorf("got = %+v, want defaults", got)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "git.json")
	want := gitsettings.Settings{PullStrategy: gitsettings.PullRebase}
	if err := gitsettings.Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := gitsettings.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("got = %+v, want %+v", got, want)
	}
}

func TestSaveRejectsAnUnknownStrategy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "git.json")
	err := gitsettings.Save(path, gitsettings.Settings{PullStrategy: "sometimes"})
	if !errors.Is(err, gitsettings.ErrInvalid) {
		t.Errorf("err = %v, want ErrInvalid", err)
	}
}

// A file written before PullStrategy existed, or with it blanked out, must
// still load with the default filled in.
func TestLoadFillsInAMissingStrategy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "git.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := gitsettings.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.PullStrategy != gitsettings.PullAuto {
		t.Errorf("strategy = %q, want auto filled in", got.PullStrategy)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/gitsettings/`
Expected: FAIL — the package does not exist.

- [ ] **Step 3: Write the implementation**

Create `internal/gitsettings/gitsettings.go`:

```go
// Package gitsettings stores app-level git-behaviour preferences — today
// just the pull strategy. Kept apart from internal/ai/settings, which is AI
// configuration only, in its own file so the two never collide.
package gitsettings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	PullAuto   = "auto"
	PullMerge  = "merge"
	PullRebase = "rebase"
)

var ErrInvalid = errors.New("gitsettings: invalid settings")

type Settings struct {
	PullStrategy string `json:"pullStrategy"`
}

func Defaults() Settings {
	return Settings{PullStrategy: PullAuto}
}

func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "git-ui", "git.json"), nil
}

// Load reads the settings file, returning defaults when it doesn't exist,
// and filling in a strategy a file from before this setting existed lacks.
func Load(path string) (Settings, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Defaults(), nil
	}
	if err != nil {
		return Settings{}, err
	}
	s := Defaults()
	if err := json.Unmarshal(data, &s); err != nil {
		return Settings{}, fmt.Errorf("gitsettings: parse %s: %w", path, err)
	}
	if s.PullStrategy == "" {
		s.PullStrategy = PullAuto
	}
	return s, nil
}

func Save(path string, s Settings) error {
	if err := validate(s); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func validate(s Settings) error {
	switch s.PullStrategy {
	case PullAuto, PullMerge, PullRebase:
		return nil
	default:
		return fmt.Errorf("%w: unknown pull strategy %q", ErrInvalid, s.PullStrategy)
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/gitsettings/ -v`
Expected: PASS for every test.

- [ ] **Step 5: Commit**

```bash
git add internal/gitsettings
git commit -m "feat(gitsettings): the pull-strategy setting"
```

---

### Task 6: App layer — Push, Pull, GetRemoteInfo, GetGitSettings/SaveGitSettings

**Files:**
- Modify: `internal/app/app.go` (add the `gitSettingsPath` field; the old `Fetch`/`Pull` were already deleted in Task 4)
- Create: `internal/app/remote.go`
- Create: `internal/app/remote_test.go`

**Interfaces:**
- Consumes: `ops.Fetch/Push/Pull/Counts/Result/AheadBehind` from Task 4; `gitsettings.Load/Save/Settings` from Task 5; `a.dir`, `a.write` from `internal/app/app.go`.
- Produces:
  ```go
  func (a *App) Fetch(id string) error         // moved, unchanged
  func (a *App) Push(id string) error
  func (a *App) Pull(id string) (ops.Result, error) // was: error
  func (a *App) GetRemoteInfo(id string) (ops.AheadBehind, error)
  func (a *App) GetGitSettings() (gitsettings.Settings, error)
  func (a *App) SaveGitSettings(s gitsettings.Settings) error
  ```

- [ ] **Step 1: Write the failing test**

Create `internal/app/remote_test.go`. `newAIMergeApp` (used elsewhere in this package) builds a repository with a conflicting history, which these tests don't want; add a smaller, self-contained helper instead, built directly on `repos.Store` the same way `App.AddRepo` itself does, only without the directory-picker dialog:

```go
package app

import (
	"context"
	"path/filepath"
	"testing"

	"git-ui/internal/gitsettings"
	"git-ui/internal/ops"
	"git-ui/internal/repos"
	"git-ui/internal/testrepo"
)

// newPlainApp builds an App over a fresh one-commit repository, with no
// merge scaffolding — for tests that don't want newAIMergeApp's conflicting
// history.
//
// WithAI is NOT optional here, even though these tests use no AI: App.emit
// falls through to wails runtime.EventsEmit when a.ai is nil, and that
// runtime's getEvents calls log.Fatalf on a context with no "events" value —
// os.Exit(1) in the middle of the suite, taking every other test in
// internal/app with it. Every stash mutation goes through writeMerge or
// writeWorktree, both of which emit. Giving it an events sink (the same
// `events` helper newAIMergeApp uses, defined in ai_test.go) is what keeps
// `go test ./internal/app/` alive. gitSettingsPath is likewise always set to
// a temp file so no test ever reads or writes the developer's real
// ~/Library/Application Support/git-ui/git.json.
func newPlainApp(t *testing.T) (a *App, r *testrepo.Repo, id string) {
	t.Helper()
	dir := t.TempDir()
	store, err := repos.Open(filepath.Join(dir, "repos.json"))
	if err != nil {
		t.Fatal(err)
	}
	a = New(store)
	WithAI(a, AIDeps{Emit: newEvents().emit})
	a.gitSettingsPath = filepath.Join(dir, "git.json")
	r = testrepo.New(t)
	r.Commit("base")
	repo, err := store.Add(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	return a, r, repo.ID
}

func TestPushSetsUpstreamThroughTheAppLayer(t *testing.T) {
	a, r, id := newPlainApp(t)
	bare := testrepo.NewBareFrom(t, r)
	r.Git("remote", "add", "origin", bare)
	r.Git("switch", "-q", "-c", "topic")
	r.Commit("on topic")

	if err := a.Push(id); err != nil {
		t.Fatal(err)
	}
	if up := r.Git("rev-parse", "--abbrev-ref", "topic@{upstream}"); up != "origin/topic" {
		t.Errorf("upstream = %q, want origin/topic", up)
	}
}

func TestPullThroughTheAppLayer(t *testing.T) {
	a, r, id := newPlainApp(t)
	bare := testrepo.NewBareFrom(t, r)
	r.Git("remote", "add", "origin", bare)
	r.Git("push", "-q", "-u", "origin", "main")

	clone := testrepo.Clone(t, bare)
	clone.Commit("from clone")
	clone.Git("push", "-q", "origin", "main")

	result, err := a.Pull(id)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != ops.Merged {
		t.Errorf("outcome = %v, want Merged", result.Outcome)
	}
}

func TestGetRemoteInfoWithNoUpstream(t *testing.T) {
	a, _, id := newPlainApp(t)
	info, err := a.GetRemoteInfo(id)
	if err != nil {
		t.Fatal(err)
	}
	if info.Ahead != 0 || info.Behind != 0 {
		t.Errorf("info = %+v, want zero with no upstream", info)
	}
}

func TestGitSettingsDefaultAndSaveRoundTrip(t *testing.T) {
	a, _, _ := newPlainApp(t) // gitSettingsPath is already a temp file

	got, err := a.GetGitSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.PullStrategy != gitsettings.PullAuto {
		t.Errorf("default = %+v, want auto", got)
	}
	if err := a.SaveGitSettings(gitsettings.Settings{PullStrategy: gitsettings.PullRebase}); err != nil {
		t.Fatal(err)
	}
	got, err = a.GetGitSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.PullStrategy != gitsettings.PullRebase {
		t.Errorf("after save = %+v, want rebase", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/app/ -run 'Push|Pull|RemoteInfo|GitSettings'`
Expected: FAIL — `a.Push`, `a.GetRemoteInfo`, `a.gitSettingsPath` etc. undefined.

- [ ] **Step 3: Write the implementation**

In `internal/app/app.go`, add one field to `App` (the old `Fetch`/`Pull` methods are already gone — Task 4 deleted them):

```go
type App struct {
	ctx    context.Context
	store  *repos.Store
	mu     sync.Mutex
	logs   map[string]*logState
	writes sync.Map // repo ID → *sync.Mutex
	ai     *aiState
	// gitSettingsPath overrides gitsettings.DefaultPath() when set — empty
	// in production, a temp path in tests.
	gitSettingsPath string
}
```

Create `internal/app/remote.go`:

```go
package app

import (
	"context"

	"git-ui/internal/gitsettings"
	"git-ui/internal/ops"
)

func (a *App) Fetch(id string) error {
	return a.write(id, func(ctx context.Context, dir string) error { return ops.Fetch(ctx, dir) })
}

func (a *App) Push(id string) error {
	return a.write(id, func(ctx context.Context, dir string) error { return ops.Push(ctx, dir) })
}

// Pull reads the configured strategy once per call and runs it under the
// write lock; the result (up to date, merged, rebased, or conflicted) goes
// back to the caller the same way MergeBranch's Result does.
func (a *App) Pull(id string) (ops.Result, error) {
	cfg, err := a.gitSettings()
	if err != nil {
		return ops.Result{}, err
	}
	var result ops.Result
	err = a.write(id, func(ctx context.Context, dir string) error {
		var pullErr error
		result, pullErr = ops.Pull(ctx, dir, cfg.PullStrategy)
		return pullErr
	})
	return result, err
}

func (a *App) GetRemoteInfo(id string) (ops.AheadBehind, error) {
	dir, err := a.dir(id)
	if err != nil {
		return ops.AheadBehind{}, err
	}
	return ops.Counts(a.ctx, dir)
}

func (a *App) gitSettingsFile() (string, error) {
	if a.gitSettingsPath != "" {
		return a.gitSettingsPath, nil
	}
	return gitsettings.DefaultPath()
}

func (a *App) gitSettings() (gitsettings.Settings, error) {
	path, err := a.gitSettingsFile()
	if err != nil {
		return gitsettings.Settings{}, err
	}
	return gitsettings.Load(path)
}

func (a *App) GetGitSettings() (gitsettings.Settings, error) { return a.gitSettings() }

func (a *App) SaveGitSettings(s gitsettings.Settings) error {
	path, err := a.gitSettingsFile()
	if err != nil {
		return err
	}
	return gitsettings.Save(path, s)
}
```

- [ ] **Step 4: Run the whole Go suite**

Run: `go vet ./... && go test ./...`
Expected: everything passes.

- [ ] **Step 5: Regenerate the bindings and commit**

```bash
export PATH=$HOME/.nvm/versions/node/v22.23.1/bin:$HOME/go/bin:$PATH
wails generate module
git checkout -- frontend/wailsjs/runtime
grep -n "Push\|GetRemoteInfo\|GetGitSettings" frontend/wailsjs/go/app/App.d.ts
git add internal/app frontend/wailsjs/go
git commit -m "feat(app): push, pull with a strategy, and the git settings"
```

---

### Task 7: `internal/stash`

**Files:**
- Create: `internal/stash/stash.go`
- Create: `internal/stash/stash_test.go`

**Interfaces:**
- Consumes: `internal/gitcmd`.
- Produces:
  ```go
  package stash
  type Entry struct {
      Index   int    `json:"index"`
      Message string `json:"message"`
      Branch  string `json:"branch"`
      Hash    string `json:"hash"`
  }
  var ErrNothingToStash = errors.New("stash: nothing to stash")
  func List(ctx context.Context, dir string) ([]Entry, error)
  func Push(ctx context.Context, dir, message string, includeUntracked bool) error // ErrNothingToStash when the worktree is clean
  func Apply(ctx context.Context, dir string, index int) error
  func Pop(ctx context.Context, dir string, index int) error
  func Drop(ctx context.Context, dir string, index int) error
  func Diff(ctx context.Context, dir string, index int) (string, error)
  ```

- [ ] **Step 1: Write the failing tests**

Create `internal/stash/stash_test.go`:

```go
package stash_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"git-ui/internal/stash"
	"git-ui/internal/testrepo"
)

var ctx = context.Background()

func base(t *testing.T) *testrepo.Repo {
	t.Helper()
	r := testrepo.New(t)
	r.WriteFile("a.txt", "one\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-m", "base")
	return r
}

func TestPushAndListRoundTrip(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "changed\n")

	if err := stash.Push(ctx, r.Dir, "work in progress", false); err != nil {
		t.Fatal(err)
	}
	if got := r.Git("status", "--porcelain"); got != "" {
		t.Errorf("status = %q, want a clean worktree after stashing", got)
	}
	entries, err := stash.List(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Index != 0 {
		t.Fatalf("entries = %+v, want one at index 0", entries)
	}
	if !strings.Contains(entries[0].Message, "work in progress") {
		t.Errorf("message = %q, missing the stash message", entries[0].Message)
	}
	if entries[0].Branch != "main" {
		t.Errorf("branch = %q, want main", entries[0].Branch)
	}
}

func TestPushIncludesUntrackedWhenAsked(t *testing.T) {
	r := base(t)
	r.WriteFile("new.txt", "untracked\n")

	if err := stash.Push(ctx, r.Dir, "with untracked", true); err != nil {
		t.Fatal(err)
	}
	if got := r.Git("status", "--porcelain"); got != "" {
		t.Errorf("status = %q, want the untracked file stashed away too", got)
	}
}

func TestApplyKeepsTheStashEntry(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "changed\n")
	if err := stash.Push(ctx, r.Dir, "wip", false); err != nil {
		t.Fatal(err)
	}
	if err := stash.Apply(ctx, r.Dir, 0); err != nil {
		t.Fatal(err)
	}
	entries, err := stash.List(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("entries = %+v, want the stash still there after Apply", entries)
	}
}

func TestPopRemovesTheStashEntry(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "changed\n")
	if err := stash.Push(ctx, r.Dir, "wip", false); err != nil {
		t.Fatal(err)
	}
	if err := stash.Pop(ctx, r.Dir, 0); err != nil {
		t.Fatal(err)
	}
	entries, err := stash.List(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %+v, want none after Pop", entries)
	}
}

func TestDrop(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "changed\n")
	if err := stash.Push(ctx, r.Dir, "wip", false); err != nil {
		t.Fatal(err)
	}
	if err := stash.Drop(ctx, r.Dir, 0); err != nil {
		t.Fatal(err)
	}
	entries, err := stash.List(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %+v, want none after Drop", entries)
	}
}

func TestDiffShowsTheStashedChange(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "changed\n")
	if err := stash.Push(ctx, r.Dir, "wip", false); err != nil {
		t.Fatal(err)
	}
	out, err := stash.Diff(ctx, r.Dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "changed") {
		t.Errorf("diff = %q, missing the change", out)
	}
}

// A repository that has never stashed has no refs/stash at all; one that
// stashed and dropped everything has an empty one. Both must read as an
// empty list, not as an error.
func TestListOfNoStashesIsEmptyNotNil(t *testing.T) {
	r := base(t)
	entries, err := stash.List(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if entries == nil || len(entries) != 0 {
		t.Errorf("entries = %#v, want an empty, non-nil slice", entries)
	}

	r.WriteFile("a.txt", "changed\n")
	if err := stash.Push(ctx, r.Dir, "wip", false); err != nil {
		t.Fatal(err)
	}
	if err := stash.Drop(ctx, r.Dir, 0); err != nil {
		t.Fatal(err)
	}
	entries, err = stash.List(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %#v, want an empty list once every stash is dropped", entries)
	}
}

// List must carry a usable hash — the whole reason it reads the reflog
// instead of `git stash list`.
func TestListCarriesTheStashCommitHash(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "changed\n")
	if err := stash.Push(ctx, r.Dir, "wip", false); err != nil {
		t.Fatal(err)
	}
	entries, err := stash.List(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want one", entries)
	}
	if entries[0].Hash != strings.TrimSpace(r.Git("rev-parse", "refs/stash")) {
		t.Errorf("hash = %q, want refs/stash", entries[0].Hash)
	}
}

func TestPushWithNothingToStash(t *testing.T) {
	r := base(t)
	if err := stash.Push(ctx, r.Dir, "wip", false); !errors.Is(err, stash.ErrNothingToStash) {
		t.Errorf("err = %v, want ErrNothingToStash", err)
	}
	entries, err := stash.List(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %+v, want none", entries)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/stash/`
Expected: FAIL — the package does not exist.

- [ ] **Step 3: Write the implementation**

Create `internal/stash/stash.go`:

```go
// Package stash lists and acts on the stash.
package stash

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"git-ui/internal/gitcmd"
)

var ErrNothingToStash = errors.New("stash: nothing to stash")

// Entry is one stash, newest first — the order the stash reflog already
// gives.
type Entry struct {
	Index   int    `json:"index"`
	Message string `json:"message"`
	Branch  string `json:"branch"`
	Hash    string `json:"hash"`
}

// List reports every stash. An empty stash is an empty slice, not an error.
//
// It reads the stash reflog rather than `git stash list`: `stash list`
// ignores --format, --pretty and -z entirely (verified against git 2.54 —
// `git stash list --format=%gd%x00%s%x00%H` prints plain
// "stash@{0}: On main: wip", with no NULs and no hash), so parsing its
// output for a hash is impossible. `reflog show` on refs/stash takes the
// same format placeholders `log` does and gives all three fields. A
// repository that has never had a stash has no refs/stash at all and makes
// `reflog show` fail with exit 128, so the ref is checked first rather than
// guessing at an exit code that also covers real failures.
func List(ctx context.Context, dir string) ([]Entry, error) {
	if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", "refs/stash"); err != nil {
		return []Entry{}, nil
	}
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "reflog", "show", "--format=%gd%x00%s%x00%H", "refs/stash")
	if err != nil {
		return nil, err
	}
	entries := []Entry{}
	out = strings.TrimRight(out, "\n")
	if out == "" {
		return entries, nil
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(line, "\x00")
		if len(fields) != 3 {
			continue
		}
		idx, ok := parseIndex(fields[0])
		if !ok {
			continue
		}
		branch, message := parseSubject(fields[1])
		entries = append(entries, Entry{Index: idx, Message: message, Branch: branch, Hash: fields[2]})
	}
	return entries, nil
}

// Push stashes the worktree. With nothing to stash git prints "No local
// changes to save" and exits 0 — a silent no-op the UI would show as a
// success with no stash to show for it, so that case becomes
// ErrNothingToStash here. (The message is stable English because gitcmd.Run
// pins LC_ALL=C.) The frontend also disables the button, mirroring Commit's
// disabled-when-nothing-staged rule; this is the backstop for the race
// between the two.
func Push(ctx context.Context, dir, message string, includeUntracked bool) error {
	args := []string{"stash", "push", "-m", message}
	if includeUntracked {
		args = append(args, "--include-untracked")
	}
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, args...)
	if err != nil {
		return err
	}
	if strings.Contains(out, "No local changes to save") {
		return ErrNothingToStash
	}
	return nil
}

func Apply(ctx context.Context, dir string, index int) error {
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "stash", "apply", ref(index))
	return err
}

func Pop(ctx context.Context, dir string, index int) error {
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "stash", "pop", ref(index))
	return err
}

func Drop(ctx context.Context, dir string, index int) error {
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "stash", "drop", ref(index))
	return err
}

func Diff(ctx context.Context, dir string, index int) (string, error) {
	return gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "stash", "show", "-p", ref(index))
}

func ref(index int) string { return fmt.Sprintf("stash@{%d}", index) }

// parseIndex pulls N out of "stash@{N}".
func parseIndex(gd string) (int, bool) {
	inner := strings.TrimSuffix(strings.TrimPrefix(gd, "stash@{"), "}")
	n, err := strconv.Atoi(inner)
	return n, err == nil
}

// parseSubject splits git's own stash subject into the branch it was taken
// from and a message: "WIP on <branch>: <hash> <original subject>" when
// stash chose the message itself, or "On <branch>: <message>" when -m gave
// one — Push above always gives one, but List must also read stashes a
// terminal created.
func parseSubject(subject string) (branch, message string) {
	for _, prefix := range []string{"WIP on ", "On "} {
		if !strings.HasPrefix(subject, prefix) {
			continue
		}
		rest := strings.TrimPrefix(subject, prefix)
		branch, message = rest, rest
		if i := strings.Index(rest, ": "); i >= 0 {
			branch, message = rest[:i], rest[i+2:]
		}
		return branch, message
	}
	return "", subject
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/stash/ -v`
Expected: PASS for every test.

- [ ] **Step 5: Commit**

```bash
git add internal/stash
git commit -m "feat(stash): list, push, apply, pop, drop and diff"
```

---

### Task 8: App layer — stash methods and the owed-drop reminder

**Files:**
- Create: `internal/app/stash.go`
- Create: `internal/app/stash_test.go`
- Modify: `internal/app/app.go` (add the `owedDrops` field)
- Modify: `internal/app/merge.go` (`writeMerge` gains the owed-drop check)

**Interfaces:**
- Consumes: `internal/stash` from Task 7; `merge.Status`, `merge.KindStash` from Task 1; `a.writeMerge`, `a.writeWorktree` (existing).
- Produces:
  ```go
  func (a *App) GetStashEntries(id string) ([]stash.Entry, error)
  func (a *App) StashPush(id, message string, includeUntracked bool) error
  func (a *App) StashApply(id string, index int) error
  func (a *App) StashPop(id string, index int) error
  func (a *App) StashDrop(id string, index int) error
  func (a *App) GetStashDiff(id string, index int) (string, error)
  func (a *App) OwedStashDrop(id string) int // the index a conflicted Pop still owes, or -1
  ```

`OwedStashDrop` is what the conflict view's conditional "Drop stash" button
reads (Task 12): a conflicted `StashPop` leaves the entry in place on
purpose, and until the conflict is resolved the user has no other way to get
rid of it from inside the app.

`GetStashDiff` ships **used by no component in this sub-project** — the
sidebar's stash rows get a context menu, not a preview pane (Task 11). It is
kept, and tested, because the spec lists it in the app surface and the
preview is the obvious next step; the deferral is deliberate, not an
oversight.

- [ ] **Step 1: Write the failing test**

Create `internal/app/stash_test.go`. Reuse `newPlainApp` from Task 6's `remote_test.go` — same package, no import needed.

```go
package app

import "testing"

func TestStashPushListApplyPopDrop(t *testing.T) {
	a, r, id := newPlainApp(t)
	r.WriteFile("f.txt", "changed\n")

	if err := a.StashPush(id, "wip", false); err != nil {
		t.Fatal(err)
	}
	entries, err := a.GetStashEntries(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want one", entries)
	}
	diff, err := a.GetStashDiff(id, 0)
	if err != nil {
		t.Fatal(err)
	}
	if diff == "" {
		t.Error("diff is empty")
	}
	if err := a.StashApply(id, 0); err != nil {
		t.Fatal(err)
	}
	if err := a.StashDrop(id, 0); err != nil {
		t.Fatal(err)
	}
	entries, err = a.GetStashEntries(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %+v, want none after Drop", entries)
	}
}

// A Pop that conflicts leaves the stash entry in place and routes the
// repository into the shared conflict view as Kind stash; resolving it by
// staging the conflicted file drops the owed stash automatically.
func TestStashPopConflictDropsAutomaticallyOnceResolved(t *testing.T) {
	a, r, id := newPlainApp(t)
	r.WriteFile("f.txt", "one\n")
	r.Git("add", "f.txt")
	r.Git("commit", "-q", "-m", "base f")
	r.WriteFile("f.txt", "stashed\n")
	r.Git("stash", "push", "-q", "-m", "wip")
	r.WriteFile("f.txt", "conflicting\n")
	r.Git("commit", "-q", "-am", "conflicting")

	if err := a.StashPop(id, 0); err != nil {
		t.Fatal(err)
	}
	st, err := a.GetMergeState(id)
	if err != nil {
		t.Fatal(err)
	}
	if st.Kind != "stash" || len(st.Conflicts) == 0 {
		t.Fatalf("state = %+v, want a stash conflict on f.txt", st)
	}

	if owed := a.OwedStashDrop(id); owed != 0 {
		t.Errorf("owed = %d, want 0 — the conflicted Pop still owes a drop", owed)
	}

	r.WriteFile("f.txt", "resolved\n")
	if err := a.StageMergeFile(id, "f.txt"); err != nil {
		t.Fatal(err)
	}
	if owed := a.OwedStashDrop(id); owed != -1 {
		t.Errorf("owed = %d, want -1 once the conflict is resolved", owed)
	}
	entries, err := a.GetStashEntries(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %+v, want the owed stash dropped once resolved", entries)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/app/ -run Stash`
Expected: FAIL — `a.StashPush` etc. undefined.

- [ ] **Step 3: Write the implementation**

In `internal/app/app.go`, add one field to `App`:

```go
type App struct {
	ctx             context.Context
	store           *repos.Store
	mu              sync.Mutex
	logs            map[string]*logState
	writes          sync.Map // repo ID → *sync.Mutex
	ai              *aiState
	gitSettingsPath string
	owedDrops       sync.Map // repo ID → stash index still to drop once resolved
}
```

In `internal/app/merge.go`, change `writeMerge`:

```go
// writeMerge runs fn under the repository's write lock, so it can't
// interleave with an agent tool call, drops a stash entry a conflicted Pop
// left behind once resolving it leaves nothing unmerged, then tells the
// merge view and any running agent's UI that something moved.
func (a *App) writeMerge(id string, fn func(ctx context.Context, dir string) error) error {
	if err := a.write(id, fn); err != nil {
		return err
	}
	a.finishOwedDrop(id)
	a.emit(EventMergeChanged, MergeChangedEvent{RepoID: id})
	return nil
}

// finishOwedDrop drops a stash entry a conflicted StashPop left behind, once
// resolving it leaves nothing unmerged. Git itself never records that a
// drop is still owed, so this in-memory reminder is the only place it
// lives — losing it (an app restart mid-resolution) never risks the
// changes themselves, only the tidiness of dropping the entry.
func (a *App) finishOwedDrop(id string) {
	v, ok := a.owedDrops.Load(id)
	if !ok {
		return
	}
	dir, err := a.dir(id)
	if err != nil {
		return
	}
	st, err := merge.Status(a.ctx, dir)
	if err != nil || st.Merging {
		return
	}
	a.owedDrops.Delete(id)
	_ = stash.Drop(a.ctx, dir, v.(int))
}
```

Add `"git-ui/internal/stash"` to `internal/app/merge.go`'s imports.

Create `internal/app/stash.go`:

```go
package app

import (
	"context"

	"git-ui/internal/ai/tools"
	"git-ui/internal/merge"
	"git-ui/internal/stash"
)

func (a *App) GetStashEntries(id string) ([]stash.Entry, error) {
	dir, err := a.dir(id)
	if err != nil {
		return nil, err
	}
	return stash.List(a.ctx, dir)
}

func (a *App) StashPush(id, message string, includeUntracked bool) error {
	return a.writeWorktree(id, func(ctx context.Context, dir string) error {
		return stash.Push(ctx, dir, message, includeUntracked)
	})
}

// StashApply may leave the repository conflicted; that is reported through
// GetMergeState (Kind stash), not as an error here, the same distinction
// merge.Start already draws for a conflicted merge.
func (a *App) StashApply(id string, index int) error {
	return a.writeMerge(id, func(ctx context.Context, dir string) error {
		return conflictOK(ctx, dir, stash.Apply(ctx, dir, index))
	})
}

// StashPop may also conflict; when it does, git leaves the stash entry in
// place on purpose, and this remembers to drop it once StageMergeFile,
// UnstageMergeFile or TakeMergeSide resolve it — see finishOwedDrop.
func (a *App) StashPop(id string, index int) error {
	return a.writeMerge(id, func(ctx context.Context, dir string) error {
		err := stash.Pop(ctx, dir, index)
		if isStashConflict(ctx, dir, err) {
			a.owedDrops.Store(id, index)
			return nil
		}
		return err
	})
}

func (a *App) StashDrop(id string, index int) error {
	return a.writeWorktree(id, func(ctx context.Context, dir string) error {
		if err := stash.Drop(ctx, dir, index); err != nil {
			return err
		}
		// Dropping by hand settles the debt a conflicted Pop left, so
		// finishOwedDrop doesn't later drop an unrelated entry that has
		// since shifted into this index.
		if v, ok := a.owedDrops.Load(id); ok && v.(int) == index {
			a.owedDrops.Delete(id)
		}
		return nil
	})
}

// OwedStashDrop is the stash index a conflicted Pop is still waiting to
// drop, or -1 when nothing is owed. The conflict view shows its "Drop
// stash" button only for the first case.
func (a *App) OwedStashDrop(id string) int {
	if v, ok := a.owedDrops.Load(id); ok {
		return v.(int)
	}
	return -1
}

func (a *App) GetStashDiff(id string, index int) (string, error) {
	dir, err := a.dir(id)
	if err != nil {
		return "", err
	}
	out, err := stash.Diff(a.ctx, dir, index)
	if err != nil {
		return "", err
	}
	return tools.Truncate(out, worktreeDiffCap), nil
}

// isStashConflict reports whether err is Apply/Pop leaving a conflict behind
// rather than a real failure.
func isStashConflict(ctx context.Context, dir string, err error) bool {
	if err == nil {
		return false
	}
	st, stErr := merge.Status(ctx, dir)
	return stErr == nil && st.Kind == merge.KindStash
}

func conflictOK(ctx context.Context, dir string, err error) error {
	if isStashConflict(ctx, dir, err) {
		return nil
	}
	return err
}
```

- [ ] **Step 4: Run the whole Go suite**

Run: `go vet ./... && go test ./...`
Expected: everything passes.

- [ ] **Step 5: Regenerate the bindings and commit**

```bash
export PATH=$HOME/.nvm/versions/node/v22.23.1/bin:$HOME/go/bin:$PATH
wails generate module
git checkout -- frontend/wailsjs/runtime
grep -n "StashPush\|StashPop\|GetStashEntries\|OwedStashDrop" frontend/wailsjs/go/app/App.d.ts
git add internal/app frontend/wailsjs/go
git commit -m "feat(app): stash push/apply/pop/drop, with the owed drop tracked in memory"
```

---

### Task 9: Frontend plumbing — types, store, api, actions

**Files:**
- Modify: `frontend/src/lib/types.ts`
- Modify: `frontend/src/lib/api.ts`
- Modify: `frontend/src/lib/stores.ts`
- Modify: `frontend/src/lib/actions.ts`
- Modify: `frontend/src/lib/merge.ts` (kind-aware labels and warnings)
- Modify: `frontend/src/lib/merge.test.ts` (its `state()` helper needs the new `kind` field)
- Create: `frontend/src/lib/remote.ts`
- Create: `frontend/src/lib/remote.test.ts`

**Interfaces:**
- Consumes: the regenerated `frontend/wailsjs/go/app/App` bindings from Tasks 6 and 8.
- Produces:
  ```ts
  export interface AheadBehind { ahead: number; behind: number }
  export interface GitSettings { pullStrategy: 'auto' | 'merge' | 'rebase' }
  export interface StashEntry { index: number; message: string; branch: string; hash: string }
  export interface PullResult { outcome: number; conflicts: string[] }
  export const PULL_UP_TO_DATE = 0
  // MergeState gains: kind, step?, total?, subject?
  export function canSync(state: MergeState | null, busy: string): boolean
  // in merge.ts:
  export interface ConflictHeader { lead: string; from: string; connector: string; into: string; detail: string }
  export function conflictHeader(state: MergeState): ConflictHeader
  export interface ConflictActions { abort: string | null; confirm: string | null; ai: boolean; done: boolean }
  export function conflictActions(state: MergeState): ConflictActions
  export function abortWarning(state: MergeState): { title: string; message: string; confirmLabel: string }
  export function commitWarning(state: MergeState): string | null // now merge-only
  ```

Every label the conflict view shows lives in `merge.ts` as a pure function
with vitest coverage, not inline in the component: `MergeView.svelte` has no
component test in this codebase's style, so anything left inline is untested,
and Task 12's whole job is getting six kinds' worth of wording right.

- [ ] **Step 1: Write the failing test**

Create `frontend/src/lib/remote.test.ts`:

```ts
import { describe, expect, it } from 'vitest'
import { canSync, conflictOwnsScreen } from './remote'
import type { MergeState } from './types'

const merging = (kind: MergeState['kind']): MergeState => ({
  kind,
  merging: true,
  from: 'a',
  into: 'b',
  conflicts: [],
  manual: [],
  staged: [],
  unstaged: [],
})

describe('conflictOwnsScreen', () => {
  it('is true for any unresolved conflict', () => {
    expect(conflictOwnsScreen(merging('merge'), false)).toBe(true)
    expect(conflictOwnsScreen(merging('rebase'), true)).toBe(true) // a dismissal is stash-only
    expect(conflictOwnsScreen(merging('stash'), false)).toBe(true)
  })
  it('is false for a dismissed stash conflict, and with nothing in progress', () => {
    expect(conflictOwnsScreen(merging('stash'), true)).toBe(false)
    expect(conflictOwnsScreen(null, false)).toBe(false)
  })
})

describe('canSync', () => {
  it('is true with nothing in progress and nothing busy', () => {
    expect(canSync(null, '')).toBe(true)
  })
  it('is false while busy', () => {
    expect(canSync(null, 'Committing…')).toBe(false)
  })
  it('is false during a merge, a rebase, or a stash conflict', () => {
    expect(canSync(merging('merge'), '')).toBe(false)
    expect(canSync(merging('rebase'), '')).toBe(false)
    expect(canSync(merging('stash'), '')).toBe(false)
  })
})
```

Then extend `frontend/src/lib/merge.test.ts`. Its `state()` helper builds a
whole `MergeState`, so it needs the new field — add `kind: 'merge'` to the
defaults object (right above `merging: true`) or every existing test in the
file stops type-checking. Then append:

```ts
import { abortWarning, commitWarning, conflictActions, conflictHeader, mergeSections } from './merge'

describe('conflictHeader', () => {
  it('names both sides of a merge', () => {
    expect(conflictHeader(state({ kind: 'merge' }))).toEqual({
      lead: 'Merging', from: 'feature', connector: 'into', into: 'main', detail: '',
    })
  })
  it('says onto for a rebase, with the commit counter', () => {
    const h = conflictHeader(state({ kind: 'rebase', step: 2, total: 5, subject: 'tidy up' }))
    expect(h.lead).toBe('Rebasing')
    expect(h.connector).toBe('onto')
    expect(h.detail).toBe('commit 2 of 5: tidy up')
  })
  it('names the picked commit for a cherry-pick', () => {
    const h = conflictHeader(state({ kind: 'cherry-pick', from: '5e6df51', subject: 'tidy up' }))
    expect(h.lead).toBe('Cherry-picking')
    expect(h.detail).toBe('tidy up')
  })
  it('has nothing to name for a stash conflict', () => {
    const h = conflictHeader(state({ kind: 'stash' }))
    expect(h).toEqual({ lead: 'Resolving stashed changes', from: '', connector: '', into: '', detail: '' })
  })
})

describe('conflictActions', () => {
  it('offers AI, abort and commit for a merge', () => {
    expect(conflictActions(state({ kind: 'merge' }))).toEqual({
      abort: 'Abort merge', confirm: 'Commit merge', ai: true, done: false,
    })
  })
  it('offers a rebase its own wording and no AI', () => {
    expect(conflictActions(state({ kind: 'rebase' }))).toEqual({
      abort: 'Abort rebase', confirm: 'Continue rebase', ai: false, done: false,
    })
  })
  it.each(['cherry-pick', 'revert', 'am'] as const)('gives %s a real abort and continue', (kind) => {
    const a = conflictActions(state({ kind }))
    expect(a.abort).toBeTruthy()
    expect(a.confirm).toBeTruthy()
    expect(a.ai).toBe(false)
  })
  it('gives a stash conflict Done instead of abort/continue', () => {
    expect(conflictActions(state({ kind: 'stash' }))).toEqual({
      abort: null, confirm: null, ai: false, done: true,
    })
  })
})

describe('abortWarning', () => {
  it('says rebase, not merge, for a rebase', () => {
    const w = abortWarning(state({ kind: 'rebase' }))
    expect(w.title).toBe('Abort rebase')
    expect(w.message).not.toMatch(/merge/)
    expect(w.confirmLabel).toBe('Abort rebase')
  })
})

describe('commitWarning', () => {
  it('is null for anything but a merge — nothing else writes a merge commit', () => {
    expect(commitWarning(state({ kind: 'rebase', unstaged: ['a.ts'] }))).toBeNull()
    expect(commitWarning(state({ kind: 'cherry-pick', unstaged: ['a.ts'] }))).toBeNull()
  })
})
```

The existing `commitWarning` tests in the file pass `state()` with no `kind`
override, which now defaults to `'merge'` — they keep passing unchanged.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `export PATH=$HOME/.nvm/versions/node/v22.23.1/bin:$PATH && cd frontend && npm run test -- --run remote merge`
Expected: FAIL — `./remote` does not exist, and `conflictHeader`/`conflictActions`/`abortWarning` are not exported from `./merge`.

- [ ] **Step 3: Write the implementation**

Create `frontend/src/lib/remote.ts`:

```ts
import type { MergeState } from './types'

// The toolbar's Pull and Push are disabled by the same rule Commit already
// follows: nothing else running, and no merge/rebase/stash conflict owns
// the repository. Fetch has no such rule — it never touches the worktree
// or the index.
export function canSync(state: MergeState | null, busy: string): boolean {
  return !busy && !state?.merging
}

// conflictOwnsScreen decides whether the conflict view takes over the
// details pane and blocks the Changes view. Every kind but a stash has a
// git-level abort, so the user always has a way out of it and it keeps the
// screen; a stash conflict can only be dismissed, and then the Changes view
// (and the toolbar's "Resolve conflicts" button) take over again.
export function conflictOwnsScreen(state: MergeState | null, stashDismissed: boolean): boolean {
  if (!state?.merging) return false
  return !(state.kind === 'stash' && stashDismissed)
}
```

In `frontend/src/lib/merge.ts`, add the label functions and scope
`commitWarning` to a merge:

```ts
export interface ConflictHeader {
  lead: string
  from: string
  connector: string
  into: string
  detail: string
}

// One table, so the six kinds' wording lives in one place instead of spread
// through MergeView's template.
const HEADINGS: Record<Exclude<ConflictKind, ''>, { lead: string; connector: string }> = {
  merge: { lead: 'Merging', connector: 'into' },
  rebase: { lead: 'Rebasing', connector: 'onto' },
  'cherry-pick': { lead: 'Cherry-picking', connector: 'onto' },
  revert: { lead: 'Reverting', connector: 'on' },
  am: { lead: 'Applying patch', connector: '' },
  stash: { lead: 'Resolving stashed changes', connector: '' },
}

export function conflictHeader(state: MergeState): ConflictHeader {
  const kind = state.kind || 'merge'
  const { lead, connector } = HEADINGS[kind]
  if (kind === 'stash') return { lead, from: '', connector: '', into: '', detail: '' }
  const detail =
    kind === 'rebase' && state.total
      ? `commit ${state.step} of ${state.total}${state.subject ? `: ${state.subject}` : ''}`
      : kind === 'merge'
        ? ''
        : (state.subject ?? '')
  return { lead, from: state.from, connector: state.into ? connector : '', into: state.into, detail }
}

export interface ConflictActions {
  abort: string | null
  confirm: string | null
  ai: boolean
  done: boolean
}

// "Resolve with AI" is merge-only: ResolveConflicts refuses anything without
// MERGE_HEAD, so showing the button elsewhere only offers an error. A stash
// conflict has no git-level abort or continue — it gets Done (and, when a
// conflicted Pop still owes one, Drop stash, which MergeView adds itself
// from OwedStashDrop rather than from this table).
const ACTIONS: Record<Exclude<ConflictKind, ''>, ConflictActions> = {
  merge: { abort: 'Abort merge', confirm: 'Commit merge', ai: true, done: false },
  rebase: { abort: 'Abort rebase', confirm: 'Continue rebase', ai: false, done: false },
  'cherry-pick': { abort: 'Abort cherry-pick', confirm: 'Continue cherry-pick', ai: false, done: false },
  revert: { abort: 'Abort revert', confirm: 'Continue revert', ai: false, done: false },
  am: { abort: 'Abort patch', confirm: 'Continue applying', ai: false, done: false },
  stash: { abort: null, confirm: null, ai: false, done: true },
}

export function conflictActions(state: MergeState): ConflictActions {
  return ACTIONS[state.kind || 'merge']
}

// abortWarning is the confirmation before throwing a resolution away. The
// wording has to follow the kind: "Abort merge / go back to where the
// branch was" is plainly wrong for a rebase or a cherry-pick.
export function abortWarning(state: MergeState): { title: string; message: string; confirmLabel: string } {
  const label = conflictActions(state).abort ?? 'Abort'
  const what = {
    merge: 'this merge',
    rebase: 'this rebase',
    'cherry-pick': 'this cherry-pick',
    revert: 'this revert',
    am: 'this patch',
    stash: 'this',
  }[state.kind || 'merge']
  return {
    title: label,
    message: `Throw away every resolution from ${what} and go back to where the branch was?`,
    confirmLabel: label,
  }
}
```

and change `commitWarning`'s first line so it only ever fires for a merge —
its whole premise ("won't be in the merge commit, which keeps this branch's
version instead") is false for a rebase, a cherry-pick or a revert, which
never write a merge commit:

```ts
export function commitWarning(state: MergeState): string | null {
  if (state.kind && state.kind !== 'merge') return null
  const n = state.unstaged.length
  // ... unchanged from here ...
}
```

Import `ConflictKind` alongside `MergeState` at the top of `merge.ts`.

In `frontend/src/lib/types.ts`, extend `MergeState` and add the new types (place near it and near the existing `WorktreeState`/`CommitInfo` block):

```ts
export type ConflictKind = 'merge' | 'rebase' | 'cherry-pick' | 'revert' | 'am' | 'stash' | ''

export interface MergeState {
  kind: ConflictKind
  merging: boolean
  from: string
  into: string
  conflicts: string[]
  manual: string[]
  staged: string[]
  unstaged: string[]
  step?: number
  total?: number
  subject?: string
}

export interface AheadBehind {
  ahead: number
  behind: number
}

export const PULL_UP_TO_DATE = 0
export const PULL_MERGED = 1
export const PULL_REBASED = 2
export const PULL_CONFLICTED = 3

export interface PullResult {
  outcome: number
  conflicts: string[]
}

export interface GitSettings {
  pullStrategy: 'auto' | 'merge' | 'rebase'
}

export interface StashEntry {
  index: number
  message: string
  branch: string
  hash: string
}
```

In `frontend/src/lib/api.ts`: update the `fetch`/`pull` lines and add the rest, and add the new types to the import at the top of the file:

```ts
import type { AIMessage, AISettings, AIStatus, AheadBehind, CommitInfo, ConflictFile, Details, Filters, GitSettings, LogPage, MergeResult, MergeState, ProviderName, PromptInfo, PullResult, Refs, Repo, ResetInfo, ResetMode, StashEntry, WorktreeState } from './types'
```

```ts
  fetch: (id: string) => call<void>(Go.Fetch(id)),
  push: (id: string) => call<void>(Go.Push(id)),
  pull: (id: string) => call<PullResult>(Go.Pull(id)),
  getRemoteInfo: (id: string) => call<AheadBehind>(Go.GetRemoteInfo(id)),
  getGitSettings: () => call<GitSettings>(Go.GetGitSettings()),
  saveGitSettings: (s: GitSettings) => call<void>(Go.SaveGitSettings(s as any)),

  getStashEntries: (id: string) => call<StashEntry[]>(Go.GetStashEntries(id)),
  stashPush: (id: string, message: string, includeUntracked: boolean) => call<void>(Go.StashPush(id, message, includeUntracked)),
  stashApply: (id: string, index: number) => call<void>(Go.StashApply(id, index)),
  stashPop: (id: string, index: number) => call<void>(Go.StashPop(id, index)),
  stashDrop: (id: string, index: number) => call<void>(Go.StashDrop(id, index)),
  getStashDiff: (id: string, index: number) => call<string>(Go.GetStashDiff(id, index)),
  owedStashDrop: (id: string) => call<number>(Go.OwedStashDrop(id)),
```

In `frontend/src/lib/stores.ts`: add two stores and their loaders next to `worktreeState`/`loadWorktreeState`, and call both from `refreshRepo`:

```ts
export const remoteInfo = writable<AheadBehind | null>(null)
export const stashEntries = writable<StashEntry[]>([])
export const gitSettings = writable<GitSettings | null>(null)
// The index a conflicted stash pop still owes a drop for, or -1.
export const owedStashDrop = writable<number>(-1)
// Set by the conflict view's "Done" for a stash conflict, which has no
// git-level abort: the files stay as they are and the view stops owning the
// screen. Cleared below whenever the conflict's kind changes or it goes
// away, so it can never hide a *different* conflict later.
export const stashConflictDismissed = writable<boolean>(false)

export async function loadRemoteInfo() {
  const repo = get(selectedRepo)
  if (!repo || repo.missing) {
    remoteInfo.set(null)
    return
  }
  try {
    const info = await api.getRemoteInfo(repo.id)
    if (get(selectedRepoId) !== repo.id) return
    remoteInfo.set(info)
  } catch {
    remoteInfo.set(null)
  }
}

export async function loadOwedStashDrop() {
  const repo = get(selectedRepo)
  if (!repo || repo.missing) {
    owedStashDrop.set(-1)
    return
  }
  try {
    const owed = await api.owedStashDrop(repo.id)
    if (get(selectedRepoId) !== repo.id) return
    owedStashDrop.set(owed)
  } catch {
    owedStashDrop.set(-1)
  }
}

export async function loadStashEntries() {
  const repo = get(selectedRepo)
  if (!repo || repo.missing) {
    stashEntries.set([])
    return
  }
  try {
    const entries = await api.getStashEntries(repo.id)
    if (get(selectedRepoId) !== repo.id) return
    stashEntries.set(entries)
  } catch {
    stashEntries.set([])
  }
}

export async function loadGitSettings() {
  try {
    gitSettings.set(await api.getGitSettings())
  } catch {
    // Left as whatever was last loaded — the toolbar has no strategy
    // picker of its own, so nothing else depends on this succeeding.
  }
}
```

```ts
export async function refreshRepo() {
  await loadRepos()
  await loadRefs()
  await loadMergeState()
  await loadWorktreeState()
  await loadRemoteInfo()
  await loadStashEntries()
  await loadOwedStashDrop()
  logVersion.update((v) => v + 1)
}
```

In the existing `loadMergeState`, clear the dismissal whenever the state it
just read is not the same stash conflict that was dismissed — add this right
after the store is set (and read the function first; keep its existing
stale-repo guard):

```ts
  // A dismissal belongs to one stash conflict only. Anything else — a new
  // kind, or nothing in progress — brings the view back.
  if (state?.kind !== 'stash') stashConflictDismissed.set(false)
```

Also reset it in `selectRepo` alongside the other per-repo stores, so
switching repositories never carries one repository's dismissal to another.

Also add `loadRemoteInfo()`, `loadStashEntries()` and `loadOwedStashDrop()` to `selectRepo`'s existing `loadMergeState(); loadWorktreeState()` pair, and import `AheadBehind`, `GitSettings`, `StashEntry` at the top of `stores.ts`.

In `frontend/src/lib/actions.ts`, add near `abortMerge`/`commitMerge` and import `promptDialog`, `PULL_UP_TO_DATE` alongside the existing imports:

```ts
Replace `abortMerge`'s hardcoded merge wording with the kind-aware one, and
stop `commitMerge` from showing the merge-commit warning for a rebase (which
`commitWarning` now returns null for anyway — this keeps the two in step):

```ts
export async function abortMerge(id: string) {
  const state = get(mergeState)
  const warning = abortWarning(state ?? ({ kind: 'merge' } as MergeState))
  const ok = await confirmDialog({ ...warning, danger: true })
  if (ok) await run(`${warning.title}…`, () => api.abortMerge(id))
}
```

(`commitMerge` itself needs no change beyond `commitWarning`'s new
merge-only guard; add `abortWarning` to the existing
`import { commitWarning, takeMessage } from './merge'` line.)

Then add the new actions:

```ts
export const fetchRemote = (id: string) => run('Fetching…', () => api.fetch(id))

export const push = (id: string) => run('Pushing…', () => api.push(id))

export async function pull(id: string) {
  busy.set('Pulling…')
  try {
    const result = await api.pull(id)
    if (result.outcome === PULL_UP_TO_DATE) toast('Already up to date.', 'info')
  } catch (e) {
    toast(errorMessage(e), 'error')
  } finally {
    busy.set('')
    await refreshRepo()
  }
}

export async function stashChanges(id: string) {
  const result = await promptDialog({
    title: 'Stash changes',
    label: 'Message (optional)',
    checkboxLabel: 'Include untracked files',
    submitLabel: 'Stash',
  })
  if (!result) return
  await run('Stashing…', () => api.stashPush(id, result.value.trim(), result.checked))
}

export const stashApply = (id: string, index: number) => run('Applying stash…', () => api.stashApply(id, index))

// The conflict view's "Done" for a stash conflict: the files stay exactly as
// they are (conflicted or not), the view just stops owning the screen. The
// flag is cleared by loadMergeState whenever the kind changes or the
// conflict goes away, so a later stash conflict shows the view again.
export const dismissStashConflict = () => stashConflictDismissed.set(true)

export async function stashPop(id: string, index: number) {
  const ok = await confirmDialog({
    title: 'Pop stash',
    message: 'Apply this stash and remove it from the list? If it conflicts, it stays until the conflict is resolved.',
    confirmLabel: 'Pop',
  })
  if (ok) await run('Popping stash…', () => api.stashPop(id, index))
}

export async function stashDrop(id: string, index: number) {
  const ok = await confirmDialog({
    title: 'Drop stash',
    message: 'Delete this stash entry for good? This cannot be undone.',
    confirmLabel: 'Drop',
    danger: true,
  })
  if (ok) await run('Dropping stash…', () => api.stashDrop(id, index))
}
```

`confirmDialog`/`promptDialog` are already imported in most files that need them via `'./ui'`; check `actions.ts`'s existing import line from `'./ui'` and add `promptDialog` to it if missing.

- [ ] **Step 4: Run the checks**

Run:
```bash
export PATH=$HOME/.nvm/versions/node/v22.23.1/bin:$PATH
cd frontend && npm run test -- --run && npm run check
```
Expected: the new `remote.test.ts` passes; `npm run check` ends with 0 ERRORS (unused-export warnings for the not-yet-wired store/action functions are expected until Tasks 10-11 use them — if `npm run check` treats those as errors rather than warnings, wire the imports into a component as you go rather than leaving them dangling).

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib
git commit -m "feat(frontend): types, api and actions for push/pull/stash"
```

---

### Task 10: Toolbar and the Settings dialog's Git section

**Files:**
- Create: `frontend/src/components/Toolbar.svelte`
- Modify: `frontend/src/components/Icon.svelte` (add `upload`)
- Modify: `frontend/src/components/LogView.svelte`
- Modify: `frontend/src/components/SettingsDialog.svelte`

**Interfaces:**
- Consumes: `canSync` (Task 9), `fetchRemote`/`push`/`pull` (Task 9), `busy`/`mergeState`/`remoteInfo`/`gitSettings` stores (Task 9).
- Produces: `Toolbar.svelte` (`export let repoId: string`), rendered inside `LogView.svelte`'s header.

- [ ] **Step 1: Add the `upload` icon**

In `frontend/src/components/Icon.svelte`, add one entry to `paths` (a vertical flip of the existing `download`):

```ts
    upload: 'M8 13.5v-8m0 0L5 8.5m3-3 3 3M3 2.5h10',
```

- [ ] **Step 2: Write `Toolbar.svelte`**

Create `frontend/src/components/Toolbar.svelte`:

```svelte
<script lang="ts">
  import Icon from './Icon.svelte'
  import { fetchRemote, pull, push } from '../lib/actions'
  import { canSync } from '../lib/remote'
  import { busy, mergeState, remoteInfo, stashConflictDismissed } from '../lib/stores'

  export let repoId: string

  $: syncable = canSync($mergeState, $busy)
</script>

<div class="toolbar">
  <button class="icon-btn" title="Fetch" disabled={!!$busy} on:click={() => fetchRemote(repoId)}>
    <Icon name="refresh" size={14} />
  </button>
  <button class="icon-btn" title="Pull" disabled={!syncable} on:click={() => pull(repoId)}>
    <Icon name="download" size={14} />
    {#if $remoteInfo?.behind}<span class="badge">{$remoteInfo.behind}</span>{/if}
  </button>
  <button class="icon-btn" title="Push" disabled={!syncable} on:click={() => push(repoId)}>
    <Icon name="upload" size={14} />
    {#if $remoteInfo?.ahead}<span class="badge">{$remoteInfo.ahead}</span>{/if}
  </button>
  <!-- The only way back into a stash conflict the user dismissed with
       "Done": without it the conflict view would be unreachable until the
       files happen to resolve. -->
  {#if $mergeState?.kind === 'stash' && $stashConflictDismissed}
    <button class="btn" on:click={() => stashConflictDismissed.set(false)}>Resolve conflicts</button>
  {/if}
</div>

<style>
  .toolbar { display: flex; align-items: center; gap: 2px; flex: none; }
  /* .icon-btn itself is global (theme.css:88) — only the badge anchor is
     local, so the shared hover/disabled styling keeps applying. */
  .toolbar :global(.icon-btn) { position: relative; }
  .badge {
    position: absolute; top: -3px; right: -3px; font-size: 9px; line-height: 1;
    padding: 1px 3px; border-radius: 6px; background: var(--accent); color: white;
  }
</style>
```

`.icon-btn` is a global class defined in `frontend/src/theme.css:88` (hover
and disabled states included), which is why the rule above is written as
`.toolbar :global(.icon-btn)` — a plain `.icon-btn { … }` in a Svelte
component would be scoped away and silently do nothing, and redefining the
class locally would drop the shared hover styling.

- [ ] **Step 3: Wire it into `LogView.svelte`**

In `frontend/src/components/LogView.svelte`, import `Toolbar` and render it in the header, before the existing "Show chat" button:

```svelte
  import Toolbar from './Toolbar.svelte'
```

```svelte
    {#if $selectedRepo && !$selectedRepo.missing}
      <Toolbar repoId={$selectedRepo.id} />
    {/if}
    {#if !$chatOpen}
```

(The existing `{#if !$chatOpen}` block for the chat-panel button stays exactly as it is — this only adds the new block directly above it, inside the same `<header>`.)

- [ ] **Step 4: Add the Git section to `SettingsDialog.svelte`**

Read the existing `commitMessage` `<section>` in `SettingsDialog.svelte` (around line 279-312) for its exact `settings`/`save` binding pattern, then add a parallel section for `gitSettings`/`pullStrategy` — a second, independent settings object loaded and saved through `api.getGitSettings`/`api.saveGitSettings` (via `loadGitSettings`/`gitSettings` from `stores.ts`), not merged into the AI `settings` object:

```svelte
<section>
  <h3>Git</h3>
  <label>
    Pull strategy
    <select bind:value={git.pullStrategy} on:change={saveGit}>
      <option value="auto">Auto — follow this repository's git config</option>
      <option value="merge">Always merge</option>
      <option value="rebase">Always rebase</option>
    </select>
  </label>
</section>
```

The dialog has no `onMount` load: it loads reactively, `$: if ($settingsOpen)
load()` (line ~39), and `load()` (line ~46) fills `settings` and `prompts`
inside a try/catch that toasts `errorMessage(e)`. Follow exactly that, with a
second local that is *not* merged into the AI `settings` object:

```ts
  let git: GitSettings = { pullStrategy: 'auto' }

  async function saveGit() {
    try {
      await api.saveGitSettings(git)
      await loadGitSettings() // keeps the store the rest of the app reads in step
    } catch (e) {
      toast(errorMessage(e), 'error')
    }
  }
```

and one added line inside the existing `load()`, in the same `try` that
already fetches the AI settings:

```ts
      git = await api.getGitSettings()
```

Import `GitSettings` from `'../lib/types'` and add `loadGitSettings` to the
existing `import { loadAISettings, settingsOpen } from '../lib/stores'` line.
`api`, `toast` and `errorMessage` are already imported.

- [ ] **Step 5: Manual check and commit**

Run:
```bash
export PATH=$HOME/.nvm/versions/node/v22.23.1/bin:$PATH
cd frontend && npm run check
```
Expected: 0 ERRORS.

```bash
git add frontend/src/components
git commit -m "feat(frontend): a fetch/pull/push toolbar and the Git settings section"
```

---

### Task 11: Sidebar Stash section, the Stash… action, and de-duplicating Fetch/Pull

**Files:**
- Modify: `frontend/src/components/RepoRefs.svelte`
- Modify: `frontend/src/components/CommitBox.svelte`
- Modify: `frontend/src/components/Sidebar.svelte` (drop the per-row Fetch/Pull icon buttons)

**Interfaces:**
- Consumes: `stashEntries` store, `stashApply`/`stashPop`/`stashDrop`/`stashChanges` actions (Task 9).
- Produces: no new exported component API — both are internal template/script additions to existing components.

- [ ] **Step 1: Add the Stash section to `RepoRefs.svelte`**

Add a `showStash` local next to the existing `showTags`, and import `stashEntries`, `stashApply`, `stashPop`, `stashDrop`, `openMenu` (already imported) from their respective modules:

```ts
  import { stashApply, stashDrop, stashPop } from '../lib/actions'
```

```ts
  let showStash = true
```

```ts
  import { stashEntries } from '../lib/stores'
```

Add a `stashMenu` function next to `tagMenu`/`branchMenu`:

```ts
  function stashMenu(event: MouseEvent, entry: { index: number }) {
    openMenu(event, [
      { label: 'Apply', action: () => stashApply(repoId, entry.index), disabled: !!$busy },
      { label: 'Pop', action: () => stashPop(repoId, entry.index), disabled: !!$busy },
      { label: 'Drop', action: () => stashDrop(repoId, entry.index), danger: true, disabled: !!$busy },
    ])
  }
```

Add the section itself, right after the existing Tags section (mirroring its structure exactly — count badge, collapsible, empty-state row):

```svelte
    <div class="section">
      <button class="section-title" on:click={() => (showStash = !showStash)}>Stash</button>
      <span class="count">{$stashEntries.length}</span>
    </div>
    {#if showStash}
      {#each $stashEntries as entry (entry.index)}
        <button class="row-item ref" on:contextmenu={(e) => stashMenu(e, entry)}>
          <span class="mark"><Icon name="download" size={12} /></span>
          <span class="ellipsis">{entry.message}</span>
        </button>
      {:else}
        <div class="none">No stashed changes</div>
      {/each}
    {/if}
```

Diffing the stash preview (opening `api.getStashDiff` in the main pane on click) is out of scope for this task — it needs a place to render that isn't part of `RepoRefs.svelte` itself; leave the row's click doing nothing beyond the context menu for now, matching how a Tag row's *click* filters the log while its *context menu* holds the destructive actions, and note this gap rather than bolt a diff viewer onto the sidebar row.

- [ ] **Step 2: Drop the duplicated Fetch/Pull buttons from `Sidebar.svelte`**

`Sidebar.svelte` gives every repository row two hover icon buttons (lines
49-50, `fetchRepo`/`pullRepo`) that the new toolbar now covers for the
selected repository — with ahead/behind badges and the merge/rebase/stash
guard the sidebar buttons lack. Delete those two `<button class="icon-btn">`
elements. **Keep** the context-menu entries (lines 12-13): they still work on
a repository that is not the selected one, which the toolbar cannot reach.

Leave `fetchRepo`/`pullRepo` in `actions.ts` — the menu still calls them —
but note that `pullRepo` and the toolbar's `pull` are now two different
things: `pullRepo` is the plain `run(...)` wrapper and shows nothing for an
up-to-date pull, `pull` reports the outcome. Change the menu to call the new
`pull`/`fetchRemote` instead and delete `fetchRepo`/`pullRepo`, so one
behaviour serves both entry points:

```ts
      { label: 'Fetch', action: () => fetchRemote(repo.id), disabled: repo.missing || !!$busy },
      { label: 'Pull', action: () => pull(repo.id), disabled: repo.missing || !!$busy || !!$mergeState?.merging },
```

- [ ] **Step 3: Add "Stash…" to `CommitBox.svelte`**

Import `stashChanges` and add a button next to the existing "Write with AI"/"Commit" buttons (around line 206-211):

```ts
  import { stashChanges } from '../lib/actions'
```

```svelte
<button class="btn" disabled={!repoId || !!$busy || !hasChanges} on:click={() => stashChanges(repoId)}>Stash…</button>
```

Place it before the primary "Commit"/"Amend" button, matching the existing
left-to-right order (secondary actions, then the primary one last).

`hasChanges` mirrors Commit's own disabled-when-nothing-staged rule, which
the spec asks for explicitly ("Stash push with nothing to stash → button
disabled"): read the component's existing reactive statement over
`$worktreeState` for the exact field names it already uses for the Commit
button, and define `hasChanges` as "any staged or unstaged or untracked
entry". `stash.Push` returning `ErrNothingToStash` (Task 7) is the backstop
for the race, and surfaces as an error toast.

- [ ] **Step 4: Manual check**

Run:
```bash
export PATH=$HOME/.nvm/versions/node/v22.23.1/bin:$PATH
cd frontend && npm run check && npm run test -- --run
```
Expected: 0 ERRORS; all vitest still passes.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components
git commit -m "feat(frontend): a Stash sidebar section, the Stash… action, one Fetch/Pull"
```

---

### Task 12: `MergeView.svelte` generalization

**Files:**
- Modify: `frontend/src/components/MergeView.svelte`
- Modify: `frontend/src/components/LogView.svelte` (the details pane's `merging` condition)
- Modify: `frontend/src/App.svelte` (the `showChanges` condition)

**Interfaces:**
- Consumes: `conflictHeader`, `conflictActions`, `abortWarning` (Task 9), `conflictOwnsScreen` (Task 9), `$owedStashDrop`/`$stashConflictDismissed` stores (Task 9).
- Produces: no new exported API — the header and action bar read the tables instead of assuming a merge.

- [ ] **Step 1: No new test here — the wording is already under test**

This codebase has no Svelte component tests, which is exactly why Task 9 put
every label and every button decision in `merge.ts`/`remote.ts` as pure
functions with vitest coverage. This task is the wiring only: if a label is
wrong, fix it in `merge.ts` and its test, not inline here.

- [ ] **Step 2: Update the header and action bar**

In `frontend/src/components/MergeView.svelte`, replace the `<header>` block. "Resolve with AI" is scoped to `kind === 'merge'` only — `ResolveConflicts`'s own `MERGE_HEAD` check (Task 3) already refuses it for a rebase or a stash conflict regardless of what the button does, but showing a button that always errors is worse than not showing it:

```svelte
  <header>
    {#if head}
      <span class="title">
        {head.lead}
        {#if head.from}<strong>{head.from}</strong>{/if}
        {#if head.connector}{head.connector} <strong>{head.into}</strong>{/if}
      </span>
      {#if head.detail}<span class="count">{head.detail}</span>{/if}
    {/if}
    <span class="count">{pending} left</span>
    <span class="spacer"></span>
    {#if acts.ai}
      <button class="btn" disabled={!!$busy || pending === 0} on:click={() => resolveConflicts(repoId)}>
        <Icon name="sparkle" size={14} /> Resolve with AI
      </button>
    {/if}
    {#if acts.abort}
      <button class="btn" disabled={!!$busy} on:click={() => abortMerge(repoId)}>{acts.abort}</button>
    {/if}
    {#if acts.confirm}
      <button class="btn primary" disabled={!!$busy || pending > 0} on:click={() => commitMerge(repoId)}>{acts.confirm}</button>
    {/if}
    {#if acts.done}
      <!-- A stash conflict has no git-level abort or continue. Drop stash is
           the only way to get rid of the entry a conflicted Pop deliberately
           kept; Done leaves the files exactly as they are and gives the
           screen back (the toolbar's "Resolve conflicts" brings it back). -->
      {#if $owedStashDrop >= 0}
        <button class="btn" disabled={!!$busy} on:click={() => stashDrop(repoId, $owedStashDrop)}>Drop stash</button>
      {/if}
      <button class="btn primary" disabled={!!$busy} on:click={dismissStashConflict}>Done</button>
    {/if}
  </header>
```

with, in the `<script>` block:

```ts
  import { conflictActions, conflictHeader } from '../lib/merge'
  import { dismissStashConflict, stashDrop } from '../lib/actions'
  import { owedStashDrop } from '../lib/stores'

  $: head = $mergeState ? conflictHeader($mergeState) : null
  $: acts = $mergeState ? conflictActions($mergeState) : { abort: null, confirm: null, ai: false, done: false }
```

- [ ] **Step 2b: Let a dismissed stash conflict give the screen back**

In `frontend/src/components/LogView.svelte`, both `{#if $mergeState?.merging}`
conditions (the splitter at line ~28 and the pane at line ~31) become
`conflictOwnsScreen($mergeState, $stashConflictDismissed)`; keep the
`|| $selectedHash` part of the first one as it is. In
`frontend/src/App.svelte`, `showChanges`'s `!$mergeState?.merging` becomes
`!conflictOwnsScreen($mergeState, $stashConflictDismissed)`, and its comment
gains a sentence: a dismissed stash conflict is the one case where the
Changes view is allowed to show a repository with unmerged entries, because
nothing else can finish it. Import `conflictOwnsScreen` from `../lib/remote`
and `stashConflictDismissed` from `../lib/stores` in both.

- [ ] **Step 3: Manual check**

Run:
```bash
export PATH=$HOME/.nvm/versions/node/v22.23.1/bin:$PATH
cd frontend && npm run check && npm run test -- --run
```
Expected: 0 ERRORS; all vitest passes.

- [ ] **Step 4: Commit**

```bash
git add frontend/src
git commit -m "feat(frontend): the conflict view serves every kind, not only a merge"
```

---

### Task 13: `make build`, run the whole suite, manual pass

**Files:** none — verification only.

- [ ] **Step 1: Full verification**

```bash
cd /Users/josfh/playground/git-ui
go vet ./... && go test ./...
export PATH=$HOME/.nvm/versions/node/v22.23.1/bin:$PATH
cd frontend && npm run check && npm run test -- --run
cd ..
make build
```
Expected: everything green; `make build` produces `build/bin/git-ui.app`.

- [ ] **Step 2: Manual pass (added to the pending list, as 2a's was)**

**This repository has no remote, and nothing here pushes to one.** Build a
throwaway remote under the scratchpad instead:

```bash
S="$TMPDIR/git-ui-2b-manual" && rm -rf "$S" && mkdir -p "$S" && cd "$S"
git init -q --bare origin.git
git clone -q origin.git work && cd work
printf 'one\n' > a.txt && git add a.txt && git commit -qm base && git push -q -u origin main
cd "$S" && git clone -q origin.git other
```

Then add `$S/work` to the app and walk through:

1. **Toolbar** — in `other`, commit and push a change; in `work`, Fetch and
   check the behind badge, then Pull, then commit locally and Push and check
   the ahead badge. Confirm the sidebar row no longer has its own Fetch/Pull
   icons and that the repo context menu still does.
2. **Pull strategy** — diverge `work` and `other` on the same file and Pull
   once under each of Settings → Git → auto / merge / rebase. The merge run
   ends in the conflict view headed "Merging", the rebase run in one headed
   "Rebasing … onto main" with the commit counter. Resolve one and Abort the
   other, and confirm the Abort dialog says *rebase*, not *merge*.
3. **Stash** — with the Changes view dirty, use Stash…, with and without
   "Include untracked"; check the sidebar Stash section's count, then Apply
   (entry stays) and Drop. With a clean tree the Stash… button is disabled.
4. **Stash conflict** — stash a change, commit a conflicting one on the same
   file, then Pop. The conflict view opens headed "Resolving stashed
   changes", with Drop stash and Done and no Abort/Continue. Press Done: the
   Changes view comes back and the toolbar shows "Resolve conflicts", which
   returns to it. Resolve every file and confirm the stash entry is gone by
   itself.
5. **Cherry-pick** — from a terminal in `work`, `git cherry-pick` a commit
   that conflicts. The app must say "Cherry-picking", not "Resolving stashed
   changes", and its Abort/Continue must actually work. Do the same for
   `git revert`. This is the check the 8-defect audit cared about most: a
   silent no-op here strands the user.
6. **`git am`** — `git format-patch -1` a conflicting commit and `git am` it
   from a terminal. The app must show "Applying patch" and its Abort must
   run `am --abort`; verify `.git/rebase-apply` is gone and the mailbox was
   not destroyed by a `rebase --abort`.

- [ ] **Step 3: Commit if the manual pass finds nothing**

```bash
git add -A
git commit -m "docs: sub-project 2b implemented, manual pass pending" --allow-empty
```

Only commit here if Step 2 changed a tracked file (e.g. a README/spec status line); otherwise skip this step entirely — there is nothing to commit.
