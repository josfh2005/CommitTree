# Chat Write Tools Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The repository chat can stage, commit, branch, check out, stash, fetch, push, pull and merge — every one shown to the user as a card and run only after Approve.

**Architecture:** A new `internal/ai/writetools` package validates a write tool call and builds a *proposal* (what will happen + the repo fingerprint it was built against). In `internal/app`, the chat's `RunTool` routes write tools to `runWriteTool`, which emits `chat:confirm`, blocks until `ConfirmChatAction` (or Stop), re-checks the fingerprint, and runs the same App method the UI uses under the per-repo write lock, then emits `repo:changed`. The frontend renders the card in the chat and refreshes the repo on `repo:changed`.

**Tech Stack:** Go (Wails v2 bindings), Svelte (legacy `$:` syntax), TypeScript, vitest.

**Spec:** `docs/superpowers/specs/2026-09-23-chat-write-tools-design.md`

## Global Constraints

- Tools, exactly: `stage_files`, `unstage_files`, `commit`, `create_branch`, `checkout_branch`, `stash_push`, `fetch`, `push`, `pull`, `merge_branch`. Nothing destructive: no reset, discard, delete branch, amend, stash drop/pop/apply, force-push, rebase.
- Every write is confirmed by the user. No auto-approve. No timeout on a pending confirmation; `StopChat` counts as rejection.
- Execution goes through the existing App methods (`StageFile`, `UnstageFile`, `CommitChanges(id, msg, false)`, `CreateBranch`, `Checkout`/`CheckoutRemote`, `StashPush`, `Fetch`, `Push`, `Pull`, `MergeBranch`) — never a second path to git.
- The per-repo write lock is not held while waiting for the user; on approval the refs fingerprint (and, for `commit`, the staged paths) must still match the proposal, else `error: the repository changed since this was proposed; check its state and propose again`.
- Busy lock → `error: another operation is running in this repository; try again when it finishes`.
- Tool result prefixes the frontend relies on: `done: …`, `rejected by the user`, `error: …`.
- Event names: `chat:confirm`, `repo:changed`. Bound methods: `ConfirmChatAction(repoID, confirmID string, approve bool) error`, `GetChatConfirm(repoID string) *ConfirmEvent`.
- "Explain in chat" is unchanged (it already uses a task responder without tools).
- Commit messages: conventional style; **no `Co-Authored-By` line or any trailer**; never `git stash`.
- Go: `go test ./...` from the repo root (needs `frontend/dist` to exist — run `make build` once if the root package fails to embed it). Frontend from `frontend/` with `source ~/.nvm/nvm.sh && nvm use 22 >/dev/null &&` prefix: `npx vitest run`, `npm run check`.

## Review Focus

1. A small model calls `commit` with nothing staged, or `push` on a detached HEAD — must be an immediate `error:` with no card. (Task 1 tests.)
2. The user commits in the terminal while a `push` card is pending, then approves — must refuse, not push something else. (Task 2 stale test.)
3. Stop pressed while a card is pending — the blocked tool must unblock, nothing runs, and a second `ConfirmChatAction` for that id errors. (Task 2 tests.)
4. Switching repository and back while a card is pending — the card must reappear and still work. (Task 3 `withPendingConfirm` tests.)
5. Pull/merge that ends in conflicts — the model must be told the conflict view is open, and the app must refresh so the view actually appears. (Task 2 result text + `repo:changed`; Task 3 listener.)

---

### Task 1: `internal/ai/writetools` — specs, validation and proposals

**Files:**
- Create: `internal/ai/writetools/writetools.go`
- Test: `internal/ai/writetools/writetools_test.go`

**Interfaces:**
- Consumes: `ai.ToolSpec`, `ai.ToolCall` (`internal/ai`); `gitcmd.Run(ctx, dir, timeout, args...) (string, error)`, `gitcmd.ReadTimeout`; `worktree.Status(ctx, dir) (worktree.State, error)` (fields `Staged`, `Unstaged`, `Untracked []FileStatus{Path, Status, OldPath}`); `refs.Fingerprint(ctx, dir) (string, error)`.
- Produces:
  - `func Specs() []ai.ToolSpec` — the ten tools, in the order listed in Global Constraints.
  - `func IsWrite(name string) bool`
  - `type Env struct { PullStrategy string }`
  - `type Proposal struct { Title string; Details []string; Fingerprint string; Staged []string; Branch, Remote, Name, Start, Message string; Paths []string; Checkout, IncludeUntracked bool }` — `Title`/`Details` are shown to the user; `Fingerprint`/`Staged` are the precondition; the rest are the normalised arguments the app executes with (so execution never re-parses model JSON).
  - `func Prepare(ctx context.Context, dir string, call ai.ToolCall, env Env) (Proposal, error)` — returns a user-facing error (no `error:` prefix; the app adds it) for invalid calls.
  - `func Recheck(ctx context.Context, dir string, p Proposal, tool string) error` — `ErrChanged` when the fingerprint differs, or (tool `commit`) the staged path set differs.
  - `var ErrChanged = errors.New("the repository changed since this was proposed; check its state and propose again")`

Validation and proposal rules (implement exactly):

| Tool | Args | Invalid → error text | Title | Details |
|---|---|---|---|---|
| `stage_files` | `paths` array of strings, ≥1 | empty → `give at least one path`; a path not in Unstaged∪Untracked → `"<p>" has no unstaged change` | `Stage N file(s)` | the paths |
| `unstage_files` | `paths` | path not in Staged → `"<p>" is not staged` | `Unstage N file(s)` | the paths |
| `commit` | `message` | blank → `the commit message is empty`; nothing staged → `nothing is staged; stage files first` | `Commit N staged file(s)` | each message line, then `Files:` and the staged paths |
| `create_branch` | `name`, `start` (default `HEAD`), `checkout` bool | `git check-ref-format --branch name` fails → `"<name>" is not a valid branch name`; `refs/heads/<name>` exists → `branch "<name>" already exists`; start does not resolve to a commit → `"<start>" is not a commit` | `Create branch <name> at <short>` + ` and switch to it` when checkout | `<short> <subject>` of the start commit |
| `checkout_branch` | `name` | not `refs/heads/<name>` and not `refs/remotes/<name>` → `no branch "<name>"`; name is the current branch → `already on <name>` | `Switch from <current> to <name>` | for a remote branch: `creates local branch <rest> tracking <name>`; sets `Remote`/`Name` split at the first `/` |
| `stash_push` | `message` optional, `include_untracked` bool | no staged/unstaged change (and no untracked when include_untracked) → `nothing to stash` | `Stash N file(s)` | message if any; `includes untracked files` when set |
| `fetch` | — | `git remote` empty → `no remote is configured` | `Fetch from <remotes joined ", ">` | — |
| `push` | — | detached HEAD → `HEAD is detached; check out a branch first`; no upstream and no `origin` remote → `no upstream and no "origin" remote`; ahead 0 → `<branch> has nothing to push to <upstream>` | with upstream: `Push <branch> to <upstream>`; without: `Publish <branch> to origin (sets upstream origin/<branch>)` | up to 5 lines of `git log --oneline -n 5 <upstream>..HEAD` (or `HEAD` when publishing), plus `… and K more` |
| `pull` | — | detached → as push; no upstream → `<branch> has no upstream` ; behind 0 → `<branch> is up to date with <upstream>` | `Pull <upstream> into <branch> (<strategy>)` | up to 5 lines of `git log --oneline -n 5 HEAD..<upstream>` |
| `merge_branch` | `branch` | not a local or remote-tracking branch → `no branch "<b>"`; equals current → `cannot merge <b> into itself`; `rev-list --count HEAD..<b>` is 0 → `<b> is already merged into <current>` | `Merge <b> into <current>` | `N commit(s)`, `a merge commit is always created` |

Every successful `Prepare` sets `Fingerprint` from `refs.Fingerprint`; `commit` also sets `Staged` (sorted staged paths). Arguments arrive as `map[string]any`: strings as `string`, booleans as `bool`, arrays as `[]any` of `string` — reject other shapes with `argument "<k>" must be <type>`.

- [ ] **Step 1: Write the failing tests** — `internal/ai/writetools/writetools_test.go`, package `writetools_test`, using `testrepo`. Cover at least:

```go
package writetools_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"git-ui/internal/ai"
	"git-ui/internal/ai/writetools"
	"git-ui/internal/testrepo"
)

var ctx = context.Background()

func prep(dir, name string, args map[string]any) (writetools.Proposal, error) {
	return writetools.Prepare(ctx, dir, ai.ToolCall{ID: "c1", Name: name, Args: args}, writetools.Env{PullStrategy: "merge"})
}

func TestSpecsListTheTenTools(t *testing.T) {
	var names []string
	for _, s := range writetools.Specs() {
		names = append(names, s.Name)
		if s.Description == "" || s.Parameters["type"] != "object" || !writetools.IsWrite(s.Name) {
			t.Errorf("%s: incomplete spec", s.Name)
		}
	}
	want := "stage_files,unstage_files,commit,create_branch,checkout_branch,stash_push,fetch,push,pull,merge_branch"
	if strings.Join(names, ",") != want {
		t.Fatalf("names = %v", names)
	}
	if writetools.IsWrite("list_refs") {
		t.Fatal("list_refs is not a write tool")
	}
}

func TestCommitNeedsAMessageAndStagedFiles(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	if _, err := prep(r.Dir, "commit", map[string]any{"message": "feat: x"}); err == nil || !strings.Contains(err.Error(), "nothing is staged") {
		t.Fatalf("err = %v", err)
	}
	r.WriteFile("a.txt", "a\n")
	r.Git("add", "a.txt")
	if _, err := prep(r.Dir, "commit", map[string]any{"message": "  "}); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("err = %v", err)
	}
	p, err := prep(r.Dir, "commit", map[string]any{"message": "feat: add a\n\nbody"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Commit 1 staged file(s)" || p.Message != "feat: add a\n\nbody" || strings.Join(p.Staged, ",") != "a.txt" || p.Fingerprint == "" {
		t.Fatalf("proposal = %+v", p)
	}
}

func TestPushProposals(t *testing.T) {
	src := testrepo.New(t)
	src.Commit("base")
	remote := testrepo.NewBareFrom(t, src)
	r := testrepo.Clone(t, remote)
	if _, err := prep(r.Dir, "push", nil); err == nil || !strings.Contains(err.Error(), "nothing to push") {
		t.Fatalf("err = %v", err)
	}
	r.Commit("one")
	p, err := prep(r.Dir, "push", nil)
	if err != nil || p.Title != "Push main to origin/main" || len(p.Details) != 1 {
		t.Fatalf("p = %+v err = %v", p, err)
	}
	r.Git("switch", "-q", "-c", "topic")
	p, err = prep(r.Dir, "push", nil)
	if err != nil || !strings.HasPrefix(p.Title, "Publish topic to origin") {
		t.Fatalf("p = %+v err = %v", p, err)
	}
	r.Git("switch", "-q", "--detach")
	if _, err := prep(r.Dir, "push", nil); err == nil || !strings.Contains(err.Error(), "detached") {
		t.Fatalf("err = %v", err)
	}
}

func TestRecheckDetectsAMovedRefAndAChangedIndex(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.WriteFile("a.txt", "a\n")
	r.Git("add", "a.txt")
	p, err := prep(r.Dir, "commit", map[string]any{"message": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if err := writetools.Recheck(ctx, r.Dir, p, "commit"); err != nil {
		t.Fatalf("unchanged repo: %v", err)
	}
	r.WriteFile("b.txt", "b\n")
	r.Git("add", "b.txt")
	if err := writetools.Recheck(ctx, r.Dir, p, "commit"); !errors.Is(err, writetools.ErrChanged) {
		t.Fatalf("staged set changed: %v", err)
	}
	p2, _ := prep(r.Dir, "create_branch", map[string]any{"name": "x"})
	r.Commit("moved")
	if err := writetools.Recheck(ctx, r.Dir, p2, "create_branch"); !errors.Is(err, writetools.ErrChanged) {
		t.Fatalf("ref moved: %v", err)
	}
}
```

Add one test per remaining tool covering its happy path and each error row of the table (stage/unstage path checks, create_branch invalid/existing/bad start and the `and switch to it` title, checkout_branch local vs `origin/x` remote with `Remote`/`Name` set and "already on", stash_push nothing-to-stash and the untracked case, fetch with no remote, pull up-to-date and behind with the strategy in the title, merge_branch self/already-merged/happy with commit count), and one for a wrong argument type (`paths: "a.txt"` → `argument "paths" must be`).

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/ai/writetools/`
Expected: FAIL — package does not exist / undefined symbols.

- [ ] **Step 3: Implement `writetools.go`.** Structure: `Specs()` built with the same `str`/`obj` helper style as `internal/ai/tools/tools.go` (add `boolean` and `array of string` helpers); `IsWrite` from a `map[string]bool` of the ten names; `Prepare` = argument decoding helpers (`stringArg`, `boolArg`, `pathsArg` returning the `argument "<k>" must be …` errors) + a `switch call.Name` with one small function per tool implementing its table row + fingerprint at the end; `Recheck` = compare `refs.Fingerprint` and, for `commit`, the sorted staged paths from `worktree.Status`. Use `gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, …)` for every read (`symbolic-ref -q --short HEAD`, `rev-parse --abbrev-ref --symbolic-full-name @{upstream}`, `rev-list --count`, `log --oneline -n 5`, `rev-parse --verify --quiet <ref>`, `check-ref-format --branch`, `remote`). Package doc comment: `// Package writetools defines the chat's write tools: their specs, argument validation and the proposal shown to the user before anything runs. Execution lives in internal/app, through the same methods the UI uses.`

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/ai/writetools/ && go vet ./internal/ai/writetools/ && gofmt -l internal/ai/writetools`
Expected: PASS, no vet output, no gofmt output.

- [ ] **Step 5: Commit**

```bash
git add internal/ai/writetools
git commit -m "feat(ai): write tool specs, validation and proposals"
```

---

### Task 2: App — confirmation, execution, events, prompt, bindings

**Files:**
- Create: `internal/app/chatwrite.go`
- Create: `internal/app/chatwrite_test.go`
- Modify: `internal/app/ai.go` (`aiState`, `WithAI`, `SendChat`'s tool list and `RunTool`)
- Modify: `internal/ai/prompts/defaults/chat.md`
- Modify: `internal/app/ai_test.go` (the `read-only` system-prompt assertion)
- Regenerate: `frontend/wailsjs/go/app/App.d.ts`, `App.js`, `frontend/wailsjs/go/models.ts`

**Interfaces:**
- Consumes (Task 1): `writetools.Specs`, `IsWrite`, `Env`, `Proposal` (fields listed in Task 1), `Prepare`, `Recheck`, `ErrChanged`.
- Produces:
  - `const EventChatConfirm = "chat:confirm"`, `const EventRepoChanged = "repo:changed"`
  - `type ConfirmEvent struct { RepoID string \`json:"repoID"\`; RunID string \`json:"runID"\`; ConfirmID string \`json:"confirmID"\`; Tool string \`json:"tool"\`; Title string \`json:"title"\`; Details []string \`json:"details"\` }`
  - `type RepoChangedEvent struct { RepoID string \`json:"repoID"\` }`
  - `func (a *App) ConfirmChatAction(repoID, confirmID string, approve bool) error`
  - `func (a *App) GetChatConfirm(repoID string) *ConfirmEvent`

- [ ] **Step 1: Write the failing tests** — `internal/app/chatwrite_test.go` (package `app`), reusing `newAIApp`, `events`, `writeLines` from `ai_test.go`. Add a fake Ollama that asks for one given write tool once, then answers text:

```go
// fakeWriteOllama asks for tool(args) on the first turn and answers "ok"
// once the tool result is in the history.
func fakeWriteOllama(t *testing.T, tool, argsJSON string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			fmt.Fprint(w, `{"models":[{"name":"qwen2.5:7b","size":1}]}`)
		case "/api/chat":
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			msgs := req["messages"].([]any)
			if msgs[len(msgs)-1].(map[string]any)["role"] != "tool" {
				writeLines(w, `{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","function":{"name":"`+tool+`","arguments":`+argsJSON+`}}]},"done":false}`, `{"message":{"content":""},"done":true}`)
				return
			}
			writeLines(w, `{"message":{"role":"assistant","content":"ok"},"done":false}`, `{"message":{"content":""},"done":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// toolResult returns the content of the saved tool message.
func toolResult(t *testing.T, a *App, id string) string {
	t.Helper()
	history, err := a.GetChat(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range history {
		if m.Role == ai.RoleTool {
			return m.Content
		}
	}
	t.Fatalf("no tool message in %+v", history)
	return ""
}
```

Tests (each starts with `a, id, ev := newAIApp(t, fakeWriteOllama(t, "create_branch", `{"name":"topic"}`).URL)` unless stated; `dir, _ := a.dir(id)` gives the repo path):
- `TestChatWriteApprovedRuns`: `SendChat` → `ev.wait(EventChatConfirm)`; assert `ConfirmEvent` fields (`Tool == "create_branch"`, title starts `Create branch topic at`); `GetChatConfirm(id)` returns it; `ConfirmChatAction(id, confirmID, true)`; wait `repo:changed` and `chat:done`; branch exists (`git rev-parse --verify refs/heads/topic` via `testrepo`-free `exec.Command` or `refs` package); `toolResult` starts `done:`; `GetChatConfirm(id)` is nil; a second `ConfirmChatAction` with the same id returns an error.
- `TestChatWriteRejectedDoesNothing`: approve=false → result `rejected by the user`; branch absent; no `repo:changed` in `ev.names()`.
- `TestChatWriteRefusesWhenTheRepoChanged`: after the confirm event, create a commit in `dir` with `exec.Command("git", "-C", dir, "commit", "--allow-empty", "-q", "-m", "moved")` (set `GIT_AUTHOR_*`/`GIT_COMMITTER_*` env or `-c user.name=t -c user.email=t@t`), then approve → result starts `error: the repository changed`; branch absent.
- `TestChatWriteBusyLock`: hold the write lock with the `started/release` pattern from `TestWriteRejectsConcurrentOperation`, approve → result starts `error: another operation is running`; release.
- `TestChatWriteStopWhilePendingRejects`: after the confirm event, `StopChat(id)` → wait `chat:done`; branch absent; `GetChatConfirm(id)` nil; `ConfirmChatAction` for that id errors.
- `TestChatWriteInvalidArgsNeverAsks`: tool `create_branch` with `{"name":"feature"}` (exists in `newTestApp`'s repo) → wait `chat:done`; no `chat:confirm` in `ev.names()`; result starts `error: branch "feature" already exists`.
- `TestConfirmChatActionUnknownID`: returns an error.
- `TestSendChatOffersWriteTools`: with the existing `fakeOllama(onChat)`, capture `req["tools"]` names and assert they include `list_refs` and `push`.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/app/ -run 'ChatWrite|ConfirmChatAction|OffersWriteTools'`
Expected: FAIL — undefined `EventChatConfirm`, `ConfirmChatAction`, `GetChatConfirm`.

- [ ] **Step 3: Implement.**

In `ai.go`: add `confirms map[string]*pendingConfirm` to `aiState` and initialise it in `WithAI`. In `SendChat`, read the pull strategy once (`gs, _ := a.gitSettings()`; empty strategy on error) and build the run with:

```go
			Tools: append(tools.Specs(), writetools.Specs()...),
			RunTool: func(ctx context.Context, call ai.ToolCall) string {
				if writetools.IsWrite(call.Name) {
					return a.runWriteTool(ctx, repoID, runID, repo.Path, call, writetools.Env{PullStrategy: gs.PullStrategy})
				}
				return tools.Run(ctx, repo.Path, call)
			},
```

`chatwrite.go`:

```go
package app

// Write tools in the chat: the model proposes, the user approves in the chat
// panel, and the change runs through the same App methods the UI uses —
// under the same per-repo write lock. The lock is NOT held while waiting for
// the user; the fingerprint re-check on approval is what keeps that safe.

const (
	EventChatConfirm = "chat:confirm"
	EventRepoChanged = "repo:changed"
)

type ConfirmEvent struct { /* as in Interfaces */ }
type RepoChangedEvent struct { RepoID string `json:"repoID"` }

type pendingConfirm struct {
	event  ConfirmEvent
	answer chan bool // buffered, capacity 1
}

func (a *App) runWriteTool(ctx context.Context, repoID, runID, dir string, call ai.ToolCall, env writetools.Env) string {
	p, err := writetools.Prepare(ctx, dir, call, env)
	if err != nil {
		return "error: " + err.Error()
	}
	id := newConfirmID() // 16 random bytes, hex (crypto/rand)
	pc := &pendingConfirm{event: ConfirmEvent{RepoID: repoID, RunID: runID, ConfirmID: id, Tool: call.Name, Title: p.Title, Details: p.Details}, answer: make(chan bool, 1)}
	a.ai.mu.Lock()
	a.ai.confirms[id] = pc
	a.ai.mu.Unlock()
	a.emit(EventChatConfirm, pc.event)

	var approved bool
	select {
	case approved = <-pc.answer:
	case <-ctx.Done():
	}
	a.ai.mu.Lock()
	delete(a.ai.confirms, id)
	a.ai.mu.Unlock()
	if !approved {
		return "rejected by the user"
	}
	if err := writetools.Recheck(ctx, dir, p, call.Name); err != nil {
		return "error: " + err.Error()
	}
	done, err := a.executeWrite(repoID, call.Name, p)
	if errors.Is(err, ErrBusy) {
		return "error: another operation is running in this repository; try again when it finishes"
	}
	if err != nil {
		return "error: " + err.Error()
	}
	a.emit(EventRepoChanged, RepoChangedEvent{RepoID: repoID})
	return "done: " + done
}
```

`executeWrite(repoID, tool string, p writetools.Proposal) (string, error)` switches on the tool and calls the App method with `p`'s normalised fields: stage/unstage loop over `p.Paths` (on a failure part-way return an error naming the paths already done: `staged a.txt; then <err>`); `CommitChanges(repoID, p.Message, false)`; `CreateBranch(repoID, p.Name, p.Start, p.Checkout)`; `Checkout(repoID, p.Name)` or `CheckoutRemote(repoID, p.Remote, p.Name)` when `p.Remote != ""`; `StashPush(repoID, p.Message, p.IncludeUntracked)`; `Fetch`; `Push`; `Pull` → on `ops.Conflicted` return `pull stopped with conflicts in N file(s); the conflict view is open — resolve them there`; `MergeBranch(repoID, p.Branch)` → on `merge.Conflicted` the same wording with "merge". Success strings are short past-tense sentences built from `p.Title` (e.g. `"Push main to origin/main" → "pushed main to origin/main"` is fine as `strings.ToLower(p.Title[:1]) + p.Title[1:]` prefixed by nothing — keep it simple: return `p.Title`).

`ConfirmChatAction`: `ErrAIDisabled` when `a.ai == nil`; look up the id under `a.ai.mu`, error `no pending confirmation %q` when absent or when its `event.RepoID != repoID`; delete it from the map and send `approve` on the buffered channel (never blocks). `GetChatConfirm`: return a copy of the pending event for `repoID`, or nil.

In `chat.md`, replace the first rule with:

```
- You can read the repository with the read tools. You can also propose changes with the write tools: stage_files, unstage_files, commit, create_branch, checkout_branch, stash_push, fetch, push, pull and merge_branch. Each write is shown to the user, who approves or rejects it before anything runs.
- Propose one write at a time and wait for its result. Only say a change happened when the tool result starts with "done:". If it says "rejected by the user", do not retry it unless asked.
- Destructive operations (reset, discard, deleting branches, amending, force-push, dropping stashes, rebase) are not available; when asked, explain which app action or git command the user can run themselves.
```

In `ai_test.go`'s `TestSendChatRunsToolsStreamsAndSaves`, change the `"read-only"` assertion to `"approves or rejects"`.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/app/ ./internal/ai/... && go vet ./... && gofmt -l internal`
Expected: PASS; no vet/gofmt output.

- [ ] **Step 5: Regenerate the Wails bindings**

Run from the repo root: `source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && ~/go/bin/wails generate module`
Expected: `frontend/wailsjs/go/app/App.d.ts` gains `ConfirmChatAction(arg1:string,arg2:string,arg3:boolean):Promise<void>` and `GetChatConfirm(arg1:string):Promise<app.ConfirmEvent>`; `models.ts` gains `app.ConfirmEvent`. Revert any unrelated file it touches (e.g. `git checkout -- frontend/wailsjs/runtime frontend/package.json.md5`).

- [ ] **Step 6: Full Go suite, then commit**

Run: `go test ./...`
Expected: all PASS (run `make build` first if the root package cannot embed `frontend/dist`; revert `frontend/package.json.md5` afterwards).

```bash
git add internal/app/chatwrite.go internal/app/chatwrite_test.go internal/app/ai.go internal/app/ai_test.go internal/ai/prompts/defaults/chat.md frontend/wailsjs/go
git commit -m "feat(ai): chat write tools confirmed by the user before they run"
```

---

### Task 3: Frontend — the confirmation card and repo refresh, plus docs

**Files:**
- Modify: `frontend/src/lib/types.ts` (event types)
- Modify: `frontend/src/lib/api.ts` (two calls)
- Modify: `frontend/src/lib/chat.ts` (+ `frontend/src/lib/chat.test.ts`)
- Modify: `frontend/src/components/ChatPanel.svelte`
- Modify: `frontend/src/App.svelte` (`repo:changed` listener)
- Modify: `docs/superpowers/specs/2026-09-23-chat-write-tools-design.md` (Status line)

**Interfaces:**
- Consumes (Task 2): event `chat:confirm` with `ConfirmEvent` JSON `{repoID, runID, confirmID, tool, title, details}`; event `repo:changed` `{repoID}`; bindings `ConfirmChatAction(repoID, confirmID, approve)`, `GetChatConfirm(repoID)` (resolves to the event or `null`); tool result summaries start with `done`, `rejected` or `error`.
- Produces:
  - `types.ts`: `export interface ChatConfirmEvent { repoID: string; runID: string; confirmID: string; tool: string; title: string; details: string[] }`, `export interface RepoChangedEvent { repoID: string }`
  - `api.ts`: `confirmChatAction(repoID, confirmID, approve)`, `getChatConfirm(repoID): Promise<ChatConfirmEvent | null>`
  - `chat.ts`: `ChatToolUse.confirm?: { id: string; title: string; details: string[]; state: 'pending' | 'done' | 'rejected' | 'failed' }`; `confirmState(summary: string): 'done' | 'rejected' | 'failed'`; `withPendingConfirm(state: ChatState, ev: ChatConfirmEvent): ChatState`; `'chat:confirm'` in `CHAT_EVENTS`.

- [ ] **Step 1: Write the failing tests** (append to `frontend/src/lib/chat.test.ts`; follow its existing fixtures for building a `ChatState` with an active run):

```ts
describe('write confirmations', () => {
  const confirm = (over: Partial<ChatConfirmEvent> = {}): ChatConfirmEvent => ({
    repoID: 'r', runID: 'run', confirmID: 'c1', tool: 'push', title: 'Push main to origin/main', details: ['a1b2c3d x'], ...over,
  })
  const running = () => {
    let s = startRun(emptyChat('r'), 'push it', 'run')
    s = applyEvent(s, 'chat:tool', { repoID: 'r', runID: 'run', name: 'push', args: {} })
    return s
  }

  it('attaches a pending card to the waiting tool', () => {
    const s = applyEvent(running(), 'chat:confirm', confirm())
    const tool = s.items[s.items.length - 1].tools[0]
    expect(tool.confirm).toEqual({ id: 'c1', title: 'Push main to origin/main', details: ['a1b2c3d x'], state: 'pending' })
  })

  it('resolves the card from the tool result', () => {
    let s = applyEvent(running(), 'chat:confirm', confirm())
    s = applyEvent(s, 'chat:tool_result', { repoID: 'r', runID: 'run', name: 'push', summary: 'rejected by the user' })
    expect(s.items[s.items.length - 1].tools[0].confirm?.state).toBe('rejected')
  })

  it('maps summaries to card states', () => {
    expect(confirmState('done: Push main to origin/main')).toBe('done')
    expect(confirmState('rejected by the user')).toBe('rejected')
    expect(confirmState('error: another operation is running')).toBe('failed')
  })

  it('restores a pending card into a reloaded conversation', () => {
    const reloaded = { repoID: 'r', runID: null, items: [{ role: 'user' as const, text: 'push it', tools: [] }] }
    const s = withPendingConfirm(reloaded, confirm())
    expect(s.runID).toBe('run')
    const last = s.items[s.items.length - 1]
    expect(last.role).toBe('assistant')
    expect(last.tools[0]).toMatchObject({ name: 'push', confirm: { id: 'c1', state: 'pending' } })
    expect(withPendingConfirm(s, confirm())).toEqual(s)
  })

  it('ignores a confirmation for another repository', () => {
    const s = running()
    expect(withPendingConfirm(s, confirm({ repoID: 'other' }))).toBe(s)
  })

  it('subscribes to chat:confirm', () => {
    expect(CHAT_EVENTS).toContain('chat:confirm')
  })
})
```

(Adjust the imports at the top of `chat.test.ts` to include `confirmState`, `withPendingConfirm`, `startRun`, `emptyChat`, `CHAT_EVENTS` and `type ChatConfirmEvent`; if `emptyChat`'s signature differs, build the state the way the file's existing tests do.)

- [ ] **Step 2: Run to verify failure**

Run: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run src/lib/chat.test.ts`
Expected: FAIL — missing exports.

- [ ] **Step 3: Implement `chat.ts`.** Add `confirm?` to `ChatToolUse`; `'chat:confirm'` to `CHAT_EVENTS` and the `Payload` union; `confirmState`:

```ts
export function confirmState(summary: string): 'done' | 'rejected' | 'failed' {
  if (summary.startsWith('done')) return 'done'
  if (summary.startsWith('rejected')) return 'rejected'
  return 'failed'
}
```

`withPendingConfirm`:

```ts
/** Puts a pending write confirmation on screen: on the tool the model is
 *  waiting on, or — after the panel re-mounted and reloaded a history that
 *  does not include the in-flight answer yet — on a fresh assistant item. */
export function withPendingConfirm(state: ChatState, ev: ChatConfirmEvent): ChatState {
  if (ev.repoID !== state.repoID) return state
  const items = state.items.slice()
  let last = items[items.length - 1]
  if (!last || last.role !== 'assistant') {
    last = { role: 'assistant', text: '', tools: [] }
    items.push(last)
  } else {
    last = { ...last, tools: last.tools.slice() }
    items[items.length - 1] = last
  }
  if (last.tools.some((t) => t.confirm?.id === ev.confirmID)) return state.runID === ev.runID ? state : { ...state, runID: ev.runID }
  const confirm = { id: ev.confirmID, title: ev.title, details: ev.details ?? [], state: 'pending' as const }
  const i = last.tools.findIndex((t) => t.name === ev.tool && t.summary === undefined && !t.confirm)
  if (i >= 0) last.tools[i] = { ...last.tools[i], confirm }
  else last.tools.push({ name: ev.tool, args: null, confirm })
  return { ...state, runID: ev.runID, items }
}
```

In `applyEvent`: add `case 'chat:confirm': return withPendingConfirm(state, payload as ChatConfirmEvent)` (it runs after the existing runID guard, so only for the current run); in `case 'chat:tool_result'`, when the matched tool has `confirm`, also set `confirm: { ...confirm, state: confirmState(p.summary) }`.

Note the idempotence test: `withPendingConfirm(s, confirm())` on a state that already shows that card must return an equal state — the early `some(...)` return handles it; make sure it returns the original `state` object's content (the test uses `toEqual`).

- [ ] **Step 4: Run to verify pass**

Run: `npx vitest run src/lib/chat.test.ts`
Expected: PASS.

- [ ] **Step 5: Types, API, panel and app listener.**
- `types.ts`: add the two interfaces.
- `api.ts`: next to `sendChat`/`stopChat`: `confirmChatAction: (repoID: string, confirmID: string, approve: boolean) => call<void>(Go.ConfirmChatAction(repoID, confirmID, approve))`, `getChatConfirm: (repoID: string) => call<ChatConfirmEvent | null>(Go.GetChatConfirm(repoID) as Promise<ChatConfirmEvent | null>)`.
- `ChatPanel.svelte`: where tools render (`{#each item.tools as tool}` ~line 149), when `tool.confirm` is set render a card instead of the one-line tool row: the title (bold), each detail line (monospace, muted), and — while `state === 'pending'` — Reject (`.btn`) and Approve (`.btn.primary`) buttons calling `decide(tool.confirm.id, approve)`; otherwise one line: `Approved and done` / `Rejected` / `Failed` plus the summary. `decide` adds the id to a local `deciding` set (buttons disabled while in it) and calls `api.confirmChatAction($selectedRepoId or the panel's repoID, id, approve)`, toasting an error via `errorMessage` on failure. After the panel loads a repository's history (~line 40-42, `state = fromMessages(repoID, messages)`), call `api.getChatConfirm(repoID)` and, if it returns an event for that repo, `state = withPendingConfirm(state, ev)` (guard against a repo switch having happened meanwhile, the same way the surrounding load code does). Card styles use existing theme tokens (`--border`, `--surface`, `--muted`, `--danger` for the failed line); keep them next to `.tool`.
- `App.svelte`: in `onMount`, beside the `worktree:changed` listener, add `EventsOn('repo:changed', (payload: RepoChangedEvent) => { if (payload?.repoID === $selectedRepo?.id) refreshRepo() })`, and call its unsubscribe in the returned cleanup. Import `refreshRepo` from `./lib/stores` and the type from `./lib/types`.

- [ ] **Step 6: Docs.** In `docs/superpowers/specs/2026-09-23-chat-write-tools-design.md` set `Status: Implemented (manual pass pending)`.

- [ ] **Step 7: Verify and commit**

Run: `npx vitest run && npm run check` (from `frontend/`), then `go test ./...` and `make build` from the root (revert `frontend/package.json.md5` if the build touches it).
Expected: all vitest PASS; svelte-check 0 errors, no new warnings in touched files; Go PASS; build succeeds.

```bash
git add frontend/src/lib/types.ts frontend/src/lib/api.ts frontend/src/lib/chat.ts frontend/src/lib/chat.test.ts frontend/src/components/ChatPanel.svelte frontend/src/App.svelte docs/superpowers/specs/2026-09-23-chat-write-tools-design.md
git commit -m "feat(chat): approve or reject the agent's changes from the chat"
```

- [ ] **Step 8: Manual checklist for the owner** (report as not run): ask the chat to commit staged work → card with message and files → Approve → commit appears in the log; push → card shows commits → Reject → nothing pushed and the model says so; "create a branch x and switch to it" → one card → branch checked out, sidebar updates; pull with a conflicting upstream → conflict view opens; Stop while a card is pending → answer ends, nothing ran; switch repository and back with a card pending → card is still there and works; commit in the terminal while a push card is pending → Approve → refused as changed.
