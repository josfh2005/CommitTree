# Status bar Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A bar along the bottom of the CommitTree window that shows one running task (operation, clone, background fetch, model download, or a bare git write) with Cancel and "+N", and the last You/AI result when idle.

**Architecture:** A frontend task registry (`frontend/src/lib/tasks.ts`): pure state functions plus one Svelte store. Every source registers into it — operations through a `beginOp` helper that replaces the `busy.set(label)` / `busy.set('')` pairs, the clone and model download from their existing event handlers, the background fetch from `autoFetch.ts`, and running git writes from a global `cmdlog:entry` listener. A new `StatusBar.svelte` renders it under the whole window. The only backend change: command-log entries carry the sidebar repository id (`repoId`).

**Tech Stack:** Go 1.x + Wails v2 (backend), Svelte 4 + TypeScript + Vitest (frontend).

**Spec:** `docs/superpowers/specs/2026-10-09-status-bar-design.md` — read it before starting any task.

## Global Constraints

- Run git as `/usr/bin/git`. Commits have **no** `Co-Authored-By` line. Never merge or push; the owner decides that.
- Node 22 for every frontend command: prefix with `source ~/.nvm/nvm.sh && nvm use 22 >/dev/null &&` (the default node v14 is x64 and breaks esbuild).
- Frontend tests: `cd frontend && npx vitest run <file>`; full suite `npx vitest run`; types `npm run check`. Go: `go test ./internal/...`.
- Every behaviour change updates `docs/spec/` in the same commit (a design doc under `docs/superpowers/` does not count).
- Bar height about 22 px; uses theme tokens (`var(--…)`) only, so Light/Dark/High contrast work.
- Copy, verbatim: "Cancel", "Cancelling…", "+N" (e.g. "+2"), "just now", "N min ago", "N h ago", `✓` / `✗` / `⊘`, "<label> failed", "<label> cancelled", "<label> — done", "Cloned <name>", "Downloaded <model>", "Fetching".
- Only You/AI tasks set the last result; Auto tasks (background fetch, Auto git writes) never do.
- Shown task: rank You/AI before Auto, then the most recently started.
- `make dev` leaves `frontend/package.json.md5` and `frontend/wailsjs/runtime/*` modified; restore them with `git checkout -- frontend/package.json.md5 frontend/wailsjs/runtime` before committing.

## Review Focus

1. **A write event arriving out of order** (its end before its running event, e.g. two goroutines emitting): the late running event must not resurrect a finished write as a stuck task — `applyEntry` keeps a bounded list of ended ids (test in Task 2).
2. **A tiny clone whose `clone:done` arrives before `CloneRepo` resolves**: no clone task may be left running forever — the clone task is only created while `cloneView` is still `running` (test in Task 4).
3. **Cancel racing the command's own end**: `"command is not running"` is swallowed, and the "Cancelling…" state disappears with the task (Task 5 component logic + Task 2 `cancelTarget` tests).
4. **A background fetch running in a repository** must show as one "Fetching" task (its git write folded in, no duplicate `git fetch …` task), and must never replace the last result, even when it fails (tests in Task 2 and Task 4).
5. **Switching repositories while an operation runs**: the task keeps the repository it started in (name and Cancel target), not the newly selected one (test in Task 3).

---

### Task 1: `repoId` on command-log entries (backend)

**Files:**
- Modify: `internal/cmdlog/log.go` (struct `Entry`)
- Modify: `internal/app/cmdlog.go` (`emitCommand`, `CommandLog`, new `repoIDForKey`)
- Modify: `frontend/src/lib/types.ts` (`CommandEntry`)
- Test: `internal/app/cmdlog_test.go`

**Interfaces:**
- Produces: JSON field `repoId` on every `cmdlog:entry` event and on every entry of `CommandLog(id).entries`; TS `CommandEntry.repoId: string`.

- [ ] **Step 1: Write the failing Go tests** (append to `internal/app/cmdlog_test.go`; `newTestApp(t)` already returns an app and a stored repo id)

```go
func TestEmittedEntriesCarryRepoID(t *testing.T) {
	a, id := newTestApp(t)
	var mu sync.Mutex
	var got []cmdlog.Entry
	a.cmdEmit = func(e cmdlog.Entry) { mu.Lock(); got = append(got, e); mu.Unlock() }
	a.started.Store(true)
	if err := a.CreateBranch(id, "with-id", "HEAD", false); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) == 0 {
		t.Fatal("no events")
	}
	for _, e := range got {
		if e.RepoID != id {
			t.Fatalf("entry %v has repoId %q, want %q", e.Args, e.RepoID, id)
		}
	}
}

func TestCommandLogEntriesCarryRepoID(t *testing.T) {
	a, id := newTestApp(t)
	if err := a.CreateBranch(id, "listed", "HEAD", false); err != nil {
		t.Fatal(err)
	}
	view, err := a.CommandLog(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Entries) == 0 {
		t.Fatal("no entries")
	}
	for _, e := range view.Entries {
		if e.RepoID != id {
			t.Fatalf("entry %v has repoId %q, want %q", e.Args, e.RepoID, id)
		}
	}
}

func TestRepoIDForKeyUnknownIsEmpty(t *testing.T) {
	a, _ := newTestApp(t)
	if got := a.repoIDForKey(cmdlog.RepoKey(t.TempDir())); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/app -run 'RepoID' -v`
Expected: compile error, `e.RepoID undefined` / `a.repoIDForKey undefined`.

- [ ] **Step 3: Implement**

In `internal/cmdlog/log.go`, add to `Entry` right after `Repo`:

```go
	// RepoID is the sidebar repository the command ran in, filled in by
	// the app layer (cmdlog does not know repository ids); empty when the
	// folder is not a listed repository, e.g. a clone's destination.
	RepoID string `json:"repoId"`
```

In `internal/app/cmdlog.go`:

```go
// repoIDForKey is the id of the listed repository — stored, then a detected
// worktree, then a detected submodule, the same order as repo() — whose
// folder has backend key key; "" when none. The mutexes are only held to
// read the maps, never while git runs, so calling it from the recorder's
// hooks cannot deadlock.
func (a *App) repoIDForKey(key string) string {
	for _, r := range a.store.List() {
		if cmdlog.RepoKey(r.Path) == key {
			return r.ID
		}
	}
	a.wtMu.Lock()
	for id, r := range a.worktrees {
		if cmdlog.RepoKey(r.Path) == key {
			a.wtMu.Unlock()
			return id
		}
	}
	a.wtMu.Unlock()
	a.smMu.Lock()
	defer a.smMu.Unlock()
	for id, r := range a.submodules {
		if cmdlog.RepoKey(r.Path) == key {
			return id
		}
	}
	return ""
}
```

Change `emitCommand` to fill the id before sending:

```go
func (a *App) emitCommand(e cmdlog.Entry) {
	if a.started.Load() && a.cmdEmit != nil {
		e.RepoID = a.repoIDForKey(e.Repo)
		a.cmdEmit(e)
	}
}
```

Change `CommandLog` to stamp each entry with `id`:

```go
	entries := a.cmds.List(dir)
	for i := range entries {
		entries[i].RepoID = id
	}
	return CommandLogView{Repo: cmdlog.RepoKey(dir), Entries: entries}, nil
```

Check before relying on it: `grep -n "wtMu\|smMu" internal/app/*.go` — confirm no code path runs git while holding `wtMu` or `smMu` (today only short map reads/writes do). If one does, say so in your report instead of shipping a deadlock. Also confirm `a.cmds.List` returns a fresh slice (so stamping it does not mutate the log); if it returns internal storage, copy first.

In `frontend/src/lib/types.ts`, add to `CommandEntry` after `repo`:

```ts
  /** The sidebar repository id the command ran in; '' outside every listed
   *  repository (e.g. a clone's destination). */
  repoId: string
```

Fix any frontend test fixture that builds a `CommandEntry` literal and now fails `npm run check` (add `repoId: ''`).

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/... && cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npm run check && npx vitest run`
Expected: all PASS, 0 svelte-check errors.

- [ ] **Step 5: Commit**

```bash
/usr/bin/git add internal/cmdlog/log.go internal/app/cmdlog.go internal/app/cmdlog_test.go frontend/src/lib/types.ts frontend/src/lib/*.test.ts
/usr/bin/git commit -m "feat(cmdlog): command entries carry the sidebar repository id"
```

(No `docs/spec/` change: `repoId` is not user-visible on its own; Task 5 documents the bar.)

---

### Task 2: Task registry core (`lib/tasks.ts`)

**Files:**
- Create: `frontend/src/lib/tasks.ts`
- Test: `frontend/src/lib/tasks.test.ts`

**Interfaces:**
- Consumes: `CommandEntry.repoId` (Task 1); `commandLine`, `outcomeText` from `./cmdlog`; `busy`, `repos`, `selectedRepoId` from `./stores`; `errorMessage` from `./ui`.
- Produces (exact names later tasks use):
  - types `TaskKind`, `TaskOutcome`, `Task`, `TaskResult`, `RunningWrite`, `TaskState`, `CancelTarget`, `EndTask`
  - pure: `EMPTY`, `outcomeOf(error?)`, `resultText(kind, label, outcome)`, `addTask(s, t)`, `updateTask(s, id, patch)`, `endTask(s, id, outcome, message, now)`, `applyEntry(s, e, now, nameOf)`, `visibleTasks(s, nameOf)`, `cancelTarget(s, t)`, `relativeTime(ms)`
  - store/imperative: `taskState`, `shownTasks`, `repoName(id)`, `beginTask(t, now?)`, `beginOp(label, repoId?)`, `finishTask(id, outcome, message?)`, `setTaskProgress(id, progress)`, `hasTask(id)`, `noteCommand(e)`

- [ ] **Step 1: Write the failing tests** — `frontend/src/lib/tasks.test.ts`

```ts
import { get } from 'svelte/store'
import { beforeEach, describe, expect, it } from 'vitest'
import {
  EMPTY, addTask, applyEntry, beginOp, beginTask, cancelTarget, endTask, finishTask, outcomeOf, relativeTime,
  resultText, setTaskProgress, taskState, visibleTasks, type Task, type TaskState,
} from './tasks'
import { busy, repos, selectedRepoId } from './stores'
import type { CommandEntry, Repo } from './types'

const names: Record<string, string> = { a: 'alpha', b: 'beta' }
const nameOf = (id: string) => names[id] ?? ''
const task = (over: Partial<Task> = {}): Task => ({
  id: 'op:1', kind: 'op', origin: 'you', label: 'Pushing…', repoId: 'a', repoName: 'alpha', progress: '', start: 1000, ...over,
})
const entry = (over: Partial<CommandEntry> = {}): CommandEntry => ({
  id: 7, repo: '/x/a', repoId: 'a', args: ['commit', '-m', 'x'], origin: 'ai', kind: 'write',
  start: new Date(5000).toISOString(), durationMs: 0, exitCode: 0, outcome: 'running',
  outputTruncated: false, outputDropped: false, ...over,
} as CommandEntry)

describe('outcomeOf', () => {
  it('is ok without an error, cancelled for a git cancel, failed otherwise', () => {
    expect(outcomeOf()).toEqual({ outcome: 'ok', message: '' })
    expect(outcomeOf(null)).toEqual({ outcome: 'ok', message: '' })
    expect(outcomeOf(new Error('git command cancelled'))).toEqual({ outcome: 'cancelled', message: 'git command cancelled' })
    expect(outcomeOf('push rejected')).toEqual({ outcome: 'failed', message: 'push rejected' })
  })
})

describe('resultText', () => {
  it('uses the past-tense table for operations', () => {
    expect(resultText('op', 'Pushing…', 'ok')).toBe('Pushed')
    expect(resultText('op', 'Deleting branch…', 'ok')).toBe('Deleted branch')
    expect(resultText('op', 'Cherry-picking…', 'ok')).toBe('Cherry-picked')
    expect(resultText('op', 'Committing merge…', 'ok')).toBe('Committed merge')
  })
  it('falls back to "<label> — done" for a label the table lacks', () => {
    expect(resultText('op', 'Frobbing…', 'ok')).toBe('Frobbing — done')
  })
  it('says failed / cancelled after the label without its ellipsis', () => {
    expect(resultText('op', 'Pulling…', 'failed')).toBe('Pulling failed')
    expect(resultText('op', 'Pushing…', 'cancelled')).toBe('Pushing cancelled')
  })
  it('names clones, model downloads and git commands', () => {
    expect(resultText('clone', 'Cloning foo', 'ok')).toBe('Cloned foo')
    expect(resultText('clone', 'Cloning foo', 'failed')).toBe('Cloning foo failed')
    expect(resultText('model', 'Downloading qwen', 'ok')).toBe('Downloaded qwen')
    expect(resultText('git', "git commit -m x", 'failed')).toBe('git commit -m x')
  })
})

describe('endTask', () => {
  it('removes the task and records a You/AI result', () => {
    const s = endTask(addTask(EMPTY, task()), 'op:1', 'ok', '', 9000)
    expect(s.tasks).toEqual([])
    expect(s.last).toEqual({ kind: 'op', outcome: 'ok', text: 'Pushed', message: '', repoId: 'a', repoName: 'alpha', end: 9000 })
  })
  it('never lets an Auto task set the last result, even a failed one', () => {
    const before: TaskState = { ...EMPTY, last: { kind: 'op', outcome: 'ok', text: 'Pushed', message: '', repoId: 'a', repoName: 'alpha', end: 1 } }
    const s = endTask(addTask(before, task({ id: 'fetch:a', kind: 'fetch', origin: 'auto', label: 'Fetching' })), 'fetch:a', 'failed', 'boom', 9000)
    expect(s.tasks).toEqual([])
    expect(s.last).toBe(before.last)
  })
  it('ignores an unknown id', () => {
    expect(endTask(EMPTY, 'nope', 'ok', '', 1)).toBe(EMPTY)
  })
})

describe('applyEntry', () => {
  it('tracks a running write and drops it on its end', () => {
    const s1 = applyEntry(EMPTY, entry(), 6000, nameOf)
    expect(s1.writes).toEqual([{ id: 7, repoId: 'a', origin: 'ai', args: ['commit', '-m', 'x'], start: 5000 }])
    const s2 = applyEntry(s1, entry({ outcome: 'ok' }), 7000, nameOf)
    expect(s2.writes).toEqual([])
    expect(s2.last).toEqual({ kind: 'git', outcome: 'ok', text: 'git commit -m x', message: '', repoId: 'a', repoName: 'alpha', end: 7000 })
  })
  it('ignores reads', () => {
    expect(applyEntry(EMPTY, entry({ kind: 'read' }), 1, nameOf)).toBe(EMPTY)
  })
  it('ignores a running event that arrives after its end', () => {
    const s1 = applyEntry(EMPTY, entry({ outcome: 'ok' }), 1, nameOf)
    expect(s1.last).toBeNull()
    const s2 = applyEntry(s1, entry(), 2, nameOf)
    expect(s2.writes).toEqual([])
  })
  it('folds a write ending inside an op of the same repository (no result of its own)', () => {
    const withOp = addTask(EMPTY, task())
    const s = applyEntry(applyEntry(withOp, entry({ origin: 'you' }), 1, nameOf), entry({ origin: 'you', outcome: 'failed', exitCode: 1 }), 2, nameOf)
    expect(s.last).toBeNull()
  })
  it('does not record an Auto write as the last result', () => {
    const s = applyEntry(applyEntry(EMPTY, entry({ origin: 'auto' }), 1, nameOf), entry({ origin: 'auto', outcome: 'ok' }), 2, nameOf)
    expect(s.last).toBeNull()
  })
  it('maps timeout to failed and keeps the reason', () => {
    const s = applyEntry(applyEntry(EMPTY, entry(), 1, nameOf), entry({ outcome: 'timeout' }), 2, nameOf)
    expect(s.last?.outcome).toBe('failed')
    expect(s.last?.message).toBe('Timed out')
  })
})

describe('visibleTasks', () => {
  it('shows a bare write as a git task, folds it into an op or fetch of its repository', () => {
    const s = applyEntry(EMPTY, entry(), 1, nameOf)
    expect(visibleTasks(s, nameOf).map((t) => [t.id, t.kind, t.label, t.repoName])).toEqual([['git:7', 'git', 'git commit -m x', 'alpha']])
    const withFetch = addTask(s, task({ id: 'fetch:a', kind: 'fetch', origin: 'auto', label: 'Fetching' }))
    expect(visibleTasks(withFetch, nameOf).map((t) => t.id)).toEqual(['fetch:a'])
  })
  it('never shows a write outside a listed repository', () => {
    expect(visibleTasks(applyEntry(EMPTY, entry({ repoId: '' }), 1, nameOf), nameOf)).toEqual([])
  })
  it('puts You/AI before Auto, then the most recent first', () => {
    let s = addTask(EMPTY, task({ id: 'fetch:b', kind: 'fetch', origin: 'auto', repoId: 'b', start: 9000 }))
    s = addTask(s, task({ id: 'op:1', start: 1000 }))
    s = addTask(s, task({ id: 'clone', kind: 'clone', repoId: '', start: 2000 }))
    expect(visibleTasks(s, nameOf).map((t) => t.id)).toEqual(['clone', 'op:1', 'fetch:b'])
  })
})

describe('cancelTarget', () => {
  it('cancels a clone, a model download, or a git task by its entry', () => {
    expect(cancelTarget(EMPTY, task({ id: 'clone', kind: 'clone' }))).toEqual({ kind: 'clone' })
    expect(cancelTarget(EMPTY, task({ id: 'model', kind: 'model' }))).toEqual({ kind: 'model' })
    expect(cancelTarget(EMPTY, task({ id: 'git:7', kind: 'git' }))).toEqual({ kind: 'command', repoId: 'a', entryId: 7 })
  })
  it('cancels the newest running write of an op repository, or nothing', () => {
    expect(cancelTarget(EMPTY, task())).toBeNull()
    let s = applyEntry(EMPTY, entry({ id: 3, start: new Date(1000).toISOString() }), 1, nameOf)
    s = applyEntry(s, entry({ id: 4, start: new Date(2000).toISOString() }), 1, nameOf)
    s = applyEntry(s, entry({ id: 5, repoId: 'b', start: new Date(3000).toISOString() }), 1, nameOf)
    expect(cancelTarget(s, task())).toEqual({ kind: 'command', repoId: 'a', entryId: 4 })
  })
})

describe('relativeTime', () => {
  it('says just now, then minutes, then hours', () => {
    expect(relativeTime(59_999)).toBe('just now')
    expect(relativeTime(60_000)).toBe('1 min ago')
    expect(relativeTime(59 * 60_000)).toBe('59 min ago')
    expect(relativeTime(2 * 3_600_000 + 5)).toBe('2 h ago')
  })
})

describe('store helpers', () => {
  beforeEach(() => {
    taskState.set(EMPTY)
    busy.set('')
    repos.set([{ id: 'a', name: 'alpha' }, { id: 'b', name: 'beta' }] as Repo[])
    selectedRepoId.set('a')
  })

  it('beginOp sets the busy label and a task, its end clears both and records the result', () => {
    const end = beginOp('Pulling…', 'b')
    expect(get(busy)).toBe('Pulling…')
    expect(get(taskState).tasks).toMatchObject([{ kind: 'op', origin: 'you', label: 'Pulling…', repoId: 'b', repoName: 'beta' }])
    end(new Error('git command cancelled'))
    expect(get(busy)).toBe('')
    expect(get(taskState).tasks).toEqual([])
    expect(get(taskState).last).toMatchObject({ outcome: 'cancelled', text: 'Pulling cancelled', repoName: 'beta' })
  })

  it('beginOp defaults to the repository selected at its start, even if the selection changes', () => {
    const end = beginOp('Pushing…')
    selectedRepoId.set('b')
    end()
    expect(get(taskState).last).toMatchObject({ text: 'Pushed', repoId: 'a', repoName: 'alpha' })
  })

  it('an end called twice records once', () => {
    const end = beginTask({ id: 'model', kind: 'model', origin: 'you', label: 'Downloading m', repoId: '', repoName: '' })
    end()
    taskState.update((s) => ({ ...s, last: null }))
    end(new Error('late'))
    expect(get(taskState).last).toBeNull()
  })

  it('setTaskProgress and finishTask act by id; unknown ids are no-ops', () => {
    beginTask({ id: 'clone', kind: 'clone', origin: 'you', label: 'Cloning foo', repoId: '', repoName: '' })
    setTaskProgress('clone', 'Receiving 42 %')
    setTaskProgress('nope', 'x')
    expect(get(taskState).tasks[0].progress).toBe('Receiving 42 %')
    finishTask('clone', 'ok')
    expect(get(taskState).last?.text).toBe('Cloned foo')
  })
})
```

- [ ] **Step 2: Run them to see them fail**

Run: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run src/lib/tasks.test.ts`
Expected: FAIL — cannot resolve `./tasks`.

- [ ] **Step 3: Implement** — `frontend/src/lib/tasks.ts`

```ts
import { derived, get, writable } from 'svelte/store'
import { commandLine, outcomeText } from './cmdlog'
import { busy, repos, selectedRepoId } from './stores'
import type { CommandEntry, CommandOrigin } from './types'
import { errorMessage } from './ui'

/** What the status bar can show (docs/spec/13-status-bar.md). */
export type TaskKind = 'op' | 'clone' | 'fetch' | 'model' | 'git'
export type TaskOutcome = 'ok' | 'failed' | 'cancelled'

export interface Task {
  /** 'op:<n>', 'clone', 'model', 'fetch:<repoId>' or 'git:<entryId>'. */
  id: string
  kind: TaskKind
  origin: CommandOrigin
  label: string
  /** '' for a clone or a model download. */
  repoId: string
  repoName: string
  /** '' or e.g. 'Receiving 42 %'. */
  progress: string
  /** ms since epoch. */
  start: number
}

export interface TaskResult {
  kind: TaskKind
  outcome: TaskOutcome
  text: string
  /** Why it failed (the tooltip); '' otherwise. */
  message: string
  repoId: string
  repoName: string
  end: number
}

/** A git write that has started and not ended, from cmdlog:entry. */
export interface RunningWrite { id: number; repoId: string; origin: CommandOrigin; args: string[]; start: number }

export interface TaskState {
  tasks: Task[]
  writes: RunningWrite[]
  /** Ids of writes already ended, so a late running event can't bring one back. */
  ended: number[]
  last: TaskResult | null
}

export type CancelTarget = { kind: 'clone' } | { kind: 'model' } | { kind: 'command'; repoId: string; entryId: number }
export type EndTask = (error?: unknown) => void

export const EMPTY: TaskState = { tasks: [], writes: [], ended: [], last: null }
const ENDED_KEPT = 200
/** gitcmd.ErrCancelled's message. */
const CANCELLED_MESSAGE = 'git command cancelled'

/** Past tense of each busy label; a label missing here reads "<label> — done". */
const PAST: Record<string, string> = {
  'Adding remote…': 'Added remote',
  'Amending…': 'Amended',
  'Applying stash…': 'Applied stash',
  'Checking out…': 'Checked out',
  'Cherry-picking…': 'Cherry-picked',
  'Committing merge…': 'Committed merge',
  'Committing…': 'Committed',
  'Continuing…': 'Continued',
  'Creating branch…': 'Created branch',
  'Creating tag…': 'Created tag',
  'Deleting branch…': 'Deleted branch',
  'Deleting remote branch…': 'Deleted remote branch',
  'Deleting tag…': 'Deleted tag',
  'Discarding…': 'Discarded',
  'Dropping stash…': 'Dropped stash',
  'Fetching…': 'Fetched',
  'Initialising…': 'Initialised',
  'Merging…': 'Merged',
  'Popping stash…': 'Popped stash',
  'Pulling…': 'Pulled',
  'Pushing branches…': 'Pushed branches',
  'Pushing…': 'Pushed',
  'Rebasing…': 'Rebased',
  'Removing remote…': 'Removed remote',
  'Resetting…': 'Reset',
  'Resolving…': 'Resolved',
  'Restarting…': 'Restarted',
  'Restoring…': 'Restored',
  'Saving git-flow…': 'Saved git-flow',
  'Saving remote…': 'Saved remote',
  'Setting up git-flow…': 'Set up git-flow',
  'Skipping…': 'Skipped',
  'Staging…': 'Staged',
  'Starting…': 'Started',
  'Stashing…': 'Stashed',
  'Syncing URL…': 'Synced URL',
  'Taking ours…': 'Took ours',
  'Taking theirs…': 'Took theirs',
  'Unstaging…': 'Unstaged',
  'Updating…': 'Updated',
}

export function outcomeOf(error?: unknown): { outcome: TaskOutcome; message: string } {
  if (error === undefined || error === null) return { outcome: 'ok', message: '' }
  const message = errorMessage(error)
  return { outcome: message.includes(CANCELLED_MESSAGE) ? 'cancelled' : 'failed', message }
}

export function resultText(kind: TaskKind, label: string, outcome: TaskOutcome): string {
  if (kind === 'git') return label
  const base = label.replace(/…$/, '')
  if (outcome === 'failed') return `${base} failed`
  if (outcome === 'cancelled') return `${base} cancelled`
  if (kind === 'clone') return base.replace(/^Cloning /, 'Cloned ')
  if (kind === 'model') return base.replace(/^Downloading /, 'Downloaded ')
  return PAST[label] ?? `${base} — done`
}

export function addTask(s: TaskState, t: Task): TaskState {
  return { ...s, tasks: [...s.tasks.filter((x) => x.id !== t.id), t] }
}

export function updateTask(s: TaskState, id: string, patch: Partial<Pick<Task, 'progress' | 'label'>>): TaskState {
  if (!s.tasks.some((t) => t.id === id)) return s
  return { ...s, tasks: s.tasks.map((t) => (t.id === id ? { ...t, ...patch } : t)) }
}

/** endTask removes task id; a You/AI task becomes the last result. */
export function endTask(s: TaskState, id: string, outcome: TaskOutcome, message: string, now: number): TaskState {
  const t = s.tasks.find((x) => x.id === id)
  if (!t) return s
  const tasks = s.tasks.filter((x) => x.id !== id)
  if (t.origin === 'auto') return { ...s, tasks }
  return {
    ...s,
    tasks,
    last: { kind: t.kind, outcome, text: resultText(t.kind, t.label, outcome), message, repoId: t.repoId, repoName: t.repoName, end: now },
  }
}

/** An op or a fetch absorbs the git writes of its repository. */
const folds = (t: Task) => t.kind === 'op' || t.kind === 'fetch'
const foldedIn = (s: TaskState, repoId: string) => s.tasks.some((t) => folds(t) && t.repoId === repoId)

/** applyEntry follows one cmdlog:entry: a running write is tracked, its end
 *  drops it — and when it was shown on its own (a listed repository with no
 *  op or fetch around it, origin You/AI), becomes the last result. Reads
 *  and ends with no running event before them only mark the id ended. */
export function applyEntry(s: TaskState, e: CommandEntry, now: number, nameOf: (id: string) => string): TaskState {
  if (e.kind !== 'write') return s
  if (e.outcome === 'running') {
    if (s.ended.includes(e.id) || s.writes.some((w) => w.id === e.id)) return s
    return { ...s, writes: [...s.writes, { id: e.id, repoId: e.repoId, origin: e.origin, args: e.args, start: Date.parse(e.start) }] }
  }
  const ended = [...s.ended, e.id].slice(-ENDED_KEPT)
  const w = s.writes.find((x) => x.id === e.id)
  if (!w) return { ...s, ended }
  const writes = s.writes.filter((x) => x.id !== e.id)
  if (w.repoId === '' || foldedIn(s, w.repoId) || w.origin === 'auto') return { ...s, writes, ended }
  const outcome: TaskOutcome = e.outcome === 'ok' ? 'ok' : e.outcome === 'cancelled' ? 'cancelled' : 'failed'
  return {
    ...s,
    writes,
    ended,
    last: {
      kind: 'git', outcome, text: commandLine(w.args), message: outcome === 'failed' ? outcomeText(e) : '',
      repoId: w.repoId, repoName: nameOf(w.repoId), end: now,
    },
  }
}

const rank = (o: CommandOrigin) => (o === 'auto' ? 1 : 0)

/** Every running task, the one the bar shows first: You/AI before Auto, then
 *  the most recently started. */
export function visibleTasks(s: TaskState, nameOf: (id: string) => string): Task[] {
  const git: Task[] = s.writes
    .filter((w) => w.repoId !== '' && !foldedIn(s, w.repoId))
    .map((w) => ({
      id: `git:${w.id}`, kind: 'git', origin: w.origin, label: commandLine(w.args),
      repoId: w.repoId, repoName: nameOf(w.repoId), progress: '', start: w.start,
    }))
  return [...s.tasks, ...git].sort((a, b) => rank(a.origin) - rank(b.origin) || b.start - a.start)
}

/** What Cancel stops for t; null when nothing can be cancelled right now
 *  (an op whose repository runs no git write at this moment). */
export function cancelTarget(s: TaskState, t: Task): CancelTarget | null {
  if (t.kind === 'clone') return { kind: 'clone' }
  if (t.kind === 'model') return { kind: 'model' }
  if (t.kind === 'git') return { kind: 'command', repoId: t.repoId, entryId: Number(t.id.slice('git:'.length)) }
  const newest = s.writes
    .filter((w) => w.repoId === t.repoId)
    .sort((a, b) => b.start - a.start || b.id - a.id)[0]
  return newest ? { kind: 'command', repoId: t.repoId, entryId: newest.id } : null
}

export function relativeTime(ms: number): string {
  if (ms < 60_000) return 'just now'
  if (ms < 3_600_000) return `${Math.floor(ms / 60_000)} min ago`
  return `${Math.floor(ms / 3_600_000)} h ago`
}

// ---- The store ----

export const taskState = writable<TaskState>(EMPTY)

export function repoName(id: string): string {
  return get(repos).find((r) => r.id === id)?.name ?? ''
}

export const shownTasks = derived([taskState, repos], ([$s]) => visibleTasks($s, repoName))

export function hasTask(id: string): boolean {
  return get(taskState).tasks.some((t) => t.id === id)
}

/** beginTask registers t, started now, and returns its end: with no error
 *  it ends ok; with one, failed — or cancelled for a git cancel. Only the
 *  first call counts. */
export function beginTask(t: Omit<Task, 'start' | 'progress'> & { progress?: string }, now = Date.now()): EndTask {
  taskState.update((s) => addTask(s, { progress: '', ...t, start: now }))
  let done = false
  return (error) => {
    if (done) return
    done = true
    const { outcome, message } = outcomeOf(error)
    taskState.update((s) => endTask(s, t.id, outcome, message, Date.now()))
  }
}

let opSeq = 0

/** beginOp sets the toolbar's busy label and registers the operation as a
 *  You task of repoId (default: the repository selected now); its end
 *  clears the label and ends the task. */
export function beginOp(label: string, repoId: string = get(selectedRepoId)): EndTask {
  busy.set(label)
  const end = beginTask({ id: `op:${++opSeq}`, kind: 'op', origin: 'you', label, repoId, repoName: repoName(repoId) })
  return (error) => {
    busy.set('')
    end(error)
  }
}

export function finishTask(id: string, outcome: TaskOutcome, message = ''): void {
  taskState.update((s) => endTask(s, id, outcome, message, Date.now()))
}

export function setTaskProgress(id: string, progress: string): void {
  taskState.update((s) => updateTask(s, id, { progress }))
}

/** noteCommand is the app-wide cmdlog:entry listener. */
export function noteCommand(e: CommandEntry): void {
  taskState.update((s) => applyEntry(s, e, Date.now(), repoName))
}
```

Notes for the implementer:
- `beginOp`'s returned end calls `busy.set('')` even when called a second time — that is the same as today's double `busy.set('')` in some `finally` blocks and is harmless. Do not "fix" it by guarding the busy clear.
- If importing `./stores` from `./tasks` creates a circular import at module init (check `stores.ts` does not import `tasks.ts`, `actions.ts` or `autoFetch.ts`), report it.

- [ ] **Step 4: Run the tests**

Run: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run src/lib/tasks.test.ts && npm run check`
Expected: all PASS, 0 errors.

- [ ] **Step 5: Commit**

```bash
/usr/bin/git add frontend/src/lib/tasks.ts frontend/src/lib/tasks.test.ts
/usr/bin/git commit -m "feat(tasks): task registry for the status bar"
```

---

### Task 3: Operations register through `beginOp`

**Files:**
- Modify: `frontend/src/lib/actions.ts` (`run`, `runOp`, and the direct busy sites in `deleteBranch`, `mergeBranch`, `commitMerge`, `resolveMergeRegion`, `chooseRegionOption`, `rebaseOnto`, `cherryPick`, `pushAll`, `pull`)
- Modify: `frontend/src/lib/flowActions.ts` (the `busy.set` pair near line 13)
- Modify: `frontend/src/components/RepoFlowTab.svelte` (near line 32)
- Modify: `frontend/src/components/RepoSettingsDialog.svelte` (near line 56)
- Test: `frontend/src/lib/actions.test.ts`

**Interfaces:**
- Consumes: `beginOp(label: string, repoId?: string): EndTask`, `taskState`, `EMPTY` (Task 2).
- Produces: every busy-label operation is a task; `busy` keeps behaving exactly as before.

Rule for every site: `busy.set(label)` becomes `const end = beginOp(label, <repo id the function already has>)`; the error the site catches is remembered (`let failure: unknown`, `failure = e` in `catch`); `busy.set('')` becomes `end(failure)`. A site that has no repository id in scope (`run`) omits it, so `beginOp` uses the selected repository. A site that clears busy early in a `catch` and again in `finally` calls `end(e)` in the `catch` (the second call is a no-op). Do not change any toast, dialog or refresh behaviour.

- [ ] **Step 1: Write the failing tests** — add to `frontend/src/lib/actions.test.ts` (it already mocks `./api` with `pull: vi.fn()` and `push`, and imports `pull`, `push`, `repos`, `selectedRepoId`)

```ts
import { EMPTY, taskState } from './tasks'

describe('operations as status-bar tasks', () => {
  beforeEach(() => {
    taskState.set(EMPTY)
    repos.set([{ id: 'r1', name: 'one' }, { id: 'r2', name: 'two' }] as Repo[])
    selectedRepoId.set('r1')
  })

  it('pull is a task of its repository while it runs, then the last result', async () => {
    let resolve!: (v: unknown) => void
    vi.mocked(api.pull).mockReturnValueOnce(new Promise((r) => (resolve = r)) as never)
    const done = pull('r2')
    expect(get(taskState).tasks).toMatchObject([{ kind: 'op', label: 'Pulling…', repoId: 'r2', repoName: 'two' }])
    selectedRepoId.set('r1')
    resolve({ outcome: 1, conflicts: [] })
    await done
    expect(get(taskState).tasks).toEqual([])
    expect(get(taskState).last).toMatchObject({ outcome: 'ok', text: 'Pulled', repoId: 'r2' })
  })

  it('a failed pull is "Pulling failed" with the reason', async () => {
    vi.mocked(api.pull).mockRejectedValueOnce(new Error('no upstream'))
    await pull('r1')
    expect(get(taskState).last).toMatchObject({ outcome: 'failed', text: 'Pulling failed', message: 'no upstream' })
  })

  it('a cancelled pull is "Pulling cancelled"', async () => {
    vi.mocked(api.pull).mockRejectedValueOnce(new Error('git command cancelled'))
    await pull('r1')
    expect(get(taskState).last).toMatchObject({ outcome: 'cancelled', text: 'Pulling cancelled' })
  })
})
```

(Adapt the resolved value of `api.pull` to what `pull` expects in this file's other pull tests — look at them for the `PULL_UP_TO_DATE` / result shape; `Repo` is imported from `./types` in this file or add it.)

Also add one test for a `run`-based action, using `deleteTag` (already imported and mocked):

```ts
  it('run-based actions register on the selected repository', async () => {
    await deleteTag('r1', 'v1')
    expect(get(taskState).last).toMatchObject({ text: 'Deleted tag', repoId: 'r1' })
  })
```

(If `deleteTag` asks for confirmation, the file's `confirmDialog` mock already resolves `true`. If its busy label is not `'Deleting tag…'`, use the label it does use and its `PAST` entry.)

- [ ] **Step 2: Run them to see them fail**

Run: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run src/lib/actions.test.ts`
Expected: the new tests FAIL (no tasks registered); the old ones still pass.

- [ ] **Step 3: Implement** — in `actions.ts`, import `beginOp` from `./tasks` and rewrite `run` and `runOp`:

```ts
async function run(label: string, fn: () => Promise<unknown>): Promise<boolean> {
  const end = beginOp(label)
  let failure: unknown
  try {
    await fn()
    return true
  } catch (e) {
    failure = e
    toast(errorMessage(e), 'error')
    return false
  } finally {
    end(failure)
    await refreshRepo()
  }
}

async function runOp(id: string, op: OpKind, label: string, fn: () => Promise<unknown>): Promise<boolean> {
  const end = beginOp(label, id)
  let failure: unknown
  try {
    await track(id, op, fn)
    return true
  } catch (e) {
    failure = e
    opError(id, e)
    return false
  } finally {
    end(failure)
    await refreshRepo()
  }
}
```

`pull`, as an example of a direct site:

```ts
export async function pull(id: string) {
  const end = beginOp('Pulling…', id)
  let failure: unknown
  try {
    const result = await track(id, 'pull', () => api.pull(id), (r) => r.conflicts?.length ?? 0)
    if (result.outcome === PULL_UP_TO_DATE) toast('Already up to date.', 'info')
    await warnMovedSubmodules(id)
  } catch (e) {
    failure = e
    opError(id, e)
  } finally {
    end(failure)
    await refreshRepo()
  }
}
```

`pushAll`: a `PartialPushError` counts as a failure too (`failure = e` before the `instanceof` check). `deleteBranch`: replace the early `busy.set('')` in `catch` with `end(e)` and the one in `finally` with `end()` (no-op after the catch's call). `commitMerge`: `const end = beginOp(label, id)` where `busy.set(label)` was (inside the `try` is fine), `failure = e` in `catch`, `end(failure)` in `finally`. `chooseRegionOption` uses its `repoID` parameter. Apply the same rule to `mergeBranch`, `resolveMergeRegion`, `rebaseOnto`, `cherryPick`, `flowActions.ts`, `RepoFlowTab.svelte` and `RepoSettingsDialog.svelte` (both components act on a repository id they hold — use it).

After editing, `grep -rn "busy.set(" frontend/src --include='*.ts' --include='*.svelte' | grep -v test` must show only `tasks.ts`. Then list every distinct busy label now passed to `beginOp`/`run`/`runOp` (`grep -rhoE "(beginOp|run|runOp)\([^)]*" frontend/src`) and make sure each fixed (non-template) label has an entry in `PAST` in `tasks.ts`; add the missing ones (with a matching assertion in `tasks.test.ts`'s `resultText` block). Template labels fall back to "— done"; list them in your report.

- [ ] **Step 4: Run the tests**

Run: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run && npm run check`
Expected: all PASS, 0 errors.

- [ ] **Step 5: Commit**

```bash
/usr/bin/git add frontend/src/lib/actions.ts frontend/src/lib/actions.test.ts frontend/src/lib/flowActions.ts frontend/src/lib/tasks.ts frontend/src/lib/tasks.test.ts frontend/src/components/RepoFlowTab.svelte frontend/src/components/RepoSettingsDialog.svelte
/usr/bin/git commit -m "feat(tasks): operations with a busy label register as tasks"
```

---

### Task 4: Clone, model download, background fetch and git writes as sources

**Files:**
- Modify: `frontend/src/lib/clone.ts` (`startClone`, `startCloneEvents`; new `cloneProgressText`)
- Create: `frontend/src/lib/modelPull.ts`
- Modify: `frontend/src/components/SettingsDialog.svelte` (use `modelPull` store + helpers; use the shared `settingsTab` store)
- Modify: `frontend/src/lib/settingsTabs.ts` (export `settingsTab` store)
- Modify: `frontend/src/lib/autoFetch.ts` (`defaultDeps.fetch` via new `fetchWithTask`)
- Modify: `frontend/src/App.svelte` (global `cmdlog:entry` → `noteCommand`)
- Test: `frontend/src/lib/clone.test.ts`, `frontend/src/lib/modelPull.test.ts` (new), `frontend/src/lib/autoFetch.test.ts`

**Interfaces:**
- Consumes: `beginTask`, `finishTask`, `setTaskProgress`, `hasTask`, `noteCommand`, `taskState`, `EMPTY` (Task 2).
- Produces: `cloneProgressText(p: CloneProgress | null): string`; `modelPull: Writable<ModelProgress | null>`, `startModelPull(name, pull)`, `modelProgress(p)`, `modelDone(p)`, `pullPercent(p)`; `fetchWithTask(id, fetch)`; `settingsTab: Writable<string>` exported from `settingsTabs.ts`. Task 5 uses `settingsTab`, `cancelClone`, `openCloneDialog`.

- [ ] **Step 1: Write the failing tests**

`clone.test.ts` (follow the file's existing `startCloneEvents` test harness — it fakes `on` with a map of handlers; reuse it):

```ts
import { EMPTY, taskState } from './tasks'

describe('clone as a status-bar task', () => {
  beforeEach(() => taskState.set(EMPTY))

  it('becomes a task once the backend accepts it, with progress, and ends with the clone', async () => {
    cloneForm.set({ url: 'https://h/foo.git', name: 'foo', nameEdited: false })
    await startClone('/p', async () => {})
    expect(get(taskState).tasks).toMatchObject([{ id: 'clone', kind: 'clone', label: 'Cloning foo', repoId: '' }])
    const handlers: Record<string, (p: any) => void> = {}
    const off = startCloneEvents({ on: ((n: string, f: any) => { handlers[n] = f; return () => {} }) as any, added: async () => {}, toast: () => {} })
    handlers['clone:progress']({ phase: 'Receiving', percent: 42, detail: '' })
    expect(get(taskState).tasks[0].progress).toBe('Receiving 42 %')
    await handlers['clone:done']({ repo: { id: 'n', name: 'foo' }, error: '', cancelled: false })
    expect(get(taskState).tasks).toEqual([])
    expect(get(taskState).last).toMatchObject({ outcome: 'ok', text: 'Cloned foo' })
    off()
  })

  it('a clone that ended before the backend call resolved leaves no task', async () => {
    cloneForm.set({ url: 'https://h/foo.git', name: 'foo', nameEdited: false })
    await startClone('/p', async () => { cloneView.set({ kind: 'form', error: '' }) })
    expect(get(taskState).tasks).toEqual([])
  })

  it('a refused clone leaves no task', async () => {
    cloneForm.set({ url: 'x', name: 'foo', nameEdited: false })
    await startClone('/p', async () => { throw new Error('bad url') })
    expect(get(taskState).tasks).toEqual([])
  })

  it('cancelled and failed clones', async () => {
    for (const [d, outcome] of [[{ repo: null, error: 'git clone cancelled', cancelled: true }, 'cancelled'], [{ repo: null, error: 'not found', cancelled: false }, 'failed']] as const) {
      taskState.set(EMPTY)
      cloneForm.set({ url: 'https://h/foo.git', name: 'foo', nameEdited: false })
      await startClone('/p', async () => {})
      const handlers: Record<string, (p: any) => void> = {}
      startCloneEvents({ on: ((n: string, f: any) => { handlers[n] = f; return () => {} }) as any, added: async () => {}, toast: () => {} })
      await handlers['clone:done'](d)
      expect(get(taskState).last?.outcome).toBe(outcome)
    }
  })
})

describe('cloneProgressText', () => {
  it('shows the phase, with its percentage when git gives one', () => {
    expect(cloneProgressText(null)).toBe('')
    expect(cloneProgressText({ phase: 'Counting', percent: 0, detail: '' })).toBe('Counting')
    expect(cloneProgressText({ phase: 'Receiving', percent: 42, detail: '' })).toBe('Receiving 42 %')
  })
})
```

(Check how `CloneDialog.svelte` renders the phase today; if it maps `phase` codes to words, `cloneProgressText` must use the same mapping — move it into `clone.ts` if it lives in the component, and adjust the expected strings.)

`modelPull.test.ts`:

```ts
import { get } from 'svelte/store'
import { beforeEach, describe, expect, it } from 'vitest'
import { modelDone, modelProgress, modelPull, pullPercent, startModelPull } from './modelPull'
import { EMPTY, taskState } from './tasks'

describe('model download as a status-bar task', () => {
  beforeEach(() => { taskState.set(EMPTY); modelPull.set(null) })

  it('is a task from before the backend call until model:done', async () => {
    await startModelPull('qwen', async () => { expect(get(taskState).tasks).toMatchObject([{ id: 'model', kind: 'model', label: 'Downloading qwen' }]) })
    modelProgress({ name: 'qwen', status: 'pulling', completed: 50, total: 200 })
    expect(get(taskState).tasks[0].progress).toBe('25 %')
    expect(get(modelPull)?.completed).toBe(50)
    modelDone({ name: 'qwen' })
    expect(get(modelPull)).toBeNull()
    expect(get(taskState).last).toMatchObject({ outcome: 'ok', text: 'Downloaded qwen' })
  })

  it('done before the call resolved leaves no task', async () => {
    await startModelPull('qwen', async () => { modelDone({ name: 'qwen', error: 'nope' }) })
    expect(get(taskState).tasks).toEqual([])
    expect(get(taskState).last?.outcome).toBe('failed')
  })

  it('a refused download ends failed and rethrows', async () => {
    await expect(startModelPull('qwen', async () => { throw new Error('ollama down') })).rejects.toThrow('ollama down')
    expect(get(taskState).tasks).toEqual([])
    expect(get(taskState).last).toMatchObject({ outcome: 'failed', message: 'ollama down' })
    expect(get(modelPull)).toBeNull()
  })

  it('cancelled', () => {
    taskState.set(EMPTY)
    void startModelPull('qwen', async () => {})
    modelDone({ name: 'qwen', error: 'canceled', canceled: true })
    expect(get(taskState).last?.outcome).toBe('cancelled')
  })

  it('pullPercent is empty until the size is known', () => {
    expect(pullPercent({ name: 'm', status: '', completed: 0, total: 0 })).toBe('')
    expect(pullPercent({ name: 'm', status: '', completed: 1, total: 3 })).toBe('33 %')
  })
})
```

`autoFetch.test.ts`:

```ts
import { fetchWithTask } from './autoFetch'
import { EMPTY, taskState } from './tasks'

describe('fetchWithTask', () => {
  beforeEach(() => taskState.set(EMPTY))

  it('is an Auto "Fetching" task of the repository while it runs, and never the last result', async () => {
    let seen: unknown[] = []
    await fetchWithTask('a', async () => { seen = get(taskState).tasks; return res() })
    expect(seen).toMatchObject([{ id: 'fetch:a', kind: 'fetch', origin: 'auto', label: 'Fetching', repoId: 'a' }])
    expect(get(taskState).tasks).toEqual([])
    expect(get(taskState).last).toBeNull()
    await expect(fetchWithTask('a', async () => { throw new Error('auth') })).rejects.toThrow('auth')
    expect(get(taskState).tasks).toEqual([])
    expect(get(taskState).last).toBeNull()
  })
})
```

(`res` and `get` exist in or are importable into this file; add `get` from `svelte/store` if missing.)

- [ ] **Step 2: Run them to see them fail**

Run: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run src/lib/clone.test.ts src/lib/modelPull.test.ts src/lib/autoFetch.test.ts`
Expected: FAIL (missing exports / no tasks).

- [ ] **Step 3: Implement**

`clone.ts`:

```ts
import { beginTask, finishTask, hasTask, setTaskProgress } from './tasks'

/** The clone's progress as the status bar shows it. */
export function cloneProgressText(p: CloneProgress | null): string {
  if (!p) return ''
  return p.percent > 0 ? `${p.phase} ${p.percent} %` : p.phase
}
```

In `startClone`, after the `status` block and before `return true`:

```ts
  // clone:done may already have arrived (a tiny clone): only a clone still
  // running becomes a task, or it would never end.
  if (get(cloneView).kind === 'running' && !hasTask('clone')) {
    beginTask({ id: 'clone', kind: 'clone', origin: 'you', label: `Cloning ${name}`, repoId: '', repoName: '' })
  }
```

In `startCloneEvents`: the progress handler also calls `setTaskProgress('clone', cloneProgressText(p))`; the done handler, first thing:

```ts
    finishTask('clone', d.cancelled ? 'cancelled' : d.repo && !d.error ? 'ok' : 'failed', d.error)
```

`modelPull.ts`:

```ts
import { writable } from 'svelte/store'
import { beginTask, finishTask, setTaskProgress } from './tasks'
import type { ModelDone, ModelProgress } from './types'
import { errorMessage } from './ui'

/** The Ollama download in progress, for Settings → Providers and the
 *  status bar; null when none. */
export const modelPull = writable<ModelProgress | null>(null)

export function pullPercent(p: ModelProgress): string {
  return p.total > 0 ? `${Math.floor((p.completed / p.total) * 100)} %` : ''
}

/** startModelPull registers the download before asking the backend, so a
 *  model:done that beats the call's own return still ends it. */
export async function startModelPull(name: string, pull: (name: string) => Promise<void>): Promise<void> {
  modelPull.set({ name, status: 'starting', completed: 0, total: 0 })
  beginTask({ id: 'model', kind: 'model', origin: 'you', label: `Downloading ${name}`, repoId: '', repoName: '' })
  try {
    await pull(name)
  } catch (e) {
    modelPull.set(null)
    finishTask('model', 'failed', errorMessage(e))
    throw e
  }
}

export function modelProgress(p: ModelProgress): void {
  modelPull.set(p)
  setTaskProgress('model', pullPercent(p))
}

export function modelDone(p: ModelDone): void {
  modelPull.set(null)
  finishTask('model', p.canceled ? 'cancelled' : p.error ? 'failed' : 'ok', p.error ?? '')
}
```

`SettingsDialog.svelte`: delete the local `let pull`; import `modelPull`, `startModelPull`, `modelProgress`, `modelDone`; the listeners become `EventsOn('model:progress', modelProgress)` and, in the `model:done` handler, `modelDone(p)` instead of `pull = null` (keep its toast and `refresh()`); `startPull` becomes:

```ts
  async function startPull(name: string) {
    const model = name.trim()
    if (!model) return
    try {
      await startModelPull(model, api.pullModel)
    } catch (e) {
      toast(errorMessage(e), 'error')
    }
  }
```

and the template reads `$modelPull` where it read `pull`.

Move the settings tab store so the status bar can open Providers: in `settingsTabs.ts` export

```ts
// The tab the Settings dialog opens on: the last one used. Shared so other
// places (the status bar) can open a given tab.
try {
  migrateRememberedTab(localStorage, 'settingsTab')
} catch {
  // Storage unavailable: the tab falls back to General.
}
export const settingsTab = persisted('settingsTab', 'general', isSettingsTab)
```

(import `persisted` from `./stores`; first check `stores.ts` does not import `settingsTabs.ts` — if it does, put the store in `stores.ts` instead.) `SettingsDialog.svelte` drops its own migrate + `persisted` call and uses `const tab = settingsTab`.

`autoFetch.ts`:

```ts
import { beginTask, repoName } from './tasks'

/** fetchWithTask runs one background fetch as an Auto "Fetching" task of
 *  repository id: shown while it runs, never the last result. */
export async function fetchWithTask(id: string, fetch: (id: string) => Promise<AutoFetchResult>): Promise<AutoFetchResult> {
  const end = beginTask({ id: `fetch:${id}`, kind: 'fetch', origin: 'auto', label: 'Fetching', repoId: id, repoName: repoName(id) })
  try {
    const r = await fetch(id)
    end()
    return r
  } catch (e) {
    end(e)
    throw e
  }
}
```

and `defaultDeps.fetch: (id) => fetchWithTask(id, api.autoFetch)`.

`App.svelte` `onMount`: add `const offCommands = EventsOn('cmdlog:entry', noteCommand)` (import `noteCommand` from `./lib/tasks`) with a comment ("Running git writes of every repository, for the status bar — the Commands panel keeps its own listener"), and `offCommands()` in the cleanup.

- [ ] **Step 4: Run the tests**

Run: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run && npm run check`
Expected: all PASS, 0 errors.

- [ ] **Step 5: Commit**

```bash
/usr/bin/git add frontend/src/lib/clone.ts frontend/src/lib/clone.test.ts frontend/src/lib/modelPull.ts frontend/src/lib/modelPull.test.ts frontend/src/lib/autoFetch.ts frontend/src/lib/autoFetch.test.ts frontend/src/lib/settingsTabs.ts frontend/src/components/SettingsDialog.svelte frontend/src/App.svelte
/usr/bin/git commit -m "feat(tasks): clone, model download, background fetch and git writes feed the registry"
```

---

### Task 5: `StatusBar.svelte`, layout, and the specification

**Files:**
- Create: `frontend/src/lib/statusBar.ts` (small pure helpers for the component)
- Create: `frontend/src/components/StatusBar.svelte`
- Modify: `frontend/src/App.svelte` (column shell + `<StatusBar />`)
- Modify: `frontend/src/components/Toasts.svelte` (`bottom` clears the bar)
- Create: `docs/spec/13-status-bar.md`
- Modify: `docs/spec/README.md`, `docs/spec/09-command-log.md`, `docs/spec/01-repositories-and-sidebar.md`, `docs/spec/05-remote-and-stash.md`, `docs/spec/06-ai.md`, `docs/spec/07-conventions-and-constraints.md`
- Test: `frontend/src/lib/statusBar.test.ts`

**Interfaces:**
- Consumes: `shownTasks`, `taskState`, `cancelTarget`, `relativeTime`, `Task`, `TaskResult` (Task 2); `cancelClone`, `cloneCancelling` (`clone.ts`); `openCloneDialog` (`actions.ts`); `settingsTab` (Task 4); `settingsOpen`, `commandsOpen`, `repos`, `selectedRepoId`, `selectRepo` (`stores.ts`); `formatElapsed`, `NOT_RUNNING_MESSAGE` (`cmdlog.ts`); `api.cancelCommand`, `api.cancelPull`, `api.cancelClone`; `toast`, `errorMessage` (`ui.ts`).

- [ ] **Step 1: Write the failing tests** — `frontend/src/lib/statusBar.test.ts`

```ts
import { describe, expect, it } from 'vitest'
import { elapsedText, outcomeIcon, openAction } from './statusBar'
import type { Repo } from './types'

const repos = [{ id: 'a', name: 'alpha', missing: false }, { id: 'm', name: 'gone', missing: true }] as Repo[]

describe('elapsedText', () => {
  it('is empty for the first second, then whole seconds', () => {
    expect(elapsedText(1000, 1999)).toBe('')
    expect(elapsedText(1000, 2000)).toBe('1 s')
    expect(elapsedText(1000, 13_500)).toBe('12 s')
  })
})

describe('outcomeIcon', () => {
  it('maps outcomes to ✓ ✗ ⊘', () => {
    expect(outcomeIcon('ok')).toBe('✓')
    expect(outcomeIcon('failed')).toBe('✗')
    expect(outcomeIcon('cancelled')).toBe('⊘')
  })
})

describe('openAction', () => {
  it('reopens a running clone, but not a finished one', () => {
    expect(openAction({ kind: 'clone', repoId: '' }, true, repos)).toEqual({ kind: 'clone' })
    expect(openAction({ kind: 'clone', repoId: '' }, false, repos)).toBeNull()
  })
  it('opens Settings → Providers for a model download', () => {
    expect(openAction({ kind: 'model', repoId: '' }, false, repos)).toEqual({ kind: 'providers' })
  })
  it('opens the Commands panel of a present repository, nothing otherwise', () => {
    for (const kind of ['op', 'fetch', 'git'] as const) {
      expect(openAction({ kind, repoId: 'a' }, false, repos)).toEqual({ kind: 'commands', repoId: 'a' })
    }
    expect(openAction({ kind: 'op', repoId: 'm' }, false, repos)).toBeNull()
    expect(openAction({ kind: 'op', repoId: 'x' }, false, repos)).toBeNull()
  })
})
```

- [ ] **Step 2: Run them to see them fail**

Run: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run src/lib/statusBar.test.ts`
Expected: FAIL — cannot resolve `./statusBar`.

- [ ] **Step 3: Implement**

`frontend/src/lib/statusBar.ts`:

```ts
import { formatElapsed } from './cmdlog'
import type { TaskKind, TaskOutcome } from './tasks'
import type { Repo } from './types'

/** Elapsed time once a task has run a full second; '' before. */
export function elapsedText(start: number, now: number): string {
  return now - start >= 1000 ? formatElapsed(now - start) : ''
}

export function outcomeIcon(o: TaskOutcome): string {
  return o === 'ok' ? '✓' : o === 'failed' ? '✗' : '⊘'
}

export type OpenAction = { kind: 'clone' } | { kind: 'providers' } | { kind: 'commands'; repoId: string }

/** Where a click on a task or the last result leads; null when nowhere.
 *  cloneRunning: whether this is the clone still in progress. */
export function openAction(t: { kind: TaskKind; repoId: string }, cloneRunning: boolean, list: Repo[]): OpenAction | null {
  if (t.kind === 'clone') return cloneRunning ? { kind: 'clone' } : null
  if (t.kind === 'model') return { kind: 'providers' }
  const repo = list.find((r) => r.id === t.repoId)
  return repo && !repo.missing ? { kind: 'commands', repoId: repo.id } : null
}
```

`frontend/src/components/StatusBar.svelte` — behaviour to implement (spec "Behaviour" section is the reference):

```svelte
<script lang="ts">
  import { onDestroy, tick } from 'svelte'
  import { api } from '../lib/api'
  import { openCloneDialog } from '../lib/actions'
  import { cancelClone } from '../lib/clone'
  import { NOT_RUNNING_MESSAGE } from '../lib/cmdlog'
  import { settingsTab } from '../lib/settingsTabs'
  import { elapsedText, openAction, outcomeIcon } from '../lib/statusBar'
  import { commandsOpen, repos, selectRepo, selectedRepoId, settingsOpen } from '../lib/stores'
  import { cancelTarget, relativeTime, shownTasks, taskState, type Task, type TaskResult } from '../lib/tasks'
  import { errorMessage, toast } from '../lib/ui'

  let now = Date.now()
  const timer = setInterval(() => (now = Date.now()), 1000)
  onDestroy(() => clearInterval(timer))

  let cancelling: Record<string, boolean> = {}
  let listOpen = false
  let chip: HTMLButtonElement
  let listEl: HTMLUListElement

  $: tasks = $shownTasks
  $: shown = tasks[0] ?? null
  $: last = $taskState.last
  // Forget "Cancelling…" for tasks that are gone; close the list when ≤ 1 left.
  $: {
    const ids = new Set(tasks.map((t) => t.id))
    const kept = Object.fromEntries(Object.entries(cancelling).filter(([id]) => ids.has(id)))
    if (Object.keys(kept).length !== Object.keys(cancelling).length) cancelling = kept
  }
  $: if (listOpen && tasks.length <= 1) listOpen = false

  const canCancel = (t: Task) => cancelTarget($taskState, t) !== null

  async function cancel(t: Task) {
    const target = cancelTarget($taskState, t)
    if (!target) return
    cancelling = { ...cancelling, [t.id]: true }
    try {
      if (target.kind === 'clone') await cancelClone(api.cancelClone)
      else if (target.kind === 'model') await api.cancelPull()
      else await api.cancelCommand(target.repoId, target.entryId)
    } catch (e) {
      const message = errorMessage(e)
      if (!message.includes(NOT_RUNNING_MESSAGE)) {
        toast(message, 'error')
        cancelling = { ...cancelling, [t.id]: false }
      }
    }
  }

  function open(t: Pick<Task | TaskResult, 'kind' | 'repoId'>, running: boolean) {
    const a = openAction(t, running && t.kind === 'clone', $repos)
    listOpen = false
    if (!a) return
    if (a.kind === 'clone') void openCloneDialog()
    else if (a.kind === 'providers') {
      settingsTab.set('providers')
      settingsOpen.set(true)
    } else {
      if (a.repoId !== $selectedRepoId) selectRepo(a.repoId)
      commandsOpen.set(true)
    }
  }

  async function toggleList() {
    listOpen = !listOpen
    if (listOpen) {
      await tick()
      listEl?.querySelector<HTMLElement>('[data-row]')?.focus()
    }
  }

  function closeList() {
    listOpen = false
    chip?.focus()
  }

  function onListKey(e: KeyboardEvent) {
    const rows = [...(listEl?.querySelectorAll<HTMLElement>('[data-row]') ?? [])]
    const i = rows.indexOf(document.activeElement as HTMLElement)
    if (e.key === 'Escape') { e.preventDefault(); closeList() }
    else if (e.key === 'ArrowDown') { e.preventDefault(); rows[Math.min(i + 1, rows.length - 1)]?.focus() }
    else if (e.key === 'ArrowUp') { e.preventDefault(); rows[Math.max(i - 1, 0)]?.focus() }
  }

  function onWindowClick(e: MouseEvent) {
    if (!listOpen) return
    const el = e.target as Node
    if (!listEl?.contains(el) && !chip?.contains(el)) listOpen = false
  }
</script>

<svelte:window on:mousedown={onWindowClick} />

<footer class="statusbar">
  <!-- Only the label is live: elapsed time and progress tick outside it. -->
  {#if shown}
    <button class="main" title={shown.label + (shown.repoName ? ` · ${shown.repoName}` : '')} on:click={() => open(shown, true)}>
      <span class="spinner" aria-hidden="true"></span>
      <span class="label" role="status" aria-live="polite">{shown.label}</span>
      {#if shown.repoName}<span class="muted">· {shown.repoName}</span>{/if}
      {#if shown.progress}<span class="muted">· {shown.progress}</span>{/if}
      {#if elapsedText(shown.start, now)}<span class="muted" aria-hidden="true">· {elapsedText(shown.start, now)}</span>{/if}
    </button>
    {#if canCancel(shown)}
      <button class="btn small" disabled={cancelling[shown.id]} on:click={() => cancel(shown)}>{cancelling[shown.id] ? 'Cancelling…' : 'Cancel'}</button>
    {/if}
  {:else if last}
    <button class="main result" class:error={last.outcome === 'failed'} title={last.message || last.text} on:click={() => open(last, false)}>
      <span class="label" role="status" aria-live="polite">{outcomeIcon(last.outcome)} {last.text}</span>
      {#if last.repoName}<span class="muted">· {last.repoName}</span>{/if}
      <span class="muted">· {relativeTime(now - last.end)}</span>
    </button>
  {:else}
    <span class="label" role="status" aria-live="polite"></span>
  {/if}
  <span class="spacer"></span>
  {#if tasks.length > 1}
    <button bind:this={chip} class="chip" aria-haspopup="true" aria-expanded={listOpen} on:click={toggleList}>+{tasks.length - 1}</button>
  {/if}
  {#if listOpen}
    <!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
    <ul class="list" bind:this={listEl} aria-label="Running tasks" on:keydown={onListKey}>
      {#each tasks as t (t.id)}
        <li>
          <button data-row class="row" on:click={() => open(t, true)}>
            <span class="spinner" aria-hidden="true"></span>
            <span class="label">{t.label}</span>
            {#if t.repoName}<span class="muted">· {t.repoName}</span>{/if}
            {#if t.progress}<span class="muted">· {t.progress}</span>{/if}
            {#if elapsedText(t.start, now)}<span class="muted">· {elapsedText(t.start, now)}</span>{/if}
          </button>
          {#if canCancel(t)}
            <button class="btn small" disabled={cancelling[t.id]} on:click={() => cancel(t)}>{cancelling[t.id] ? 'Cancelling…' : 'Cancel'}</button>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</footer>

<style>
  .statusbar { position: relative; flex: none; height: 22px; display: flex; align-items: center; gap: 6px; padding: 0 8px; border-top: 1px solid var(--border); background: var(--sidebar); font-size: 12px; color: var(--text); min-width: 0; }
  .main { display: flex; align-items: center; gap: 4px; min-width: 0; overflow: hidden; background: none; border: 0; padding: 0; color: inherit; font: inherit; cursor: pointer; }
  .label { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; min-width: 0; }
  .muted { color: var(--text-muted); white-space: nowrap; }
  .result.error .label { color: var(--danger); }
  .spacer { flex: 1; }
  .chip { font: inherit; font-size: 11px; padding: 0 6px; border-radius: 8px; border: 1px solid var(--border); background: var(--surface); color: inherit; cursor: pointer; }
  .list { position: absolute; right: 8px; bottom: 26px; z-index: 50; margin: 0; padding: 4px; list-style: none; min-width: 320px; max-width: min(560px, calc(100vw - 16px)); max-height: 50vh; overflow: auto; background: var(--surface); border: 1px solid var(--border); border-radius: 6px; box-shadow: 0 4px 16px rgba(0, 0, 0, 0.2); }
  .list li { display: flex; align-items: center; gap: 6px; }
  .row { flex: 1; display: flex; align-items: center; gap: 4px; min-width: 0; padding: 4px 6px; background: none; border: 0; color: inherit; font: inherit; text-align: left; cursor: pointer; border-radius: 4px; }
  .row:hover, .row:focus-visible { background: var(--hover); }
  .spinner { flex: none; width: 10px; height: 10px; border: 2px solid var(--border); border-top-color: var(--accent); border-radius: 50%; animation: spin 0.8s linear infinite; }
  .btn.small { font-size: 11px; padding: 0 6px; height: 18px; }
  @keyframes spin { to { transform: rotate(360deg); } }
</style>
```

Before writing the styles, check the real token names in the app's global stylesheet (`grep -n -- "--" frontend/src/style.css` or wherever `:root` tokens are defined) and the spinner the Commands panel already uses (`CommandLogPanel.svelte`) — reuse its spinner class/markup instead of a new one if it is global, and replace any token above that does not exist (`--text-muted`, `--danger`, `--hover`, `--accent`, `--sidebar`, `--surface`, `--border`, `--text`) with the existing equivalent. Check the High contrast theme still gives the bar a visible top border.

`App.svelte`: wrap the existing `<div class="app">…</div>` and the bar in a column:

```svelte
<div class="shell">
  <div class="app">…unchanged…</div>
  <StatusBar />
</div>
```

```css
  .shell { display: flex; flex-direction: column; height: 100%; }
  .app { display: flex; flex: 1; min-height: 0; }
```

(replacing `.app { display: flex; height: 100%; }`). Import `StatusBar`.

`Toasts.svelte`: `.toasts { … bottom: 38px; … }` (16 px above the 22 px bar). Check other `position: fixed; bottom:` elements (`grep -rn "bottom:" frontend/src/components`) and lift any that would now sit on the bar.

Specification — write `docs/spec/13-status-bar.md` from the design spec's "Behaviour" section, in the specification's own voice (what the app does, no file or component names; see `09-command-log.md` for tone): layout, which task is shown, Cancel, "+N" list and its keys, last result and its texts (with the past-tense rule and the fallback), clicking, accessibility, and what is not a task (AI chat thinking, commit-message generation). Then:
- `README.md`: add the row `| [Status bar](13-status-bar.md) | The bar along the bottom of the window: the task running now, Cancel, and how the last one ended |` before "Conventions and constraints".
- `09-command-log.md`, "Running commands and Cancel": one sentence — a running write can also be seen and cancelled from the status bar (`13-status-bar.md`), within its operation when it belongs to one.
- `01-repositories-and-sidebar.md`, the clone paragraph: a clone continued in the background shows in the status bar with its phase and percentage; clicking it reopens the progress; its Cancel stops it as the dialog's does.
- `05-remote-and-stash.md`, "Background fetch": each repository's background fetch shows in the status bar as an automatic "Fetching" task while it runs and never becomes the bar's last result.
- `06-ai.md`, where the Ollama model download is described: it shows in the status bar with its percentage and Cancel, and clicking it opens Settings → Providers.
- `07-conventions-and-constraints.md`, where long operations announce themselves (the busy label): every operation with a busy label also shows in the status bar, and its end becomes the bar's last result.

- [ ] **Step 4: Run the tests and the type check**

Run: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run && npm run check && cd .. && go test ./internal/...`
Expected: all PASS, 0 svelte-check errors and no new warnings in `StatusBar.svelte`.

- [ ] **Step 5: Commit**

```bash
/usr/bin/git add frontend/src/lib/statusBar.ts frontend/src/lib/statusBar.test.ts frontend/src/components/StatusBar.svelte frontend/src/App.svelte frontend/src/components/Toasts.svelte docs/spec/
/usr/bin/git commit -m "feat(ui): status bar with the running task, Cancel, +N list and last result"
```

---

### Final: spec-compliance review (whole branch)

Not an implementation task — the controller dispatches a reviewer over `main..HEAD` whose brief is: check every requirement of `docs/superpowers/specs/2026-10-09-status-bar-design.md` and of the new/changed `docs/spec/` pages against the code (not only the diff), list gaps as Critical/Important/Minor, and confirm the five Review Focus items hold. Gaps are fixed before `make dev` is run for the owner's manual pass.
