# Notifications minors Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the six user-visible notification / background-fetch minors.

**Architecture:** Go keeps the pause state and notification delivery
(`internal/app`); the frontend reads the paused remotes on every repository
refresh and shows them on the toolbar's Fetch; chat notifications move from
the tool call to its result.

**Tech Stack:** Go 1.x + Wails v2.16, Svelte + TypeScript, vitest.

**Spec:** `docs/superpowers/specs/2026-10-02-notifications-minors-design.md`

## Global Constraints

- Every behaviour change commits together with its `docs/spec` update.
- Commits have no `Co-Authored-By` line. In this worktree run git as `/usr/bin/git`.
- Go tests: `go test ./internal/...`; frontend tests: `cd frontend && npx vitest run` (node 22: `source ~/.nvm/nvm.sh && nvm use 22`). Run `npm ci` in `frontend` first if `node_modules` is missing.
- Bindings: after adding a Go method run `~/go/bin/wails generate module`, then `/usr/bin/git checkout -- frontend/wailsjs/runtime`; commit `frontend/wailsjs/go/app/App.{d.ts,js}` (and `models.ts` only if it changed).
- Tooltip text, exactly: `Background fetch paused for <remotes joined by ", ">: authentication failed. Fetch to retry.`
- Linux notification action: category id `open`, action `{ID: "open", Title: "Open"}`.

Two deliberate narrowings of the spec, recorded in the spec in Task 3:
the paused list is kept for the **selected** repository only (like
`remoteInfo`), and `--warning` is defined in the light and dark blocks only
(the high-contrast blocks don't redefine `--danger` either; they inherit).

## Review Focus

- A pull that fails for a reason other than conflicts (network, dirty tree) must **not** unpause — Task 1 tests it.
- A background fetch whose remote just failed auth must make the dot appear without the user doing anything — Task 3 (`outcome` refreshes on non-empty `authFailed`).
- Switching repositories must not show the previous repository's dot — Task 3 (`loadPausedRemotes` guards on the selected id, `selectRepo` loads it).
- A second notification while the macOS permission dialog is open must not block or be lost silently — Task 4 (returns `ErrNotificationsDenied` → toast).
- A rejected decision card must neither notify nor swallow the "Chat answer finished" notification — Task 5.

---

### Task 1: Conflicted pull resumes the pause; timeout sentence

**Files:**
- Modify: `internal/app/remote.go` (`Pull`)
- Test: `internal/app/autopause_test.go`
- Modify: `docs/spec/05-remote-and-stash.md` ("Background fetch" paragraph)

**Interfaces:**
- Consumes: `a.paused.resume(key string)`, `cmdlog.RepoKey(dir string) string`, `ops.Conflicted`.
- Produces: nothing new.

- [ ] **Step 1: Write the failing tests** — append to `internal/app/autopause_test.go`:

```go
// conflictingClone sets up r with origin, and pushes a commit to origin that
// conflicts with a local commit of r on the same file.
func conflictingClone(t *testing.T, r *testrepo.Repo) {
	t.Helper()
	bare := testrepo.NewBareFrom(t, r)
	r.Git("remote", "add", "origin", bare)
	r.Git("push", "-q", "-u", "origin", "main")
	clone := testrepo.Clone(t, bare)
	clone.WriteFile("same.txt", "theirs\n")
	clone.Git("add", "same.txt")
	clone.Git("commit", "-q", "-m", "theirs")
	clone.Git("push", "-q", "origin", "main")
	r.WriteFile("same.txt", "ours\n")
	r.Git("add", "same.txt")
	r.Git("commit", "-q", "-m", "ours")
	// The default "auto" strategy runs a plain pull; without this, git
	// refuses divergent branches before merging and there is no conflict.
	r.Git("config", "pull.rebase", "false")
}

func TestAConflictedPullUnpauses(t *testing.T) {
	a, r, id := newPlainApp(t)
	conflictingClone(t, r)
	dir, err := a.dir(id)
	if err != nil {
		t.Fatal(err)
	}
	key := cmdlog.RepoKey(dir)
	a.paused.pause(key, []string{"origin"})

	res, err := a.Pull(id)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != ops.Conflicted {
		t.Fatalf("outcome = %v, want Conflicted", res.Outcome)
	}
	if a.paused.paused(key, "origin") {
		t.Error("a pull that reached the remote and stopped on conflicts left origin paused")
	}
}

func TestAFailedPullKeepsThePause(t *testing.T) {
	a, r, id := newPlainApp(t)
	r.Git("remote", "add", "origin", filepath.Join(t.TempDir(), "missing.git"))
	r.Git("config", "branch.main.remote", "origin")
	r.Git("config", "branch.main.merge", "refs/heads/main")
	dir, err := a.dir(id)
	if err != nil {
		t.Fatal(err)
	}
	key := cmdlog.RepoKey(dir)
	a.paused.pause(key, []string{"origin"})

	if _, err := a.Pull(id); err == nil {
		t.Fatal("want the pull to fail")
	}
	if !a.paused.paused(key, "origin") {
		t.Error("a failed pull unpaused origin")
	}
}
```

Add `"path/filepath"` and `"git-ui/internal/ops"` to the test file's imports.

- [ ] **Step 2: Run them**

Run: `go test ./internal/app/ -run 'TestAConflictedPullUnpauses|TestAFailedPullKeepsThePause' -v`
Expected: `TestAConflictedPullUnpauses` FAILS ("left origin paused"); `TestAFailedPullKeepsThePause` passes.

- [ ] **Step 3: Implement** — in `internal/app/remote.go`, `Pull`'s closure becomes:

```go
	err = a.write(id, func(ctx context.Context, dir string) error {
		var pullErr error
		result, pullErr = ops.Pull(ctx, dir, cfg.PullStrategy)
		// A pull that stopped on conflicts exits non-zero, but its fetch
		// reached the remote: credentials work, so background fetches resume.
		if pullErr == nil && result.Outcome == ops.Conflicted {
			a.paused.resume(cmdlog.RepoKey(dir))
		}
		return pullErr
	})
```

Add `"git-ui/internal/cmdlog"` to the imports.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/app/ -run 'Pull|Pause|AutoFetch' -v`
Expected: PASS.

- [ ] **Step 5: Spec** — in `docs/spec/05-remote-and-stash.md`, "Background fetch":
  - "until any fetch or pull of the repository succeeds, whether from the toolbar, the AI chat or a git-flow action, or the app restarts." → "until any fetch or pull of the repository succeeds — a pull that stops on conflicts counts, since it reached the remote — whether from the toolbar, the AI chat or a git-flow action, or the app restarts."
  - "It times out after 60 s," → "Each remote's fetch times out after 60 s;" (keep the rest of the sentence: "never shows the busy label, …" — adjust punctuation so it reads: "Each remote's fetch times out after 60 s. A background fetch never shows the busy label, and when it changes …").

- [ ] **Step 6: Commit**

```bash
/usr/bin/git add internal/app/remote.go internal/app/autopause_test.go docs/spec/05-remote-and-stash.md
/usr/bin/git commit -m "fix(app): a pull that stops on conflicts resumes background fetches"
```

---

### Task 2: `AutoFetchPaused` in Go

**Files:**
- Modify: `internal/app/autopause.go` (add `list`)
- Modify: `internal/app/autofetch.go` (add `AutoFetchPaused`)
- Test: `internal/app/autopause_test.go`
- Regenerate: `frontend/wailsjs/go/app/App.d.ts`, `App.js`

**Interfaces:**
- Produces: `func (p *autoPause) list(key string) []string` (sorted, never nil); `func (a *App) AutoFetchPaused(id string) ([]string, error)`; JS binding `AutoFetchPaused(arg1:string):Promise<Array<string>>`.

- [ ] **Step 1: Failing test** — append to `internal/app/autopause_test.go`:

```go
func TestAutoFetchPausedListsTheRepositorysPausedRemotes(t *testing.T) {
	a, _, id := newPlainApp(t)
	got, err := a.AutoFetchPaused(id)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("got %#v, want an empty, non-nil list", got)
	}
	dir, err := a.dir(id)
	if err != nil {
		t.Fatal(err)
	}
	a.paused.pause(cmdlog.RepoKey(dir), []string{"upstream", "origin"})
	a.paused.pause("/somewhere/else", []string{"other"})
	if got, _ = a.AutoFetchPaused(id); !reflect.DeepEqual(got, []string{"origin", "upstream"}) {
		t.Fatalf("got %v, want [origin upstream]", got)
	}
	if _, err := a.AutoFetchPaused("no-such-id"); err == nil {
		t.Error("want an error for an unknown repository")
	}
}
```

Add `"reflect"` to the imports.

- [ ] **Step 2: Run** — `go test ./internal/app/ -run TestAutoFetchPaused -v` → FAIL (undefined `AutoFetchPaused`).

- [ ] **Step 3: Implement** — `internal/app/autopause.go`, after `resume` (add `"sort"` import):

```go
// list is key's paused remotes, sorted; empty, not nil, when none.
func (p *autoPause) list(key string) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := []string{}
	for r := range p.remotes[key] {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}
```

`internal/app/autofetch.go`, at the end:

```go
// AutoFetchPaused is the remotes of repository id that background fetches
// skip for want of credentials, for the toolbar's Fetch dot.
func (a *App) AutoFetchPaused(id string) ([]string, error) {
	dir, err := a.dir(id)
	if err != nil {
		return nil, err
	}
	return a.paused.list(cmdlog.RepoKey(dir)), nil
}
```

- [ ] **Step 4: Run** — `go test ./internal/app/ -v -run 'Pause|AutoFetch'` → PASS.

- [ ] **Step 5: Bindings** — `~/go/bin/wails generate module && /usr/bin/git checkout -- frontend/wailsjs/runtime`; check `frontend/wailsjs/go/app/App.d.ts` has `AutoFetchPaused(arg1:string):Promise<Array<string>>`.

- [ ] **Step 6: Commit**

```bash
/usr/bin/git add internal/app/autopause.go internal/app/autofetch.go internal/app/autopause_test.go frontend/wailsjs/go/app/App.d.ts frontend/wailsjs/go/app/App.js
/usr/bin/git commit -m "feat(app): AutoFetchPaused lists a repository's paused remotes"
```

---

### Task 3: Amber dot on Fetch

**Files:**
- Modify: `frontend/src/lib/api.ts` (add `autoFetchPaused`)
- Modify: `frontend/src/lib/autoFetch.ts` (`pausedTooltip`; `outcome` refresh on `authFailed`)
- Modify: `frontend/src/lib/stores.ts` (`pausedRemotes` store, `loadPausedRemotes`, call it from `refreshRepo` and `selectRepo`)
- Modify: `frontend/src/lib/toolbar.ts` (input `paused`, item field `dot`)
- Modify: `frontend/src/components/Toolbar.svelte` (pass `paused`, render dot)
- Modify: `frontend/src/theme.css` (`--warning`)
- Test: `frontend/src/lib/autoFetch.test.ts`, `frontend/src/lib/toolbar.test.ts`
- Modify: `docs/spec/05-remote-and-stash.md` (toolbar table Fetch row, "Background fetch" paragraph); `docs/superpowers/specs/2026-10-02-notifications-minors-design.md` (the two narrowings)

**Interfaces:**
- Consumes: Go binding `AutoFetchPaused(id)` from Task 2.
- Produces: `pausedTooltip(remotes: string[]): string`; `pausedRemotes: Writable<string[]>`; `loadPausedRemotes(): Promise<void>`; `ToolbarInput.paused: string[]`; `ToolbarItem.dot: boolean`.

- [ ] **Step 1: Failing tests**

`frontend/src/lib/autoFetch.test.ts` — import `pausedTooltip` too, and add:

```ts
describe('pausedTooltip', () => {
  it('is empty with nothing paused and names every paused remote', () => {
    expect(pausedTooltip([])).toBe('')
    expect(pausedTooltip(['origin'])).toBe('Background fetch paused for origin: authentication failed. Fetch to retry.')
    expect(pausedTooltip(['origin', 'upstream'])).toBe('Background fetch paused for origin, upstream: authentication failed. Fetch to retry.')
  })
})
```

and inside `describe('outcome', …)`:

```ts
  it('refreshes the selected repository when a remote just got paused', () => {
    expect(outcome('a', { result: res({ authFailed: ['origin'] }) }, sel).refresh).toBe(true)
    expect(outcome('a', { result: res({ authFailed: [] }) }, sel).refresh).toBe(false)
    expect(outcome('b', { result: res({ authFailed: ['origin'] }) }, sel).refresh).toBe(false)
  })
```

`frontend/src/lib/toolbar.test.ts` — the `input` helper gains `paused: []` in its defaults, and add:

```ts
  it('marks Fetch with a dot and says why while a remote is paused', () => {
    expect(item({}, 'fetch')).toMatchObject({ dot: false, title: 'Fetch from all remotes' })
    expect(item({ paused: ['origin'] }, 'fetch')).toMatchObject({
      enabled: true, dot: true, title: 'Background fetch paused for origin: authentication failed. Fetch to retry.',
    })
    expect(item({ paused: ['origin'], busy: 'Pushing…' }, 'fetch')).toMatchObject({ enabled: false, dot: true, title: 'Pushing…' })
    expect(item({ paused: ['origin'] }, 'pull').dot).toBe(false)
  })
```

- [ ] **Step 2: Run** — `cd frontend && npx vitest run src/lib/autoFetch.test.ts src/lib/toolbar.test.ts` → FAIL.

- [ ] **Step 3: Implement**

`frontend/src/lib/autoFetch.ts`:

```ts
/** The toolbar Fetch's tooltip while background fetches skip remotes that
 *  failed for want of credentials; '' when none is paused. */
export function pausedTooltip(remotes: string[]): string {
  if (remotes.length === 0) return ''
  return `Background fetch paused for ${remotes.join(', ')}: authentication failed. Fetch to retry.`
}
```

and in `outcome`:

```ts
  // A newly paused remote changed no refs but must show on the toolbar.
  const changed = res.refsChanged || (res.authFailed?.length ?? 0) > 0
  const action: AutoFetchAction = { refresh: changed && id === s.selectedId && !s.busy }
```

`frontend/src/lib/api.ts`, after `autoFetch`:

```ts
  autoFetchPaused: (id: string) => call<string[]>(Go.AutoFetchPaused(id)),
```

`frontend/src/lib/stores.ts` — next to `remoteInfo` (line ~262):

```ts
/** The selected repository's remotes that background fetches skip for want
 *  of credentials (Go's autoPause); the toolbar's Fetch shows a dot. */
export const pausedRemotes = writable<string[]>([])
```

after `loadRemoteInfo`:

```ts
export async function loadPausedRemotes() {
  const repo = get(selectedRepo)
  if (!repo || repo.missing) {
    pausedRemotes.set([])
    return
  }
  try {
    const list = await api.autoFetchPaused(repo.id)
    if (get(selectedRepoId) !== repo.id) return
    pausedRemotes.set(list ?? [])
  } catch {
    pausedRemotes.set([])
  }
}
```

In `refreshRepo` add `await loadPausedRemotes()` after `await loadRemoteInfo()`. In `selectRepo`, inside the `if (get(selectedRepoId) !== id)` block add `pausedRemotes.set([])`, and after `loadRemoteInfo()` add `loadPausedRemotes()`.

`frontend/src/lib/toolbar.ts`:
- `import { pausedTooltip } from './autoFetch'` — **check first** that `autoFetch.ts` does not import `toolbar.ts` (it doesn't today); if importing `autoFetch.ts` pulls `stores.ts` into toolbar tests and that breaks them, move `pausedTooltip` into `toolbar.ts` instead and import it from there in `autoFetch.test.ts`.
- `ToolbarInput` gains `paused: string[]`.
- `ToolbarItem` gains `dot: boolean`; the `item` factory's defaults gain `dot: false`.
- The fetch row becomes:

```ts
    item('fetch', 'Fetch', 'refresh', 'sync', first([!!i.busy, i.busy]), pausedTooltip(i.paused) || 'Fetch from all remotes', { dot: i.paused.length > 0 }),
```

`frontend/src/components/Toolbar.svelte`:
- import `pausedRemotes` from `../lib/stores`; pass `paused: $pausedRemotes` to `toolbarItems`.
- In the icon span, after the badge: `{#if item.dot}<span class="dot" aria-hidden="true"></span>{/if}`
- Style: `.dot { position: absolute; top: -2px; right: -3px; width: 7px; height: 7px; border-radius: 50%; background: var(--warning); }`

`frontend/src/theme.css`: after `--danger` in `:root` add `--warning: #d4920a;`, and in `:root[data-theme='dark']` add `--warning: #e0a526;`.

- [ ] **Step 4: Run** — `cd frontend && npx vitest run` → all PASS; `npx svelte-check` (or `npm run check` if defined) → no new errors.

- [ ] **Step 5: Spec**
  - `docs/spec/05-remote-and-stash.md` toolbar table, Fetch row → `| Fetch | Fetch, below; an amber dot while background fetches skip a remote that needs credentials, with the tooltip "Background fetch paused for <remotes>: authentication failed. Fetch to retry." | busy |`
  - "Background fetch" paragraph, after "A repository whose remotes are all stopped this way is skipped." add: "Meanwhile the toolbar's Fetch shows an amber dot and names the stopped remotes in its tooltip."
  - In the design spec, section 3: replace "per-repository store (`pausedRemotes`, `Record<repoID, string[]>`)" with "store for the selected repository (`pausedRemotes: string[]`, like `remoteInfo`)", and "defined for light, dark and both high-contrast themes next to `--danger`" with "defined for light and dark next to `--danger`; high contrast inherits it, as it does `--danger`".

- [ ] **Step 6: Commit**

```bash
/usr/bin/git add frontend/src docs/spec/05-remote-and-stash.md docs/superpowers/specs/2026-10-02-notifications-minors-design.md
/usr/bin/git commit -m "feat(toolbar): Fetch shows an amber dot while a remote's background fetch is paused"
```

---

### Task 4: Permission request without holding the mutex

**Files:**
- Modify: `internal/app/notify.go` (`Notify`)
- Test: `internal/app/notify_test.go`

**Interfaces:**
- Produces: `fakeNotifier.block chan struct{}` and `fakeNotifier.asking chan struct{}` (test-only), used again in Task 6.

- [ ] **Step 1: Failing test** — in `notify_test.go`, give `fakeNotifier` two fields `asking, block chan struct{}` and make `RequestAuthorization`:

```go
func (f *fakeNotifier) RequestAuthorization() (bool, error) {
	f.requests++
	if f.asking != nil {
		close(f.asking)
		<-f.block
	}
	f.authorized = f.grant
	return f.grant, nil
}
```

Add the test:

```go
func TestPermissionDialogDoesNotBlockOtherNotifications(t *testing.T) {
	a, _ := newTestApp(t)
	f := &fakeNotifier{grant: true, asking: make(chan struct{}), block: make(chan struct{})}
	a.startNotifications(f)
	first := make(chan error, 1)
	go func() { first <- a.Notify(note) }()
	<-f.asking

	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := a.Notify(note); !errors.Is(err, ErrNotificationsDenied) {
			t.Errorf("second Notify during the dialog: err = %v, want ErrNotificationsDenied", err)
		}
		if got := a.NotificationStatus(); got != "not allowed" {
			t.Errorf("status during the dialog = %q", got)
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Notify or NotificationStatus waited for the permission dialog")
	}

	close(f.block)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if len(f.sent) != 1 || f.requests != 1 {
		t.Fatalf("sent=%d requests=%d", len(f.sent), f.requests)
	}
}
```

Add `"time"` to the imports.

- [ ] **Step 2: Run** — `go test ./internal/app/ -run TestPermissionDialog -race -v` → FAIL (timeout message).

- [ ] **Step 3: Implement** — replace `Notify` in `internal/app/notify.go`:

```go
// Notify sends an OS notification, asking for permission the first time it
// is missing. The error says why nothing was shown; the frontend then
// falls back to an in-app toast. The permission dialog waits for the user
// without the lock held: a notification due meanwhile sees permission
// missing and already asked for, so it becomes a toast.
func (a *App) Notify(n Notification) error {
	a.notes.mu.Lock()
	nt, initErr := a.notes.n, a.notes.initErr
	if nt == nil {
		a.notes.mu.Unlock()
		return errNotificationsNotStarted
	}
	if initErr != nil {
		a.notes.mu.Unlock()
		return initErr
	}
	ok, err := nt.Authorized()
	ask := err == nil && !ok && !a.notes.asked
	if ask {
		a.notes.asked = true
	}
	a.notes.mu.Unlock()
	if err != nil {
		return err
	}
	if ask {
		if ok, err = nt.RequestAuthorization(); err != nil {
			return err
		}
	}
	if !ok {
		return ErrNotificationsDenied
	}
	a.notes.mu.Lock()
	defer a.notes.mu.Unlock()
	return nt.Send(runtime.NotificationOptions{
		ID:    n.ID,
		Title: n.Title,
		Body:  n.Body,
		Data:  map[string]interface{}{"repoID": n.RepoID, "target": n.Target},
	})
}
```

- [ ] **Step 4: Run** — `go test ./internal/app/ -run 'Notif|Permission' -race -v` → PASS.

- [ ] **Step 5: Commit** (no behaviour visible in docs/spec: permission is still asked once per run)

```bash
/usr/bin/git add internal/app/notify.go internal/app/notify_test.go
/usr/bin/git commit -m "fix(notify): don't hold the lock while macOS asks for permission"
```

---

### Task 5: Decision-card notification from the tool result

**Files:**
- Modify: `frontend/src/lib/notifyChat.ts`
- Test: `frontend/src/lib/notifyChat.test.ts`
- Modify: `docs/spec/11-notifications.md` (table row)

**Interfaces:**
- Consumes: `choiceState(tool: { summary?: string }): ChoiceState | null` from `./chat` ('pending' = card shown); `ChatToolResultEvent` from `./types`.

- [ ] **Step 1: Failing tests** — in `notifyChat.test.ts` replace the test "a decision card asks for the user and silences the finished turn" and "other tools say nothing" with:

```ts
  const shown = 'Shown to the user as a card with 2 options; the user will choose.'

  it('a shown decision card asks for the user and silences the finished turn', () => {
    let { watch } = watchChat(emptyWatch(), 'chat:start', start, 0)
    const card = watchChat(watch, 'chat:tool_result', { repoID: 'r1', runID: 'run1', name: 'propose_options', summary: shown }, 5)
    expect(card.event).toEqual({ category: 'ai', repoID: 'r1', target: 'chat', body: 'The AI has a decision for you' })
    const done = watchChat(card.watch, 'chat:done', { repoID: 'r1', runID: 'run1' }, 60_000)
    expect(done.event).toBeNull()
  })

  it('a rejected card says nothing and the finished turn still notifies', () => {
    let { watch } = watchChat(emptyWatch(), 'chat:start', start, 0)
    const card = watchChat(watch, 'chat:tool_result', { repoID: 'r1', runID: 'run1', name: 'propose_options', summary: 'options must be a list of {label, text}' }, 5)
    expect(card.event).toBeNull()
    const done = watchChat(card.watch, 'chat:done', { repoID: 'r1', runID: 'run1' }, 60_000)
    expect(done.event?.category).toBe('done')
  })

  it('tool calls and other tools say nothing', () => {
    const { watch } = watchChat(emptyWatch(), 'chat:start', start, 0)
    expect(watchChat(watch, 'chat:tool', { repoID: 'r1', runID: 'run1', name: 'propose_options', args: {} }, 5).event).toBeNull()
    expect(watchChat(watch, 'chat:tool_result', { repoID: 'r1', runID: 'run1', name: 'git_log', summary: shown }, 5).event).toBeNull()
  })

  it('watches tool results, not tool calls', () => {
    expect(CHAT_WATCH_EVENTS).toContain('chat:tool_result')
    expect(CHAT_WATCH_EVENTS).not.toContain('chat:tool')
  })
```

Import `CHAT_WATCH_EVENTS` too.

- [ ] **Step 2: Run** — `cd frontend && npx vitest run src/lib/notifyChat.test.ts` → FAIL.

- [ ] **Step 3: Implement** — `notifyChat.ts`:
  - imports: `import { choiceState } from './chat'` and replace `ChatToolEvent` with `ChatToolResultEvent` in the type import. **Check** `chat.ts` doesn't import `notifyChat.ts` (it doesn't today).
  - `CHAT_WATCH_EVENTS = ['chat:start', 'chat:confirm', 'chat:tool_result', 'chat:done', 'chat:error'] as const`
  - Update the doc comment: "A run that showed a decision card does not also notify that it finished."
  - Replace the `'chat:tool'` case with:

```ts
    case 'chat:tool_result': {
      // Only a card the tool accepted is on screen; a rejected one is just
      // a failed call the model will retry.
      const t = payload as ChatToolResultEvent
      if (t.name !== 'propose_options' || choiceState(t) !== 'pending') return { watch: w, event: null }
      const run = w.runs[t.runID]
      const watch = run ? { runs: { ...w.runs, [t.runID]: { ...run, decided: true } } } : w
      return { watch, event: { category: 'ai', repoID: t.repoID, target: 'chat', body: 'The AI has a decision for you' } }
    }
```

- [ ] **Step 4: Run** — `cd frontend && npx vitest run` → PASS.

- [ ] **Step 5: Spec** — `docs/spec/11-notifications.md` table row `| | A new decision card | always |` → `| | A decision card shown to you | always; not when the card was refused and the AI retries |`. Also the "Chat answer finished" row: "raised no decision card" → "showed no decision card".

- [ ] **Step 6: Commit**

```bash
/usr/bin/git add frontend/src/lib/notifyChat.ts frontend/src/lib/notifyChat.test.ts docs/spec/11-notifications.md
/usr/bin/git commit -m "fix(notify): a refused decision card doesn't notify"
```

---

### Task 6: Linux notifications open only from their Open button

**Files:**
- Modify: `internal/app/notify.go`
- Test: `internal/app/notify_test.go`
- Modify: `docs/spec/11-notifications.md`

**Interfaces:**
- Consumes: Task 4's `Notify` shape.
- Produces: `notifier` gains `RegisterCategory(runtime.NotificationCategory) error` and `SendWithActions(runtime.NotificationOptions) error`; `notifyState.goos string`; const `openCategory = "open"`, `openAction = "open"`.

- [ ] **Step 1: Failing tests** — `fakeNotifier` gains `categories []runtime.NotificationCategory` and `sentWithActions []runtime.NotificationOptions`, plus:

```go
func (f *fakeNotifier) RegisterCategory(c runtime.NotificationCategory) error {
	f.categories = append(f.categories, c)
	return nil
}
func (f *fakeNotifier) SendWithActions(o runtime.NotificationOptions) error {
	f.sentWithActions = append(f.sentWithActions, o)
	return nil
}
```

Tests:

```go
func TestLinuxNotificationsOpenOnlyFromTheOpenButton(t *testing.T) {
	a, _ := newTestApp(t)
	var mu sync.Mutex
	var got []NotifyOpenEvent
	WithAI(a, AIDeps{Emit: func(name string, data any) {
		if name == EventNotifyOpen {
			mu.Lock()
			got = append(got, data.(NotifyOpenEvent))
			mu.Unlock()
		}
	}})
	a.notes.goos = "linux"
	f := &fakeNotifier{authorized: true}
	a.startNotifications(f)
	if len(f.categories) != 1 || f.categories[0].ID != "open" || len(f.categories[0].Actions) != 1 ||
		f.categories[0].Actions[0].ID != "open" || f.categories[0].Actions[0].Title != "Open" {
		t.Fatalf("categories = %+v", f.categories)
	}
	if err := a.Notify(note); err != nil {
		t.Fatal(err)
	}
	if len(f.sent) != 0 || len(f.sentWithActions) != 1 || f.sentWithActions[0].CategoryID != "open" {
		t.Fatalf("sent=%d withActions=%+v", len(f.sent), f.sentWithActions)
	}
	info := map[string]interface{}{"repoID": "r1", "target": "repo"}
	f.respond(runtime.NotificationResult{Response: runtime.NotificationResponse{ActionIdentifier: "DEFAULT_ACTION", UserInfo: info}})
	f.respond(runtime.NotificationResult{Response: runtime.NotificationResponse{ActionIdentifier: "open", UserInfo: info}})
	mu.Lock()
	defer mu.Unlock()
	if f.shown != 1 || len(got) != 1 || got[0] != (NotifyOpenEvent{RepoID: "r1", Target: "repo"}) {
		t.Fatalf("shown=%d events=%+v", f.shown, got)
	}
}

func TestMacNotificationsKeepPlainSend(t *testing.T) {
	a, _ := newTestApp(t)
	a.notes.goos = "darwin"
	f := &fakeNotifier{authorized: true}
	a.startNotifications(f)
	if err := a.Notify(note); err != nil {
		t.Fatal(err)
	}
	if len(f.categories) != 0 || len(f.sentWithActions) != 0 || len(f.sent) != 1 {
		t.Fatalf("categories=%d withActions=%d sent=%d", len(f.categories), len(f.sentWithActions), len(f.sent))
	}
}
```

Also set `a.notes.goos = "darwin"` at the top of `TestNotificationClickOpensTheRepo` (it asserts the macOS rule: `DEFAULT_ACTION` opens) and of `TestNotifyAsksOnceThenSends` (it inspects `f.sent`), so they hold when the suite runs on Linux.

- [ ] **Step 2: Run** — `go test ./internal/app/ -run 'Linux|Mac' -v` → FAIL to compile (`goos`, methods missing).

- [ ] **Step 3: Implement** in `internal/app/notify.go`:
  - import `goruntime "runtime"`.
  - consts:

```go
// On Linux the system reports a click on a notification and a close by the
// user the same way (Wails' DEFAULT_ACTION), so a notification there carries
// an Open button and only that button opens it.
const (
	openCategory = "open"
	openAction   = "open"
)
```

  - `notifier` interface: add `RegisterCategory(runtime.NotificationCategory) error` and `SendWithActions(runtime.NotificationOptions) error`; `wailsNotifier`:

```go
func (w wailsNotifier) RegisterCategory(c runtime.NotificationCategory) error {
	return runtime.RegisterNotificationCategory(w.ctx, c)
}
func (w wailsNotifier) SendWithActions(o runtime.NotificationOptions) error {
	return runtime.SendNotificationWithActions(w.ctx, o)
}
```

  - `notifyState` gains `goos string // runtime.GOOS unless a test sets it`.
  - `startNotifications`, after a successful `Init`:

```go
	if a.notes.goos == "" {
		a.notes.goos = goruntime.GOOS
	}
	linux := a.notes.goos == "linux"
	if linux {
		// Without the category the notification is plain and opens nothing,
		// which still beats opening on close.
		_ = n.RegisterCategory(runtime.NotificationCategory{ID: openCategory, Actions: []runtime.NotificationAction{{ID: openAction, Title: "Open"}}})
	}
```

    and the response callback's guard becomes:

```go
		if r.Error != nil {
			return
		}
		id := r.Response.ActionIdentifier
		if linux && id != openAction || !linux && strings.Contains(strings.ToLower(id), "dismiss") {
			return
		}
```

    (remove the old Linux comment there.)
  - In `Notify`, capture `linux := a.notes.goos == "linux"` in the first locked section, and the send becomes:

```go
	opts := runtime.NotificationOptions{
		ID:    n.ID,
		Title: n.Title,
		Body:  n.Body,
		Data:  map[string]interface{}{"repoID": n.RepoID, "target": n.Target},
	}
	a.notes.mu.Lock()
	defer a.notes.mu.Unlock()
	if linux {
		opts.CategoryID = openCategory
		return nt.SendWithActions(opts)
	}
	return nt.Send(opts)
```

- [ ] **Step 4: Run** — `go test ./internal/app/ -race -v -run 'Notif|Permission|Linux|Mac'` → PASS; `GOOS=linux go vet ./internal/app/` → clean.

- [ ] **Step 5: Spec** — `docs/spec/11-notifications.md`: replace the paragraph "On Linux, closing a notification also brings the window forward: the system reports it the same way as a click." with "On Linux a notification has an **Open** button, which is what opens it; clicking its text or closing it does nothing, because the system reports both the same way."

- [ ] **Step 6: Commit**

```bash
/usr/bin/git add internal/app/notify.go internal/app/notify_test.go docs/spec/11-notifications.md
/usr/bin/git commit -m "fix(notify): on Linux only the Open button opens a notification"
```

---

### Task 7: Full check, rebuild, reopen

- [ ] `go test ./... -race` → PASS.
- [ ] `cd frontend && npx vitest run && npm run build` → PASS.
- [ ] `/usr/bin/git status` clean apart from nothing; `frontend/wailsjs/runtime` unchanged.
- [ ] Rebuild and reopen: quit any running CommitTree dev instance, then `make dev` in the background (node 22) and check the window opens; the toolbar Fetch shows no dot on an ordinary repository.
- [ ] Update memory `git-ui-notifications.md` after merge.
