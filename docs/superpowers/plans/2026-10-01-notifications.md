# Notifications (phase 1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Tell the user about finished long operations, the AI needing them and problems — an OS notification when the window is not focused, an in-app toast when it is focused but the event is out of sight.

**Architecture:** The frontend decides (`lib/notifyRules.ts` pure rules and texts, `lib/notifyChat.ts` pure chat-event watcher, `lib/notify.ts` impure glue: settings, focus, toasts, `track`/`opError`), Go delivers (`internal/app/notify.go` wraps Wails v2.16's notification runtime behind a small interface and reports clicks as `notify:open`).

**Tech Stack:** Go 1.x + Wails v2.16, Svelte + TypeScript, vitest.

**Spec:** `docs/superpowers/specs/2026-10-01-notifications-design.md`

## Global Constraints

- Successes notify only after **≥ 10 000 ms** (`DONE_THRESHOLD_MS`); failures, conflicts and "AI needs you" always notify. Not a setting.
- Notification id is `<repoID>:<category>`; categories are exactly `done`, `ai`, `problem`.
- Settings stored client-side with `persisted(...)` in `lib/stores.ts`, keys `notifyEnabled`, `notifyDone`, `notifyAi`, `notifyProblem`, all default `true`.
- Permission is requested lazily (first `Notify` call that finds it not granted), at most once per process, never at startup.
- Failure bodies use the first line of the error, truncated to 120 characters.
- Errors keep their existing error toast; never a second toast for the same failure.
- Behaviour changes update `docs/spec/` in the same commit.
- In this worktree run git as `/usr/bin/git` (the rtk hook clashes with worktree protection). Commit messages carry **no** `Co-Authored-By` line.
- Frontend commands run under node 22: prefix with `source ~/.nvm/nvm.sh && nvm use 22 >/dev/null &&`.
- After Go binding changes regenerate bindings: `source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && ~/go/bin/wails generate module` (commit the changed `frontend/wailsjs/go/**` files; `frontend/wailsjs/runtime` must stay untouched — `git checkout -- frontend/wailsjs/runtime` if it changed).

## Review Focus

- **System notifications unavailable or denied** (`make dev` on macOS has no bundle id) → the event still reaches the user as an in-app toast, without errors piling up. Test: Task 2, "falls back to a toast when the OS notification fails".
- **A failure in a repository that is not the selected one, window focused** → exactly one error toast, prefixed with the repository name and with a View button; no extra info toast. Test: Task 2, "opError prefixes other repositories" + "a toasted failure adds no second toast".
- **A clicked notification for a repository that was removed meanwhile** → nothing is selected, nothing breaks. Test: Task 2, "openTarget ignores unknown repositories".
- **Very long or multi-line git error** → body is the first line, ≤ 120 characters. Test: Task 1, "failureBody keeps the first line, 120 chars".
- **Operations finishing in two repositories at once** → two notifications (ids differ by repo), not one replacing the other. Test: Task 2, "system id is repo plus category".

---

### Task 1: Pure rules and texts (`notifyRules.ts`)

**Files:**
- Create: `frontend/src/lib/notifyRules.ts`
- Test: `frontend/src/lib/notifyRules.test.ts`

**Interfaces:**
- Produces:
  - `type NotifyCategory = 'done' | 'ai' | 'problem'`
  - `type NotifyTarget = 'repo' | 'chat'`
  - `type Delivery = 'none' | 'toast' | 'system'`
  - `interface NotifyEvent { category: NotifyCategory; repoID: string; body: string; target: NotifyTarget; durationMs?: number; toasted?: boolean }`
  - `interface NotifyContext { enabled: boolean; done: boolean; ai: boolean; problem: boolean; focused: boolean; activeRepoID: string; chatOpen: boolean }`
  - `const DONE_THRESHOLD_MS = 10_000`
  - `type OpKind = 'fetch' | 'pull' | 'push' | 'merge' | 'rebase' | 'cherry-pick' | 'submodules' | 'flow' | 'stash'`
  - `decide(e: NotifyEvent, c: NotifyContext): Delivery`
  - `firstLine(text: string, max?: number): string` (default max 120)
  - `formatSeconds(ms: number): string` → `"14 s"`
  - `doneBody(op: OpKind, ms: number): string`
  - `failureBody(op: OpKind, message: string): string`
  - `conflictsBody(op: OpKind, files: number): string`
  - `notificationId(e: NotifyEvent): string`
  - `statusText(status: string): string`

- [ ] **Step 1: Write the failing test**

```ts
// frontend/src/lib/notifyRules.test.ts
import { describe, expect, it } from 'vitest'
import { conflictsBody, decide, doneBody, failureBody, firstLine, formatSeconds, notificationId, statusText, type NotifyContext, type NotifyEvent } from './notifyRules'

const ctx = (over: Partial<NotifyContext> = {}): NotifyContext => ({
  enabled: true, done: true, ai: true, problem: true, focused: false, activeRepoID: 'a', chatOpen: true, ...over,
})
const ev = (over: Partial<NotifyEvent> = {}): NotifyEvent => ({ category: 'problem', repoID: 'a', body: 'x', target: 'repo', ...over })

describe('decide', () => {
  it('is silent when the master switch or the category is off', () => {
    expect(decide(ev(), ctx({ enabled: false }))).toBe('none')
    expect(decide(ev({ category: 'ai' }), ctx({ ai: false }))).toBe('none')
    expect(decide(ev({ category: 'done', durationMs: 20_000 }), ctx({ done: false }))).toBe('none')
    expect(decide(ev(), ctx({ problem: false }))).toBe('none')
  })

  it('drops successes under 10 s but never problems or ai events', () => {
    expect(decide(ev({ category: 'done', durationMs: 9_999 }), ctx())).toBe('none')
    expect(decide(ev({ category: 'done' }), ctx())).toBe('none')
    expect(decide(ev({ category: 'done', durationMs: 10_000 }), ctx())).toBe('system')
    expect(decide(ev({ category: 'problem', durationMs: 1 }), ctx())).toBe('system')
    expect(decide(ev({ category: 'ai', durationMs: 1 }), ctx())).toBe('system')
  })

  it('sends a system notification whenever the window is not focused', () => {
    expect(decide(ev({ repoID: 'a' }), ctx({ focused: false, activeRepoID: 'a' }))).toBe('system')
  })

  it('is silent when focused and in sight', () => {
    expect(decide(ev({ repoID: 'a' }), ctx({ focused: true }))).toBe('none')
    expect(decide(ev({ category: 'ai', target: 'chat' }), ctx({ focused: true, chatOpen: true }))).toBe('none')
  })

  it('toasts when focused but out of sight', () => {
    expect(decide(ev({ repoID: 'b' }), ctx({ focused: true }))).toBe('toast')
    expect(decide(ev({ category: 'ai', target: 'chat' }), ctx({ focused: true, chatOpen: false }))).toBe('toast')
  })
})

describe('texts', () => {
  it('failureBody keeps the first line, 120 chars', () => {
    const long = 'a'.repeat(200)
    expect(failureBody('push', `fatal: Authentication failed\nhint: more`)).toBe('Push failed: fatal: Authentication failed')
    expect(failureBody('pull', long)).toBe(`Pull failed: ${'a'.repeat(120)}`)
    expect(firstLine('\n\n  second line  \nthird')).toBe('second line')
  })

  it('formats the other bodies', () => {
    expect(formatSeconds(14_400)).toBe('14 s')
    expect(doneBody('push', 14_400)).toBe('Push finished · 14 s')
    expect(doneBody('submodules', 10_000)).toBe('Submodule update finished · 10 s')
    expect(conflictsBody('merge', 1)).toBe('Conflicts in 1 file after the merge')
    expect(conflictsBody('stash', 3)).toBe('Conflicts in 3 files after the stash')
  })

  it('system id is repo plus category', () => {
    expect(notificationId(ev({ repoID: 'r1', category: 'done' }))).toBe('r1:done')
    expect(notificationId(ev({ repoID: 'r2', category: 'done' }))).toBe('r2:done')
  })

  it('describes the permission status', () => {
    expect(statusText('allowed')).toBe('System notifications are allowed.')
    expect(statusText('not allowed')).toMatch(/not allowed yet/)
    expect(statusText('unavailable: no bundle')).toBe('System notifications are unavailable (no bundle); in-app toasts are used instead.')
    expect(statusText('')).toBe('')
  })
})
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run src/lib/notifyRules.test.ts`
Expected: FAIL — cannot resolve `./notifyRules`.

- [ ] **Step 3: Implement**

```ts
// frontend/src/lib/notifyRules.ts

/** The rules behind notifications (docs/spec/11-notifications.md): pure, so
 *  every combination of settings, focus and what is on screen is testable. */

export type NotifyCategory = 'done' | 'ai' | 'problem'
export type NotifyTarget = 'repo' | 'chat'
export type Delivery = 'none' | 'toast' | 'system'

export interface NotifyEvent {
  category: NotifyCategory
  repoID: string
  /** One short sentence; the title is the repository name. */
  body: string
  /** What View or a click opens: the repository, or its chat. */
  target: NotifyTarget
  /** How long the operation or the turn took; `done` events need it. */
  durationMs?: number
  /** The caller already showed it as an error toast (a failed operation),
   *  so the toast path must not add a second one. */
  toasted?: boolean
}

export interface NotifyContext {
  enabled: boolean
  done: boolean
  ai: boolean
  problem: boolean
  focused: boolean
  activeRepoID: string
  chatOpen: boolean
}

export const DONE_THRESHOLD_MS = 10_000

export type OpKind = 'fetch' | 'pull' | 'push' | 'merge' | 'rebase' | 'cherry-pick' | 'submodules' | 'flow' | 'stash'

const OP_LABEL: Record<OpKind, string> = {
  fetch: 'Fetch', pull: 'Pull', push: 'Push', merge: 'Merge', rebase: 'Rebase',
  'cherry-pick': 'Cherry-pick', submodules: 'Submodule update', flow: 'git-flow', stash: 'Stash',
}

export function decide(e: NotifyEvent, c: NotifyContext): Delivery {
  if (!c.enabled || !c[e.category]) return 'none'
  if (e.category === 'done' && (e.durationMs ?? 0) < DONE_THRESHOLD_MS) return 'none'
  if (!c.focused) return 'system'
  const inSight = e.repoID === c.activeRepoID && (e.target !== 'chat' || c.chatOpen)
  return inSight ? 'none' : 'toast'
}

export function firstLine(text: string, max = 120): string {
  const line = text.split('\n').map((l) => l.trim()).find((l) => l !== '') ?? ''
  return line.slice(0, max)
}

export const formatSeconds = (ms: number) => `${Math.round(ms / 1000)} s`

export const doneBody = (op: OpKind, ms: number) => `${OP_LABEL[op]} finished · ${formatSeconds(ms)}`

export const failureBody = (op: OpKind, message: string) => `${OP_LABEL[op]} failed: ${firstLine(message)}`

export const conflictsBody = (op: OpKind, files: number) =>
  `Conflicts in ${files} file${files === 1 ? '' : 's'} after the ${OP_LABEL[op].toLowerCase()}`

export const notificationId = (e: NotifyEvent) => `${e.repoID}:${e.category}`

/** Settings' line for Go's NotificationStatus. */
export function statusText(status: string): string {
  if (status === 'allowed') return 'System notifications are allowed.'
  if (status === 'not allowed')
    return 'System notifications are not allowed yet — CommitTree asks the first time it needs one, or allow them in the system settings.'
  if (status.startsWith('unavailable: '))
    return `System notifications are unavailable (${status.slice('unavailable: '.length)}); in-app toasts are used instead.`
  return ''
}
```

- [ ] **Step 4: Run it to verify it passes**

Run: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run src/lib/notifyRules.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
/usr/bin/git add frontend/src/lib/notifyRules.ts frontend/src/lib/notifyRules.test.ts
/usr/bin/git commit -m "feat(notify): pure rules and texts for notifications"
```

---

### Task 2: Go delivery (`internal/app/notify.go`) and the frontend glue (`notify.ts`)

**Files:**
- Create: `internal/app/notify.go`, `internal/app/notify_test.go`
- Modify: `internal/app/app.go` (`App` struct: add `notes notifyState`; `Startup`: call `a.startNotifications(wailsNotifier{ctx})`)
- Modify: `internal/app/terminal.go:56` (`Shutdown` also calls `a.stopNotifications()`)
- Modify: `frontend/wailsjs/go/app/App.{js,d.ts}`, `frontend/wailsjs/go/models.ts` (regenerated)
- Modify: `frontend/src/lib/api.ts` (add `notify`, `notificationStatus`)
- Modify: `frontend/src/lib/stores.ts` (four persisted settings)
- Create: `frontend/src/lib/notify.ts`, `frontend/src/lib/notify.test.ts`
- Modify: `docs/superpowers/specs/2026-10-01-notifications-design.md` (status values: `allowed`, `not allowed`, `unavailable: <reason>` — Wails' check cannot tell "undecided" from "denied")

**Interfaces:**
- Consumes (Task 1): `NotifyEvent`, `NotifyTarget`, `Delivery`, `OpKind`, `decide`, `notificationId`, `doneBody`, `failureBody`, `conflictsBody`.
- Produces:
  - Go bindings `Notify(n Notification) error`, `NotificationStatus() string`; event `notify:open` with `{repoID, target}`.
  - TS `api.notify(n: { id: string; title: string; body: string; repoID: string; target: NotifyTarget }): Promise<void>`, `api.notificationStatus(): Promise<string>`
  - stores `notifyEnabled`, `notifyDone`, `notifyAi`, `notifyProblem: Writable<boolean>`
  - `notify.ts`: `windowFocused: Writable<boolean>`, `notify(e: NotifyEvent): Promise<Delivery>`, `openTarget(repoID: string, target: NotifyTarget): void`, `opError(id: string, e: unknown): void`, `track<T>(id: string, op: OpKind, fn: () => Promise<T>, conflictsOf?: (r: T) => number): Promise<T>`, `notifyStashConflicts(id: string): void`

- [ ] **Step 1: Write the failing Go test**

```go
// internal/app/notify_test.go
package app

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type fakeNotifier struct {
	initErr    error
	authorized bool
	grant      bool
	requests   int
	sent       []runtime.NotificationOptions
	respond    func(runtime.NotificationResult)
	shown      int
	cleaned    int
}

func (f *fakeNotifier) Init() error                 { return f.initErr }
func (f *fakeNotifier) Authorized() (bool, error)   { return f.authorized, nil }
func (f *fakeNotifier) RequestAuthorization() (bool, error) {
	f.requests++
	f.authorized = f.grant
	return f.grant, nil
}
func (f *fakeNotifier) Send(o runtime.NotificationOptions) error { f.sent = append(f.sent, o); return nil }
func (f *fakeNotifier) OnResponse(cb func(runtime.NotificationResult)) { f.respond = cb }
func (f *fakeNotifier) ShowWindow()                                    { f.shown++ }
func (f *fakeNotifier) Cleanup()                                       { f.cleaned++ }

var note = Notification{ID: "r1:done", Title: "repo", Body: "Push finished · 14 s", RepoID: "r1", Target: "repo"}

func TestNotifyBeforeStartupFails(t *testing.T) {
	a, _ := newTestApp(t)
	if err := a.Notify(note); err == nil {
		t.Fatal("want an error before startup")
	}
	if got := a.NotificationStatus(); !strings.HasPrefix(got, "unavailable: ") {
		t.Fatalf("status = %q", got)
	}
}

func TestNotifyAsksOnceThenSends(t *testing.T) {
	a, _ := newTestApp(t)
	f := &fakeNotifier{grant: true}
	a.startNotifications(f)
	if err := a.Notify(note); err != nil {
		t.Fatal(err)
	}
	if err := a.Notify(note); err != nil {
		t.Fatal(err)
	}
	if f.requests != 1 || len(f.sent) != 2 {
		t.Fatalf("requests=%d sent=%d", f.requests, len(f.sent))
	}
	s := f.sent[0]
	if s.ID != "r1:done" || s.Title != "repo" || s.Body != note.Body || s.Data["repoID"] != "r1" || s.Data["target"] != "repo" {
		t.Fatalf("sent %+v", s)
	}
	if got := a.NotificationStatus(); got != "allowed" {
		t.Fatalf("status = %q", got)
	}
}

func TestNotifyDeniedAsksOnlyOnce(t *testing.T) {
	a, _ := newTestApp(t)
	f := &fakeNotifier{grant: false}
	a.startNotifications(f)
	for i := 0; i < 2; i++ {
		if err := a.Notify(note); !errors.Is(err, ErrNotificationsDenied) {
			t.Fatalf("call %d: err = %v", i, err)
		}
	}
	if f.requests != 1 || len(f.sent) != 0 {
		t.Fatalf("requests=%d sent=%d", f.requests, len(f.sent))
	}
	if got := a.NotificationStatus(); got != "not allowed" {
		t.Fatalf("status = %q", got)
	}
}

func TestNotifyInitErrorIsTheReason(t *testing.T) {
	a, _ := newTestApp(t)
	f := &fakeNotifier{initErr: errors.New("notifications require a valid bundle identifier")}
	a.startNotifications(f)
	if err := a.Notify(note); err == nil || !strings.Contains(err.Error(), "bundle") {
		t.Fatalf("err = %v", err)
	}
	if got := a.NotificationStatus(); got != "unavailable: notifications require a valid bundle identifier" {
		t.Fatalf("status = %q", got)
	}
	a.stopNotifications()
	if f.cleaned != 0 {
		t.Fatal("cleanup after a failed init")
	}
}

func TestNotificationClickOpensTheRepo(t *testing.T) {
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
	f := &fakeNotifier{authorized: true}
	a.startNotifications(f)
	f.respond(runtime.NotificationResult{Response: runtime.NotificationResponse{
		ActionIdentifier: "com.apple.UNNotificationDismissActionIdentifier",
		UserInfo:         map[string]interface{}{"repoID": "r1", "target": "chat"},
	}})
	f.respond(runtime.NotificationResult{Response: runtime.NotificationResponse{
		ActionIdentifier: "DEFAULT_ACTION",
		UserInfo:         map[string]interface{}{"repoID": "r1", "target": "chat"},
	}})
	mu.Lock()
	defer mu.Unlock()
	if f.shown != 1 || len(got) != 1 || got[0] != (NotifyOpenEvent{RepoID: "r1", Target: "chat"}) {
		t.Fatalf("shown=%d events=%+v", f.shown, got)
	}
	a.stopNotifications()
	if f.cleaned != 1 {
		t.Fatalf("cleaned=%d", f.cleaned)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/app -run 'Notif' -count=1`
Expected: FAIL — `undefined: Notification`, `startNotifications`, etc.

- [ ] **Step 3: Implement `notify.go` and wire it**

```go
// internal/app/notify.go
package app

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// EventNotifyOpen asks the frontend to show what a clicked OS notification
// was about: select RepoID and, for Target "chat", open its chat.
const EventNotifyOpen = "notify:open"

var (
	ErrNotificationsDenied     = errors.New("notifications are not allowed for CommitTree in the system settings")
	errNotificationsNotStarted = errors.New("not started")
)

// Notification is one OS notification the frontend asks for; the frontend
// decides whether one is due (see lib/notifyRules.ts).
type Notification struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	RepoID string `json:"repoID"`
	Target string `json:"target"`
}

// NotifyOpenEvent is EventNotifyOpen's payload.
type NotifyOpenEvent struct {
	RepoID string `json:"repoID"`
	Target string `json:"target"`
}

// notifier is the part of the Wails runtime notifications use; tests pass
// a fake, since the real one needs a running window.
type notifier interface {
	Init() error
	Authorized() (bool, error)
	RequestAuthorization() (bool, error)
	Send(runtime.NotificationOptions) error
	OnResponse(func(runtime.NotificationResult))
	ShowWindow()
	Cleanup()
}

type wailsNotifier struct{ ctx context.Context }

func (w wailsNotifier) Init() error               { return runtime.InitializeNotifications(w.ctx) }
func (w wailsNotifier) Authorized() (bool, error) { return runtime.CheckNotificationAuthorization(w.ctx) }
func (w wailsNotifier) RequestAuthorization() (bool, error) {
	return runtime.RequestNotificationAuthorization(w.ctx)
}
func (w wailsNotifier) Send(o runtime.NotificationOptions) error { return runtime.SendNotification(w.ctx, o) }
func (w wailsNotifier) OnResponse(cb func(runtime.NotificationResult)) {
	runtime.OnNotificationResponse(w.ctx, cb)
}
func (w wailsNotifier) ShowWindow() {
	runtime.WindowUnminimise(w.ctx)
	runtime.WindowShow(w.ctx)
}
func (w wailsNotifier) Cleanup() { runtime.CleanupNotifications(w.ctx) }

// notifyState is nil-notifier until Startup; initErr is why notifications
// are unavailable (e.g. no bundle identifier under `wails dev` on macOS),
// and asked records that permission was already requested this run.
type notifyState struct {
	mu      sync.Mutex
	n       notifier
	initErr error
	asked   bool
}

func (a *App) startNotifications(n notifier) {
	a.notes.mu.Lock()
	defer a.notes.mu.Unlock()
	a.notes.n = n
	if err := n.Init(); err != nil {
		a.notes.initErr = err
		return
	}
	n.OnResponse(func(r runtime.NotificationResult) {
		if r.Error != nil || strings.Contains(strings.ToLower(r.Response.ActionIdentifier), "dismiss") {
			return
		}
		repoID, _ := r.Response.UserInfo["repoID"].(string)
		target, _ := r.Response.UserInfo["target"].(string)
		n.ShowWindow()
		a.emit(EventNotifyOpen, NotifyOpenEvent{RepoID: repoID, Target: target})
	})
}

func (a *App) stopNotifications() {
	a.notes.mu.Lock()
	defer a.notes.mu.Unlock()
	if a.notes.n != nil && a.notes.initErr == nil {
		a.notes.n.Cleanup()
	}
}

// Notify sends an OS notification, asking for permission the first time it
// is missing. The error says why nothing was shown; the frontend then
// falls back to an in-app toast.
func (a *App) Notify(n Notification) error {
	a.notes.mu.Lock()
	defer a.notes.mu.Unlock()
	if a.notes.n == nil {
		return errNotificationsNotStarted
	}
	if a.notes.initErr != nil {
		return a.notes.initErr
	}
	ok, err := a.notes.n.Authorized()
	if err != nil {
		return err
	}
	if !ok && !a.notes.asked {
		a.notes.asked = true
		if ok, err = a.notes.n.RequestAuthorization(); err != nil {
			return err
		}
	}
	if !ok {
		return ErrNotificationsDenied
	}
	return a.notes.n.Send(runtime.NotificationOptions{
		ID:    n.ID,
		Title: n.Title,
		Body:  n.Body,
		Data:  map[string]interface{}{"repoID": n.RepoID, "target": n.Target},
	})
}

// NotificationStatus is "allowed", "not allowed" or "unavailable: <reason>",
// for the line under Settings → Notifications.
func (a *App) NotificationStatus() string {
	a.notes.mu.Lock()
	defer a.notes.mu.Unlock()
	switch {
	case a.notes.n == nil:
		return "unavailable: " + errNotificationsNotStarted.Error()
	case a.notes.initErr != nil:
		return "unavailable: " + a.notes.initErr.Error()
	}
	ok, err := a.notes.n.Authorized()
	if err != nil {
		return "unavailable: " + err.Error()
	}
	if ok {
		return "allowed"
	}
	return "not allowed"
}
```

In `internal/app/app.go`, add to the `App` struct (after `submodules map[string]repos.Repo`):

```go
	// notes delivers OS notifications; see notify.go.
	notes notifyState
```

and in `Startup`, after `a.started.Store(true)`:

```go
	a.startNotifications(wailsNotifier{ctx})
```

In `internal/app/terminal.go`, replace `func (a *App) Shutdown(ctx context.Context) { a.term.CloseAll() }` with:

```go
func (a *App) Shutdown(ctx context.Context) {
	a.term.CloseAll()
	a.stopNotifications()
}
```

- [ ] **Step 4: Run Go tests**

Run: `go test ./internal/app -count=1`
Expected: PASS (all, including the new `Notif` tests).

- [ ] **Step 5: Regenerate bindings**

Run: `source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && ~/go/bin/wails generate module && /usr/bin/git checkout -- frontend/wailsjs/runtime 2>/dev/null; /usr/bin/git status --short frontend/wailsjs`
Expected: `App.js`, `App.d.ts` (and `models.ts`) modified with `Notify` and `NotificationStatus`.

- [ ] **Step 6: Write the failing frontend test**

```ts
// frontend/src/lib/notify.test.ts
import { get } from 'svelte/store'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('./api', () => ({ api: { notify: vi.fn(), listRepos: vi.fn().mockResolvedValue([]) } }))

// The real selectRepo loads refs, merge state, … through api calls this
// mock lacks; only the selection matters here.
vi.mock('./stores', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./stores')>()
  return { ...actual, selectRepo: vi.fn((id: string) => actual.selectedRepoId.set(id)) }
})

import { api } from './api'
import { notify, notifyStashConflicts, openTarget, opError, track, windowFocused } from './notify'
import { chatOpen, mergeState, notifyEnabled, repos, selectedRepoId } from './stores'
import type { MergeState, Repo } from './types'
import { toasts } from './ui'

const repo = (id: string, name: string) => ({ id, name, path: `/x/${id}`, missing: false, branch: 'main' }) as Repo

beforeEach(() => {
  vi.mocked(api.notify).mockReset().mockResolvedValue(undefined)
  toasts.set([])
  repos.set([repo('a', 'alpha'), repo('b', 'beta')])
  selectedRepoId.set('a')
  chatOpen.set(true)
  notifyEnabled.set(true)
  windowFocused.set(true)
})

describe('notify', () => {
  it('sends a system notification when unfocused', async () => {
    windowFocused.set(false)
    expect(await notify({ category: 'problem', repoID: 'b', body: 'Push failed: x', target: 'repo' })).toBe('system')
    expect(api.notify).toHaveBeenCalledWith({ id: 'b:problem', title: 'beta', body: 'Push failed: x', repoID: 'b', target: 'repo' })
    expect(get(toasts)).toEqual([])
  })

  it('falls back to a toast when the OS notification fails', async () => {
    windowFocused.set(false)
    vi.mocked(api.notify).mockRejectedValue('notifications require a valid bundle identifier')
    await notify({ category: 'ai', repoID: 'a', body: 'The AI has a decision for you', target: 'chat' })
    expect(get(toasts).map((t) => [t.message, t.kind, t.action?.label])).toEqual([['alpha: The AI has a decision for you', 'info', 'View']])
  })

  it('toasts out-of-sight events and stays silent for in-sight ones', async () => {
    await notify({ category: 'problem', repoID: 'a', body: 'Conflicts in 1 file after the merge', target: 'repo' })
    expect(get(toasts)).toEqual([])
    await notify({ category: 'problem', repoID: 'b', body: 'Conflicts in 1 file after the merge', target: 'repo' })
    expect(get(toasts).map((t) => t.message)).toEqual(['beta: Conflicts in 1 file after the merge'])
    expect(api.notify).not.toHaveBeenCalled()
  })

  it('a toasted failure adds no second toast', async () => {
    await notify({ category: 'problem', repoID: 'b', body: 'Push failed: x', target: 'repo', toasted: true })
    expect(get(toasts)).toEqual([])
  })

  it('falls back to the app name for an unknown repository', async () => {
    windowFocused.set(false)
    await notify({ category: 'problem', repoID: 'gone', body: 'x', target: 'repo' })
    expect(vi.mocked(api.notify).mock.calls[0][0].title).toBe('CommitTree')
  })
})

describe('openTarget', () => {
  it('selects the repository and opens the chat for chat targets', () => {
    chatOpen.set(false)
    openTarget('b', 'chat')
    expect(get(selectedRepoId)).toBe('b')
    expect(get(chatOpen)).toBe(true)
  })

  it('openTarget ignores unknown repositories', () => {
    openTarget('gone', 'repo')
    expect(get(selectedRepoId)).toBe('a')
  })
})

describe('opError', () => {
  it('keeps the plain error toast for the selected repository', () => {
    opError('a', new Error('boom'))
    expect(get(toasts).map((t) => [t.message, t.kind, t.action])).toEqual([['boom', 'error', undefined]])
  })

  it('opError prefixes other repositories', () => {
    opError('b', 'boom')
    expect(get(toasts).map((t) => [t.message, t.kind, t.action?.label])).toEqual([['beta: boom', 'error', 'View']])
  })
})

describe('track', () => {
  it('notifies done only past the threshold', async () => {
    windowFocused.set(false)
    const now = vi.spyOn(Date, 'now')
    now.mockReturnValueOnce(0).mockReturnValueOnce(4_000)
    await track('a', 'push', async () => undefined)
    expect(api.notify).not.toHaveBeenCalled()
    now.mockReturnValueOnce(0).mockReturnValueOnce(14_400)
    await track('a', 'push', async () => undefined)
    expect(vi.mocked(api.notify).mock.calls[0][0]).toMatchObject({ id: 'a:done', body: 'Push finished · 14 s' })
    now.mockRestore()
  })

  it('conflicts beat finished', async () => {
    windowFocused.set(false)
    const r = await track('a', 'merge', async () => ({ conflicts: ['x', 'y'] }), (r) => r.conflicts.length)
    expect(r.conflicts).toEqual(['x', 'y'])
    expect(api.notify).toHaveBeenCalledTimes(1)
    expect(vi.mocked(api.notify).mock.calls[0][0]).toMatchObject({ id: 'a:problem', body: 'Conflicts in 2 files after the merge' })
  })

  it('a failure notifies the problem and rethrows', async () => {
    windowFocused.set(false)
    await expect(track('a', 'pull', async () => { throw new Error('fatal: Authentication failed\nmore') })).rejects.toThrow('fatal')
    expect(api.notify).toHaveBeenCalledTimes(1)
    expect(vi.mocked(api.notify).mock.calls[0][0]).toMatchObject({ id: 'a:problem', body: 'Pull failed: fatal: Authentication failed' })
  })
})

describe('notifyStashConflicts', () => {
  const state = (over: Partial<MergeState>): MergeState => ({ kind: 'stash', merging: true, from: '', into: '', conflicts: ['f'], manual: [], staged: [], unstaged: [], ...over })

  it('notifies a stash conflict on the selected repository only', async () => {
    windowFocused.set(false)
    mergeState.set(state({}))
    notifyStashConflicts('b')
    notifyStashConflicts('a')
    await Promise.resolve()
    expect(api.notify).toHaveBeenCalledTimes(1)
    expect(vi.mocked(api.notify).mock.calls[0][0]).toMatchObject({ id: 'a:problem', body: 'Conflicts in 1 file after the stash' })
  })

  it('ignores a clean apply', async () => {
    windowFocused.set(false)
    mergeState.set(state({ merging: false, conflicts: [] }))
    notifyStashConflicts('a')
    await Promise.resolve()
    expect(api.notify).not.toHaveBeenCalled()
  })
})
```

- [ ] **Step 7: Run it to verify it fails**

Run: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run src/lib/notify.test.ts`
Expected: FAIL — cannot resolve `./notify`.

- [ ] **Step 8: Implement api, stores and `notify.ts`**

In `frontend/src/lib/api.ts` add `NotifyTarget` to the imports (`import type { NotifyTarget } from './notifyRules'`) and, after the `commandLog…` lines:

```ts
  notify: (n: { id: string; title: string; body: string; repoID: string; target: NotifyTarget }) => call<void>(Go.Notify(n as any)),
  notificationStatus: () => call<string>(Go.NotificationStatus()),
```

In `frontend/src/lib/stores.ts`, after `highContrast`:

```ts
const isBool = (v: unknown): v is boolean => typeof v === 'boolean'
/** Settings → General → Notifications (docs/spec/11-notifications.md). */
export const notifyEnabled = persisted('notifyEnabled', true, isBool)
export const notifyDone = persisted('notifyDone', true, isBool)
export const notifyAi = persisted('notifyAi', true, isBool)
export const notifyProblem = persisted('notifyProblem', true, isBool)
```

```ts
// frontend/src/lib/notify.ts
import { get, writable } from 'svelte/store'
import { api } from './api'
import { conflictsBody, decide, doneBody, failureBody, notificationId, type Delivery, type NotifyEvent, type NotifyTarget, type OpKind } from './notifyRules'
import { chatOpen, mergeState, notifyAi, notifyDone, notifyEnabled, notifyProblem, repos, selectRepo, selectedRepoId } from './stores'
import { errorMessage, toast } from './ui'

/** Whether the window has focus, kept live by startNotifications. */
export const windowFocused = writable(typeof document !== 'undefined' && document.hasFocus())

const repoName = (id: string) => get(repos).find((r) => r.id === id)?.name ?? 'CommitTree'

/** What View or a notification click opens; a repository removed since is ignored. */
export function openTarget(repoID: string, target: NotifyTarget) {
  if (!get(repos).some((r) => r.id === repoID)) return
  selectRepo(repoID)
  if (target === 'chat') chatOpen.set(true)
}

const viewAction = (repoID: string, target: NotifyTarget) => ({ label: 'View', run: () => openTarget(repoID, target) })

/** notify applies the rules and delivers: an OS notification, or a toast
 *  (also when the OS one cannot be shown). Resolves to what was decided. */
export async function notify(e: NotifyEvent): Promise<Delivery> {
  const d = decide(e, {
    enabled: get(notifyEnabled), done: get(notifyDone), ai: get(notifyAi), problem: get(notifyProblem),
    focused: get(windowFocused), activeRepoID: get(selectedRepoId), chatOpen: get(chatOpen),
  })
  if (d === 'system') {
    try {
      await api.notify({ id: notificationId(e), title: repoName(e.repoID), body: e.body, repoID: e.repoID, target: e.target })
      return d
    } catch {
      // Not allowed or not available: the toast below is seen on return.
    }
  }
  if (d !== 'none' && !e.toasted) toast(`${repoName(e.repoID)}: ${e.body}`, 'info', viewAction(e.repoID, e.target))
  return d
}

/** The error toast of a failed operation: as before for the selected
 *  repository; another one's carries its name and a View button. */
export function opError(id: string, e: unknown) {
  const message = errorMessage(e)
  if (id === get(selectedRepoId)) toast(message, 'error')
  else toast(`${repoName(id)}: ${message}`, 'error', viewAction(id, 'repo'))
}

/** track runs one user operation and notifies how it ended: conflicts,
 *  else finished (the 10 s threshold is decide's); a failure is notified
 *  and rethrown for the caller's opError. */
export async function track<T>(id: string, op: OpKind, fn: () => Promise<T>, conflictsOf?: (r: T) => number): Promise<T> {
  const start = Date.now()
  let result: T
  try {
    result = await fn()
  } catch (e) {
    void notify({ category: 'problem', repoID: id, target: 'repo', body: failureBody(op, errorMessage(e)), toasted: true })
    throw e
  }
  const files = conflictsOf ? conflictsOf(result) : 0
  if (files > 0) {
    void notify({ category: 'problem', repoID: id, target: 'repo', body: conflictsBody(op, files) })
  } else {
    const ms = Date.now() - start
    void notify({ category: 'done', repoID: id, target: 'repo', body: doneBody(op, ms), durationMs: ms })
  }
  return result
}

/** A stash apply/pop reports a conflict as success and leaves it in the
 *  merge state (refreshed for the selected repository only). */
export function notifyStashConflicts(id: string) {
  const s = get(mergeState)
  if (id !== get(selectedRepoId) || !s?.merging || s.kind !== 'stash' || s.conflicts.length === 0) return
  void notify({ category: 'problem', repoID: id, target: 'repo', body: conflictsBody('stash', s.conflicts.length) })
}
```

Note for the `track` test: `track` calls `Date.now()` exactly twice on success (start, end) — keep it that way or adjust the test's `mockReturnValueOnce` pairs.

In `docs/superpowers/specs/2026-10-01-notifications-design.md`, replace the `NotificationStatus` bullet's value list "`allowed`, `denied`, `undecided` or `unavailable: <reason>`" with "`allowed`, `not allowed` or `unavailable: <reason>` (Wails' check cannot tell undecided from denied)".

- [ ] **Step 9: Run the tests**

Run: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run src/lib/notify.test.ts src/lib/notifyRules.test.ts && npm run check`
Expected: PASS, svelte-check 0 errors.

- [ ] **Step 10: Commit**

```bash
/usr/bin/git add internal/app/notify.go internal/app/notify_test.go internal/app/app.go internal/app/terminal.go frontend/wailsjs/go frontend/src/lib/api.ts frontend/src/lib/stores.ts frontend/src/lib/notify.ts frontend/src/lib/notify.test.ts docs/superpowers/specs/2026-10-01-notifications-design.md
/usr/bin/git commit -m "feat(notify): OS notification delivery in Go and the frontend notify glue"
```

---

### Task 3: Operations notify (actions.ts, flowActions.ts) + spec 11

**Files:**
- Modify: `frontend/src/lib/actions.ts` (add `runOp`; `fetchRemote`, `push`, `pull`, `mergeBranch`, `rebaseOnto`, `cherryPick`, `initAllSubmodules`, `updateAllSubmodules`, `stashApply`, `stashPop`)
- Modify: `frontend/src/lib/flowActions.ts` (`busyDo` takes the repo id and tracks; `runFinish` reports conflicts)
- Test: `frontend/src/lib/actions.test.ts`, `frontend/src/lib/flowActions.test.ts`
- Create: `docs/spec/11-notifications.md`; Modify: `docs/spec/README.md`, `docs/spec/05-remote-and-stash.md`

**Interfaces:**
- Consumes (Task 2): `track`, `opError`, `notifyStashConflicts` from `./notify`.
- Produces: nothing new for later tasks.

- [ ] **Step 1: Write the failing tests**

Add to the `vi.mock('./api', …)` object in `frontend/src/lib/actions.test.ts`: `push: vi.fn().mockResolvedValue(undefined), pull: vi.fn(), notify: vi.fn().mockResolvedValue(undefined),` and add `pull, push, mergeBranch` to the `./actions` import, `repos, selectedRepoId` to the `./stores` import, `windowFocused` from `./notify`. Then append:

```ts
describe('operations notify', () => {
  beforeEach(() => {
    toasts.set([])
    vi.mocked(api.notify).mockClear()
    repos.set([{ id: 'r1', name: 'alpha', path: '/a', missing: false, branch: 'main' } as Repo, { id: 'r2', name: 'beta', path: '/b', missing: false, branch: 'main' } as Repo])
    selectedRepoId.set('r1')
    windowFocused.set(false)
  })

  it('a slow push notifies finished', async () => {
    const now = vi.spyOn(Date, 'now').mockReturnValueOnce(0).mockReturnValueOnce(12_000)
    await push('r1')
    now.mockRestore()
    expect(vi.mocked(api.notify).mock.calls[0][0]).toMatchObject({ id: 'r1:done', body: 'Push finished · 12 s' })
  })

  it('a failed push on another repository: one error toast with its name, plus the problem notification', async () => {
    vi.mocked(api.push).mockRejectedValueOnce(new Error('fatal: Authentication failed'))
    await push('r2')
    expect(get(toasts).map((t) => [t.message, t.kind])).toEqual([['beta: fatal: Authentication failed', 'error']])
    expect(vi.mocked(api.notify).mock.calls[0][0]).toMatchObject({ id: 'r2:problem', body: 'Push failed: fatal: Authentication failed' })
  })

  it('a pull with conflicts notifies the conflicts, not finished', async () => {
    vi.mocked(api.pull).mockResolvedValueOnce({ outcome: 2, conflicts: ['a.txt'] })
    await pull('r1')
    expect(api.notify).toHaveBeenCalledTimes(1)
    expect(vi.mocked(api.notify).mock.calls[0][0]).toMatchObject({ id: 'r1:problem', body: 'Conflicts in 1 file after the pull' })
  })

  it('a merge with conflicts notifies the conflicts', async () => {
    vi.mocked(api.mergeBranch).mockResolvedValueOnce({ outcome: 2, conflicts: ['a', 'b'] })
    await mergeBranch('r1', { name: 'feature' } as Branch, 'main')
    expect(vi.mocked(api.notify).mock.calls[0][0]).toMatchObject({ body: 'Conflicts in 2 files after the merge' })
  })
})
```

(If `mergeBranch`'s `confirmMerge` calls `api.branchCounts` for a local branch with an upstream, the `{ name: 'feature' }` branch has no `upstream`, so it goes straight to the mocked `confirmDialog`. If `warnMovedSubmodules` touches the api, it only does so for repos with `submoduleCount`, which these have not.)

In `frontend/src/lib/flowActions.test.ts` add `notify: vi.fn().mockResolvedValue(undefined)` to the api mock, import `windowFocused` from `./notify` and `api`, and append inside `describe('runFinish', …)`:

```ts
  it('a conflicted finish notifies the conflicts', async () => {
    windowFocused.set(false)
    vi.mocked(api.finishFlow).mockResolvedValue({ outcome: 'conflicted', target: 'develop', conflicts: ['a', 'b'], merged: [], notes: [] })
    await runFinish('r1', 'hotfix/h', [])
    expect(vi.mocked(api.notify).mock.calls.at(-1)?.[0]).toMatchObject({ id: 'r1:problem', body: 'Conflicts in 2 files after the git-flow' })
  })
```

- [ ] **Step 2: Run them to verify they fail**

Run: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run src/lib/actions.test.ts src/lib/flowActions.test.ts`
Expected: the new tests FAIL (no `api.notify` calls; plain error toast without the repository name).

- [ ] **Step 3: Implement in `actions.ts`**

Add the import: `import { notifyStashConflicts, opError, track } from './notify'` and `import type { OpKind } from './notifyRules'`.

After `run`, add:

```ts
// runOp is run for the operations that notify (docs/spec/11-notifications.md):
// it tracks how the operation ended, and a failure's toast names the
// repository when it is not the selected one.
async function runOp(id: string, op: OpKind, label: string, fn: () => Promise<unknown>): Promise<boolean> {
  busy.set(label)
  try {
    await track(id, op, fn)
    return true
  } catch (e) {
    opError(id, e)
    return false
  } finally {
    busy.set('')
    await refreshRepo()
  }
}
```

Replace:

```ts
export const fetchRemote = (id: string) => runOp(id, 'fetch', 'Fetching…', () => api.fetch(id))

export const push = (id: string) => runOp(id, 'push', 'Pushing…', () => api.push(id))
```

In `pull`: `const result = await track(id, 'pull', () => api.pull(id), (r) => r.conflicts?.length ?? 0)` and `toast(errorMessage(e), 'error')` → `opError(id, e)`.

In `mergeBranch`: `const result = await track(id, 'merge', () => api.mergeBranch(id, label), (r) => r.conflicts?.length ?? 0)` and its catch → `opError(id, e)`.

In `rebaseOnto` (the `api.rebaseOnto` call only, not `getRebasePreview`): `const result = await track(id, 'rebase', () => api.rebaseOnto(id, onto), (r) => r.conflicts?.length ?? 0)`; catch → `opError(id, e)`.

In `cherryPick`: `const result = await track(id, 'cherry-pick', () => api.cherryPick(id, hash), (r) => r.conflicts?.length ?? 0)`; catch → `opError(id, e)`.

In `initAllSubmodules`: `if (ok) await runOp(parentId, 'submodules', 'Initialising…', () => api.initAllSubmodules(parentId))`; in `updateAllSubmodules`: `if (ok) await runOp(parentId, 'submodules', 'Updating…', () => api.updateAllSubmodules(parentId))`.

In `stashApply`:

```ts
  const ok = stashApplyAction(result.checked) === 'pop'
    ? await run('Popping stash…', () => api.stashPop(id, index))
    : await run('Applying stash…', () => api.stashApply(id, index))
  if (ok) notifyStashConflicts(id)
```

In `stashPop`: `if (ok && (await run('Popping stash…', () => api.stashPop(id, index)))) notifyStashConflicts(id)`.

- [ ] **Step 4: Implement in `flowActions.ts`**

```ts
import { opError, track } from './notify'

async function busyDo<T>(repoId: string, label: string, fn: () => Promise<T>, conflictsOf?: (r: T) => number) {
  busy.set(label)
  try {
    await track(repoId, 'flow', fn, conflictsOf)
  } catch (e) {
    opError(repoId, e)
  } finally {
    busy.set('')
    await refreshRepo()
  }
}
```

Every `busyDo(label, async () => …)` call gains `repoId` as first argument (`initFlow`, `startBranch`, `runFinish`). In `runFinish` the callback returns `res` and passes `conflictsOf`:

```ts
export async function runFinish(repoId: string, branch: string, releases: string[]) {
  await busyDo(repoId, `Finishing ${branch}…`, async () => {
    const res = await api.finishFlow(repoId, branch, releases)
    if (res.outcome === 'conflicted') {
      pendingFinish.set({ repoId, branch, releases, target: res.target })
      toast(conflictMessage(branch, res))
    } else {
      pendingFinish.set(null)
      toast(finishedMessage(branch, res))
    }
    return res
  }, (r) => (r.outcome === 'conflicted' ? r.conflicts.length || 1 : 0))
}
```

Remove `errorMessage` from the `./ui` import only if no other use remains (`openFlowMenu` still uses it — keep it).

- [ ] **Step 5: Write `docs/spec/11-notifications.md`** (operations part; Task 5 adds the AI and Settings sections)

```markdown
# Notifications

CommitTree tells the user about the events they were waiting for or that
need them, and nothing else: an OS notification when the window is not
focused, an in-app toast when it is focused but the event is out of sight.

## Events

| Category | Event | Notifies when |
|---|---|---|
| Operations finished | Fetch, Pull, Push, Merge, Rebase, Cherry-pick, Initialise/Update all submodules, git-flow init/start/finish | succeeded and took 10 s or longer |
| Problems | One of those operations failed | always |
| | Conflicts left after Merge, Pull, Rebase, Cherry-pick, git-flow finish, Stash apply/pop | always |

One user operation is one event, however many git commands it runs. An
operation notifies once: conflicts or a failure replace "finished".
Staging, discarding, reads, terminal commands, creating branches or tags
never notify.

The title is the repository name; the body one sentence — "Push finished ·
14 s", "Pull failed: <first line of the error, at most 120 characters>",
"Conflicts in 3 files after the merge".

## Where it appears

1. Window not focused → an OS notification. Clicking it brings the window
   forward on that repository.
2. Window focused, event in the selected repository → nothing new; the
   screen already shows it.
3. Window focused, another repository → a toast "<repo>: <body>" with
   **View**, which selects that repository.

A failed operation keeps its error toast, never a second one; when the
repository is not the selected one, that toast starts with its name and
has **View**.

Each repository and category shows at most one OS notification: a newer
one replaces the older (macOS, Linux; Windows stacks them). When the OS
notification cannot be shown — not allowed, or not available (an unbundled
development build on macOS) — the toast is shown instead.

Permission is asked the first time an OS notification is due, never at
startup, and at most once per run.
```

In `docs/spec/README.md` add after the git-flow row:

```markdown
| [Notifications](11-notifications.md) | Which events notify, and when as an OS notification or a toast |
```

In `docs/spec/05-remote-and-stash.md`, at the end of the "## Fetch, pull and push" section's intro paragraph (before "### Fetch"), add:

```markdown
A fetch, pull or push that takes 10 s or longer, fails or leaves conflicts
notifies — see [Notifications](11-notifications.md).
```

- [ ] **Step 6: Run all frontend tests and the type check**

Run: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run && npm run check`
Expected: all PASS, 0 errors.

- [ ] **Step 7: Commit**

```bash
/usr/bin/git add frontend/src/lib/actions.ts frontend/src/lib/actions.test.ts frontend/src/lib/flowActions.ts frontend/src/lib/flowActions.test.ts docs/spec/11-notifications.md docs/spec/README.md docs/spec/05-remote-and-stash.md
/usr/bin/git commit -m "feat(notify): long, failed and conflicted operations notify"
```

---

### Task 4: Chat watcher (`notifyChat.ts`)

**Files:**
- Create: `frontend/src/lib/notifyChat.ts`, `frontend/src/lib/notifyChat.test.ts`

**Interfaces:**
- Consumes (Task 1): `NotifyEvent`, `firstLine`, `formatSeconds`.
- Produces:
  - `interface ChatWatch { runs: Record<string, { repoID: string; start: number; decided: boolean }> }`
  - `emptyWatch(): ChatWatch`
  - `CHAT_WATCH_EVENTS = ['chat:start', 'chat:confirm', 'chat:tool', 'chat:done', 'chat:error'] as const`
  - `watchChat(w: ChatWatch, name: string, payload: unknown, now: number): { watch: ChatWatch; event: NotifyEvent | null }`

Behaviour: `chat:start` records the run. `chat:confirm` → `ai` event "The AI is waiting for you to confirm: <title>". `chat:tool` with `name === 'propose_options'` → `ai` event "The AI has a decision for you" and marks the run `decided`. `chat:done` → drops the run; unless `decided`, a `done` event "Chat answer finished · N s" with `durationMs`. `chat:error` → drops the run and gives a `problem` event "Chat answer failed: <first line>". All chat events use `target: 'chat'`. Events for a run never started (e.g. app reloaded mid-run) still give `ai`/`problem` events; `chat:done` for an unknown run gives nothing. (A stopped answer arrives as `chat:done`; the user pressed Stop in the focused window, so `decide` keeps it silent.)

- [ ] **Step 1: Write the failing test**

```ts
// frontend/src/lib/notifyChat.test.ts
import { describe, expect, it } from 'vitest'
import { emptyWatch, watchChat } from './notifyChat'

const start = { repoID: 'r1', runID: 'run1', text: 'hi', provider: 'anthropic', model: 'm' }

describe('watchChat', () => {
  it('a finished turn reports its duration', () => {
    let { watch } = watchChat(emptyWatch(), 'chat:start', start, 1_000)
    const r = watchChat(watch, 'chat:done', { repoID: 'r1', runID: 'run1' }, 13_000)
    expect(r.event).toEqual({ category: 'done', repoID: 'r1', target: 'chat', body: 'Chat answer finished · 12 s', durationMs: 12_000 })
    expect(r.watch.runs).toEqual({})
  })

  it('a write card asks for the user', () => {
    const { watch } = watchChat(emptyWatch(), 'chat:start', start, 0)
    const r = watchChat(watch, 'chat:confirm', { repoID: 'r1', runID: 'run1', confirmID: 'c', tool: 'commit', title: 'Commit 2 files', details: [] }, 5)
    expect(r.event).toEqual({ category: 'ai', repoID: 'r1', target: 'chat', body: 'The AI is waiting for you to confirm: Commit 2 files' })
  })

  it('a decision card asks for the user and silences the finished turn', () => {
    let { watch } = watchChat(emptyWatch(), 'chat:start', start, 0)
    const card = watchChat(watch, 'chat:tool', { repoID: 'r1', runID: 'run1', name: 'propose_options', args: {} }, 5)
    expect(card.event).toEqual({ category: 'ai', repoID: 'r1', target: 'chat', body: 'The AI has a decision for you' })
    const done = watchChat(card.watch, 'chat:done', { repoID: 'r1', runID: 'run1' }, 60_000)
    expect(done.event).toBeNull()
  })

  it('other tools say nothing', () => {
    const { watch } = watchChat(emptyWatch(), 'chat:start', start, 0)
    expect(watchChat(watch, 'chat:tool', { repoID: 'r1', runID: 'run1', name: 'git_log', args: {} }, 5).event).toBeNull()
  })

  it('an error is a problem', () => {
    const { watch } = watchChat(emptyWatch(), 'chat:start', start, 0)
    const r = watchChat(watch, 'chat:error', { repoID: 'r1', runID: 'run1', message: 'model not found\ndetails', code: 'model_missing' }, 5)
    expect(r.event).toEqual({ category: 'problem', repoID: 'r1', target: 'chat', body: 'Chat answer failed: model not found' })
    expect(r.watch.runs).toEqual({})
  })

  it('done for an unknown run says nothing', () => {
    expect(watchChat(emptyWatch(), 'chat:done', { repoID: 'r1', runID: 'x' }, 5).event).toBeNull()
  })
})
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run src/lib/notifyChat.test.ts`
Expected: FAIL — cannot resolve `./notifyChat`.

- [ ] **Step 3: Implement**

```ts
// frontend/src/lib/notifyChat.ts
import { firstLine, formatSeconds, type NotifyEvent } from './notifyRules'
import type { ChatConfirmEvent, ChatErrorEvent, ChatStartEvent, ChatToolEvent } from './types'

/** The chat runs being watched for notifications, by run id. A run that
 *  raised a decision card does not also notify that it finished. */
export interface ChatWatch {
  runs: Record<string, { repoID: string; start: number; decided: boolean }>
}

export const emptyWatch = (): ChatWatch => ({ runs: {} })

export const CHAT_WATCH_EVENTS = ['chat:start', 'chat:confirm', 'chat:tool', 'chat:done', 'chat:error'] as const

type Run = { repoID: string; runID: string }

export function watchChat(w: ChatWatch, name: string, payload: unknown, now: number): { watch: ChatWatch; event: NotifyEvent | null } {
  const p = payload as Run
  const without = () => {
    const runs = { ...w.runs }
    delete runs[p.runID]
    return { runs }
  }
  switch (name) {
    case 'chat:start': {
      const s = payload as ChatStartEvent
      return { watch: { runs: { ...w.runs, [s.runID]: { repoID: s.repoID, start: now, decided: false } } }, event: null }
    }
    case 'chat:confirm': {
      const c = payload as ChatConfirmEvent
      return { watch: w, event: { category: 'ai', repoID: c.repoID, target: 'chat', body: `The AI is waiting for you to confirm: ${c.title}` } }
    }
    case 'chat:tool': {
      const t = payload as ChatToolEvent
      if (t.name !== 'propose_options') return { watch: w, event: null }
      const run = w.runs[t.runID]
      const watch = run ? { runs: { ...w.runs, [t.runID]: { ...run, decided: true } } } : w
      return { watch, event: { category: 'ai', repoID: t.repoID, target: 'chat', body: 'The AI has a decision for you' } }
    }
    case 'chat:done': {
      const run = w.runs[p.runID]
      if (!run || run.decided) return { watch: without(), event: null }
      const ms = now - run.start
      return { watch: without(), event: { category: 'done', repoID: run.repoID, target: 'chat', body: `Chat answer finished · ${formatSeconds(ms)}`, durationMs: ms } }
    }
    case 'chat:error': {
      const e = payload as ChatErrorEvent
      return { watch: without(), event: { category: 'problem', repoID: e.repoID, target: 'chat', body: `Chat answer failed: ${firstLine(e.message)}` } }
    }
  }
  return { watch: w, event: null }
}
```

- [ ] **Step 4: Run it to verify it passes**

Run: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run src/lib/notifyChat.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
/usr/bin/git add frontend/src/lib/notifyChat.ts frontend/src/lib/notifyChat.test.ts
/usr/bin/git commit -m "feat(notify): watch chat events for cards, finished and failed answers"
```

---

### Task 5: App wiring, Settings section, spec, rebuild

**Files:**
- Modify: `frontend/src/lib/notify.ts` (add `startNotifications`)
- Modify: `frontend/src/App.svelte` (call it in `onMount`)
- Modify: `frontend/src/components/SettingsDialog.svelte` (Notifications section in General)
- Test: `frontend/src/lib/notify.test.ts`
- Modify: `docs/spec/11-notifications.md`, `docs/spec/06-ai.md`

**Interfaces:**
- Consumes: `watchChat`, `emptyWatch`, `CHAT_WATCH_EVENTS` (Task 4); `notify`, `openTarget`, `windowFocused` (Task 2); `statusText` (Task 1); `api.notificationStatus` (Task 2).
- Produces: `startNotifications(on?: typeof EventsOn): () => void`.

- [ ] **Step 1: Write the failing test**

Append to `frontend/src/lib/notify.test.ts` (and add `startNotifications` to its `./notify` import):

```ts
describe('startNotifications', () => {
  it('routes notify:open and chat events', async () => {
    const handlers: Record<string, (p: unknown) => void> = {}
    const on = ((name: string, cb: (p: unknown) => void) => {
      handlers[name] = cb
      return () => delete handlers[name]
    }) as any
    const stop = startNotifications(on)
    handlers['notify:open']({ repoID: 'b', target: 'repo' })
    expect(get(selectedRepoId)).toBe('b')

    windowFocused.set(false)
    handlers['chat:confirm']({ repoID: 'a', runID: 'x', confirmID: 'c', tool: 'push', title: 'Push main', details: [] })
    await Promise.resolve()
    expect(vi.mocked(api.notify).mock.calls[0][0]).toMatchObject({ id: 'a:ai', body: 'The AI is waiting for you to confirm: Push main', target: 'chat' })

    window.dispatchEvent(new Event('focus'))
    expect(get(windowFocused)).toBe(true)
    window.dispatchEvent(new Event('blur'))
    expect(get(windowFocused)).toBe(false)

    stop()
    expect(Object.keys(handlers)).toEqual([])
  })
})
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run src/lib/notify.test.ts`
Expected: FAIL — `startNotifications` is not exported.

- [ ] **Step 3: Implement `startNotifications`**

Add to `frontend/src/lib/notify.ts`:

```ts
import { EventsOn } from '../../wailsjs/runtime/runtime'
import { CHAT_WATCH_EVENTS, emptyWatch, watchChat } from './notifyChat'

/** startNotifications keeps windowFocused live, opens what a clicked OS
 *  notification was about, and turns chat events into notifications —
 *  here, app-wide, since the chat panel is unmounted while closed. */
export function startNotifications(on: typeof EventsOn = EventsOn): () => void {
  const focus = () => windowFocused.set(true)
  const blur = () => windowFocused.set(false)
  window.addEventListener('focus', focus)
  window.addEventListener('blur', blur)
  const offOpen = on('notify:open', (p: { repoID: string; target: NotifyTarget }) => openTarget(p.repoID, p.target === 'chat' ? 'chat' : 'repo'))
  let watch = emptyWatch()
  const offChat = CHAT_WATCH_EVENTS.map((name) =>
    on(name, (payload: unknown) => {
      const r = watchChat(watch, name, payload, Date.now())
      watch = r.watch
      if (r.event) void notify(r.event)
    }),
  )
  return () => {
    window.removeEventListener('focus', focus)
    window.removeEventListener('blur', blur)
    offOpen()
    offChat.forEach((off) => off())
  }
}
```

If vitest cannot import `../../wailsjs/runtime/runtime` (it reads `window.runtime` lazily, so importing is fine; calling is not), the test passes `on` explicitly and never calls the default.

In `frontend/src/App.svelte`: import `import { startNotifications } from './lib/notify'`; in `onMount` after `const stopFocus = startFocusRefresh()` add `const stopNotifications = startNotifications()`, and call `stopNotifications()` in the returned cleanup.

- [ ] **Step 4: Settings section**

In `frontend/src/components/SettingsDialog.svelte`: add to the stores import `notifyAi, notifyDone, notifyEnabled, notifyProblem`; add `import { statusText } from '../lib/notifyRules'`; `api` is already imported there (check; if not, `import { api } from '../lib/api'`). In the script:

```ts
  // Read again on every open: the user may have changed it in the system settings.
  let notifyStatus = ''
  $: if ($settingsOpen) api.notificationStatus().then((s) => (notifyStatus = s)).catch(() => (notifyStatus = ''))
```

In the General tab, between the Appearance and Git sections:

```svelte
            <section>
              <h4>Notifications</h4>
              <label class="row check">
                <input type="checkbox" bind:checked={$notifyEnabled} />
                <span>Show notifications</span>
              </label>
              <label class="row check sub">
                <input type="checkbox" bind:checked={$notifyDone} disabled={!$notifyEnabled} />
                <span>Finished operations (10 s or longer)</span>
              </label>
              <label class="row check sub">
                <input type="checkbox" bind:checked={$notifyAi} disabled={!$notifyEnabled} />
                <span>The AI needs you</span>
              </label>
              <label class="row check sub">
                <input type="checkbox" bind:checked={$notifyProblem} disabled={!$notifyEnabled} />
                <span>Problems — failures and conflicts</span>
              </label>
              {#if statusText(notifyStatus)}<p class="hint">{statusText(notifyStatus)}</p>{/if}
            </section>
```

and in `<style>`: `.check.sub { padding-left: 22px; }`.

- [ ] **Step 5: Complete `docs/spec/11-notifications.md`**

Add rows to the Events table:

```markdown
| Operations finished | Chat answer finished | the answer took 10 s or longer and raised no decision card |
| The AI needs you | A write card waits for confirmation | always |
| | A new decision card | always |
| Problems | A chat answer ended with an error | always |
```

Extend "Where it appears" rule 2 to: "Window focused, event in the selected repository — and, for chat events, the chat panel open → nothing new." and rule 3: "…another repository, or the chat closed → a toast "<repo>: <body>" with **View**, which selects that repository (and opens the chat for chat events)." Then add:

```markdown
## Settings

Settings → General → Notifications: **Show notifications** turns them all
off; **Finished operations (10 s or longer)**, **The AI needs you** and
**Problems — failures and conflicts** turn off one category each. All on by
default; kept on this computer, not per repository. Below them a line says
whether system notifications are allowed, not allowed yet, or unavailable
(and why) — in the last two cases toasts are used instead.

A failed operation's error toast does not depend on these settings: it is
shown as it always was.
```

In `docs/spec/06-ai.md`, in "### The card itself" (or at the end of "## Write tools, each confirmed by the user"), add: "A write card and a decision card waiting for the user notify — see [Notifications](11-notifications.md)."

- [ ] **Step 6: Run everything**

Run: `go test ./... -count=1 && cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run && npm run check`
Expected: all PASS, svelte-check 0 errors.

- [ ] **Step 7: Commit**

```bash
/usr/bin/git add frontend/src/lib/notify.ts frontend/src/lib/notify.test.ts frontend/src/App.svelte frontend/src/components/SettingsDialog.svelte docs/spec/11-notifications.md docs/spec/06-ai.md
/usr/bin/git commit -m "feat(notify): chat notifications, notification click and Settings section"
```

- [ ] **Step 8: Rebuild and reopen the app**

Run (background): `make dev`
Check by hand: Settings → General shows the Notifications section and a status line; push in a repository, switch to another app — after a ≥ 10 s operation, a notification or (unbundled dev build) a toast on return; a failing push on a non-selected repository shows "<repo>: …" with View. For the real OS notification: `make build` and open `build/bin/CommitTree.app`.
