# Rebase and cherry-pick Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Start a plain rebase and a single-commit cherry-pick from the UI, let the AI conflict resolver work on their conflicts one step at a time, name the conflict sides, add "Skip this commit", and give the chat a `cherry_pick` write tool.

**Architecture:** Start/Skip/Preview/Fingerprint live in `internal/merge` beside `merge.Start`, since that package already owns Status/Continue/Abort for every conflicted kind. The resolver ties a run to `merge.Fingerprint` (`kind:commit`) instead of `MERGE_HEAD`, and `mergetools` prints side descriptions that `merge.Status` computes. The frontend adds menu entries (with a disabled-reason tooltip), confirmation dialogs built from pure `lib/` helpers, and Skip plus named sides in `MergeView`.

**Tech Stack:** Go, Wails v2 bindings, Svelte (legacy `$:` syntax), TypeScript, vitest.

**Spec:** `docs/superpowers/specs/2026-09-24-rebase-cherry-pick-design.md`

## Global Constraints

- Every behaviour change updates the affected `docs/spec/` file **in the same commit** (the design doc under `docs/superpowers/specs/` does not count). Each task names its `docs/spec/` file.
- Every non-interactive git call that can open an editor runs with `GIT_EDITOR=true`, via `gitcmd.RunEnv(ctx, dir, gitcmd.HookTimeout, noEditor, ...)`.
- Rebase and cherry-pick run with `-c merge.conflictStyle=zdiff3`, like `merge.Start`.
- Refs and hashes from the UI or the model pass `checkRef` (no empty value, no leading `-`) before reaching git.
- All writes go through `a.write` (the per-repository write lock).
- Copy strings, exactly: "Commit or stash your changes first", "Finish the `<kind>` in progress first", "No branch is checked out", "Cherry-picking a merge commit isn't supported", "Already up to date", "Nothing to apply: those changes are already on `<branch>`", "Skip this commit".
- Git commits: conventional prefix (`feat(merge): …`, `feat(ui): …`), **no `Co-Authored-By` line**.
- Go checks: `go test ./... && go vet ./... && gofmt -l internal` (no files listed). Frontend checks, from `frontend/`: `npm test && npm run check`. Use node 22: `source ~/.nvm/nvm.sh && nvm use 22 >/dev/null`.
- Regenerate bindings after adding exported App methods: `source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && ~/go/bin/wails generate module`, then `git checkout -- frontend/wailsjs/runtime` if it was touched.

## Review Focus

1. **Onto is a remote-tracking branch** (`origin/main`) from the sidebar: the rebase must run against it, not fail on the name. Test in Task 1 (`TestRebaseOntoARemoteTrackingBranch`).
2. **An untracked file is in the way** of a replayed commit: git stops, and the user must not be left in a half-started rebase. Test in Task 1 (`TestRebaseStoppedByAnUntrackedFileLeavesNothingBehind`).
3. **A rebase step is emptied by the user's resolution** (took the base's side wholesale): Continue alone may fail, and Continue + Skip must always get out. Test in Task 2 (`TestAnEmptiedRebaseStepCanAlwaysBeLeft`).
4. **A resolver run outlives its rebase step** (step 1 continued in a terminal while the run is still going): its tool calls must be refused on step 2. Test in Task 4 (`TestResolveConflictsRefusesTheNextRebaseStep`).
5. **The user's own git config** (`rebase.autoStash=true`, `merge.conflictStyle=merge`): the dirty-tree refusal must happen before git would autostash, and markers must still be zdiff3. Test in Task 1 (`TestRebaseRefusesTrackedChangesEvenWithAutoStash`).

---

### Task 1: `merge.Rebase`, `RebasePreview`, `IsAncestorOfHead`, preflight

**Files:**
- Create: `internal/merge/rebase.go`
- Modify: `internal/merge/merge.go` (Outcome constants)
- Test: `internal/merge/rebase_test.go`

**Interfaces:**
- Produces:
  - `const (Merged Outcome = iota; Conflicted; UpToDate; Rebased; Picked; NothingToApply)`. Append the new three **after** `UpToDate`, because the frontend already uses 0/1/2.
  - `var ErrOperationInProgress, ErrDirtyWorktree, ErrDetachedHead error`
  - `func HasTrackedChanges(ctx context.Context, dir string) (bool, error)`
  - `func preflight(ctx context.Context, dir string) error` (unexported; Task 2 uses it)
  - `func Rebase(ctx context.Context, dir, onto string) (Result, error)`
  - `type Preview struct { Commits, Merges, Published int; Upstream string }` with JSON tags `commits`, `merges`, `published`, `upstream`
  - `func RebasePreview(ctx context.Context, dir, onto string) (Preview, error)`
  - `func IsAncestorOfHead(ctx context.Context, dir, rev string) (bool, error)`
  - `func countRevs(ctx context.Context, dir string, args ...string) (int, error)` (unexported)

- [ ] **Step 1: Write the failing tests** in `internal/merge/rebase_test.go`:

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

// diverged: main and feature each have one commit of their own after base,
// touching different files, and feature is checked out.
func diverged(t *testing.T) *testrepo.Repo {
	t.Helper()
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("switch", "-q", "-c", "feature")
	r.Commit("on feature")
	r.Git("switch", "-q", "main")
	r.Commit("on main")
	r.Git("switch", "-q", "feature")
	return r
}

func TestRebaseReplaysCommits(t *testing.T) {
	r := diverged(t)
	got, err := Rebase(context.Background(), r.Dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != Rebased {
		t.Fatalf("outcome = %v, want Rebased", got.Outcome)
	}
	r.Git("merge-base", "--is-ancestor", "main", "HEAD")
	if n := r.Git("rev-list", "--count", "main..HEAD"); n != "1" {
		t.Errorf("commits on top of main = %s, want 1", n)
	}
}

func TestRebaseUpToDate(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("switch", "-q", "-c", "feature")
	r.Commit("on feature")
	got, err := Rebase(context.Background(), r.Dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != UpToDate {
		t.Fatalf("outcome = %v, want UpToDate", got.Outcome)
	}
}

func TestRebaseConflicted(t *testing.T) {
	r := conflicting(t)
	r.Git("switch", "-q", "feature")
	got, err := Rebase(context.Background(), r.Dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != Conflicted || strings.Join(got.Conflicts, ",") != "greeting.txt" {
		t.Fatalf("result = %+v, want Conflicted on greeting.txt", got)
	}
	if st := status(t, r.Dir); st.Kind != KindRebase {
		t.Fatalf("kind = %q, want rebase", st.Kind)
	}
	data, _ := os.ReadFile(filepath.Join(r.Dir, "greeting.txt"))
	if !strings.Contains(string(data), "|||||||") {
		t.Errorf("greeting.txt = %q, want zdiff3 markers with the ancestor", data)
	}
}

func TestRebaseOntoARemoteTrackingBranch(t *testing.T) {
	r := diverged(t)
	r.Git("update-ref", "refs/remotes/origin/main", r.Git("rev-parse", "main"))
	got, err := Rebase(context.Background(), r.Dir, "origin/main")
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != Rebased {
		t.Fatalf("outcome = %v, want Rebased", got.Outcome)
	}
}

func TestRebaseRefusesTrackedChangesEvenWithAutoStash(t *testing.T) {
	r := diverged(t)
	r.Git("config", "rebase.autoStash", "true")
	r.WriteFile("file-1.txt", "edited\n") // tracked since "base"
	before := r.Git("rev-parse", "HEAD")
	if _, err := Rebase(context.Background(), r.Dir, "main"); !errors.Is(err, ErrDirtyWorktree) {
		t.Fatalf("err = %v, want ErrDirtyWorktree", err)
	}
	if r.Git("rev-parse", "HEAD") != before {
		t.Error("HEAD moved on a refused rebase")
	}
}

func TestRebaseAllowsUntrackedFiles(t *testing.T) {
	r := diverged(t)
	r.WriteFile("scratch.txt", "not tracked\n")
	if _, err := Rebase(context.Background(), r.Dir, "main"); err != nil {
		t.Fatal(err)
	}
}

func TestRebaseStoppedByAnUntrackedFileLeavesNothingBehind(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("switch", "-q", "-c", "feature")
	r.WriteFile("clash.txt", "feature\n")
	r.Git("add", "clash.txt")
	r.Git("commit", "-q", "-m", "add clash")
	r.Git("switch", "-q", "main")
	r.Commit("on main")
	r.Git("switch", "-q", "feature")
	r.Git("rm", "-q", "--cached", "clash.txt")
	r.Git("commit", "-q", "-m", "untrack clash") // clash.txt stays on disk, untracked
	before := r.Git("rev-parse", "HEAD")
	_, err := Rebase(context.Background(), r.Dir, "main")
	if err == nil {
		t.Fatal("want git's refusal to overwrite the untracked file")
	}
	if st := status(t, r.Dir); st.Merging {
		t.Fatalf("state = %+v, want no rebase left behind", st)
	}
	if r.Git("rev-parse", "HEAD") != before {
		t.Error("HEAD moved")
	}
}

func TestRebaseRefusesWhileAnotherOperationIsInProgress(t *testing.T) {
	r := conflicting(t)
	r.GitFails("cherry-pick", "feature")
	if _, err := Rebase(context.Background(), r.Dir, "feature"); !errors.Is(err, ErrOperationInProgress) {
		t.Fatalf("err = %v, want ErrOperationInProgress", err)
	}
}

func TestRebaseRefusesADetachedHead(t *testing.T) {
	r := diverged(t)
	r.Git("switch", "-q", "--detach")
	if _, err := Rebase(context.Background(), r.Dir, "main"); !errors.Is(err, ErrDetachedHead) {
		t.Fatalf("err = %v, want ErrDetachedHead", err)
	}
}

func TestRebaseUnknownRefLeavesNothing(t *testing.T) {
	r := diverged(t)
	if _, err := Rebase(context.Background(), r.Dir, "nope"); err == nil {
		t.Fatal("want an error")
	}
	if _, err := Rebase(context.Background(), r.Dir, "--root"); !errors.Is(err, ErrInvalidRef) {
		t.Fatalf("err = %v, want ErrInvalidRef", err)
	}
	if st := status(t, r.Dir); st.Merging {
		t.Fatalf("state = %+v", st)
	}
}

func TestRebasePreview(t *testing.T) {
	r := diverged(t) // feature: 1 commit ahead of main
	r.Commit("second on feature")
	r.Git("branch", "pub", "HEAD~1") // "published": the first feature commit
	r.Git("config", "branch.feature.remote", ".")
	r.Git("config", "branch.feature.merge", "refs/heads/pub")
	r.Git("switch", "-q", "-c", "side", "HEAD~1")
	r.Commit("on side")
	r.Git("switch", "-q", "feature")
	r.Git("merge", "-q", "--no-ff", "--no-edit", "side")

	p, err := RebasePreview(context.Background(), r.Dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	// Replayed: "on feature", "second on feature", "on side" (merges are flattened, not replayed).
	if p.Commits != 3 || p.Merges != 1 || p.Published != 1 || p.Upstream != "pub" {
		t.Fatalf("preview = %+v, want 3 commits, 1 merge, 1 published on pub", p)
	}
}

func TestRebasePreviewWithoutUpstream(t *testing.T) {
	r := diverged(t)
	p, err := RebasePreview(context.Background(), r.Dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if p.Commits != 1 || p.Published != 0 || p.Upstream != "" {
		t.Fatalf("preview = %+v", p)
	}
}

func TestIsAncestorOfHead(t *testing.T) {
	r := diverged(t)
	base := r.Git("rev-parse", "HEAD~1")
	for rev, want := range map[string]bool{base: true, "main": false, "HEAD": true} {
		got, err := IsAncestorOfHead(context.Background(), r.Dir, rev)
		if err != nil || got != want {
			t.Errorf("IsAncestorOfHead(%s) = %v, %v; want %v", rev, got, err, want)
		}
	}
	if _, err := IsAncestorOfHead(context.Background(), r.Dir, "nope"); err == nil {
		t.Error("want an error for an unknown rev")
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/merge/ -run 'Rebase|IsAncestor'`. Expected: FAIL to compile (`Rebase` undefined).

- [ ] **Step 3: Implement.** In `internal/merge/merge.go`, extend the Outcome block:

```go
const (
	Merged Outcome = iota
	Conflicted
	UpToDate
	Rebased        // a rebase replayed or fast-forwarded the branch
	Picked         // a cherry-pick made its commit
	NothingToApply // a cherry-pick whose changes the branch already has
)
```

Create `internal/merge/rebase.go`:

```go
package merge

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"git-ui/internal/gitcmd"
)

var (
	// ErrOperationInProgress: git refuses to start one sequencer operation
	// inside another, and its conflicts would not be this one's.
	ErrOperationInProgress = errors.New("finish the operation in progress first")
	// ErrDirtyWorktree is checked before git runs, so a user's
	// rebase.autoStash never stashes behind the app's back.
	ErrDirtyWorktree = errors.New("commit or stash your changes first")
	ErrDetachedHead  = errors.New("no branch is checked out")
)

// HasTrackedChanges reports staged or unstaged changes to tracked files.
// Untracked files don't count (git refuses on its own if one is in the way),
// nor do submodules, which `git rebase` ignores when it checks for a clean tree.
func HasTrackedChanges(ctx context.Context, dir string) (bool, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "status", "--porcelain", "--untracked-files=no", "--ignore-submodules=all")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// preflight is what a rebase or a cherry-pick needs before git runs: a
// branch checked out, nothing else in progress, and a clean tracked tree.
func preflight(ctx context.Context, dir string) error {
	if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "symbolic-ref", "-q", "HEAD"); err != nil {
		return ErrDetachedHead
	}
	st, err := Status(ctx, dir)
	if err != nil {
		return err
	}
	if st.Merging {
		return ErrOperationInProgress
	}
	dirty, err := HasTrackedChanges(ctx, dir)
	if err != nil {
		return err
	}
	if dirty {
		return ErrDirtyWorktree
	}
	return nil
}

// Rebase replays the current branch's commits onto onto. A rebase stopped on
// conflicts is left for the Conflicts view; one git stopped for any other
// reason (an untracked file in the way, a hook) is aborted so the user is
// never left in a half-started rebase they did not see begin.
func Rebase(ctx context.Context, dir, onto string) (Result, error) {
	if err := checkRef(onto); err != nil {
		return Result{}, err
	}
	if err := preflight(ctx, dir); err != nil {
		return Result{}, err
	}
	before, err := head(ctx, dir)
	if err != nil {
		return Result{}, err
	}
	_, err = gitcmd.RunEnv(ctx, dir, gitcmd.HookTimeout, noEditor,
		"-c", "merge.conflictStyle=zdiff3", "rebase", onto)
	if err == nil {
		after, err := head(ctx, dir)
		if err != nil {
			return Result{}, err
		}
		if after == before {
			return Result{Outcome: UpToDate}, nil
		}
		return Result{Outcome: Rebased}, nil
	}
	if _, ok := inRebase(ctx, dir); ok {
		if conflicts, listErr := Unmerged(ctx, dir); listErr == nil && len(conflicts) > 0 {
			return Result{Outcome: Conflicted, Conflicts: conflicts}, nil
		}
		_, _ = gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rebase", "--abort")
	}
	return Result{}, err
}

// Preview is what the rebase confirmation says: how many commits will be
// replayed, how many merges flattened, and how many are already on the
// upstream (so a force-push would be needed).
type Preview struct {
	Commits   int    `json:"commits"`
	Merges    int    `json:"merges"`
	Published int    `json:"published"`
	Upstream  string `json:"upstream"`
}

func RebasePreview(ctx context.Context, dir, onto string) (Preview, error) {
	var p Preview
	if err := checkRef(onto); err != nil {
		return p, err
	}
	var err error
	if p.Commits, err = countRevs(ctx, dir, "--no-merges", "HEAD", "^"+onto); err != nil {
		return p, err
	}
	if p.Merges, err = countRevs(ctx, dir, "--merges", "HEAD", "^"+onto); err != nil {
		return p, err
	}
	up, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil {
		return p, nil // no upstream: nothing can have been pushed
	}
	p.Upstream = strings.TrimSpace(up)
	// ^@{upstream}, not the abbreviated name, which a local branch could share.
	local, err := countRevs(ctx, dir, "--no-merges", "HEAD", "^"+onto, "^@{upstream}")
	if err != nil {
		return p, err
	}
	p.Published = p.Commits - local
	return p, nil
}

// IsAncestorOfHead reports whether rev is already contained in HEAD: a
// rebase onto it has nothing to do, and a cherry-pick of it nothing to apply.
func IsAncestorOfHead(ctx context.Context, dir, rev string) (bool, error) {
	if err := checkRef(rev); err != nil {
		return false, err
	}
	if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", rev+"^{commit}"); err != nil {
		return false, err
	}
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "merge-base", "--is-ancestor", rev, "HEAD")
	if err == nil {
		return true, nil
	}
	var gerr *gitcmd.Error
	if errors.As(err, &gerr) && gerr.ExitCode == 1 {
		return false, nil
	}
	return false, err
}

func countRevs(ctx context.Context, dir string, args ...string) (int, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, append([]string{"rev-list", "--count"}, args...)...)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(out))
}
```

If `TestRebaseStoppedByAnUntrackedFileLeavesNothingBehind` shows that git refuses **before** creating a rebase directory, the test still passes. If git stops **inside** the rebase, the `rebase --abort` branch is what makes it pass. Either way, keep the test.

- [ ] **Step 4: Run** `go test ./internal/merge/ && go vet ./internal/merge/ && gofmt -l internal`. Expected: PASS, no files listed.

- [ ] **Step 5: Commit** (no user-visible behaviour yet, so no `docs/spec/` change):

```bash
git add internal/merge/rebase.go internal/merge/rebase_test.go internal/merge/merge.go
git commit -m "feat(merge): start a rebase, preview it, and check ancestry"
```

---

### Task 2: `merge.CherryPick` and `merge.Skip`

**Files:**
- Create: `internal/merge/cherrypick.go`
- Test: `internal/merge/cherrypick_test.go`

**Interfaces:**
- Consumes: `preflight`, `checkRef`, `head`, `pickedCommit`, `Unmerged`, `noEditor`, `sequencer`, Outcomes `Picked`, `NothingToApply`, `Conflicted` (Task 1).
- Produces:
  - `var ErrMergeCommit, ErrNothingToSkip error`
  - `func CherryPick(ctx context.Context, dir, rev string) (Result, error)`
  - `func Skip(ctx context.Context, dir string) error`

- [ ] **Step 1: Write the failing tests** in `internal/merge/cherrypick_test.go`:

```go
package merge

import (
	"context"
	"errors"
	"strings"
	"testing"

	"git-ui/internal/testrepo"
)

func TestCherryPickClean(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("switch", "-q", "-c", "feature")
	picked := r.Commit("on feature")
	r.Git("switch", "-q", "main")
	got, err := CherryPick(context.Background(), r.Dir, picked)
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != Picked {
		t.Fatalf("outcome = %v, want Picked", got.Outcome)
	}
	if s := r.Git("log", "-1", "--format=%s"); s != "on feature" {
		t.Errorf("HEAD subject = %q", s)
	}
}

func TestCherryPickConflicted(t *testing.T) {
	r := conflicting(t) // on main
	got, err := CherryPick(context.Background(), r.Dir, "feature")
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != Conflicted || strings.Join(got.Conflicts, ",") != "greeting.txt" {
		t.Fatalf("result = %+v", got)
	}
	if st := status(t, r.Dir); st.Kind != KindCherryPick {
		t.Fatalf("kind = %q", st.Kind)
	}
}

func TestCherryPickOfChangesAlreadyThereIsNothingToApply(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("switch", "-q", "-c", "feature")
	r.WriteFile("a.txt", "same\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-m", "add a on feature")
	r.Git("switch", "-q", "main")
	r.WriteFile("a.txt", "same\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-m", "add a on main")
	before := r.Git("rev-parse", "HEAD")

	got, err := CherryPick(context.Background(), r.Dir, "feature")
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != NothingToApply {
		t.Fatalf("outcome = %v, want NothingToApply", got.Outcome)
	}
	if st := status(t, r.Dir); st.Merging {
		t.Fatalf("state = %+v, want no cherry-pick left in progress", st)
	}
	if r.Git("rev-parse", "HEAD") != before {
		t.Error("HEAD moved")
	}
}

func TestCherryPickRefusesAMergeCommit(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("switch", "-q", "-c", "side")
	r.Commit("on side")
	r.Git("switch", "-q", "main")
	r.Commit("on main")
	r.Git("merge", "-q", "--no-ff", "--no-edit", "side")
	mergeCommit := r.Git("rev-parse", "HEAD")
	r.Git("switch", "-q", "-c", "other", "HEAD~1")
	if _, err := CherryPick(context.Background(), r.Dir, mergeCommit); !errors.Is(err, ErrMergeCommit) {
		t.Fatalf("err = %v, want ErrMergeCommit", err)
	}
}

func TestCherryPickRefusesTrackedChanges(t *testing.T) {
	r := conflicting(t)
	r.WriteFile("greeting.txt", "dirty\n")
	if _, err := CherryPick(context.Background(), r.Dir, "feature"); !errors.Is(err, ErrDirtyWorktree) {
		t.Fatalf("err = %v, want ErrDirtyWorktree", err)
	}
}

func TestSkipACherryPick(t *testing.T) {
	r := conflicting(t)
	before := r.Git("rev-parse", "HEAD")
	if _, err := CherryPick(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	if err := Skip(context.Background(), r.Dir); err != nil {
		t.Fatal(err)
	}
	if st := status(t, r.Dir); st.Merging {
		t.Fatalf("state = %+v", st)
	}
	if r.Git("rev-parse", "HEAD") != before {
		t.Error("a skipped cherry-pick moved HEAD")
	}
}

func TestSkipARebaseStep(t *testing.T) {
	r := conflicting(t)
	r.Git("switch", "-q", "feature")
	if _, err := Rebase(context.Background(), r.Dir, "main"); err != nil {
		t.Fatal(err)
	}
	if err := Skip(context.Background(), r.Dir); err != nil {
		t.Fatal(err)
	}
	if st := status(t, r.Dir); st.Merging {
		t.Fatalf("state = %+v", st)
	}
	// The only feature commit was skipped: feature now sits on main.
	if r.Git("rev-parse", "HEAD") != r.Git("rev-parse", "main") {
		t.Error("want feature == main after skipping its only commit")
	}
}

func TestSkipRefusesAMerge(t *testing.T) {
	r := conflicting(t)
	if _, err := Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	if err := Skip(context.Background(), r.Dir); !errors.Is(err, ErrNothingToSkip) {
		t.Fatalf("err = %v, want ErrNothingToSkip", err)
	}
}

// Taking the base's side wholesale empties the replayed commit. Depending on
// git's version and backend, Continue then either finishes or refuses with
// "No changes"; either way Continue followed by Skip must get the user out.
func TestAnEmptiedRebaseStepCanAlwaysBeLeft(t *testing.T) {
	r := conflicting(t)
	r.Git("switch", "-q", "feature")
	if _, err := Rebase(context.Background(), r.Dir, "main"); err != nil {
		t.Fatal(err)
	}
	r.WriteFile("greeting.txt", "hi\n") // main's version
	r.Git("add", "greeting.txt")
	if err := Continue(context.Background(), r.Dir); err != nil {
		if err := Skip(context.Background(), r.Dir); err != nil {
			t.Fatalf("Continue failed and Skip failed too: %v", err)
		}
	}
	if st := status(t, r.Dir); st.Merging {
		t.Fatalf("state = %+v, want the rebase finished", st)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/merge/ -run 'CherryPick|Skip|Emptied'`. Expected: FAIL to compile.

- [ ] **Step 3: Implement** `internal/merge/cherrypick.go`:

```go
package merge

import (
	"context"
	"errors"
	"strings"

	"git-ui/internal/gitcmd"
)

var (
	ErrMergeCommit   = errors.New("cherry-picking a merge commit isn't supported")
	ErrNothingToSkip = errors.New("only a rebase or a cherry-pick step can be skipped")
)

// CherryPick applies one commit on top of the current branch. An emptied
// pick (the branch already has those changes) is skipped here and reported
// as NothingToApply, so the user is never left holding a CHERRY_PICK_HEAD
// with nothing to resolve. Any other stop that is not a conflict is aborted.
func CherryPick(ctx context.Context, dir, rev string) (Result, error) {
	if err := checkRef(rev); err != nil {
		return Result{}, err
	}
	parents, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-list", "--parents", "-n", "1", rev+"^{commit}")
	if err != nil {
		return Result{}, err
	}
	if len(strings.Fields(parents)) > 2 {
		return Result{}, ErrMergeCommit
	}
	if err := preflight(ctx, dir); err != nil {
		return Result{}, err
	}
	before, err := head(ctx, dir)
	if err != nil {
		return Result{}, err
	}
	_, err = gitcmd.RunEnv(ctx, dir, gitcmd.HookTimeout, noEditor,
		"-c", "merge.conflictStyle=zdiff3", "cherry-pick", rev)
	if err == nil {
		if after, headErr := head(ctx, dir); headErr == nil && after == before {
			return Result{Outcome: NothingToApply}, nil
		}
		return Result{Outcome: Picked}, nil
	}
	if pickedCommit(ctx, dir, "CHERRY_PICK_HEAD") == "" {
		return Result{}, err
	}
	if conflicts, listErr := Unmerged(ctx, dir); listErr == nil && len(conflicts) > 0 {
		return Result{Outcome: Conflicted, Conflicts: conflicts}, nil
	}
	// No conflicts and an index identical to HEAD: the pick came out empty.
	if _, diffErr := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "diff", "--cached", "--quiet", "HEAD"); diffErr == nil {
		if _, skipErr := gitcmd.RunEnv(ctx, dir, gitcmd.HookTimeout, noEditor, "cherry-pick", "--skip"); skipErr == nil {
			return Result{Outcome: NothingToApply}, nil
		}
	}
	_, _ = gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "cherry-pick", "--abort")
	return Result{}, err
}

// Skip drops the commit a rebase or a cherry-pick is stopped on and moves on.
func Skip(ctx context.Context, dir string) error {
	st, err := Status(ctx, dir)
	if err != nil {
		return err
	}
	if st.Kind != KindRebase && st.Kind != KindCherryPick {
		return ErrNothingToSkip
	}
	_, err = gitcmd.RunEnv(ctx, dir, gitcmd.HookTimeout, noEditor, sequencer[st.Kind], "--skip")
	return err
}
```

- [ ] **Step 4: Run** `go test ./internal/merge/ && go vet ./internal/merge/ && gofmt -l internal`. Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/merge/cherrypick.go internal/merge/cherrypick_test.go
git commit -m "feat(merge): cherry-pick one commit and skip a rebase or cherry-pick step"
```

---

### Task 3: `merge.Fingerprint` and named sides in `Status`

**Files:**
- Create: `internal/merge/fingerprint.go`
- Modify: `internal/merge/state.go` (the `State` struct and the kind `switch` in `Status`)
- Test: `internal/merge/fingerprint_test.go`

**Interfaces:**
- Consumes: `pickedCommit`, `inRebase`, `isApplyingMailbox`, `head`, `sequencerFromInto` (existing).
- Produces:
  - `func Fingerprint(ctx context.Context, dir string) string`: `"merge:<hash>"`, `"cherry-pick:<hash>"`, `"revert:<hash>"`, `"rebase:<REBASE_HEAD or HEAD>"`, `"am:<HEAD>"`, or `""`.
  - `State` fields `OursLabel`, `TheirsLabel` (JSON `oursLabel`, `theirsLabel`, `omitempty`) and `OursDescription`, `TheirsDescription` (JSON `oursDescription`, `theirsDescription`, `omitempty`). Set only for merge, rebase and cherry-pick.

- [ ] **Step 1: Write the failing tests** in `internal/merge/fingerprint_test.go`:

```go
package merge

import (
	"context"
	"strings"
	"testing"

	"git-ui/internal/testrepo"
)

// twoStepRebase stops a rebase of feature onto main on its first of two
// conflicting commits.
func twoStepRebase(t *testing.T) *testrepo.Repo {
	t.Helper()
	r := conflicting(t) // feature: "greet in spanish"; main: "greet informally"
	r.Git("switch", "-q", "feature")
	r.WriteFile("greeting.txt", "hola!!\n")
	r.Git("commit", "-q", "-am", "shout in spanish")
	r.GitFails("rebase", "main")
	return r
}

func TestFingerprintIsEmptyWithNothingInProgress(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	if fp := Fingerprint(context.Background(), r.Dir); fp != "" {
		t.Fatalf("fingerprint = %q", fp)
	}
}

func TestFingerprintNamesTheKindAndCommit(t *testing.T) {
	r := conflicting(t)
	feature := r.Git("rev-parse", "feature")
	r.GitFails("merge", "feature")
	if fp := Fingerprint(context.Background(), r.Dir); fp != "merge:"+feature {
		t.Errorf("merge fingerprint = %q", fp)
	}
	r.Git("merge", "--abort")
	r.GitFails("cherry-pick", "feature")
	if fp := Fingerprint(context.Background(), r.Dir); fp != "cherry-pick:"+feature {
		t.Errorf("cherry-pick fingerprint = %q", fp)
	}
}

func TestFingerprintChangesBetweenRebaseSteps(t *testing.T) {
	r := twoStepRebase(t)
	first := Fingerprint(context.Background(), r.Dir)
	if !strings.HasPrefix(first, "rebase:") {
		t.Fatalf("fingerprint = %q", first)
	}
	r.WriteFile("greeting.txt", "resolved one\n")
	r.Git("add", "greeting.txt")
	if err := Continue(context.Background(), r.Dir); err != nil {
		t.Fatal(err)
	}
	if st := status(t, r.Dir); st.Kind != KindRebase || st.Step != 2 {
		t.Fatalf("state = %+v, want stopped on step 2", st)
	}
	if second := Fingerprint(context.Background(), r.Dir); second == first || !strings.HasPrefix(second, "rebase:") {
		t.Fatalf("second fingerprint = %q, first = %q", second, first)
	}
}

func TestStatusNamesTheSides(t *testing.T) {
	t.Run("merge", func(t *testing.T) {
		r := conflicting(t)
		r.GitFails("merge", "feature")
		st := status(t, r.Dir)
		if st.OursLabel != "main" || st.TheirsLabel != "feature" {
			t.Fatalf("labels = %q / %q", st.OursLabel, st.TheirsLabel)
		}
		if !strings.Contains(st.OursDescription, "merging into") || !strings.Contains(st.TheirsDescription, "being merged") {
			t.Fatalf("descriptions = %q / %q", st.OursDescription, st.TheirsDescription)
		}
	})
	t.Run("rebase", func(t *testing.T) {
		r := conflicting(t)
		r.Git("switch", "-q", "feature")
		short := r.Git("rev-parse", "--short", "feature")
		r.GitFails("rebase", "main")
		st := status(t, r.Dir)
		if st.OursLabel != "main" || st.TheirsLabel != short+" greet in spanish" {
			t.Fatalf("labels = %q / %q", st.OursLabel, st.TheirsLabel)
		}
		if !strings.Contains(st.OursDescription, "rebased onto") || !strings.Contains(st.TheirsDescription, "being replayed") {
			t.Fatalf("descriptions = %q / %q", st.OursDescription, st.TheirsDescription)
		}
	})
	t.Run("cherry-pick", func(t *testing.T) {
		r := conflicting(t)
		short := r.Git("rev-parse", "--short", "feature")
		r.GitFails("cherry-pick", "feature")
		st := status(t, r.Dir)
		if st.OursLabel != "main" || st.TheirsLabel != short+" greet in spanish" {
			t.Fatalf("labels = %q / %q", st.OursLabel, st.TheirsLabel)
		}
		if !strings.Contains(st.TheirsDescription, "being cherry-picked") {
			t.Fatalf("description = %q", st.TheirsDescription)
		}
	})
}
```

- [ ] **Step 2: Run** `go test ./internal/merge/ -run 'Fingerprint|NamesTheSides'`. Expected: FAIL to compile.

- [ ] **Step 3: Implement.** Create `internal/merge/fingerprint.go`:

```go
package merge

import "context"

// Fingerprint identifies the operation in progress as "<kind>:<commit>", so
// something started for one (an AI resolver run) can tell when it is gone.
// A rebase is keyed by the commit being replayed, so each stopped step is a
// different operation. It checks the markers in Status's order and returns
// "" when nothing with a marker is in progress (a stash conflict has none).
func Fingerprint(ctx context.Context, dir string) string {
	for _, k := range []struct {
		kind Kind
		ref  string
	}{{KindCherryPick, "CHERRY_PICK_HEAD"}, {KindRevert, "REVERT_HEAD"}, {KindMerge, "MERGE_HEAD"}} {
		if h := pickedCommit(ctx, dir, k.ref); h != "" {
			return string(k.kind) + ":" + h
		}
	}
	rebaseDir, ok := inRebase(ctx, dir)
	if !ok {
		return ""
	}
	h, _ := head(ctx, dir)
	if isApplyingMailbox(rebaseDir) {
		return string(KindAM) + ":" + h
	}
	if replayed := pickedCommit(ctx, dir, "REBASE_HEAD"); replayed != "" {
		return string(KindRebase) + ":" + replayed
	}
	return string(KindRebase) + ":" + h // older backend: HEAD moves with every step
}
```

In `internal/merge/state.go`, add these to `State` after `Subject`:

```go
	// OursLabel and TheirsLabel name the two sides of a conflict for the
	// view ("main", "a1b2c3 fix login"); the descriptions add what each
	// side is, for the AI resolver. A rebase swaps git's ours and theirs
	// relative to a merge, which is why the names come from here. Set for
	// merge, rebase and cherry-pick only.
	OursLabel         string `json:"oursLabel,omitempty"`
	TheirsLabel       string `json:"theirsLabel,omitempty"`
	OursDescription   string `json:"oursDescription,omitempty"`
	TheirsDescription string `json:"theirsDescription,omitempty"`
```

In `Status`, right after the kind `switch` ends and before `if st.Kind == "" {`, add `nameSides(ctx, dir, &st)`. Add this function to `state.go`:

```go
func nameSides(ctx context.Context, dir string, st *State) {
	commit := func(short, subject string) string { return strings.TrimSpace(short + " " + subject) }
	switch st.Kind {
	case KindMerge:
		st.OursLabel, st.TheirsLabel = st.Into, st.From
		st.OursDescription = st.Into + " (the branch you are merging into)"
		st.TheirsDescription = st.From + " (the branch being merged)"
	case KindCherryPick:
		st.OursLabel, st.TheirsLabel = st.Into, commit(st.From, st.Subject)
		st.OursDescription = st.Into + " (the current branch)"
		st.TheirsDescription = st.From + " \"" + st.Subject + "\" (the commit being cherry-picked)"
	case KindRebase:
		st.OursLabel = st.Into
		st.OursDescription = st.Into + " (the base being rebased onto)"
		if short, subject := sequencerFromInto(ctx, dir, "REBASE_HEAD"); short != "" {
			st.TheirsLabel = commit(short, subject)
			st.TheirsDescription = short + " \"" + subject + "\" (your commit being replayed)"
		} else {
			st.TheirsLabel = st.From
			st.TheirsDescription = st.From + " (your commits being replayed)"
		}
	}
}
```

- [ ] **Step 4: Run** `go test ./internal/merge/ && go vet ./internal/merge/ && gofmt -l internal`. Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/merge/fingerprint.go internal/merge/fingerprint_test.go internal/merge/state.go
git commit -m "feat(merge): fingerprint the operation in progress and name its sides"
```

---

### Task 4: AI resolver for rebase and cherry-pick

**Files:**
- Modify: `internal/ai/mergetools/mergetools.go` (`Run`, `readConflict`)
- Modify: `internal/ai/mergetools/mergetools_test.go` (every `Run(` call gains `mergetools.Sides{}` or `Sides{}`, plus one new test)
- Modify: `internal/app/merge.go` (`ResolveConflicts`, `runMergeTool`; delete `mergeHead`)
- Modify: `internal/app/merge_test.go` (new tests)
- Modify: `frontend/src/lib/merge.ts` (`ACTIONS`, and the comment above it), `frontend/src/lib/merge.test.ts`
- Modify: `docs/spec/04-conflicts.md` ("The AI conflict resolver" section, Rule 7), `docs/spec/06-ai.md` (resolver section around lines 267–301, rule 7 around line 356)

**Interfaces:**
- Consumes: `merge.Fingerprint`, `State.OursDescription/TheirsDescription` (Task 3).
- Produces: `type mergetools.Sides struct{ Ours, Theirs string }`; `func mergetools.Run(ctx context.Context, dir string, call ai.ToolCall, sides Sides) (string, bool)`.

- [ ] **Step 1: Write the failing tests.**

In `internal/ai/mergetools/mergetools_test.go` (package `mergetools_test`; `conflicted(t)` and `call(name, args)` already exist there), add:

```go
func TestReadConflictPrintsTheGivenSides(t *testing.T) {
	r := conflicted(t)
	c := call("read_conflict", map[string]any{"path": "greeting.txt", "hunk": 0.0})
	out, _ := mergetools.Run(context.Background(), r.Dir, c, mergetools.Sides{
		Ours: "main (the base being rebased onto)", Theirs: `a1b2c3 "fix" (your commit being replayed)`,
	})
	if !strings.Contains(out, "--- main (the base being rebased onto) ---") || !strings.Contains(out, `--- a1b2c3 "fix" (your commit being replayed) ---`) {
		t.Fatalf("out = %s", out)
	}
	out, _ = mergetools.Run(context.Background(), r.Dir, c, mergetools.Sides{})
	if !strings.Contains(out, "--- our side (the branch you are merging into) ---") {
		t.Fatalf("zero Sides must keep the merge wording; out = %s", out)
	}
}
```

Every existing `mergetools.Run(ctx, dir, c)` call in this file becomes `mergetools.Run(ctx, dir, c, mergetools.Sides{})`.

In `internal/app/merge_test.go`, add:

```go
// newAIRebaseApp is newAIMergeApp stopped in a rebase of feature onto main
// with two conflicting steps instead of in a merge.
func newAIRebaseApp(t *testing.T, ollamaURL string) (*App, *testrepo.Repo, string, *events) {
	t.Helper()
	a, r, id, ev := newAIMergeApp(t, ollamaURL)
	_ = a.AbortMerge(id)
	r.Git("switch", "-q", "feature")
	r.WriteFile("greeting.txt", "hola!!\n")
	r.Git("commit", "-q", "-am", "shout")
	r.GitFails("rebase", "main")
	return a, r, id, ev
}

func TestResolveConflictsWorksOnARebase(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeLines(w, `{"message":{"role":"assistant","content":"Hecho."},"done":false}`, `{"message":{"content":""},"done":true}`)
	}))
	t.Cleanup(srv.Close)
	a, _, id, ev := newAIRebaseApp(t, srv.URL)
	if err := a.ResolveConflicts(id, "run1"); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventStart)
	ev.wait(t, agent.EventDone)
	history, err := a.GetChat(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) == 0 || !strings.Contains(history[0].Content, "rebasing feature onto main") {
		t.Errorf("history = %#v, want the request to name the rebase", history)
	}
}

func TestResolveConflictsRefusesTheNextRebaseStep(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		msgs := req["messages"].([]any)
		if msgs[len(msgs)-1].(map[string]any)["role"] == "tool" {
			writeLines(w, `{"message":{"role":"assistant","content":"Vale."},"done":false}`, `{"message":{"content":""},"done":true}`)
			return
		}
		<-release
		writeLines(w, `{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","function":{"name":"resolve_hunk","arguments":{"path":"greeting.txt","hunk":0,"resolved":"stale\n"}}}]},"done":false}`, `{"message":{"content":""},"done":true}`)
	}))
	t.Cleanup(srv.Close)
	a, r, id, ev := newAIRebaseApp(t, srv.URL)
	if err := a.ResolveConflicts(id, "run1"); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventStart)
	// Step 1 is finished behind the run's back (a terminal), leaving step 2.
	r.WriteFile("greeting.txt", "step one\n")
	r.Git("add", "greeting.txt")
	if err := merge.Continue(context.Background(), r.Dir); err != nil {
		t.Fatal(err)
	}
	close(release)
	ev.wait(t, agent.EventDone)

	data, _ := os.ReadFile(filepath.Join(r.Dir, "greeting.txt"))
	if strings.Contains(string(data), "stale") || !strings.Contains(string(data), "<<<<<<<") {
		t.Fatalf("greeting.txt = %q: the run edited the next step", data)
	}
}

func TestResolveConflictsRefusesARevert(t *testing.T) {
	a, r, id, _ := newAIMergeApp(t, "http://127.0.0.1:0")
	_ = a.AbortMerge(id) // back on main, clean
	r.WriteFile("greeting.txt", "a\n")
	r.Git("commit", "-q", "-am", "a")
	r.WriteFile("greeting.txt", "b\n")
	r.Git("commit", "-q", "-am", "b")
	middle := r.Git("rev-parse", "HEAD")
	r.WriteFile("greeting.txt", "c\n")
	r.Git("commit", "-q", "-am", "c")
	r.GitFails("revert", "--no-edit", middle) // b→a conflicts with c

	err := a.ResolveConflicts(id, "run1")
	if err == nil || !strings.Contains(err.Error(), "merges, rebases and cherry-picks") {
		t.Fatalf("err = %v, want a refusal naming the three kinds", err)
	}
}
```

In `frontend/src/lib/merge.test.ts`, add:

```ts
it('offers Resolve with AI for merge, rebase and cherry-pick only', () => {
  const base = { merging: true, from: 'a', into: 'b', conflicts: ['x'], manual: [], staged: [], unstaged: [] }
  for (const kind of ['merge', 'rebase', 'cherry-pick'] as const) expect(conflictActions({ ...base, kind }).ai).toBe(true)
  for (const kind of ['revert', 'am', 'stash'] as const) expect(conflictActions({ ...base, kind }).ai).toBe(false)
})
```

(Add `conflictActions` to the file's import from `./merge` if it is not there yet.)

- [ ] **Step 2: Run** `go test ./internal/ai/mergetools/ ./internal/app/ -run 'ReadConflict|ResolveConflicts'` and `cd frontend && npm test`. Expected: FAIL (compile error on `Run` arity, rebase refusal, `ai: false`).

- [ ] **Step 3: Implement.**

`mergetools.go`: add the type, and thread it through `Run` into `readConflict`:

```go
// Sides describes the two sides of a conflict for the model. A rebase swaps
// git's ours and theirs relative to a merge, so the words come from the
// caller, who knows which operation is in progress. The zero value keeps
// the merge wording.
type Sides struct{ Ours, Theirs string }

func Run(ctx context.Context, dir string, call ai.ToolCall, sides Sides) (string, bool) {
	// ...
	case "read_conflict":
		return tools.Truncate(readConflict(ctx, dir, call.Args, sides), MaxResult), false
```

In `readConflict(ctx, dir, args, sides Sides)`, replace the two hard-coded lines with:

```go
	ours, theirs := sides.Ours, sides.Theirs
	if ours == "" {
		ours = "our side (the branch you are merging into)"
	}
	if theirs == "" {
		theirs = "their side (the branch being merged)"
	}
	fmt.Fprintf(&b, "--- %s ---\n%s\n", ours, h.Ours)
	fmt.Fprintf(&b, "--- %s ---\n%s\n", theirs, h.Theirs)
```

Also change the package comment's "the merge in progress" to "the merge, rebase or cherry-pick in progress". Change `listConflicts`' "This repository is not merging." to "Nothing is being merged, rebased or cherry-picked."

`internal/app/merge.go`:
- Delete `mergeHead`.
- Add:

```go
// resolvable are the kinds the conflict agent works on. Each has a
// fingerprint that changes when the operation (or, for a rebase, the step)
// does, which is what keeps a run from reaching into the next one.
var resolvable = map[merge.Kind]bool{merge.KindMerge: true, merge.KindRebase: true, merge.KindCherryPick: true}

// resolveRequest is the user message a resolve run starts with.
func resolveRequest(st merge.State) string {
	switch st.Kind {
	case merge.KindRebase:
		s := fmt.Sprintf("Resolve the conflicts from rebasing %s onto %s", st.From, st.Into)
		if st.Total > 0 {
			s += fmt.Sprintf(" (commit %d of %d: %s)", st.Step, st.Total, st.Subject)
		}
		return s
	case merge.KindCherryPick:
		return fmt.Sprintf("Resolve the conflicts from cherry-picking %s %q onto %s", st.From, st.Subject, st.Into)
	}
	return fmt.Sprintf("Resolve the conflicts from merging %s into %s", st.From, st.Into)
}

// kindGuidance is appended to the (user-editable) resolver prompt, so the
// model knows what each side is trying to do whatever the prompt says.
func kindGuidance(k merge.Kind) string {
	switch k {
	case merge.KindRebase:
		return "This is a rebase, one commit at a time. The base side is the code being rebased onto and is already final; re-apply the intent of the commit being replayed on top of it, without undoing what the base changed."
	case merge.KindCherryPick:
		return "This is a cherry-pick. The current branch's side is final; apply the intent of the commit being cherry-picked on top of it, without undoing what the current branch changed."
	}
	return "This is a merge. Keep the intent of both branches."
}
```

- In `ResolveConflicts`, replace the block from `// The run belongs to this merge` through the `startedFor == ""` check with:

```go
	if !resolvable[st.Kind] {
		return fmt.Errorf("the AI resolver handles merges, rebases and cherry-picks, not a %s", st.Kind)
	}
	// The run belongs to this operation (for a rebase, this step); its tools
	// refuse to act on any other.
	startedFor := merge.Fingerprint(a.ctx, repo.Path)
	if startedFor == "" {
		return errors.New("this repository is not merging")
	}
	sides := mergetools.Sides{Ours: st.OursDescription, Theirs: st.TheirsDescription}
```

  Replace `text := fmt.Sprintf("Resolve the conflicts from merging …")` with `text := resolveRequest(st)`. After `system, err = a.ai.deps.Prompts.Get(...)` succeeds, add `system += "\n\n" + kindGuidance(st.Kind)` (inside the `if err == nil` block, after the call). Change the tool dispatch to `a.runMergeTool(ctx, repoID, startedFor, sides, call)`.
- `runMergeTool(ctx context.Context, repoID, startedFor string, sides mergetools.Sides, call ai.ToolCall)`: the check becomes

```go
		if merge.Fingerprint(ctx, dir) != startedFor {
			out = "The operation this run was started for is no longer in progress; stop."
			return nil
		}
		out, changed = mergetools.Run(ctx, dir, call, sides)
```

  Update the doc comment's "the merge the run was started for" to "the operation the run was started for".
- `gitcmd` may now be unused in `app/merge.go`, but `GetConflictFile` still uses it, so leave the import.

`frontend/src/lib/merge.ts`: set `ai: true` for `rebase` and `'cherry-pick'` in `ACTIONS`. Replace the comment above `ACTIONS` with:

```ts
// "Resolve with AI" is offered for a merge, a rebase and a cherry-pick:
// ResolveConflicts ties a run to that operation's fingerprint (for a rebase,
// the step being replayed) and refuses every other kind. A stash conflict
// has no git-level abort or continue — it gets Done (and, when a conflicted
// Pop still owes one, Drop stash, which MergeView adds itself from
// OwedStashDrop rather than from this table).
```

`docs/spec/04-conflicts.md`: replace the second paragraph of "## The AI conflict resolver" ("The button is offered for a merge only, …in this view.") with:

```markdown
The button is offered for a merge, a rebase and a cherry-pick, and the
resolver refuses to run for any other kind. A run is tied to the operation
it started for by a fingerprint: the kind plus the commit being combined —
`MERGE_HEAD` for a merge, `CHERRY_PICK_HEAD` for a cherry-pick, and for a
rebase the commit currently being replayed (`REBASE_HEAD`, or `HEAD` on the
older backend that has none). Every tool call re-checks that fingerprint
before touching anything, which stops a run from editing a merge that has
since been aborted or replaced, and — because a rebase's fingerprint changes
with every step — from reaching into the next step's conflicts after the
one it was started for has been continued or skipped. A rebase with several
conflicting steps therefore needs one run per step. Revert, applied-patch
and stash conflicts are not offered the resolver.

What the resolver reads names each side for what it is rather than "ours"
and "theirs", since a rebase swaps the two relative to a merge: for a rebase
the base side is the branch being rebased onto and the other is the commit
being replayed; for a cherry-pick, the current branch and the picked commit.
Its instructions say, per kind, which side is final (a rebase's base, a
cherry-pick's current branch) and whose intent is to be carried over.
```

  Replace Rule 7 with:

```markdown
7. The AI resolver is offered only for a merge, a rebase or a cherry-pick,
   ties itself to that operation's fingerprint (for a rebase, the step being
   replayed), and refuses to keep acting once the fingerprint changes.
```

`docs/spec/06-ai.md`: in the resolver section (from "Once a merge, rebase or stash application is in conflict", around line 267), change every statement that the resolver works on merges only, or checks `MERGE_HEAD`, to say it works on a merge, a rebase or a cherry-pick and checks the operation's fingerprint, one rebase step per run. State that the kind-specific instructions are appended by the application after the user-editable prompt, so editing the prompt cannot remove them. Update rule 7 (around line 356) to: "Every mergetools call re-validates that its target file belongs to the operation the run was started for — the same kind and commit (for a rebase, the same step) — and refuses otherwise."

- [ ] **Step 4: Run** `go test ./... && go vet ./... && gofmt -l internal`, and from `frontend/`: `npm test && npm run check`. Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ai/mergetools internal/app/merge.go internal/app/merge_test.go frontend/src/lib/merge.ts frontend/src/lib/merge.test.ts docs/spec/04-conflicts.md docs/spec/06-ai.md
git commit -m "feat(ai): resolve rebase and cherry-pick conflicts, one step per run"
```

---

### Task 5: App bindings, Wails bindings and `api.ts`

**Files:**
- Create: `internal/app/rebase.go`, `internal/app/rebase_test.go`
- Modify (generated): `frontend/wailsjs/go/app/App.js`, `App.d.ts`, `frontend/wailsjs/go/models.ts`
- Modify: `frontend/src/lib/api.ts`, `frontend/src/lib/types.ts`

**Interfaces:**
- Consumes: `merge.Rebase`, `merge.RebasePreview`, `merge.CherryPick`, `merge.Skip`, `merge.IsAncestorOfHead` (Tasks 1–2).
- Produces (Go): `RebaseOnto(id, onto string) (merge.Result, error)`, `GetRebasePreview(id, onto string) (merge.Preview, error)`, `CherryPick(id, rev string) (merge.Result, error)`, `SkipStep(id string) error`, `IsAncestorOfHead(id, rev string) (bool, error)`.
- Produces (TS, `api`): `rebaseOnto(id, onto): Promise<MergeResult>`, `getRebasePreview(id, onto): Promise<RebasePreview>`, `cherryPick(id, rev): Promise<MergeResult>`, `skipStep(id): Promise<void>`, `isAncestorOfHead(id, rev): Promise<boolean>`.
- Produces (TS, `types.ts`): `REBASED = 3`, `PICKED = 4`, `NOTHING_TO_APPLY = 5`, `interface RebasePreview { commits: number; merges: number; published: number; upstream: string }`, and `MergeState` gains `oursLabel?: string; theirsLabel?: string`.

- [ ] **Step 1: Write the failing tests** in `internal/app/rebase_test.go`:

```go
package app

import (
	"testing"

	"git-ui/internal/merge"
)

func TestRebaseOntoLeavesConflictsForTheView(t *testing.T) {
	a, r, id := newMergeApp(t)
	r.Git("switch", "-q", "feature")
	result, err := a.RebaseOnto(id, "main")
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != merge.Conflicted {
		t.Fatalf("outcome = %v", result.Outcome)
	}
	st, err := a.GetMergeState(id)
	if err != nil || st.Kind != merge.KindRebase {
		t.Fatalf("state = %+v, %v", st, err)
	}
	if err := a.SkipStep(id); err != nil {
		t.Fatal(err)
	}
	if st, _ := a.GetMergeState(id); st.Merging {
		t.Fatalf("state after skip = %+v", st)
	}
}

func TestCherryPickAndPreviewThroughTheApp(t *testing.T) {
	a, r, id := newMergeApp(t) // on main; feature conflicts with it
	p, err := a.GetRebasePreview(id, "feature")
	if err != nil || p.Commits != 1 {
		t.Fatalf("preview = %+v, %v", p, err)
	}
	contained, err := a.IsAncestorOfHead(id, "feature")
	if err != nil || contained {
		t.Fatalf("IsAncestorOfHead(feature) = %v, %v", contained, err)
	}
	result, err := a.CherryPick(id, "feature")
	if err != nil || result.Outcome != merge.Conflicted {
		t.Fatalf("result = %+v, %v", result, err)
	}
	_ = r
}
```

- [ ] **Step 2: Run** `go test ./internal/app/ -run 'RebaseOnto|CherryPickAndPreview'`. Expected: FAIL to compile.

- [ ] **Step 3: Implement** `internal/app/rebase.go`:

```go
package app

import (
	"context"

	"git-ui/internal/merge"
)

// RebaseOnto replays the current branch onto onto (a branch, remote branch
// or commit). A conflicted rebase is left for the Conflicts view.
func (a *App) RebaseOnto(id, onto string) (merge.Result, error) {
	var result merge.Result
	err := a.write(id, func(ctx context.Context, dir string) error {
		var err error
		result, err = merge.Rebase(ctx, dir, onto)
		return err
	})
	return result, err
}

// GetRebasePreview is what the rebase confirmation shows.
func (a *App) GetRebasePreview(id, onto string) (merge.Preview, error) {
	dir, err := a.dir(id)
	if err != nil {
		return merge.Preview{}, err
	}
	return merge.RebasePreview(a.ctx, dir, onto)
}

// CherryPick applies one commit on top of the current branch.
func (a *App) CherryPick(id, rev string) (merge.Result, error) {
	var result merge.Result
	err := a.write(id, func(ctx context.Context, dir string) error {
		var err error
		result, err = merge.CherryPick(ctx, dir, rev)
		return err
	})
	return result, err
}

// SkipStep drops the commit a rebase or cherry-pick is stopped on. Like
// CommitMerge and AbortMerge it stops any resolver run first.
func (a *App) SkipStep(id string) error {
	a.stopRun(id)
	return a.write(id, func(ctx context.Context, dir string) error { return merge.Skip(ctx, dir) })
}

// IsAncestorOfHead lets the menus disable a rebase or cherry-pick with
// nothing to do.
func (a *App) IsAncestorOfHead(id, rev string) (bool, error) {
	dir, err := a.dir(id)
	if err != nil {
		return false, err
	}
	return merge.IsAncestorOfHead(a.ctx, dir, rev)
}
```

Regenerate the Wails bindings (see Global Constraints). In `frontend/src/lib/types.ts`, after `UP_TO_DATE`:

```ts
export const REBASED = 3
export const PICKED = 4
export const NOTHING_TO_APPLY = 5

export interface RebasePreview {
  commits: number
  merges: number
  published: number
  upstream: string
}
```

Add `oursLabel?: string` and `theirsLabel?: string` to `MergeState`. In `frontend/src/lib/api.ts`, next to `mergeBranch` (add `RebasePreview` to the type import):

```ts
  rebaseOnto: (id: string, onto: string) => call<MergeResult>(Go.RebaseOnto(id, onto)),
  getRebasePreview: (id: string, onto: string) => call<RebasePreview>(Go.GetRebasePreview(id, onto)),
  cherryPick: (id: string, rev: string) => call<MergeResult>(Go.CherryPick(id, rev)),
  skipStep: (id: string) => call<void>(Go.SkipStep(id)),
  isAncestorOfHead: (id: string, rev: string) => call<boolean>(Go.IsAncestorOfHead(id, rev)),
```

- [ ] **Step 4: Run** `go test ./... && go vet ./... && gofmt -l internal`, and from `frontend/`: `npm test && npm run check`. Expected: PASS.

- [ ] **Step 5: Commit** (bindings only, nothing user-visible yet):

```bash
git add internal/app/rebase.go internal/app/rebase_test.go frontend/wailsjs frontend/src/lib/api.ts frontend/src/lib/types.ts
git commit -m "feat(app): bind rebase, cherry-pick, skip and ancestry checks"
```

---

### Task 6: `cherry_pick` chat write tool

**Files:**
- Modify: `internal/ai/writetools/writetools.go` (`writeToolNames`, `Proposal`, `Specs`, `Prepare`, new `prepareCherryPick`)
- Modify: `internal/ai/writetools/writetools_test.go` (the `want` names list; new tests)
- Modify: `internal/app/chatwrite.go` (execute `cherry_pick`)
- Modify: `internal/ai/prompts/defaults/chat.md` (write tools list)
- Modify: `docs/spec/06-ai.md` (the write tools list/table; the "not offered" list keeps rebase)

**Interfaces:**
- Consumes: `merge.Status`, `merge.HasTrackedChanges`, `merge.IsAncestorOfHead`, `App.CherryPick`, `merge.Conflicted`, `merge.NothingToApply`.
- Produces: tool `cherry_pick` with argument `commit`; `Proposal.Commit string` (full hash).

- [ ] **Step 1: Write the failing tests** in `writetools_test.go`. Change `want` to end with `...,pull,merge_branch,cherry_pick`, then add:

```go
func TestCherryPickProposal(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("switch", "-q", "-c", "feature")
	picked := r.Commit("fix login")
	r.Git("switch", "-q", "main")
	short := r.Git("rev-parse", "--short", picked)

	p, err := prep(r.Dir, "cherry_pick", map[string]any{"commit": short})
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Cherry-pick "+short+" fix login onto main" || p.Commit != picked {
		t.Fatalf("proposal = %+v", p)
	}
}

func TestCherryPickRefusals(t *testing.T) {
	r := testrepo.New(t)
	base := r.Commit("base")
	r.Git("switch", "-q", "-c", "side")
	r.Commit("on side")
	r.Git("switch", "-q", "main")
	r.Commit("on main")
	r.Git("merge", "-q", "--no-ff", "--no-edit", "side")
	mergeCommit := r.Git("rev-parse", "HEAD")
	r.Git("switch", "-q", "-c", "other", base)
	picked := r.Commit("pick me")
	r.Git("switch", "-q", "main")

	for _, c := range []struct{ commit, want string }{
		{"-x", "invalid commit"},
		{"nope", `no commit "nope"`},
		{base, "already on main"},
		{mergeCommit, "merge commit"},
	} {
		if _, err := prep(r.Dir, "cherry_pick", map[string]any{"commit": c.commit}); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("cherry_pick(%s): err = %v, want %q", c.commit, err, c.want)
		}
	}
	r.WriteFile("file-1.txt", "dirty\n")
	if _, err := prep(r.Dir, "cherry_pick", map[string]any{"commit": picked}); err == nil || !strings.Contains(err.Error(), "commit or stash") {
		t.Errorf("dirty: err = %v", err)
	}
	r.Git("checkout", "--", "file-1.txt")
	r.Git("switch", "-q", "--detach")
	if _, err := prep(r.Dir, "cherry_pick", map[string]any{"commit": picked}); err == nil || !strings.Contains(err.Error(), "detached") {
		t.Errorf("detached: err = %v", err)
	}
}
```

In `internal/app/chatwrite_test.go`, follow the existing `merge_branch` execution test pattern: propose, approve, and assert the result text. Add one case where the pick conflicts (result contains "cherry-pick stopped with conflicts") and one clean pick (result is the title). Find that test by searching the file for `merge_branch`, and copy its structure with `cherry_pick` / `{"commit": ...}`.

- [ ] **Step 2: Run** `go test ./internal/ai/writetools/ ./internal/app/ -run 'CherryPick|Names|Specs'`. Expected: FAIL.

- [ ] **Step 3: Implement.** In `writetools.go`, add `"cherry_pick": true` to `writeToolNames`, add `Commit string` to `Proposal`, and add a spec after `merge_branch`:

```go
		{
			Name:        "cherry_pick",
			Description: "Apply one commit from elsewhere on top of the current branch, as a new commit. Not for merge commits.",
			Parameters:  obj(map[string]any{"commit": str("Hash (full or abbreviated) of the commit to apply")}, "commit"),
		},
```

In `Prepare`, add `case "cherry_pick": p, err = prepareCherryPick(ctx, dir, call.Args)`. Add `"git-ui/internal/merge"` to the imports, then:

```go
// --- cherry_pick ---

func prepareCherryPick(ctx context.Context, dir string, args map[string]any) (Proposal, error) {
	commit, err := stringArg(args, "commit")
	if err != nil {
		return Proposal{}, err
	}
	if commit == "" || strings.HasPrefix(commit, "-") {
		return Proposal{}, fmt.Errorf("invalid commit %q", commit)
	}
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", commit+"^{commit}")
	if err != nil {
		return Proposal{}, fmt.Errorf("no commit %q", commit)
	}
	hash := strings.TrimSpace(out)
	current := currentBranch(ctx, dir)
	if current == "" {
		return Proposal{}, errors.New("HEAD is detached; check out a branch first")
	}
	if st, err := merge.Status(ctx, dir); err != nil {
		return Proposal{}, err
	} else if st.Merging {
		return Proposal{}, fmt.Errorf("finish the %s in progress first", st.Kind)
	}
	parents, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-list", "--parents", "-n", "1", hash)
	if err != nil {
		return Proposal{}, err
	}
	if len(strings.Fields(parents)) > 2 {
		return Proposal{}, errors.New("that is a merge commit; cherry-picking a merge commit isn't supported")
	}
	meta, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "log", "-1", "--format=%h%x00%s", hash)
	if err != nil {
		return Proposal{}, err
	}
	short, subject, _ := strings.Cut(strings.TrimSpace(meta), "\x00")
	if contained, err := merge.IsAncestorOfHead(ctx, dir, hash); err != nil {
		return Proposal{}, err
	} else if contained {
		return Proposal{}, fmt.Errorf("%s is already on %s", short, current)
	}
	if dirty, err := merge.HasTrackedChanges(ctx, dir); err != nil {
		return Proposal{}, err
	} else if dirty {
		return Proposal{}, errors.New("there are uncommitted changes; commit or stash your changes first")
	}
	return Proposal{
		Title:   fmt.Sprintf("Cherry-pick %s %s onto %s", short, subject, current),
		Details: []string{"creates one new commit on " + current},
		Commit:  hash,
	}, nil
}
```

The dirty check comes last because the "invalid commit" and "no commit" cases must win over a dirty tree. The Refusals test orders its cases so that this matters.

In `internal/app/chatwrite.go`, before `default:`:

```go
	case "cherry_pick":
		result, err := a.CherryPick(repoID, p.Commit)
		if err != nil {
			return "", err
		}
		switch result.Outcome {
		case merge.Conflicted:
			return fmt.Sprintf("cherry-pick stopped with conflicts in %d file(s); the conflict view is open — resolve them there", len(result.Conflicts)), nil
		case merge.NothingToApply:
			return "nothing to apply: those changes are already on the branch", nil
		}
		return p.Title, nil
```

In `chat.md`, change the write-tools list to "…, pull, merge_branch and cherry_pick." Keep "rebase" in the destructive list.

In `docs/spec/06-ai.md`, add `cherry_pick` wherever the write tools are listed (and to any table of their refusals). Its refusals: invalid or unknown commit, detached head, an operation in progress, a merge commit, already on the branch, uncommitted tracked changes. A conflicted pick opens the Conflicts view, and an emptied one reports "nothing to apply". State that rebase remains not offered because it rewrites history.

- [ ] **Step 4: Run** `go test ./... && go vet ./... && gofmt -l internal`. Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ai/writetools internal/app/chatwrite.go internal/app/chatwrite_test.go internal/ai/prompts/defaults/chat.md docs/spec/06-ai.md
git commit -m "feat(ai): let the chat propose a cherry-pick"
```

---

### Task 7: Frontend logic: messages, disabled reasons, actions

**Files:**
- Create: `frontend/src/lib/rebase.ts`, `frontend/src/lib/rebase.test.ts`
- Modify: `frontend/src/lib/ui.ts` (`MenuItem.title`, `openMenuAsync`)
- Modify: `frontend/src/components/ContextMenu.svelte` (render `title`)
- Modify: `frontend/src/lib/actions.ts` (`rebaseOnto`, `cherryPick`, `skipStep`; `takeMergeSide` wording)
- Modify: `frontend/src/lib/merge.ts` (`sideLabel`, `skipWarning`, `isEmptyStepError`), `frontend/src/lib/merge.test.ts`

**Interfaces:**
- Consumes: `api.rebaseOnto`, `api.getRebasePreview`, `api.cherryPick`, `api.skipStep`, types from Task 5.
- Produces:
  - `rebase.ts`: `rebaseMessage(head: string, onto: string, p: RebasePreview): string`, `rebaseBlocker(o: { isHead: boolean; contained: boolean; detached: boolean; busy: boolean; kind: ConflictKind | '' }, head: string, target: string): string | null`, `cherryPickBlocker(o: { contained: boolean; isMerge: boolean; detached: boolean; busy: boolean; kind: ConflictKind | '' }): string | null`, `doneMessage(outcome: number, what: { op: 'rebase' | 'cherry-pick'; head: string; target: string; commits?: number }): string | null`
  - `merge.ts`: `sideLabel(label: string | undefined, fallback: string): string` (truncates to 40 chars with `…`), `skipWarning(state: MergeState): { title: string; message: string; confirmLabel: string }`, `isEmptyStepError(message: string): boolean`
  - `ui.ts`: `MenuItem.title?: string`; `openMenuAsync(event: MouseEvent, build: () => Promise<MenuItem[]>): Promise<void>`
  - `actions.ts`: `rebaseOnto(id: string, onto: string, ontoLabel: string, head: string): Promise<void>`, `cherryPick(id: string, hash: string, short: string, subject: string, head: string): Promise<void>`, `skipStep(id: string): Promise<void>`

- [ ] **Step 1: Write the failing tests** in `frontend/src/lib/rebase.test.ts`:

```ts
import { describe, expect, it } from 'vitest'
import { cherryPickBlocker, doneMessage, rebaseBlocker, rebaseMessage } from './rebase'
import { NOTHING_TO_APPLY, PICKED, REBASED, UP_TO_DATE } from './types'

const p = { commits: 3, merges: 0, published: 0, upstream: '' }
const free = { isHead: false, contained: false, detached: false, busy: false, kind: '' as const }

describe('rebaseMessage', () => {
  it('says how many commits are replayed', () => {
    expect(rebaseMessage('feature', 'main', p)).toBe('Rebase feature onto main? 3 commits will be replayed on top of main.')
  })
  it('warns about published commits and flattened merges', () => {
    const m = rebaseMessage('feature', 'main', { commits: 1, merges: 1, published: 1, upstream: 'origin/feature' })
    expect(m).toContain('1 commit will be replayed')
    expect(m).toContain("1 of these commits is already on origin/feature. After rebasing you'll need to force-push, which git-ui doesn't do.")
    expect(m).toContain('1 merge commit in this range will be flattened.')
  })
})

describe('rebaseBlocker', () => {
  it('allows a free rebase', () => expect(rebaseBlocker(free, 'feature', 'main')).toBeNull())
  it('explains each refusal', () => {
    expect(rebaseBlocker({ ...free, isHead: true }, 'feature', 'feature')).toBe('This is the current branch')
    expect(rebaseBlocker({ ...free, contained: true }, 'feature', 'main')).toBe('feature already contains main')
    expect(rebaseBlocker({ ...free, detached: true }, '', 'main')).toBe('No branch is checked out')
    expect(rebaseBlocker({ ...free, busy: true }, 'feature', 'main')).toBe('Another operation is running')
    expect(rebaseBlocker({ ...free, kind: 'merge' }, 'feature', 'main')).toBe('Finish the merge in progress first')
  })
})

describe('cherryPickBlocker', () => {
  const c = { contained: false, isMerge: false, detached: false, busy: false, kind: '' as const }
  it('allows a free pick', () => expect(cherryPickBlocker(c)).toBeNull())
  it('explains each refusal', () => {
    expect(cherryPickBlocker({ ...c, contained: true })).toBe('Already on this branch')
    expect(cherryPickBlocker({ ...c, isMerge: true })).toBe("Cherry-picking a merge commit isn't supported")
    expect(cherryPickBlocker({ ...c, kind: 'rebase' })).toBe('Finish the rebase in progress first')
  })
})

describe('doneMessage', () => {
  it('words each outcome', () => {
    expect(doneMessage(REBASED, { op: 'rebase', head: 'feature', target: 'main', commits: 3 })).toBe('Rebased feature onto main — 3 commits')
    expect(doneMessage(REBASED, { op: 'rebase', head: 'feature', target: 'main', commits: 0 })).toBe('Rebased feature onto main')
    expect(doneMessage(UP_TO_DATE, { op: 'rebase', head: 'feature', target: 'main' })).toBe('Already up to date')
    expect(doneMessage(PICKED, { op: 'cherry-pick', head: 'feature', target: 'a1b2c3' })).toBe('Cherry-picked a1b2c3 onto feature')
    expect(doneMessage(NOTHING_TO_APPLY, { op: 'cherry-pick', head: 'feature', target: 'a1b2c3' })).toBe('Nothing to apply: those changes are already on feature')
  })
})
```

Add to `frontend/src/lib/merge.test.ts` (and extend its import from `./merge`):

```ts
describe('sideLabel', () => {
  it('falls back and truncates', () => {
    expect(sideLabel(undefined, 'ours')).toBe('ours')
    expect(sideLabel('main', 'ours')).toBe('main')
    expect(sideLabel('a1b2c3 ' + 'x'.repeat(60), 'theirs')).toHaveLength(40)
    expect(sideLabel('a1b2c3 ' + 'x'.repeat(60), 'theirs').endsWith('…')).toBe(true)
  })
})

describe('skipWarning', () => {
  it('names the commit being skipped', () => {
    const w = skipWarning({ kind: 'rebase', merging: true, from: 'feature', into: 'main', conflicts: [], manual: [], staged: [], unstaged: [], theirsLabel: 'a1b2c3 fix login' })
    expect(w.title).toBe('Skip this commit')
    expect(w.message).toBe('a1b2c3 fix login will not be applied. Its changes are dropped from the result.')
  })
})

describe('isEmptyStepError', () => {
  it('recognises git’s empty-step messages', () => {
    expect(isEmptyStepError("No changes - did you forget to use 'git add'?")).toBe(true)
    expect(isEmptyStepError('The previous cherry-pick is now empty, possibly due to conflict resolution.')).toBe(true)
    expect(isEmptyStepError('nothing to commit, working tree clean')).toBe(true)
    expect(isEmptyStepError('fatal: bad revision')).toBe(false)
  })
})
```

- [ ] **Step 2: Run** from `frontend/`: `npm test`. Expected: FAIL (module `./rebase` not found).

- [ ] **Step 3: Implement.** `frontend/src/lib/rebase.ts`:

```ts
import type { ConflictKind, RebasePreview } from './types'
import { NOTHING_TO_APPLY, PICKED, REBASED, UP_TO_DATE } from './types'

const plural = (n: number, one: string, many: string) => (n === 1 ? `1 ${one}` : `${n} ${many}`)

/** rebaseMessage is the confirmation before a rebase; warnings only when they apply. */
export function rebaseMessage(head: string, onto: string, p: RebasePreview): string {
  const parts = [`Rebase ${head} onto ${onto}? ${plural(p.commits, 'commit', 'commits')} will be replayed on top of ${onto}.`]
  if (p.published > 0 && p.upstream) {
    const these = p.published === 1 ? '1 of these commits is' : `${p.published} of these commits are`
    parts.push(`⚠ ${these} already on ${p.upstream}. After rebasing you'll need to force-push, which git-ui doesn't do.`)
  }
  if (p.merges > 0) {
    parts.push(`⚠ ${plural(p.merges, 'merge commit', 'merge commits')} in this range will be flattened.`)
  }
  return parts.join('\n\n')
}

const inProgress = (kind: ConflictKind | '') => (kind ? `Finish the ${kind} in progress first` : null)

/** rebaseBlocker is why a rebase entry is disabled, or null when it is not. */
export function rebaseBlocker(
  o: { isHead: boolean; contained: boolean; detached: boolean; busy: boolean; kind: ConflictKind | '' },
  head: string,
  target: string,
): string | null {
  if (o.isHead) return 'This is the current branch'
  if (o.detached) return 'No branch is checked out'
  if (o.busy) return 'Another operation is running'
  const blocked = inProgress(o.kind)
  if (blocked) return blocked
  if (o.contained) return `${head} already contains ${target}`
  return null
}

/** cherryPickBlocker is why the cherry-pick entry is disabled, or null. */
export function cherryPickBlocker(o: { contained: boolean; isMerge: boolean; detached: boolean; busy: boolean; kind: ConflictKind | '' }): string | null {
  if (o.isMerge) return "Cherry-picking a merge commit isn't supported"
  if (o.detached) return 'No branch is checked out'
  if (o.busy) return 'Another operation is running'
  const blocked = inProgress(o.kind)
  if (blocked) return blocked
  if (o.contained) return 'Already on this branch'
  return null
}

/** doneMessage is the notice after a rebase or cherry-pick that did not stop on conflicts. */
export function doneMessage(outcome: number, w: { op: 'rebase' | 'cherry-pick'; head: string; target: string; commits?: number }): string | null {
  if (outcome === REBASED) return `Rebased ${w.head} onto ${w.target}${w.commits ? ` — ${plural(w.commits, 'commit', 'commits')}` : ''}`
  if (outcome === UP_TO_DATE) return 'Already up to date'
  if (outcome === PICKED) return `Cherry-picked ${w.target} onto ${w.head}`
  if (outcome === NOTHING_TO_APPLY) return `Nothing to apply: those changes are already on ${w.head}`
  return null
}
```

Check that the `confirmDialog` message area renders `\n\n` as separate paragraphs (look at `DialogHost.svelte`). If it does not, add `white-space: pre-line` to the message element's style there.

`merge.ts` additions:

```ts
/** sideLabel is a conflict side's name for a menu: the backend's label, or the fallback, kept short. */
export function sideLabel(label: string | undefined, fallback: string): string {
  const s = label || fallback
  return s.length > 40 ? s.slice(0, 39) + '…' : s
}

export function skipWarning(state: MergeState): { title: string; message: string; confirmLabel: string } {
  const what = state.theirsLabel || 'This commit'
  return { title: 'Skip this commit', message: `${what} will not be applied. Its changes are dropped from the result.`, confirmLabel: 'Skip commit' }
}

/** isEmptyStepError recognises git refusing to continue a step whose resolution left nothing to commit. */
export function isEmptyStepError(message: string): boolean {
  return /No changes - did you forget|is now empty|nothing to commit/i.test(message)
}
```

Add `skip: boolean` to `ConflictActions`: true for `rebase` and `'cherry-pick'`, false for the rest. Update every `ACTIONS` entry accordingly.

`ui.ts`: add `title?: string` to `MenuItem`, plus:

```ts
/** openMenuAsync opens a menu whose items need a backend answer first (e.g. whether a commit is already in HEAD). */
export async function openMenuAsync(event: MouseEvent, build: () => Promise<MenuItem[]>) {
  event.preventDefault()
  event.stopPropagation()
  const { clientX: x, clientY: y } = event
  menu.set({ x, y, items: await build() })
}
```

`ContextMenu.svelte`: add `title={item.disabled ? item.title : undefined}` to the item `<button>`. Disabled buttons don't fire mouse events in some engines. If the tooltip doesn't show on a disabled button in the Wails WebView during manual testing, wrap the label in `<span title=…>` and give disabled items `pointer-events: auto`.

`actions.ts` (import `rebaseMessage`, `doneMessage` from `./rebase`; `skipWarning`, `isEmptyStepError` from `./merge`; `CONFLICTED` from `./types`):

```ts
export async function rebaseOnto(id: string, onto: string, ontoLabel: string, head: string) {
  let preview: RebasePreview
  try {
    preview = await api.getRebasePreview(id, onto)
  } catch (e) {
    toast(errorMessage(e), 'error')
    return
  }
  const ok = await confirmDialog({ title: 'Rebase', message: rebaseMessage(head, ontoLabel, preview), confirmLabel: 'Rebase' })
  if (!ok) return
  busy.set('Rebasing…')
  try {
    const result = await api.rebaseOnto(id, onto)
    const msg = doneMessage(result.outcome, { op: 'rebase', head, target: ontoLabel, commits: preview.commits })
    if (msg) toast(msg, 'info')
    await warnMovedSubmodules(id)
  } catch (e) {
    toast(errorMessage(e), 'error')
  } finally {
    busy.set('')
    await refreshRepo()
  }
}

export async function cherryPick(id: string, hash: string, short: string, subject: string, head: string) {
  const ok = await confirmDialog({ title: 'Cherry-pick', message: `Cherry-pick ${short} ${subject} onto ${head}?`, confirmLabel: 'Cherry-pick' })
  if (!ok) return
  busy.set('Cherry-picking…')
  try {
    const result = await api.cherryPick(id, hash)
    const msg = doneMessage(result.outcome, { op: 'cherry-pick', head, target: short })
    if (msg) toast(msg, 'info')
    await warnMovedSubmodules(id)
  } catch (e) {
    toast(errorMessage(e), 'error')
  } finally {
    busy.set('')
    await refreshRepo()
  }
}

export async function skipStep(id: string) {
  const state = get(mergeState)
  const ok = await confirmDialog({ ...skipWarning(state ?? ({ kind: 'rebase' } as MergeState)), danger: true })
  if (ok) await run('Skipping…', () => api.skipStep(id))
}
```

Change `commitMerge` so that an empty-step failure offers Skip. Replace its last line with:

```ts
  try {
    busy.set('Continuing…')
    await api.commitMerge(id)
  } catch (e) {
    const message = errorMessage(e)
    const kind = get(mergeState)?.kind
    if ((kind === 'rebase' || kind === 'cherry-pick') && isEmptyStepError(message)) {
      const skip = await confirmDialog({ title: 'Nothing to commit', message: `${message}\n\nSkip this commit instead?`, confirmLabel: 'Skip this commit' })
      if (skip) await run('Skipping…', () => api.skipStep(id))
    } else {
      toast(message, 'error')
    }
  } finally {
    busy.set('')
    await refreshRepo()
  }
```

Before writing this, check how `run(...)` does busy, error and refresh, and mirror it so the flow stays identical (read `run` in `actions.ts`). If `run` also calls `loadMergeState`, call that here as well.

`takeMergeSide(id, path, side, branch)` is unchanged in signature. `MergeView` will pass the named label as `branch` in Task 8, so `takeMessage` already reads "Replace x with main's version…". Change only the dialog title and confirm label from "Take ours/Take theirs" to `` `Take ${branch}` ``.

- [ ] **Step 4: Run** from `frontend/`: `npm test && npm run check`. Expected: PASS.

- [ ] **Step 5: Commit** (actions not yet reachable from any menu):

```bash
git add frontend/src/lib frontend/src/components/ContextMenu.svelte
git commit -m "feat(ui): rebase and cherry-pick actions, messages and disabled reasons"
```

---

### Task 8: Frontend wiring: menus, Conflicts view, spec

**Files:**
- Modify: `frontend/src/components/RepoRefs.svelte` (`branchMenu`)
- Modify: `frontend/src/components/LogList.svelte` (`commitMenu`)
- Modify: `frontend/src/components/MergeView.svelte` (`manualMenu`, header buttons)
- Modify: `docs/spec/01-repositories-and-sidebar.md` (branch action table around line 292; write-lock list around line 406)
- Modify: `docs/spec/02-log-and-history.md` (row context menu paragraph around line 201)
- Modify: `docs/spec/04-conflicts.md` (Per-file actions, Continue and abort, Rules)

**Interfaces:**
- Consumes: everything from Task 7, plus `api.isAncestorOfHead`.

- [ ] **Step 1: Branch menu.** In `RepoRefs.svelte`, make `branchMenu` build its items asynchronously (import `openMenuAsync`, `rebaseBlocker`, `rebaseOnto`, `api`):

```ts
  function branchMenu(event: MouseEvent, b: Branch) {
    const head = $refs?.head ?? ''
    openMenuAsync(event, async () => {
      const contained = b.current ? false : await api.isAncestorOfHead(repoId, branchLabel(b)).catch(() => false)
      const rebaseWhy = rebaseBlocker(
        { isHead: b.current, contained, detached: !!$refs?.detached, busy: !!$busy, kind: $mergeState?.merging ? $mergeState.kind : '' },
        head,
        branchLabel(b),
      )
      return [
        { label: 'Check out', action: () => checkoutBranch(repoId, b), disabled: b.current || !!b.worktree || !!$busy },
        {
          label: `Merge ${branchLabel(b)} into ${head}`,
          action: () => mergeBranch(repoId, b, head),
          disabled: b.current || !!$busy || !!$refs?.detached || !!$mergeState?.merging,
        },
        {
          label: `Rebase ${head} onto ${branchLabel(b)}`,
          action: () => rebaseOnto(repoId, branchLabel(b), branchLabel(b), head),
          disabled: rebaseWhy !== null,
          title: rebaseWhy ?? undefined,
        },
        { label: 'New branch from here…', action: () => newBranch(repoId, branchLabel(b), branchLabel(b)) },
        { label: 'New tag here…', action: () => newTag(repoId, branchLabel(b), branchLabel(b)) },
        { label: b.remote ? 'Delete on remote…' : 'Delete…', action: () => deleteBranch(repoId, b), danger: true, disabled: b.current || !!b.worktree },
      ]
    })
  }
```

(`branchLabel(b)` already yields `origin/main` for a remote branch. Check its definition before relying on this.)

- [ ] **Step 2: Log row menu.** In `LogList.svelte` (import `openMenuAsync`, `rebaseBlocker`, `cherryPickBlocker`, `rebaseOnto`, `cherryPick`):

```ts
  function commitMenu(event: MouseEvent, row: LogRow) {
    selectedHash.set(row.hash)
    const head = $refs?.head ?? ''
    const common = { detached: !!$refs?.detached || !head, busy: !!$busy, kind: $mergeState?.merging ? $mergeState.kind : ('' as const) }
    openMenuAsync(event, async () => {
      const contained = await api.isAncestorOfHead(repoId, row.hash).catch(() => false)
      const rebaseWhy = rebaseBlocker({ ...common, isHead: row.hash === $refs?.headHash, contained }, head, row.short)
      const pickWhy = cherryPickBlocker({ ...common, contained, isMerge: row.parents.length > 1 })
      return [
        { label: '✨ Explain in chat', action: () => explain(row) },
        { label: 'Check out (detached)…', action: () => checkoutCommit(repoId, row.hash) },
        { label: 'New branch here…', action: () => newBranch(repoId, row.hash, row.short) },
        { label: 'New tag here…', action: () => newTag(repoId, row.hash, row.short) },
        { label: `Cherry-pick onto ${head || 'branch'}`, action: () => cherryPick(repoId, row.hash, row.short, row.subject, head), disabled: pickWhy !== null, title: pickWhy ?? undefined },
        { label: `Rebase ${head || 'branch'} onto this commit`, action: () => rebaseOnto(repoId, row.hash, row.short, head), disabled: rebaseWhy !== null, title: rebaseWhy ?? undefined },
        resetItem(row),
        { label: 'Copy hash', action: () => copyText(row.hash) },
      ]
    })
  }
```

(Check the `LogRow` field names for the subject and parents in `types.ts`, `Commit` near line 8, and adjust `row.subject` / `row.parents` if they differ.)

- [ ] **Step 3: Conflicts view.** In `MergeView.svelte`:
  - `manualMenu`: name the sides from the state (import `sideLabel`):

```ts
    const ours = sideLabel($mergeState?.oursLabel, $mergeState?.into || 'ours')
    const theirs = sideLabel($mergeState?.theirsLabel, $mergeState?.from || 'theirs')
    openMenu(event, [
      { label: `Take ${ours}`, action: () => takeMergeSide(repoId, file.path, 'ours', ours), disabled: !!$busy },
      { label: `Take ${theirs}`, action: () => takeMergeSide(repoId, file.path, 'theirs', theirs), disabled: !!$busy },
    ])
```

  - Header: after the abort button, add (import `skipStep`):

```svelte
    {#if acts.skip}
      <button class="btn" disabled={!!$busy} on:click={() => skipStep(repoId)}>Skip this commit</button>
    {/if}
```

- [ ] **Step 4: Spec.** Update in this same commit:
  - `docs/spec/01-repositories-and-sidebar.md`: add a row to the branch action table after Merge. Action: "Rebase `<head>` onto `<branch>`". Refused when: "It is the current branch, the head already contains it, a write is running, the head is detached, or any conflicted operation is in progress. Uncommitted changes to tracked files refuse it on click ('Commit or stash your changes first'). The confirmation states how many commits are replayed and warns, without blocking, when some are already on the upstream (a force-push, which the application does not offer, would be needed) or when merge commits in the range will be flattened." Also say that a disabled entry shows its reason as a tooltip. Add rebase, cherry-pick and skip to the list of writes that share the write lock.
  - `docs/spec/02-log-and-history.md`: in the row context menu paragraph, add "cherry-picking the commit onto the current branch" and "rebasing the current branch onto the commit", with their refusals. Cherry-pick: a merge commit, already contained in `HEAD`, detached head, a write running, an operation in progress; uncommitted tracked changes refuse it on click. Rebase: same as the branch menu. Both confirm first. A cherry-pick whose changes the branch already has reports "Nothing to apply" and leaves nothing in progress.
  - `docs/spec/04-conflicts.md`:
    - In "Per-file actions", replace the Manual row's actions with "Take `<side>` (right-click)", with the side named as follows. Merge: current branch / branch merged in. Rebase: the branch being rebased onto / the commit being replayed ("short hash + subject"). Cherry-pick: the current branch / the picked commit. Revert, applied patch and stash keep "ours / theirs". Add a sentence explaining that a rebase swaps git's ours and theirs, which is why the view names them.
    - In "Continue and abort", add a "Skip this commit" paragraph: rebase and cherry-pick only, always available, confirms, stops a resolver run first, runs git's own `--skip`. Also: when Continue fails because the step came out empty, the error offers Skip.
    - Add to Rules: "9. Skip is offered only for a rebase or a cherry-pick, and stops any resolver run before it runs."

- [ ] **Step 5: Verify.** From `frontend/`: `npm test && npm run check`. From the root: `go test ./...`. Then build and open the app (`make dev`, node 22) against the demo repo `/tmp/git-ui-demo`. Recreate it if it is missing: branches `feature/dark-mode`, and `feature/conflict` which conflicts with `main`. Check by hand:
  1. With `feature/conflict` checked out, the sidebar menu on `main` reads "Rebase feature/conflict onto main" → the confirmation shows the commit count → Conflicts shows the rebase header, "Take main / Take <hash subject>" on manual files, and "Skip this commit" and "Resolve with AI" are visible.
  2. The log row menu on a `feature/dark-mode` commit → "Cherry-pick onto main" → it applies, and a toast appears.
  3. The same entry on a commit already in `main` is disabled, and hovering it shows "Already on this branch".
  4. A dirty tracked file → rebase → toast "Commit or stash your changes first".

  Report any check that could not be done instead of claiming it.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components docs/spec/01-repositories-and-sidebar.md docs/spec/02-log-and-history.md docs/spec/04-conflicts.md
git commit -m "feat(ui): rebase and cherry-pick from the sidebar and the log, skip and named sides in Conflicts"
```

---

## Self-review notes

- Spec coverage: starting a rebase (T1, T5, T7, T8), preview warnings (T1, T7), cherry-pick with confirmation (T2, T5, T7, T8), dirty refusal (T1 preflight, T6), outcomes and notices (T1, T2, T7), named sides (T3, T4 model, T8 view), Skip and the empty-step error (T2, T7, T8), resolver for three kinds with fingerprint (T3, T4), chat `cherry_pick` with rebase still excluded (T6), `IsAncestorOfHead` (T1, T5, T8), docs/spec in the same commits (T4, T6, T8).
- Out of scope, and absent from every task: interactive rebase, multi-commit picks, autostash, force-push, `-m` picks, the resolver for revert/am/stash.
