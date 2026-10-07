# Push all branches + ahead/behind badges — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Push can publish every branch with something to push (or ask current vs all, per a setting), and local branch rows in the sidebar show ↑ahead in red and ↓behind in blue.

**Architecture:** Go `refs.List` gains ahead/behind/gone/local-upstream fields from `%(upstream:track)` (no extra command). A new `internal/ops/pushall.go` computes the set and runs one `git push --porcelain` per remote, parsing per-ref outcomes; `App.PushAll` exposes it under the write lock. `gitsettings` gains `pushScope`. The frontend gets a pure `lib/push.ts` (decision, dialog options, summaries, tooltips), a generic `results` dialog kind, badges in `BranchRow.svelte`, and the flow in `actions.ts`, the toolbar, the repo row menu and Settings → General.

**Tech Stack:** Go + Wails v2.16, Svelte + TypeScript, vitest.

**Spec:** `docs/superpowers/specs/2026-10-06-push-all-branches-design.md`

## Global Constraints

- Every worktree command starts with `cd /Users/josfh/playground/git-ui/.claude/worktrees/committest-push-all-branches-336883 &&`. Git is `/usr/bin/git`. Commits never carry `Co-Authored-By`.
- Every behaviour change commits with its `docs/spec` update in the same commit.
- Go tests: `go test ./internal/...`; vet/format: `go vet ./... && gofmt -l internal` (no output).
- Frontend: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run`; types `npm run check`.
- Bindings: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npm run build`, then from the worktree root `~/go/bin/wails generate module`, then `/usr/bin/git checkout -- frontend/wailsjs/runtime`; commit `frontend/wailsjs/go/app/App.{d.ts,js}` and `frontend/wailsjs/go/models.ts`.
- Texts, exactly: setting label "Push"; options "Ask each time" / "Current branch only" / "All branches"; menu item "Push all branches"; dialog title "Push <repo>", select label "Push", options "Current branch (<name>)" / "All branches (N)", message "Change the default in Settings → General.", confirm "Push" / "Push N branches" ("Push 1 branch"); busy label "Pushing branches…"; toasts "Pushed <branch>", "Pushed N branches", ", M already up to date" suffix, "Everything up to date", "Nothing to push"; results dialog title "Push results — <repo>"; rejection reason "The remote has commits you don't have — pull <branch> first"; failure message "N of M branches were not pushed"; cancelled reason "Cancelled"; missing line reason "No result from git"; toolbar tooltips "Push <branch>" / "Push all branches" / "Push — asks current or all branches"; badge tooltip "N commits to push to <upstream> · M commits to pull, as of the last fetch" (zero part left out, "1 commit" singular).
- Colours (checked ≥ 4.5:1 on `--sidebar`): `--ahead` light `#c0392b`, dark `#e5675a`, high-contrast light `#a1271b`, high-contrast dark `#ff8a7d`; `--behind` light `#1f6fbf`, dark `#6aa9ec`, high-contrast light `#154f8f`, high-contrast dark `#8cc4ff`.
- Never `--force`, never `--tags`/`--follow-tags`, never `--atomic`.

## Review Focus

- **A remote that moved since the last fetch** (badge says ahead, git says "fetch first"): the branch must come back `rejected` with the pull-first reason, not `failed`, and the other branches still go — Task 2 test `TestPushAllPushesTheOthersWhenOneIsRejected` (a never fetches).
- **A broken remote among several**: only its branches fail, with git's stderr as the reason; the other remote's branches are pushed — Task 2 test `TestPushAllGroupsByRemoteAndFailsOnlyTheBrokenOne`.
- **A branch tracking another local branch** (`branch --track child main`) must never be pushed (it would `git push .` and move a local branch) and must not count in "All branches (N)" — Task 2 test `TestPushAllLeavesOutBranchesWithNothingToPushOrNowhereToGo` and Task 4 test "leaves out a branch tracking a local branch".
- **Push from the repo row menu of a repository that is not selected**: the decision and the dialog must use that repository's refs and name, and the toast/dialog must name it — Task 7 test "asks with the other repository's refs and names it".
- **The results dialog while busy**: the dialog must open only after the busy label clears and the refs reload, so the toolbar is usable behind it — Task 7 (dialog opened in `finally` after `refreshRepo`) and its test "shows the results dialog after a partial failure, not an error toast".

---

### Task 1: refs — ahead/behind on local branches

**Files:**
- Modify: `internal/refs/list.go`
- Test: `internal/refs/list_test.go`

**Interfaces:**
- Produces: `refs.Branch` fields `Ahead int` (`json:"ahead,omitempty"`), `Behind int` (`json:"behind,omitempty"`), `UpstreamGone bool` (`json:"upstreamGone,omitempty"`), `UpstreamLocal bool` (`json:"upstreamLocal,omitempty"`); `func ParseTrack(s string) (ahead, behind int, gone bool)`.

- [ ] **Step 1: Failing tests** — append to `internal/refs/list_test.go`:

```go
func TestParseTrack(t *testing.T) {
	cases := []struct {
		in            string
		ahead, behind int
		gone          bool
	}{
		{"", 0, 0, false},
		{"ahead 2", 2, 0, false},
		{"behind 3", 0, 3, false},
		{"ahead 2, behind 3", 2, 3, false},
		{"gone", 0, 0, true},
	}
	for _, c := range cases {
		a, b, g := refs.ParseTrack(c.in)
		if a != c.ahead || b != c.behind || g != c.gone {
			t.Errorf("ParseTrack(%q) = %d, %d, %v; want %d, %d, %v", c.in, a, b, g, c.ahead, c.behind, c.gone)
		}
	}
}

func TestListCountsAheadBehindGoneAndLocalUpstreams(t *testing.T) {
	src := testrepo.New(t)
	src.Commit("first")
	src.Git("branch", "feature")
	src.Git("branch", "doomed")
	bare := testrepo.NewBareFrom(t, src)
	r := testrepo.Clone(t, bare)
	r.Git("branch", "--track", "feature", "origin/feature")
	r.Git("branch", "--track", "doomed", "origin/doomed")
	r.Git("branch", "--track", "child", "main")
	r.Commit("local on main")

	other := testrepo.Clone(t, bare)
	other.Git("switch", "-q", "feature")
	other.Commit("remote on feature")
	other.Git("push", "-q", "origin", "feature")
	other.Git("push", "-q", "origin", "--delete", "doomed")
	r.Git("fetch", "-q", "--prune")

	got, err := refs.List(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]refs.Branch{}
	for _, b := range got.Local {
		by[b.Name] = b
	}
	if b := by["main"]; b.Ahead != 1 || b.Behind != 0 || b.UpstreamGone || b.UpstreamLocal {
		t.Errorf("main = %+v, want 1 ahead", b)
	}
	if b := by["feature"]; b.Ahead != 0 || b.Behind != 1 {
		t.Errorf("feature = %+v, want 1 behind", b)
	}
	if b := by["doomed"]; !b.UpstreamGone || b.Ahead != 0 || b.Behind != 0 {
		t.Errorf("doomed = %+v, want upstream gone", b)
	}
	if b := by["child"]; !b.UpstreamLocal || b.Behind != 1 {
		t.Errorf("child = %+v, want a local upstream 1 behind", b)
	}
}
```

- [ ] **Step 2: Run, expect FAIL** — `go test ./internal/refs/` → compile error `undefined: refs.ParseTrack` / unknown fields.

- [ ] **Step 3: Implement** in `internal/refs/list.go`:

Add `"fmt"` to the imports. Add the fields to `Branch`, after `Upstream`:

```go
	// Ahead and Behind compare a local branch with its upstream as the last
	// fetch left it (%(upstream:track)); zero without an upstream.
	Ahead  int `json:"ahead,omitempty"`
	Behind int `json:"behind,omitempty"`
	// UpstreamGone: the upstream is configured but its remote branch no
	// longer exists. UpstreamLocal: the upstream is another local branch.
	UpstreamGone  bool `json:"upstreamGone,omitempty"`
	UpstreamLocal bool `json:"upstreamLocal,omitempty"`
```

Replace `refFormat` and the field count / local case in `List`:

```go
const refFormat = "%(refname)%00%(objectname)%00%(*objectname)%00%(HEAD)%00%(upstream:short)%00%(upstream:track,nobracket)%00%(upstream:remotename)"
```

```go
		f := strings.Split(line, "\x00")
		if len(f) != 7 {
			continue
		}
		ref, hash, peeled, head, upstream, track, upstreamRemote := f[0], f[1], f[2], f[3], f[4], f[5], f[6]
		switch {
		case strings.HasPrefix(ref, "refs/heads/"):
			ahead, behind, gone := ParseTrack(track)
			r.Local = append(r.Local, Branch{Name: strings.TrimPrefix(ref, "refs/heads/"),
				Hash: hash, Current: head == "*", Upstream: upstream,
				Ahead: ahead, Behind: behind, UpstreamGone: gone,
				UpstreamLocal: upstream != "" && upstreamRemote == "."})
```

(The `refs/tags/` and `refs/remotes/` cases are unchanged.) Add after `splitRemote`:

```go
// ParseTrack reads %(upstream:track,nobracket): "ahead 2", "behind 1",
// "ahead 2, behind 1", "gone" or "" (up to date, or no upstream).
func ParseTrack(s string) (ahead, behind int, gone bool) {
	if s == "gone" {
		return 0, 0, true
	}
	for _, part := range strings.Split(s, ", ") {
		var n int
		if _, err := fmt.Sscanf(part, "ahead %d", &n); err == nil {
			ahead = n
		} else if _, err := fmt.Sscanf(part, "behind %d", &n); err == nil {
			behind = n
		}
	}
	return ahead, behind, false
}
```

- [ ] **Step 4: Run, expect PASS** — `go test ./internal/refs/ ./internal/app/` (the app tests compare refs in places).

- [ ] **Step 5: Commit** (data only, no user-visible change yet, so no docs/spec edit):

```bash
/usr/bin/git add internal/refs/list.go internal/refs/list_test.go
/usr/bin/git commit -m "feat(refs): ahead/behind, gone and local upstream on local branches"
```

---

### Task 2: ops — PushAll

**Files:**
- Create: `internal/ops/pushall.go`, `internal/ops/pushall_test.go`, `internal/ops/pushall_parse_test.go`

**Interfaces:**
- Consumes: `refs.ParseTrack` (Task 1).
- Produces: `type ops.PushStatus string` with `ops.PushPushed = "pushed"`, `ops.PushUpToDate = "upToDate"`, `ops.PushRejected = "rejected"`, `ops.PushFailed = "failed"`; `type ops.BranchPushResult struct { Branch string \`json:"branch"\`; Target string \`json:"target"\`; Status PushStatus \`json:"status"\`; Reason string \`json:"reason,omitempty"\` }`; `func ops.PushAll(ctx context.Context, dir string) ([]BranchPushResult, error)` — never nil on success (empty slice when nothing to push).

- [ ] **Step 1: Failing parse tests** — `internal/ops/pushall_parse_test.go`:

```go
package ops

import "testing"

func TestParsePorcelainAndClassify(t *testing.T) {
	out := "To ../r.git\n" +
		" \trefs/heads/ff:refs/heads/other\t86a06b8..8a98883\n" +
		"!\trefs/heads/main:refs/heads/main\t[rejected] (fetch first)\n" +
		"!\trefs/heads/nff:refs/heads/nff\t[rejected] (non-fast-forward)\n" +
		"=\trefs/heads/same:refs/heads/same\t[up to date]\n" +
		"*\trefs/heads/new:refs/heads/new\t[new branch]\n" +
		"+\trefs/heads/forced:refs/heads/forced\t1111111...2222222 (forced update)\n" +
		"!\trefs/heads/hook:refs/heads/hook\t[remote rejected] (pre-receive hook declined)\n" +
		"Done\n"
	lines := parsePorcelain(out)
	want := map[string]struct {
		status PushStatus
		reason string
	}{
		"ff":     {PushPushed, ""},
		"main":   {PushRejected, "The remote has commits you don't have — pull main first"},
		"nff":    {PushRejected, "The remote has commits you don't have — pull nff first"},
		"same":   {PushUpToDate, ""},
		"new":    {PushPushed, ""},
		"forced": {PushPushed, ""},
		"hook":   {PushRejected, "[remote rejected] (pre-receive hook declined)"},
	}
	if len(lines) != len(want) {
		t.Fatalf("parsed %d lines, want %d: %+v", len(lines), len(want), lines)
	}
	for branch, w := range want {
		l, ok := lines["refs/heads/"+branch]
		if !ok {
			t.Fatalf("no line for %s", branch)
		}
		status, reason := classifyPushLine(l, branch)
		if status != w.status || reason != w.reason {
			t.Errorf("%s = %s %q, want %s %q", branch, status, reason, w.status, w.reason)
		}
	}
}

func TestParsePorcelainToleratesATrimmedFlagAndNoise(t *testing.T) {
	lines := parsePorcelain("\trefs/heads/a:refs/heads/a\t1..2\n")
	if s, _ := classifyPushLine(lines["refs/heads/a"], "a"); s != PushPushed {
		t.Errorf("trimmed space flag = %s, want pushed", s)
	}
	if got := parsePorcelain(""); len(got) != 0 {
		t.Errorf("empty output parsed to %+v", got)
	}
	if got := parsePorcelain("fatal: 'x' does not appear to be a git repository\n"); len(got) != 0 {
		t.Errorf("error text parsed to %+v", got)
	}
}
```

- [ ] **Step 2: Failing integration tests** — `internal/ops/pushall_test.go`:

```go
package ops_test

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"git-ui/internal/ops"
	"git-ui/internal/testrepo"
)

// aheadClone returns a clone of a remote with branches main, feature and
// quiet, all tracking origin; main and feature each have one commit the
// remote lacks, quiet has none.
func aheadClone(t *testing.T) (*testrepo.Repo, string) {
	t.Helper()
	src := testrepo.New(t)
	src.Commit("base")
	src.Git("branch", "feature")
	src.Git("branch", "quiet")
	bare := testrepo.NewBareFrom(t, src)
	a := testrepo.Clone(t, bare)
	a.Git("branch", "--track", "feature", "origin/feature")
	a.Git("branch", "--track", "quiet", "origin/quiet")
	a.Commit("main local")
	a.Git("switch", "-q", "feature")
	a.Commit("feature local")
	a.Git("switch", "-q", "main")
	return a, bare
}

// remoteHash is what the remote's branch points at, from ls-remote.
func remoteHash(r *testrepo.Repo, remote, branch string) string {
	return strings.Fields(r.Git("ls-remote", remote, "refs/heads/"+branch) + " ")[0]
}

func pushAll(t *testing.T, r *testrepo.Repo) []ops.BranchPushResult {
	t.Helper()
	got, err := ops.PushAll(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestPushAllPushesTheCurrentBranchAndTheOthersAhead(t *testing.T) {
	a, _ := aheadClone(t)
	got := pushAll(t, a)
	want := []ops.BranchPushResult{
		{Branch: "main", Target: "origin/main", Status: ops.PushPushed},
		{Branch: "feature", Target: "origin/feature", Status: ops.PushPushed},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if remoteHash(a, "origin", "feature") != a.Git("rev-parse", "feature") {
		t.Error("feature did not reach the remote")
	}
}

func TestPushAllReportsTheCurrentBranchUpToDate(t *testing.T) {
	a, _ := aheadClone(t)
	a.Git("push", "-q", "origin", "main")
	got := pushAll(t, a)
	want := []ops.BranchPushResult{
		{Branch: "main", Target: "origin/main", Status: ops.PushUpToDate},
		{Branch: "feature", Target: "origin/feature", Status: ops.PushPushed},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestPushAllPushesTheOthersWhenOneIsRejected(t *testing.T) {
	a, bare := aheadClone(t)
	b := testrepo.Clone(t, bare)
	b.Commit("remote main")
	b.Git("push", "-q", "origin", "main")

	got := pushAll(t, a) // a never fetched: its badge still says "ahead"
	want := []ops.BranchPushResult{
		{Branch: "main", Target: "origin/main", Status: ops.PushRejected, Reason: "The remote has commits you don't have — pull main first"},
		{Branch: "feature", Target: "origin/feature", Status: ops.PushPushed},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestPushAllPublishesACurrentBranchWithNoUpstream(t *testing.T) {
	a, _ := aheadClone(t)
	a.Git("switch", "-q", "-c", "topic")
	a.Commit("topic")
	got := pushAll(t, a)
	want := []ops.BranchPushResult{
		{Branch: "topic", Target: "origin/topic", Status: ops.PushPushed},
		{Branch: "feature", Target: "origin/feature", Status: ops.PushPushed},
		{Branch: "main", Target: "origin/main", Status: ops.PushPushed},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if up := a.Git("rev-parse", "--abbrev-ref", "topic@{upstream}"); up != "origin/topic" {
		t.Errorf("topic upstream = %q, want origin/topic", up)
	}
}

func TestPushAllLeavesOutBranchesWithNothingToPushOrNowhereToGo(t *testing.T) {
	a, bare := aheadClone(t)
	a.Git("push", "-q", "origin", "main") // current, up to date
	a.Git("branch", "loose")              // no upstream
	a.Git("branch", "--track", "child", "main")
	a.Git("switch", "-q", "child")
	a.Commit("child") // ahead of local main: must never be pushed
	a.Git("switch", "-q", "main")
	b := testrepo.Clone(t, bare)
	b.Git("push", "-q", "origin", "--delete", "feature")
	a.Git("fetch", "-q", "--prune") // feature: ahead, but its upstream is gone

	got := pushAll(t, a)
	want := []ops.BranchPushResult{{Branch: "main", Target: "origin/main", Status: ops.PushUpToDate}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestPushAllPushesToTheUpstreamName(t *testing.T) {
	a, _ := aheadClone(t)
	a.Git("push", "-q", "origin", "main")
	a.Git("push", "-q", "origin", "feature~1:refs/heads/renamed")
	a.Git("fetch", "-q", "origin")
	a.Git("branch", "-q", "--set-upstream-to=origin/renamed", "feature")
	got := pushAll(t, a)
	want := []ops.BranchPushResult{
		{Branch: "main", Target: "origin/main", Status: ops.PushUpToDate},
		{Branch: "feature", Target: "origin/renamed", Status: ops.PushPushed},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if remoteHash(a, "origin", "renamed") != a.Git("rev-parse", "feature") {
		t.Error("feature was not pushed to renamed")
	}
}

func TestPushAllGroupsByRemoteAndFailsOnlyTheBrokenOne(t *testing.T) {
	a, _ := aheadClone(t)
	backup := testrepo.NewBareFrom(t, a)
	a.Git("remote", "add", "backup", backup)
	a.Git("fetch", "-q", "backup")
	a.Git("branch", "-q", "--set-upstream-to=backup/quiet", "quiet")
	a.Git("switch", "-q", "quiet")
	a.Commit("quiet local")
	a.Git("switch", "-q", "main")
	a.Git("remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing.git"))

	got := pushAll(t, a)
	if len(got) != 3 || got[0].Branch != "main" || got[1].Branch != "feature" || got[2].Branch != "quiet" {
		t.Fatalf("got %+v, want main, feature, quiet", got)
	}
	for _, r := range got[:2] {
		if r.Status != ops.PushFailed || !strings.Contains(r.Reason, "does not appear to be a git repository") {
			t.Errorf("%s = %s %q, want failed with git's message", r.Branch, r.Status, r.Reason)
		}
	}
	if got[2] != (ops.BranchPushResult{Branch: "quiet", Target: "backup/quiet", Status: ops.PushPushed}) {
		t.Errorf("quiet = %+v, want pushed to backup/quiet", got[2])
	}
}

func TestPushAllWithADetachedHeadPushesOnlyTheOthers(t *testing.T) {
	a, _ := aheadClone(t)
	a.Git("switch", "-q", "--detach", "HEAD")
	got := pushAll(t, a)
	want := []ops.BranchPushResult{
		{Branch: "feature", Target: "origin/feature", Status: ops.PushPushed},
		{Branch: "main", Target: "origin/main", Status: ops.PushPushed},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestPushAllWithNothingToPushReturnsNoResults(t *testing.T) {
	src := testrepo.New(t)
	src.Commit("base")
	a := testrepo.Clone(t, testrepo.NewBareFrom(t, src))
	a.Git("switch", "-q", "--detach", "HEAD")
	got := pushAll(t, a)
	if got == nil || len(got) != 0 {
		t.Fatalf("got %#v, want an empty, non-nil slice", got)
	}
}
```

- [ ] **Step 3: Run, expect FAIL** — `go test ./internal/ops/ -run 'PushAll|Porcelain'` → `undefined: ops.PushAll` / `parsePorcelain`.

- [ ] **Step 4: Implement** — `internal/ops/pushall.go`:

```go
package ops

import (
	"context"
	"errors"
	"sort"
	"strings"

	"git-ui/internal/gitcmd"
	"git-ui/internal/refs"
)

// PushStatus is how one branch of a push of all branches ended.
type PushStatus string

const (
	PushPushed   PushStatus = "pushed"
	PushUpToDate PushStatus = "upToDate"
	PushRejected PushStatus = "rejected"
	PushFailed   PushStatus = "failed"
)

// BranchPushResult is one branch's outcome; Target is <remote>/<branch>.
type BranchPushResult struct {
	Branch string     `json:"branch"`
	Target string     `json:"target"`
	Status PushStatus `json:"status"`
	Reason string     `json:"reason,omitempty"`
}

type pushTarget struct {
	branch, remote, remoteRef string
	setUpstream               bool
}

const pushFormat = "%(refname:strip=2)%00%(HEAD)%00%(upstream:remotename)%00%(upstream:remoteref)%00%(upstream:track,nobracket)"

// pushTargets is what PushAll pushes (docs/spec/05-remote-and-stash.md): the
// current branch first — published to origin when it has no upstream —
// then, by name, every other local branch ahead of a live upstream on a
// remote. A branch tracking another local branch (remote ".") is never
// pushed: that would move the local branch, not publish anything.
func pushTargets(ctx context.Context, dir string) ([]pushTarget, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "for-each-ref", "--format="+pushFormat, "refs/heads")
	if err != nil {
		return nil, err
	}
	var current, others []pushTarget
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Split(line, "\x00")
		if len(f) != 5 {
			continue
		}
		name, head, remote, remoteRef, track := f[0], f[1], f[2], f[3], f[4]
		ahead, _, gone := refs.ParseTrack(track)
		onRemote := remote != "" && remote != "." && remoteRef != ""
		switch {
		case head == "*" && remote == "":
			current = append(current, pushTarget{branch: name, remote: "origin", remoteRef: "refs/heads/" + name, setUpstream: true})
		case head == "*" && onRemote:
			current = append(current, pushTarget{branch: name, remote: remote, remoteRef: remoteRef})
		case head != "*" && onRemote && !gone && ahead > 0:
			others = append(others, pushTarget{branch: name, remote: remote, remoteRef: remoteRef})
		}
	}
	return append(current, others...), nil
}

// PushAll pushes the current branch and every other local branch ahead of
// its upstream, one `git push --porcelain` per remote in name order. Never
// forced and not atomic: each branch succeeds or fails on its own, and a
// failure to reach one remote fails only that remote's branches.
func PushAll(ctx context.Context, dir string) ([]BranchPushResult, error) {
	targets, err := pushTargets(ctx, dir)
	if err != nil {
		return nil, err
	}
	results := make([]BranchPushResult, len(targets))
	groups := map[string][]int{}
	for i, t := range targets {
		results[i] = BranchPushResult{Branch: t.branch, Target: t.remote + "/" + strings.TrimPrefix(t.remoteRef, "refs/heads/")}
		groups[t.remote] = append(groups[t.remote], i)
	}
	remotes := make([]string, 0, len(groups))
	for r := range groups {
		remotes = append(remotes, r)
	}
	sort.Strings(remotes)
	for _, remote := range remotes {
		pushGroup(ctx, dir, remote, groups[remote], targets, results)
	}
	return results, nil
}

// pushGroup pushes the targets at idx, all on remote, and fills in their
// results. -u is only for a current branch being published; on the others
// it re-sets the upstream they already have.
func pushGroup(ctx context.Context, dir, remote string, idx []int, targets []pushTarget, results []BranchPushResult) {
	args := []string{"push", "--porcelain"}
	for _, i := range idx {
		if targets[i].setUpstream {
			args = append(args, "-u")
			break
		}
	}
	args = append(args, "--", remote)
	for _, i := range idx {
		args = append(args, "refs/heads/"+targets[i].branch+":"+targets[i].remoteRef)
	}
	out, runErr := gitcmd.Run(ctx, dir, gitcmd.NetworkTimeout, args...)
	lines := parsePorcelain(out)
	for _, i := range idx {
		line, ok := lines["refs/heads/"+targets[i].branch]
		switch {
		case ok:
			results[i].Status, results[i].Reason = classifyPushLine(line, targets[i].branch)
		case len(lines) == 0 && runErr != nil:
			results[i].Status, results[i].Reason = PushFailed, pushErrorText(runErr)
		default:
			results[i].Status, results[i].Reason = PushFailed, "No result from git"
		}
	}
}

type porcelainLine struct {
	flag    byte
	summary string
}

// parsePorcelain reads `git push --porcelain`: one "<flag>\t<src>:<dst>\t<summary>"
// line per ref between "To <url>" and "Done", keyed by src. A space flag
// that lost its space to trimming reads as a plain fast-forward.
func parsePorcelain(out string) map[string]porcelainLine {
	lines := map[string]porcelainLine{}
	for _, l := range strings.Split(out, "\n") {
		f := strings.SplitN(l, "\t", 3)
		if len(f) != 3 || len(f[0]) > 1 {
			continue
		}
		src, _, ok := strings.Cut(f[1], ":")
		if !ok {
			continue
		}
		flag := byte(' ')
		if f[0] != "" {
			flag = f[0][0]
		}
		lines[src] = porcelainLine{flag: flag, summary: f[2]}
	}
	return lines
}

func classifyPushLine(l porcelainLine, branch string) (PushStatus, string) {
	switch l.flag {
	case ' ', '*', '+':
		return PushPushed, ""
	case '=':
		return PushUpToDate, ""
	case '!':
		if strings.Contains(l.summary, "(non-fast-forward)") || strings.Contains(l.summary, "(fetch first)") {
			return PushRejected, "The remote has commits you don't have — pull " + branch + " first"
		}
		return PushRejected, l.summary
	default:
		return PushFailed, l.summary
	}
}

// pushErrorText is the reason shown for every branch of a remote whose
// push printed no ref line: git's own message, or "Cancelled".
func pushErrorText(err error) string {
	if errors.Is(err, gitcmd.ErrCancelled) {
		return "Cancelled"
	}
	var gerr *gitcmd.Error
	if errors.As(err, &gerr) && strings.TrimSpace(gerr.Stderr) != "" {
		return strings.TrimSpace(gerr.Stderr)
	}
	return err.Error()
}
```

- [ ] **Step 5: Run, expect PASS** — `go test ./internal/ops/ && go vet ./internal/ops/ && gofmt -l internal/ops` (no gofmt output).

- [ ] **Step 6: Commit** (no user-visible change yet):

```bash
/usr/bin/git add internal/ops/pushall.go internal/ops/pushall_test.go internal/ops/pushall_parse_test.go
/usr/bin/git commit -m "feat(ops): push every branch with something to push, one porcelain push per remote"
```

---

### Task 3: push scope setting, App.PushAll, bindings

**Files:**
- Modify: `internal/gitsettings/gitsettings.go`, `internal/gitsettings/gitsettings_test.go`, `internal/app/remote.go`, `internal/app/remote_test.go`, `frontend/src/lib/api.ts`, `frontend/src/lib/types.ts`
- Regenerate: `frontend/wailsjs/go/app/App.{d.ts,js}`, `frontend/wailsjs/go/models.ts`

**Interfaces:**
- Consumes: `ops.PushAll`, `ops.BranchPushResult` (Task 2); refs fields (Task 1).
- Produces: `gitsettings.PushAsk = "ask"`, `PushCurrent = "current"`, `PushAll = "all"`; `Settings.PushScope string \`json:"pushScope"\``; `func (a *App) PushAll(id string) ([]ops.BranchPushResult, error)`; TS `api.pushAll(id: string): Promise<BranchPushResult[]>`; TS types `PushScope`, `BranchPushResult`, `GitSettings.pushScope`, `Branch.ahead? / behind? / upstreamGone? / upstreamLocal?`.

- [ ] **Step 1: Failing Go tests.** In `internal/gitsettings/gitsettings_test.go`, change `TestSaveAndLoadRoundTrip`'s `want` to `gitsettings.Settings{PullStrategy: gitsettings.PullRebase, PushScope: gitsettings.PushCurrent}` and append (add `"os"` and `"errors"` to the imports if missing):

```go
func TestPushScopeDefaultsToAsk(t *testing.T) {
	if got := gitsettings.Defaults().PushScope; got != gitsettings.PushAsk {
		t.Errorf("default = %q, want ask", got)
	}
	path := filepath.Join(t.TempDir(), "git.json")
	if err := os.WriteFile(path, []byte(`{"pullStrategy":"merge"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := gitsettings.Load(path)
	if err != nil || got.PushScope != gitsettings.PushAsk || got.PullStrategy != gitsettings.PullMerge {
		t.Fatalf("got %+v, %v; want merge + ask", got, err)
	}
}

func TestSaveFillsAMissingPushScope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "git.json")
	if err := gitsettings.Save(path, gitsettings.Settings{PullStrategy: gitsettings.PullAuto}); err != nil {
		t.Fatal(err)
	}
	if got, _ := gitsettings.Load(path); got.PushScope != gitsettings.PushAsk {
		t.Errorf("push scope = %q, want ask", got.PushScope)
	}
}

func TestSaveRejectsAnUnknownPushScope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "git.json")
	err := gitsettings.Save(path, gitsettings.Settings{PullStrategy: gitsettings.PullAuto, PushScope: "some"})
	if !errors.Is(err, gitsettings.ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}
```

Append to `internal/app/remote_test.go` (add `"errors"` to the imports):

```go
func TestPushAllThroughTheAppLayer(t *testing.T) {
	a, r, id := newPlainApp(t)
	bare := testrepo.NewBareFrom(t, r)
	r.Git("remote", "add", "origin", bare)
	r.Git("push", "-q", "-u", "origin", "main")
	r.Git("switch", "-q", "-c", "topic")
	r.Commit("on topic")

	results, err := a.PushAll(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0] != (ops.BranchPushResult{Branch: "topic", Target: "origin/topic", Status: ops.PushPushed}) {
		t.Fatalf("results = %+v", results)
	}
}

func TestPushAllRefusesWhileAnotherWriteRuns(t *testing.T) {
	a, _, id := newPlainApp(t)
	unlock, err := a.lockWrite(id)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if _, err := a.PushAll(id); !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v, want ErrBusy", err)
	}
}
```

(`main` is up to date and is not current, so only `topic` is pushed.)

- [ ] **Step 2: Run, expect FAIL** — `go test ./internal/gitsettings/ ./internal/app/ -run 'Push|RoundTrip'` → undefined `PushScope` / `PushAll`.

- [ ] **Step 3: Implement `gitsettings`.** Package comment's first line: `// Package gitsettings stores app-level git-behaviour preferences — the pull` / `// strategy and the push scope. Kept apart ...` (rest unchanged). Add:

```go
const (
	PushAsk     = "ask"
	PushCurrent = "current"
	PushAll     = "all"
)
```

`Settings` gains `PushScope string \`json:"pushScope"\``; `Defaults()` returns `Settings{PullStrategy: PullAuto, PushScope: PushAsk}`; in `Load`, after the pull strategy fill-in add `if s.PushScope == "" { s.PushScope = PushAsk }` and extend its comment to "…lacks either setting"; at the top of `Save` add:

```go
	// A caller from before the push scope existed saves without one.
	if s.PushScope == "" {
		s.PushScope = PushAsk
	}
```

Replace `validate`:

```go
func validate(s Settings) error {
	switch s.PullStrategy {
	case PullAuto, PullMerge, PullRebase:
	default:
		return fmt.Errorf("%w: unknown pull strategy %q", ErrInvalid, s.PullStrategy)
	}
	switch s.PushScope {
	case PushAsk, PushCurrent, PushAll:
		return nil
	default:
		return fmt.Errorf("%w: unknown push scope %q", ErrInvalid, s.PushScope)
	}
}
```

- [ ] **Step 4: Implement `App.PushAll`** in `internal/app/remote.go`, after `Push`:

```go
// PushAll pushes the current branch and every other branch ahead of its
// upstream (docs/spec/05-remote-and-stash.md). Each branch's outcome comes
// back as a result; only a failure to start (busy, missing) is an error.
func (a *App) PushAll(id string) ([]ops.BranchPushResult, error) {
	var results []ops.BranchPushResult
	err := a.write(id, func(ctx context.Context, dir string) error {
		var pushErr error
		results, pushErr = ops.PushAll(ctx, dir)
		return pushErr
	})
	return results, err
}
```

- [ ] **Step 5: Run, expect PASS** — `go test ./internal/... && go vet ./... && gofmt -l internal`.

- [ ] **Step 6: Bindings** — run the three binding commands from Global Constraints. Check `frontend/wailsjs/go/app/App.d.ts` has `export function PushAll(arg1:string):Promise<Array<ops.BranchPushResult>>;` and `models.ts` has `BranchPushResult` and the new `Branch` fields.

- [ ] **Step 7: TS types and api.** In `frontend/src/lib/types.ts`, add to `interface Branch` after `upstream: string`:

```ts
  /** Commits the branch has that its upstream lacks, as of the last fetch. */
  ahead?: number
  /** Commits the upstream has that the branch lacks, as of the last fetch. */
  behind?: number
  /** The upstream is configured but its remote branch no longer exists. */
  upstreamGone?: boolean
  /** The upstream is another local branch, not a remote one. */
  upstreamLocal?: boolean
```

Replace `interface GitSettings` with:

```ts
export type PushScope = 'ask' | 'current' | 'all'

export interface GitSettings {
  pullStrategy: 'auto' | 'merge' | 'rebase'
  pushScope: PushScope
}

/** One branch's outcome of a push of all branches (Go's ops.BranchPushResult). */
export interface BranchPushResult {
  branch: string
  /** <remote>/<branch> */
  target: string
  status: 'pushed' | 'upToDate' | 'rejected' | 'failed'
  reason?: string
}
```

(Keep any other field `GitSettings` already had.) In `frontend/src/lib/api.ts`, add `BranchPushResult` to the type import and, after `push:`, add:

```ts
  pushAll: (id: string) => call<BranchPushResult[]>(Go.PushAll(id)),
```

In `frontend/src/components/SettingsDialog.svelte` change `let git: GitSettings = { pullStrategy: 'auto' }` to `let git: GitSettings = { pullStrategy: 'auto', pushScope: 'ask' }` (type check only; the select comes in Task 7).

- [ ] **Step 8: Check** — `npm run check` and `npx vitest run` → PASS.

- [ ] **Step 9: Commit** (no user-visible change yet):

```bash
/usr/bin/git add internal/gitsettings internal/app/remote.go internal/app/remote_test.go frontend/wailsjs/go frontend/src/lib/types.ts frontend/src/lib/api.ts frontend/src/components/SettingsDialog.svelte
/usr/bin/git commit -m "feat(app): push scope setting and PushAll binding"
```

---

### Task 4: pure push helpers

**Files:**
- Create: `frontend/src/lib/push.ts`, `frontend/src/lib/push.test.ts`

**Interfaces:**
- Consumes: TS types from Task 3; `ChoiceOptions` from `./ui`.
- Produces (all exported from `push.ts`):
  - `type PushChoice = 'current' | 'all'`
  - `othersAhead(refs: Refs | null): Branch[]`
  - `pushDecision(scope: PushScope, refs: Refs | null): PushChoice | 'ask'`
  - `pushCount(refs: Refs | null): number`
  - `pushChoiceOptions(repoName: string, refs: Refs | null): ChoiceOptions<PushChoice>`
  - `pushFailed(r: BranchPushResult): boolean`
  - `pushedMessage(results: BranchPushResult[]): string`
  - `failedMessage(results: BranchPushResult[]): string`
  - `pushResultRows(results: BranchPushResult[]): ResultRow[]` (`ResultRow` from Task 6 — to keep this task standalone, `push.ts` declares and exports `interface ResultRow { mark: string; label: string; detail: string; tone: 'ok' | 'muted' | 'error' }`; Task 6 imports it from here)
  - `class PartialPushError extends Error { results: BranchPushResult[] }`
  - `pushTitle(scope: PushScope, refs: Refs | null): string`
  - `trackTitle(b: Branch): string`

- [ ] **Step 1: Failing tests** — `frontend/src/lib/push.test.ts`:

```ts
import { describe, expect, it } from 'vitest'
import { failedMessage, othersAhead, PartialPushError, pushChoiceOptions, pushCount, pushDecision, pushedMessage, pushResultRows, pushTitle, trackTitle } from './push'
import type { Branch, BranchPushResult, Refs } from './types'

const b = (name: string, over: Partial<Branch> = {}): Branch => ({ name, remote: '', hash: 'h', current: false, upstream: `origin/${name}`, ...over })
const refs = (local: Branch[], over: Partial<Refs> = {}): Refs => ({ head: 'main', headHash: 'h', detached: false, local, remotes: [], tags: [], ...over })
const res = (branch: string, status: BranchPushResult['status'], reason?: string): BranchPushResult => ({ branch, target: `origin/${branch}`, status, ...(reason ? { reason } : {}) })

describe('othersAhead / pushCount', () => {
  const all = refs([
    b('main', { current: true, ahead: 2 }),
    b('feature', { ahead: 1 }),
    b('quiet'),
    b('behind', { behind: 3 }),
    b('gone', { ahead: 1, upstreamGone: true }),
    b('child', { ahead: 1, upstream: 'main', upstreamLocal: true }),
    b('loose', { upstream: '', ahead: 0 }),
  ])
  it('keeps only other branches ahead of a live remote upstream', () => {
    expect(othersAhead(all).map((x) => x.name)).toEqual(['feature'])
  })
  it('leaves out a branch tracking a local branch', () => {
    expect(othersAhead(all).some((x) => x.name === 'child')).toBe(false)
  })
  it('counts the current branch plus the others', () => {
    expect(pushCount(all)).toBe(2)
    expect(pushCount(refs([b('feature', { ahead: 1 })], { detached: true, head: '' }))).toBe(1)
    expect(pushCount(refs([b('main', { current: true, upstream: 'dev', upstreamLocal: true }), b('feature', { ahead: 1 })]))).toBe(1)
    expect(pushCount(null)).toBe(0)
  })
})

describe('pushDecision', () => {
  const lone = refs([b('main', { current: true, ahead: 1 })])
  const more = refs([b('main', { current: true }), b('feature', { ahead: 1 })])
  it('follows a fixed setting', () => {
    expect(pushDecision('current', more)).toBe('current')
    expect(pushDecision('all', lone)).toBe('all')
  })
  it('asks only when another branch has commits to push', () => {
    expect(pushDecision('ask', lone)).toBe('current')
    expect(pushDecision('ask', more)).toBe('ask')
    expect(pushDecision('ask', null)).toBe('current')
  })
})

describe('pushChoiceOptions', () => {
  it('offers the current branch first and names the count', () => {
    const o = pushChoiceOptions('alpha', refs([b('main', { current: true }), b('a', { ahead: 1 }), b('c', { ahead: 4 })]))
    expect(o.title).toBe('Push alpha')
    expect(o.label).toBe('Push')
    expect(o.options).toEqual([{ value: 'current', label: 'Current branch (main)' }, { value: 'all', label: 'All branches (3)' }])
    expect(o.value).toBe('current')
    expect(o.message('current')).toBe('Change the default in Settings → General.')
    expect(o.confirmLabel('current')).toBe('Push')
    expect(o.confirmLabel('all')).toBe('Push 3 branches')
  })
  it('offers only all branches with a detached HEAD', () => {
    const o = pushChoiceOptions('alpha', refs([b('a', { ahead: 1 })], { detached: true, head: '' }))
    expect(o.options).toEqual([{ value: 'all', label: 'All branches (1)' }])
    expect(o.value).toBe('all')
    expect(o.confirmLabel('all')).toBe('Push 1 branch')
  })
})

describe('result summaries', () => {
  it('sums up a push in which nothing failed', () => {
    expect(pushedMessage([])).toBe('Nothing to push')
    expect(pushedMessage([res('main', 'pushed')])).toBe('Pushed main')
    expect(pushedMessage([res('main', 'pushed'), res('a', 'pushed')])).toBe('Pushed 2 branches')
    expect(pushedMessage([res('main', 'upToDate'), res('a', 'pushed'), res('b', 'pushed')])).toBe('Pushed 2 branches, 1 already up to date')
    expect(pushedMessage([res('main', 'upToDate')])).toBe('Everything up to date')
  })
  it('counts the branches that were not pushed', () => {
    const r = [res('main', 'rejected', 'pull'), res('a', 'pushed'), res('b', 'failed', 'auth')]
    expect(failedMessage(r)).toBe('2 of 3 branches were not pushed')
    const e = new PartialPushError(r)
    expect(e.message).toBe('2 of 3 branches were not pushed')
    expect(e.results).toBe(r)
    expect(e).toBeInstanceOf(Error)
  })
  it('makes one dialog row per branch, in order', () => {
    expect(pushResultRows([res('main', 'rejected', 'The remote has commits you don\'t have — pull main first'), res('a', 'pushed'), res('b', 'upToDate')])).toEqual([
      { mark: '✗', label: 'main → origin/main', detail: "The remote has commits you don't have — pull main first", tone: 'error' },
      { mark: '✓', label: 'a → origin/a', detail: 'Pushed', tone: 'ok' },
      { mark: '—', label: 'b → origin/b', detail: 'Up to date', tone: 'muted' },
    ])
  })
})

describe('tooltips', () => {
  it('says what a click on Push does', () => {
    const r = refs([b('main', { current: true })])
    expect(pushTitle('current', r)).toBe('Push main')
    expect(pushTitle('current', refs([], { detached: true, head: '' }))).toBe('Push')
    expect(pushTitle('all', r)).toBe('Push all branches')
    expect(pushTitle('ask', r)).toBe('Push — asks current or all branches')
  })
  it('explains the badges, leaving out a zero part', () => {
    expect(trackTitle(b('main', { ahead: 2, behind: 1 }))).toBe('2 commits to push to origin/main · 1 commit to pull, as of the last fetch')
    expect(trackTitle(b('main', { ahead: 1 }))).toBe('1 commit to push to origin/main, as of the last fetch')
    expect(trackTitle(b('main', { behind: 3 }))).toBe('3 commits to pull, as of the last fetch')
    expect(trackTitle(b('main'))).toBe('')
    expect(trackTitle(b('main', { remote: 'origin', ahead: 2 }))).toBe('')
  })
})
```

- [ ] **Step 2: Run, expect FAIL** — `npx vitest run src/lib/push.test.ts` → cannot resolve `./push`.

- [ ] **Step 3: Implement** — `frontend/src/lib/push.ts`:

```ts
import type { Branch, BranchPushResult, PushScope, Refs } from './types'
import type { ChoiceOptions } from './ui'

/** Push: what a click on Push and the "push all" choice do
 *  (docs/spec/05-remote-and-stash.md). Pure, so every rule is tested. */

export type PushChoice = 'current' | 'all'

/** One row of a results dialog. */
export interface ResultRow { mark: string; label: string; detail: string; tone: 'ok' | 'muted' | 'error' }

const commits = (n: number) => (n === 1 ? '1 commit' : `${n} commits`)
const branches = (n: number) => (n === 1 ? '1 branch' : `${n} branches`)

/** The local branches besides the current one that "All branches" pushes:
 *  ahead of a live upstream on a remote, as the last fetch saw it. */
export function othersAhead(refs: Refs | null): Branch[] {
  return (refs?.local ?? []).filter((b) => !b.current && !!b.upstream && !b.upstreamGone && !b.upstreamLocal && (b.ahead ?? 0) > 0)
}

/** N in "All branches (N)": the current branch (unless HEAD is detached or
 *  it tracks another local branch) plus the others ahead. */
export function pushCount(refs: Refs | null): number {
  const current = refs && !refs.detached ? refs.local.find((b) => b.current) : undefined
  return (current && !current.upstreamLocal ? 1 : 0) + othersAhead(refs).length
}

/** What a click on Push does: the setting, except that "ask" pushes the
 *  current branch without asking when no other branch has commits to push. */
export function pushDecision(scope: PushScope, refs: Refs | null): PushChoice | 'ask' {
  if (scope !== 'ask') return scope
  return othersAhead(refs).length > 0 ? 'ask' : 'current'
}

export function pushChoiceOptions(repoName: string, refs: Refs | null): ChoiceOptions<PushChoice> {
  const n = pushCount(refs)
  const options: { value: PushChoice; label: string }[] = []
  if (refs && !refs.detached) options.push({ value: 'current', label: `Current branch (${refs.head})` })
  options.push({ value: 'all', label: `All branches (${n})` })
  return {
    title: `Push ${repoName}`,
    label: 'Push',
    options,
    value: options[0].value,
    message: () => 'Change the default in Settings → General.',
    confirmLabel: (v) => (v === 'all' ? `Push ${branches(n)}` : 'Push'),
  }
}

export const pushFailed = (r: BranchPushResult) => r.status === 'rejected' || r.status === 'failed'

/** The toast after a push of all branches in which nothing failed. */
export function pushedMessage(results: BranchPushResult[]): string {
  if (results.length === 0) return 'Nothing to push'
  const pushed = results.filter((r) => r.status === 'pushed')
  if (pushed.length === 0) return 'Everything up to date'
  const head = pushed.length === 1 ? `Pushed ${pushed[0].branch}` : `Pushed ${pushed.length} branches`
  const upToDate = results.length - pushed.length
  return upToDate > 0 ? `${head}, ${upToDate} already up to date` : head
}

export const failedMessage = (results: BranchPushResult[]) =>
  `${results.filter(pushFailed).length} of ${results.length} branches were not pushed`

export function pushResultRows(results: BranchPushResult[]): ResultRow[] {
  return results.map((r) => {
    const label = `${r.branch} → ${r.target}`
    if (pushFailed(r)) return { mark: '✗', label, detail: r.reason ?? '', tone: 'error' }
    if (r.status === 'upToDate') return { mark: '—', label, detail: 'Up to date', tone: 'muted' }
    return { mark: '✓', label, detail: 'Pushed', tone: 'ok' }
  })
}

/** Thrown inside track() so a push with branches left behind notifies as a
 *  failed push; carries the results for the dialog. */
export class PartialPushError extends Error {
  results: BranchPushResult[]
  constructor(results: BranchPushResult[]) {
    super(failedMessage(results))
    this.results = results
  }
}

/** The toolbar Push button's tooltip: what a click will do. */
export function pushTitle(scope: PushScope, refs: Refs | null): string {
  if (scope === 'all') return 'Push all branches'
  if (scope === 'ask') return 'Push — asks current or all branches'
  return refs?.head && !refs.detached ? `Push ${refs.head}` : 'Push'
}

/** The ahead/behind badges' tooltip on a local branch row; '' when there
 *  is nothing to show (remote-tracking rows never show badges). */
export function trackTitle(b: Branch): string {
  if (b.remote) return ''
  const parts: string[] = []
  if (b.ahead) parts.push(`${commits(b.ahead)} to push to ${b.upstream}`)
  if (b.behind) parts.push(`${commits(b.behind)} to pull`)
  return parts.length ? `${parts.join(' · ')}, as of the last fetch` : ''
}
```

- [ ] **Step 4: Run, expect PASS** — `npx vitest run src/lib/push.test.ts && npm run check`.

- [ ] **Step 5: Commit**:

```bash
/usr/bin/git add frontend/src/lib/push.ts frontend/src/lib/push.test.ts
/usr/bin/git commit -m "feat(push): pure rules for push scope, the choice dialog, summaries and badge tooltips"
```

---

### Task 5: ahead/behind badges on branch rows

**Files:**
- Modify: `frontend/src/theme.css`, `frontend/src/components/BranchRow.svelte`, `docs/spec/01-repositories-and-sidebar.md`

**Interfaces:**
- Consumes: `trackTitle` (Task 4); `Branch.ahead/behind` (Tasks 1, 3).

- [ ] **Step 1: Tokens** — in `frontend/src/theme.css` add, next to `--danger` in each block:
  - `:root { … }`: `--ahead: #c0392b;` `--behind: #1f6fbf;`
  - `:root[data-theme='dark'] { … }`: `--ahead: #e5675a;` `--behind: #6aa9ec;`
  - `:root[data-contrast='high'] { … }`: `--ahead: #a1271b;` `--behind: #154f8f;`
  - `:root[data-theme='dark'][data-contrast='high'] { … }`: `--ahead: #ff8a7d;` `--behind: #8cc4ff;`

- [ ] **Step 2: Badges** — in `frontend/src/components/BranchRow.svelte`, add `import { trackTitle } from '../lib/push'` and, after the `elsewhere` reactive line, `$: track = trackTitle(branch)`. In the markup, right after `<span class="ellipsis" …>{text}</span>`:

```svelte
  {#if track}
    <span class="track" title={track}>
      {#if branch.ahead}<span class="ahead">↑{branch.ahead}</span>{/if}
      {#if branch.behind}<span class="behind">↓{branch.behind}</span>{/if}
    </span>
  {/if}
```

Add to `<style>`:

```css
  /* Ahead/behind its upstream, as of the last fetch: small coloured numbers,
     no pill, so a list of branches stays calm. */
  .track { margin-left: auto; flex: none; display: inline-flex; gap: 4px; font-size: 11px; font-variant-numeric: tabular-nums; }
  .ahead { color: var(--ahead); }
  .behind { color: var(--behind); }
  .track + .wt, .track + .filtered { margin-left: 6px; }
```

- [ ] **Step 3: docs/spec** — in `docs/spec/01-repositories-and-sidebar.md`, `### Branches` section, insert a paragraph before "Local branches whose name contains no `/` are listed loose.":

```markdown
A local branch with an upstream shows how far apart they are, as of the
last fetch, at the right of its row (before the "worktree" badge and the
funnel icon): `↑N` in red for commits the branch has that the upstream
lacks, `↓M` in blue for commits the upstream has that the branch lacks,
both when they have diverged. Nothing shows when both are zero, without an
upstream, or when the upstream is gone. The tooltip reads "N commits to push
to <upstream> · M commits to pull, as of the last fetch", leaving out a zero
part. The counts come with the branch list, so they refresh whenever it
does (after a fetch — background ones included —, pull, push, commit,
checkout…), in every expanded repository. Remote-tracking branches and
collapsed folders show none. The two colours keep a 4.5:1 contrast on the
sidebar in light, dark and both high-contrast themes.
```

- [ ] **Step 4: Check** — `npm run check && npx vitest run`.

- [ ] **Step 5: Commit**:

```bash
/usr/bin/git add frontend/src/theme.css frontend/src/components/BranchRow.svelte docs/spec/01-repositories-and-sidebar.md
/usr/bin/git commit -m "feat(sidebar): ahead/behind badges on local branch rows"
```

---

### Task 6: results dialog kind

**Files:**
- Modify: `frontend/src/lib/ui.ts`, `frontend/src/components/DialogHost.svelte`
- Test: `frontend/src/lib/ui.test.ts`

**Interfaces:**
- Consumes: `ResultRow` from `./push` (Task 4).
- Produces: `interface ResultsOptions { title: string; message?: string; rows: ResultRow[] }`; Dialog kind `'results'`; `resultsDialog(options: ResultsOptions): Promise<void>`.

- [ ] **Step 1: Failing test** — append to `frontend/src/lib/ui.test.ts` (merge imports with the file's existing ones: `get` from `svelte/store`, `dialog`, `resultsDialog` from `./ui`):

```ts
describe('resultsDialog', () => {
  it('shows the rows and resolves once closed', async () => {
    const rows = [{ mark: '✓', label: 'a → origin/a', detail: 'Pushed', tone: 'ok' as const }]
    let done = false
    const p = resultsDialog({ title: 'Push results — alpha', rows }).then(() => (done = true))
    const d = get(dialog)
    expect(d).toMatchObject({ kind: 'results', title: 'Push results — alpha', rows })
    if (d?.kind !== 'results') throw new Error('not a results dialog')
    d.resolve()
    await p
    expect(done).toBe(true)
  })
})
```

- [ ] **Step 2: Run, expect FAIL** — `npx vitest run src/lib/ui.test.ts` → `resultsDialog` is not exported.

- [ ] **Step 3: Implement `ui.ts`** — add `import type { ResultRow } from './push'`; after `FormOptions`'s helpers add:

```ts
/** A list of outcomes with a single OK, e.g. a push of all branches in
 *  which some branches were not pushed. */
export interface ResultsOptions { title: string; message?: string; rows: ResultRow[] }
```

Add `| (ResultsOptions & { kind: 'results'; resolve: () => void })` to `Dialog`, and after `formDialog`:

```ts
export const resultsDialog = (options: ResultsOptions) =>
  new Promise<void>((resolve) => dialog.set({ ...options, kind: 'results', resolve }))
```

- [ ] **Step 4: Implement `DialogHost.svelte`:**
  - `submitLabel`: insert `: $dialog?.kind === 'results' ? 'OK'` before the final `: ($dialog?.submitLabel ?? 'OK')`.
  - `finish`: insert `else if (current.kind === 'results') current.resolve()` before the final `else`.
  - Markup: before the final `{:else}` of the kind chain add:

```svelte
      {:else if $dialog.kind === 'results'}
        {#if $dialog.message}<p>{$dialog.message}</p>{/if}
        <ul class="results">
          {#each $dialog.rows as row}
            <li class={row.tone}><span class="mark">{row.mark}</span><span class="label">{row.label}</span><span class="detail">{row.detail}</span></li>
          {/each}
        </ul>
```

  - Buttons: wrap the Cancel button in `{#if $dialog.kind !== 'results'}…{/if}`, and change the submit button's `use:focus={$dialog.kind === 'confirm'}` to `use:focus={$dialog.kind === 'confirm' || $dialog.kind === 'results'}`.
  - Style:

```css
  .results { list-style: none; margin: 0; padding: 0; max-height: 50vh; overflow: auto; display: grid; gap: 8px; }
  .results li { display: grid; grid-template-columns: 16px 1fr; column-gap: 8px; }
  .results .detail { grid-column: 2; font-size: 12px; color: var(--muted); white-space: pre-wrap; overflow-wrap: anywhere; }
  .results .error .mark, .results .error .detail { color: var(--danger); }
  .results .ok .mark { color: var(--accent); }
  .results .muted .mark { color: var(--muted); }
```

- [ ] **Step 5: Run, expect PASS** — `npx vitest run && npm run check`.

- [ ] **Step 6: Commit** (no caller yet):

```bash
/usr/bin/git add frontend/src/lib/ui.ts frontend/src/lib/ui.test.ts frontend/src/components/DialogHost.svelte
/usr/bin/git commit -m "feat(ui): results dialog with one row per outcome and a single OK"
```

---

### Task 7: the push flow, setting, menu and docs

**Files:**
- Modify: `frontend/src/lib/actions.ts`, `frontend/src/lib/actions.test.ts`, `frontend/src/lib/toolbar.ts`, `frontend/src/lib/toolbar.test.ts`, `frontend/src/components/Toolbar.svelte`, `frontend/src/lib/repoMenu.ts`, `frontend/src/lib/repoMenu.test.ts`, `frontend/src/components/RepoRow.svelte`, `frontend/src/components/SettingsDialog.svelte`, `frontend/src/App.svelte`
- Docs: `docs/spec/05-remote-and-stash.md`, `docs/spec/01-repositories-and-sidebar.md`, `docs/spec/07-conventions-and-constraints.md`, `docs/spec/11-notifications.md`

**Interfaces:**
- Consumes: everything above.
- Produces: `actions.push(id: string): Promise<boolean>` (now follows the setting), `actions.pushAll(id: string): Promise<boolean>`; `ToolbarInput.pushScope?: PushScope`; `RepoMenuId` gains `'push-all'`.

- [ ] **Step 1: Failing action tests** — in `frontend/src/lib/actions.test.ts`:
  - add to the `api` mock: `pushAll: vi.fn().mockResolvedValue([]),`
  - add to the `ui` mock: `choiceDialog: vi.fn().mockResolvedValue(null),` and `resultsDialog: vi.fn().mockResolvedValue(undefined),`
  - import `pushAll` from `./actions`, `gitSettings` from `./stores`, `choiceDialog, resultsDialog` from `./ui`, and `Refs` from `./types`.
  - append:

```ts
describe('push follows the push scope', () => {
  const ahead = (over: Partial<Refs> = {}): Refs => ({
    head: 'main', headHash: 'h', detached: false, remotes: [], tags: [],
    local: [
      { name: 'main', remote: '', hash: 'h', current: true, upstream: 'origin/main' },
      { name: 'feature', remote: '', hash: 'h', current: false, upstream: 'origin/feature', ahead: 2 },
    ],
    ...over,
  })
  beforeEach(() => {
    toasts.set([])
    vi.mocked(api.push).mockClear()
    vi.mocked(api.pushAll).mockReset().mockResolvedValue([])
    vi.mocked(api.getRefs).mockReset().mockResolvedValue(null as unknown as Refs)
    vi.mocked(choiceDialog).mockReset().mockResolvedValue(null)
    vi.mocked(resultsDialog).mockClear()
    repos.set([{ id: 'r1', name: 'alpha', path: '/a', missing: false, branch: 'main' } as Repo, { id: 'r2', name: 'beta', path: '/b', missing: false, branch: 'main' } as Repo])
    selectedRepoId.set('r1')
    gitSettings.set({ pullStrategy: 'auto', pushScope: 'ask' })
  })

  it('pushes the current branch without asking when no other branch is ahead', async () => {
    await push('r1')
    expect(choiceDialog).not.toHaveBeenCalled()
    expect(api.push).toHaveBeenCalledWith('r1')
  })

  it('asks with the other repository\'s refs and names it', async () => {
    vi.mocked(api.getRefs).mockResolvedValue(ahead())
    await push('r2')
    expect(api.getRefs).toHaveBeenCalledWith('r2')
    expect(vi.mocked(choiceDialog).mock.calls[0][0]).toMatchObject({ title: 'Push beta' })
    expect(api.push).not.toHaveBeenCalled() // cancelled
    expect(api.pushAll).not.toHaveBeenCalled()
  })

  it('runs the branch chosen in the dialog', async () => {
    vi.mocked(api.getRefs).mockResolvedValue(ahead())
    vi.mocked(choiceDialog).mockResolvedValue('all')
    await push('r1')
    expect(api.pushAll).toHaveBeenCalledWith('r1')
  })

  it('a fixed setting never asks', async () => {
    vi.mocked(api.getRefs).mockResolvedValue(ahead())
    gitSettings.set({ pullStrategy: 'auto', pushScope: 'current' })
    await push('r1')
    gitSettings.set({ pullStrategy: 'auto', pushScope: 'all' })
    await push('r1')
    expect(choiceDialog).not.toHaveBeenCalled()
    expect(api.push).toHaveBeenCalledTimes(1)
    expect(api.pushAll).toHaveBeenCalledTimes(1)
  })

  it('toasts a push of all branches in which nothing failed', async () => {
    vi.mocked(api.pushAll).mockResolvedValue([
      { branch: 'main', target: 'origin/main', status: 'upToDate' },
      { branch: 'feature', target: 'origin/feature', status: 'pushed' },
    ])
    expect(await pushAll('r1')).toBe(true)
    expect(get(toasts).map((t) => [t.message, t.kind])).toEqual([['Pushed feature, 1 already up to date', 'info']])
    expect(resultsDialog).not.toHaveBeenCalled()
  })

  it('names another repository in the toast', async () => {
    await pushAll('r2')
    expect(get(toasts).map((t) => t.message)).toEqual(['beta: Nothing to push'])
  })

  it('shows the results dialog after a partial failure, not an error toast', async () => {
    windowFocused.set(false)
    vi.mocked(api.notify).mockClear()
    vi.mocked(api.pushAll).mockResolvedValue([
      { branch: 'main', target: 'origin/main', status: 'rejected', reason: "The remote has commits you don't have — pull main first" },
      { branch: 'feature', target: 'origin/feature', status: 'pushed' },
    ])
    expect(await pushAll('r1')).toBe(false)
    expect(get(toasts)).toEqual([])
    expect(vi.mocked(resultsDialog).mock.calls[0][0]).toMatchObject({ title: 'Push results — alpha' })
    expect(vi.mocked(resultsDialog).mock.calls[0][0].rows).toHaveLength(2)
    expect(vi.mocked(api.notify).mock.calls[0][0]).toMatchObject({ id: 'r1:problem', body: 'Push failed: 1 of 2 branches were not pushed' })
  })

  it('a push of all branches that cannot start shows the error toast', async () => {
    vi.mocked(api.pushAll).mockRejectedValue(new Error('another operation is running'))
    expect(await pushAll('r1')).toBe(false)
    expect(get(toasts).map((t) => t.kind)).toEqual(['error'])
    expect(resultsDialog).not.toHaveBeenCalled()
  })
})
```

  Toolbar test — append to `frontend/src/lib/toolbar.test.ts`:

```ts
it('the Push tooltip says what a click does', () => {
  expect(item({ pushScope: 'current' }, 'push').title).toBe('Push main')
  expect(item({ pushScope: 'all' }, 'push').title).toBe('Push all branches')
  expect(item({}, 'push').title).toBe('Push — asks current or all branches')
  expect(item({ busy: 'Pushing branches…', pushScope: 'all' }, 'push').title).toBe('Pushing branches…')
})
```

  Repo menu test — in `frontend/src/lib/repoMenu.test.ts`, change both `['fetch', 'pull', 'push']` expectations to `['fetch', 'pull', 'push', 'push-all']`.

- [ ] **Step 2: Run, expect FAIL** — `npx vitest run` → `pushAll` not exported, tooltip and menu mismatches.

- [ ] **Step 3: `actions.ts`.** Add `gitSettings` to the `./stores` import; add `resultsDialog` to the `./ui` import; add `BranchPushResult, Refs` to the `./types` import; add `import { PartialPushError, pushChoiceOptions, pushDecision, pushedMessage, pushFailed, pushResultRows } from './push'`. Replace the `push` line with:

```ts
const repoLabel = (id: string) => get(repos).find((r) => r.id === id)?.name ?? 'repository'

// refsOf reads a repository's refs fresh — the row menu can push a
// repository that is not the selected one; unreadable refs ask nothing.
async function refsOf(id: string): Promise<Refs | null> {
  try {
    return await api.getRefs(id)
  } catch {
    return null
  }
}

// push follows Settings → General → Push: the current branch, all branches,
// or — the default — a choice, asked only when another branch has commits
// to push (docs/spec/05-remote-and-stash.md).
export async function push(id: string): Promise<boolean> {
  const scope = get(gitSettings)?.pushScope ?? 'ask'
  const current = scope === 'ask' ? await refsOf(id) : null
  let choice = pushDecision(scope, current)
  if (choice === 'ask') {
    const picked = await choiceDialog(pushChoiceOptions(repoLabel(id), current))
    if (!picked) return false
    choice = picked
  }
  return choice === 'all' ? pushAll(id) : runOp(id, 'push', 'Pushing…', () => api.push(id))
}

// pushAll pushes every branch with something to push. A toast sums up a
// push in which nothing failed; otherwise a dialog lists every branch —
// opened once the busy label is gone and the refs reloaded — and the push
// notifies as failed.
export async function pushAll(id: string): Promise<boolean> {
  busy.set('Pushing branches…')
  let partial: BranchPushResult[] | null = null
  try {
    const results = await track(id, 'push', async () => {
      const res = (await api.pushAll(id)) ?? []
      if (res.some(pushFailed)) throw new PartialPushError(res)
      return res
    })
    const message = pushedMessage(results)
    toast(id === get(selectedRepoId) ? message : `${repoLabel(id)}: ${message}`, 'info')
    return true
  } catch (e) {
    if (e instanceof PartialPushError) partial = e.results
    else opError(id, e)
    return false
  } finally {
    busy.set('')
    await refreshRepo()
    if (partial) void resultsDialog({ title: `Push results — ${repoLabel(id)}`, rows: pushResultRows(partial) })
  }
}
```

- [ ] **Step 4: Toolbar.** In `frontend/src/lib/toolbar.ts`: import `pushTitle` from `./push` and `PushScope` (type) from `./types`; add to `ToolbarInput`:

```ts
  /** Settings → General → Push; the Push tooltip says what a click does. */
  pushScope?: PushScope
```

  and change the push item's title argument from `'Push'` to `pushTitle(i.pushScope ?? 'ask', i.refs)`. In `frontend/src/components/Toolbar.svelte`, add `gitSettings` to its stores import and `pushScope: $gitSettings?.pushScope ?? 'ask'` to the object passed to `toolbarItems`.

- [ ] **Step 5: Repo menu.** In `frontend/src/lib/repoMenu.ts`: add `'push-all'` to `RepoMenuId` and change `sync` to `['fetch', 'pull', 'push', 'push-all']`. In `frontend/src/components/RepoRow.svelte`: import `pushAll` from `../lib/actions`; in the linked-worktree map add after `push:`

```ts
          'push-all': { label: 'Push all branches', action: () => pushAll(repo.id), disabled: !!$busy || !!$mergeState?.merging },
```

  and in the main map after `push:`

```ts
      'push-all': { label: 'Push all branches', action: () => pushAll(repo.id), disabled: repo.missing || !!$busy || !!$mergeState?.merging },
```

- [ ] **Step 6: Setting.** In `frontend/src/components/SettingsDialog.svelte`, after the Pull strategy `<label>` inside the Git section:

```svelte
              <label>
                <span>Push</span>
                <select bind:value={git.pushScope} on:change={saveGit}>
                  <option value="ask">Ask each time</option>
                  <option value="current">Current branch only</option>
                  <option value="all">All branches</option>
                </select>
              </label>
```

  In `frontend/src/App.svelte`, add `loadGitSettings` to the stores import and call `loadGitSettings()` in `onMount` right after `loadRepos().then(loadRefs)` (until now the store was only filled after saving Settings, so the toolbar and Push would read the default).

- [ ] **Step 7: Run, expect PASS** — `npx vitest run && npm run check`.

- [ ] **Step 8: docs/spec.**
  - `docs/spec/05-remote-and-stash.md`: replace the whole `### Push` subsection (from `### Push` up to, not including, `### Pull`) with:

```markdown
### Push

Settings → General → **Push** says what a click on Push (toolbar or the
repository row's menu) does: *Ask each time* (the default), *Current branch
only* or *All branches*. The toolbar button's tooltip follows it: "Push
`<branch>`", "Push all branches" or "Push — asks current or all branches";
its badge always counts the current branch.

With *Ask each time*, Push pushes the current branch without asking when no
other local branch is ahead of its upstream (counted from the last fetch).
Otherwise a dialog "Push `<repository>`" offers *Current branch (`<name>`)*
(selected) and *All branches (N)* — N counts the current branch plus the
others ahead — with "Change the default in Settings → General." under it;
confirming reads "Push" or "Push N branches", Cancel pushes nothing. With a
detached HEAD it offers only *All branches (N)*. The repository row's menu
also has **Push all branches**, which pushes all branches whatever the
setting says.

**Current branch** publishes the checked-out branch:

- If the branch already has an upstream, Push pushes to it as-is.
- If the branch has no upstream yet, Push sets one on the remote named
  `origin`, publishing under the branch's own name. This is a deliberate
  default rather than a prompt: a first push almost always means "publish
  this to the usual remote," and asking every time would slow down the
  common case.
- A detached HEAD has nothing to publish; Push refuses.

**All branches** pushes the current branch (as above; to its upstream even
when that is gone, and up to date counts) plus every other local branch
whose upstream is on a remote, still exists, and is behind it according to
the last fetch. A branch that tracks another local branch, or has no
upstream, is never pushed this way; with a detached HEAD only the other
branches go. Each branch goes to its upstream by name
(`branch.<name>.pushRemote` and `push.default` are not consulted), with one
`git push --porcelain` per remote. Nothing is forced, no tag is pushed, and
the push is not atomic: a branch the remote rejects does not stop the
others. The busy label reads "Pushing branches…".

When nothing failed, a toast sums it up: "Pushed `<branch>`" or "Pushed N
branches", plus ", M already up to date" when some were; "Everything up to
date" when none moved; "Nothing to push" when no branch qualified. When any
branch was not pushed, a dialog "Push results — `<repository>`" lists every
branch with its target: ✓ pushed, — up to date, ✗ and the reason. A branch
the remote has moved on from reads "The remote has commits you don't have —
pull `<branch>` first"; any other rejection shows git's reason. A remote
that cannot be reached (network, authentication, a missing remote) fails
all of its branches with git's message, and the other remotes are still
pushed; a push cancelled from the Commands panel reads "Cancelled".
```

  - In the toolbar table of the same file, the Push row's Action cell becomes: `Push, below (following Settings → General → Push); the current branch's ahead count as a badge`.
  - `docs/spec/01-repositories-and-sidebar.md`: in the repository row's context menu paragraph, both "Fetch, Pull, Push" become "Fetch, Pull, Push, Push all branches"; in the "Fetch, Pull, Push, the show-in-file-manager action…" sentence (around line 280), insert "Push all branches," after "Push,"; and in "has every other row action (fetch, pull, push) refused" use "(fetch, pull, push, push all branches)".
  - `docs/spec/07-conventions-and-constraints.md`: "**General**: Appearance and the Git pull strategy." → "**General**: Appearance, the Git pull strategy and what Push does (ask, current branch, all branches)."
  - `docs/spec/11-notifications.md`: after the table's paragraph that starts "One user operation is one event", add a sentence to that paragraph: "A push of all branches in which any branch was not pushed is a failed Push: "Push failed: N of M branches were not pushed"."

- [ ] **Step 9: Commit**:

```bash
/usr/bin/git add frontend/src docs/spec
/usr/bin/git commit -m "feat(push): push all branches or ask, per Settings → General → Push; Push all branches in the repo menu"
```

---

### Task 8: Full check, rebuild, reopen

- [ ] `go test ./... -race` → PASS; `go vet ./...`; `gofmt -l internal` → no output.
- [ ] Frontend: `npx vitest run`, `npm run check`, `npm run build` → PASS.
- [ ] `/usr/bin/git status` clean; `frontend/wailsjs/runtime` unchanged.
- [ ] Demo repository for the owner's manual pass (in the session scratchpad, not `/tmp`): a bare remote plus a clone with `main` ahead 1, `feature` ahead 2, `diverged` ahead 1 / behind 1, `quiet` up to date, `loose` without upstream; a second clone pushes to `main` after the first fetched, so `main` is rejected. Write its path and the steps into the report.
- [ ] After merge (owner's call): `make dev` in the background (node 22) and check the window opens; Settings → General shows "Push"; the sidebar shows ↑/↓ on the demo's branches; Push asks; All branches gives the results dialog with `main` rejected.
