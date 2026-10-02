# Repository settings (remotes) + grouped repository menu — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A per-repository settings dialog whose Remotes tab lists, adds, edits, removes and tests remotes; and a repository context menu grouped with separators.

**Architecture:** Go `internal/ops/remotes.go` runs the git commands; `internal/app/remotes.go` exposes them to Wails (writes under `a.write`). The frontend gets a separator entry for menus, a pure `repoMenu.ts` for the repository menu's order, a pure `remoteForm.ts` for validation, and `RepoSettingsDialog.svelte`.

**Tech Stack:** Go + Wails v2.16, Svelte + TypeScript, vitest.

**Spec:** `docs/superpowers/specs/2026-10-02-repo-settings-remotes-design.md`

## Global Constraints

- Every behaviour change commits with its `docs/spec` update. Commits without `Co-Authored-By`. Git as `/usr/bin/git`.
- Go tests `go test ./internal/...`; frontend `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run`; types `npm run check`.
- Bindings: `cd frontend && npm run build` first, then `~/go/bin/wails generate module`, then `/usr/bin/git checkout -- frontend/wailsjs/runtime`; commit `frontend/wailsjs/go/app/App.{d.ts,js}` and `frontend/wailsjs/go/models.ts` (new `ops.Remote`, `ops.RemoteTest`).
- Texts, exactly: menu "Repository settings…"; disabled tooltip "The repository's folder is missing"; dialog title "<name> settings"; tab "Remotes"; "No remotes yet."; "Add remote"; "Testing…"; "Connected"; "Authentication failed"; "Timed out"; "A remote named <name> already exists"; confirm title "Remove remote <name>?", body "Its remote branches leave the log, and branches that track it lose their upstream.", button "Remove".
- Test timeout 30 s; separator height 9 px in the menu clamp.

## Review Focus

- A URL with spaces or a name starting with `-` must not be read as a git option — `--` before positional args (Task 1 test: name `-x` fails with git's error, nothing else happens).
- Removing the remote the checked-out branch tracks must leave the app consistent: the sidebar, log and toolbar refresh (Task 5 calls `refreshRepo` when the repo is selected).
- Escape while a confirm dialog sits on top of Repository settings must close only the confirm — Task 5 (guard on `$dialog`).
- Opening Repository settings for a non-selected repository must act on that repository, not the selected one — Task 5 (every call uses `$repoSettings.repoID`).
- A Test hanging on an unreachable host must not lock the repository — Task 1/2 (`TestRemote` is not under the write lock; 30 s timeout).

---

### Task 1: ops — list, add, set-url, remove, test

**Files:**
- Create: `internal/ops/remotes.go`, `internal/ops/remotes_test.go`

**Interfaces:**
- Produces:
  - `type Remote struct { Name string \`json:"name"\`; FetchURL string \`json:"fetchURL"\`; PushURL string \`json:"pushURL"\` }`
  - `type RemoteTest struct { OK bool \`json:"ok"\`; Message string \`json:"message"\` }`
  - `func ListRemotes(ctx context.Context, dir string) ([]Remote, error)`
  - `func AddRemote(ctx context.Context, dir, name, url string) error`
  - `func SetRemoteURL(ctx context.Context, dir, name, url string) error`
  - `func RemoveRemote(ctx context.Context, dir, name string) error`
  - `func TestRemote(ctx context.Context, dir, name string) (RemoteTest, error)`
  - `const RemoteTestTimeout = 30 * time.Second`

- [ ] **Step 1: Failing tests** — `internal/ops/remotes_test.go`:

```go
package ops_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"git-ui/internal/ops"
	"git-ui/internal/testrepo"
)

func TestRemotesListAddSetRemove(t *testing.T) {
	ctx := context.Background()
	r := testrepo.New(t)
	r.Commit("base")
	got, err := ops.ListRemotes(ctx, r.Dir)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty repo: %#v, %v", got, err)
	}
	if err := ops.AddRemote(ctx, r.Dir, " upstream ", " https://example.com/u.git "); err != nil {
		t.Fatal(err)
	}
	if err := ops.AddRemote(ctx, r.Dir, "origin", "https://example.com/o.git"); err != nil {
		t.Fatal(err)
	}
	got, _ = ops.ListRemotes(ctx, r.Dir)
	want := []ops.Remote{
		{Name: "origin", FetchURL: "https://example.com/o.git", PushURL: "https://example.com/o.git"},
		{Name: "upstream", FetchURL: "https://example.com/u.git", PushURL: "https://example.com/u.git"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	if err := ops.AddRemote(ctx, r.Dir, "origin", "x"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate add: %v", err)
	}
	if err := ops.AddRemote(ctx, r.Dir, "-x", "y"); err == nil {
		t.Fatal("a name starting with - must fail, not be read as an option")
	}
	if err := ops.AddRemote(ctx, r.Dir, "", "y"); err == nil {
		t.Fatal("empty name must fail")
	}
	if err := ops.SetRemoteURL(ctx, r.Dir, "origin", "https://example.com/new.git"); err != nil {
		t.Fatal(err)
	}
	if u := r.Git("remote", "get-url", "origin"); u != "https://example.com/new.git" {
		t.Fatalf("url = %q", u)
	}
	if err := ops.SetRemoteURL(ctx, r.Dir, "origin", "  "); err == nil {
		t.Fatal("empty url must fail")
	}
	if err := ops.RemoveRemote(ctx, r.Dir, "upstream"); err != nil {
		t.Fatal(err)
	}
	got, _ = ops.ListRemotes(ctx, r.Dir)
	if len(got) != 1 || got[0].Name != "origin" {
		t.Fatalf("after remove: %+v", got)
	}
}

func TestRemoteTest(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	ctx := context.Background()
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("remote", "add", "good", testrepo.NewBareFrom(t, r))
	r.Git("remote", "add", "locked", lockedServer(t))
	r.Git("remote", "add", "gone", filepath.Join(t.TempDir(), "missing.git"))

	for _, c := range []struct {
		name string
		ok   bool
		msg  string
	}{
		{"good", true, "Connected"},
		{"locked", false, "Authentication failed"},
	} {
		res, err := ops.TestRemote(ctx, r.Dir, c.name)
		if err != nil || res.OK != c.ok || res.Message != c.msg {
			t.Errorf("%s: %+v, %v", c.name, res, err)
		}
	}
	res, err := ops.TestRemote(ctx, r.Dir, "gone")
	if err != nil || res.OK || res.Message == "" || strings.Contains(res.Message, "\n") {
		t.Errorf("gone: %+v, %v", res, err)
	}
}
```

(`lockedServer` already exists in `internal/ops/autofetch_test.go`, same package.)

- [ ] **Step 2: Run** — `go test ./internal/ops/ -run 'TestRemotes|TestRemoteTest'` → build FAIL (undefined).

- [ ] **Step 3: Implement** — `internal/ops/remotes.go`:

```go
package ops

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"git-ui/internal/gitcmd"
)

// RemoteTestTimeout bounds a connection test: long enough for a slow
// server, short enough that the Remotes tab never seems stuck.
const RemoteTestTimeout = 30 * time.Second

// Remote is one configured remote (docs/spec/12-repository-settings.md).
type Remote struct {
	Name     string `json:"name"`
	FetchURL string `json:"fetchURL"`
	PushURL  string `json:"pushURL"`
}

// RemoteTest is how a connection test ended: Message is "Connected", or
// why not, in one line.
type RemoteTest struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

var errEmptyRemoteField = errors.New("a remote needs a name and a URL")

// ListRemotes reads `git remote -v`, sorted by name; empty, not nil.
func ListRemotes(ctx context.Context, dir string) ([]Remote, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "remote", "-v")
	if err != nil {
		return nil, err
	}
	byName := map[string]*Remote{}
	for _, line := range strings.Split(out, "\n") {
		// "<name>\t<url> (fetch)" or "(push)"
		name, rest, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		i := strings.LastIndex(rest, " (")
		if i < 0 {
			continue
		}
		url, kind := rest[:i], strings.TrimSuffix(rest[i+2:], ")")
		r := byName[name]
		if r == nil {
			r = &Remote{Name: name}
			byName[name] = r
		}
		if kind == "push" {
			r.PushURL = url
		} else {
			r.FetchURL = url
		}
	}
	list := []Remote{}
	for _, r := range byName {
		list = append(list, *r)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, nil
}

func AddRemote(ctx context.Context, dir, name, url string) error {
	name, url = strings.TrimSpace(name), strings.TrimSpace(url)
	if name == "" || url == "" {
		return errEmptyRemoteField
	}
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "remote", "add", "--", name, url)
	return err
}

func SetRemoteURL(ctx context.Context, dir, name, url string) error {
	name, url = strings.TrimSpace(name), strings.TrimSpace(url)
	if name == "" || url == "" {
		return errEmptyRemoteField
	}
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "remote", "set-url", "--", name, url)
	return err
}

// RemoveRemote removes a remote with its remote-tracking branches; branches
// that tracked it lose their upstream (git's own behaviour).
func RemoveRemote(ctx context.Context, dir, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errEmptyRemoteField
	}
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "remote", "remove", "--", name)
	return err
}

// TestRemote asks the remote for its branches without ever prompting. A
// failure to reach it is a result, not an error.
func TestRemote(ctx context.Context, dir, name string) (RemoteTest, error) {
	_, err := gitcmd.RunEnv(ctx, dir, RemoteTestTimeout, NoPromptEnv, "ls-remote", "--heads", "--", strings.TrimSpace(name))
	switch {
	case err == nil:
		return RemoteTest{OK: true, Message: "Connected"}, nil
	case IsAuthError(err):
		return RemoteTest{Message: "Authentication failed"}, nil
	case errors.Is(err, gitcmd.ErrTimeout):
		return RemoteTest{Message: "Timed out"}, nil
	}
	var gerr *gitcmd.Error
	if !errors.As(err, &gerr) {
		return RemoteTest{}, err
	}
	return RemoteTest{Message: firstNonEmptyLine(gerr.Stderr, err.Error())}, nil
}

func firstNonEmptyLine(texts ...string) string {
	for _, t := range texts {
		for _, l := range strings.Split(t, "\n") {
			if l = strings.TrimSpace(l); l != "" {
				return l
			}
		}
	}
	return ""
}
```

**Check first** that `NoPromptEnv` is the exported name in `internal/ops/autofetch.go` (it is used by `AutoFetch`), and that `ls-remote` with `--` before the remote name is accepted by the installed git (`git ls-remote --heads -- origin`); if `--` is rejected, drop it for ls-remote only (the name is validated by having to exist as a remote) and ledger the ruling.

- [ ] **Step 4: Run** — `go test ./internal/ops/ -run 'TestRemotes|TestRemoteTest' -v` → PASS.

- [ ] **Step 5: Commit**

```bash
/usr/bin/git add internal/ops/remotes.go internal/ops/remotes_test.go
/usr/bin/git commit -m "feat(ops): list, add, set-url, remove and test remotes"
```

---

### Task 2: app — Wails methods, pause cleanup, bindings

**Files:**
- Create: `internal/app/remotes.go`, `internal/app/remotes_test.go`
- Modify: `internal/app/autopause.go` (add `forget`)
- Regenerate: `frontend/wailsjs/go/app/App.{d.ts,js}`, `frontend/wailsjs/go/models.ts`

**Interfaces:**
- Consumes: Task 1's ops functions and types.
- Produces: `App.ListRemotes(id string) ([]ops.Remote, error)`, `App.AddRemote(id, name, url string) error`, `App.SetRemoteURL(id, name, url string) error`, `App.RemoveRemote(id, name string) error`, `App.TestRemote(id, name string) (ops.RemoteTest, error)`; `func (p *autoPause) forget(key, remote string)`.

- [ ] **Step 1: Failing test** — `internal/app/remotes_test.go`:

```go
package app

import (
	"testing"

	"git-ui/internal/cmdlog"
	"git-ui/internal/testrepo"
)

func TestRemotesThroughTheAppLayer(t *testing.T) {
	a, r, id := newPlainApp(t)
	bare := testrepo.NewBareFrom(t, r)
	if err := a.AddRemote(id, "origin", bare); err != nil {
		t.Fatal(err)
	}
	if err := a.AddRemote(id, "backup", bare); err != nil {
		t.Fatal(err)
	}
	list, err := a.ListRemotes(id)
	if err != nil || len(list) != 2 || list[0].Name != "backup" {
		t.Fatalf("list = %+v, %v", list, err)
	}
	res, err := a.TestRemote(id, "origin")
	if err != nil || !res.OK {
		t.Fatalf("test = %+v, %v", res, err)
	}

	dir, _ := a.dir(id)
	key := cmdlog.RepoKey(dir)
	a.paused.pause(key, []string{"origin", "backup"})
	if err := a.SetRemoteURL(id, "origin", bare); err != nil {
		t.Fatal(err)
	}
	if a.paused.paused(key, "origin") || !a.paused.paused(key, "backup") {
		t.Fatal("set-url must clear only that remote's pause")
	}
	if err := a.RemoveRemote(id, "backup"); err != nil {
		t.Fatal(err)
	}
	if a.paused.paused(key, "backup") {
		t.Fatal("remove must clear the remote's pause")
	}
	if list, _ = a.ListRemotes(id); len(list) != 1 {
		t.Fatalf("after remove: %+v", list)
	}
	if _, err := a.ListRemotes("no-such-id"); err == nil {
		t.Error("want an error for an unknown repository")
	}
}
```

- [ ] **Step 2: Run** — `go test ./internal/app/ -run TestRemotesThroughTheAppLayer` → build FAIL.

- [ ] **Step 3: Implement**

`internal/app/autopause.go`, after `resume`:

```go
// forget unpauses one remote of key: it was removed or now points
// elsewhere, so its old credential failure no longer says anything.
func (p *autoPause) forget(key, remote string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.remotes[key], remote)
}
```

`internal/app/remotes.go`:

```go
package app

import (
	"context"

	"git-ui/internal/cmdlog"
	"git-ui/internal/ops"
)

// The Remotes tab of Repository settings (docs/spec/12-repository-settings.md).

func (a *App) ListRemotes(id string) ([]ops.Remote, error) {
	dir, err := a.dir(id)
	if err != nil {
		return nil, err
	}
	return ops.ListRemotes(a.ctx, dir)
}

func (a *App) AddRemote(id, name, url string) error {
	return a.write(id, func(ctx context.Context, dir string) error { return ops.AddRemote(ctx, dir, name, url) })
}

func (a *App) SetRemoteURL(id, name, url string) error {
	return a.write(id, func(ctx context.Context, dir string) error {
		if err := ops.SetRemoteURL(ctx, dir, name, url); err != nil {
			return err
		}
		a.paused.forget(cmdlog.RepoKey(dir), name)
		return nil
	})
}

func (a *App) RemoveRemote(id, name string) error {
	return a.write(id, func(ctx context.Context, dir string) error {
		if err := ops.RemoveRemote(ctx, dir, name); err != nil {
			return err
		}
		a.paused.forget(cmdlog.RepoKey(dir), name)
		return nil
	})
}

// TestRemote is a read: it never takes the write lock, so a slow server
// can't block the repository's other operations.
func (a *App) TestRemote(id, name string) (ops.RemoteTest, error) {
	dir, err := a.dir(id)
	if err != nil {
		return ops.RemoteTest{}, err
	}
	return ops.TestRemote(a.ctx, dir, name)
}
```

Note: `name` from the frontend is already trimmed (Task 4's form trims), and ops trims again; `forget` uses the untrimmed `name`, so trim it: use `strings.TrimSpace(name)` in both `forget` calls (add `"strings"` import).

- [ ] **Step 4: Run** — `go test ./internal/app/ -run 'Remote|Pause' -v` → PASS.

- [ ] **Step 5: Bindings** — per Global Constraints. Check `App.d.ts` has the five functions and `models.ts` has `ops.Remote` and `ops.RemoteTest`.

- [ ] **Step 6: Commit**

```bash
/usr/bin/git add internal/app/remotes.go internal/app/remotes_test.go internal/app/autopause.go frontend/wailsjs/go
/usr/bin/git commit -m "feat(app): remote management methods; removing or re-pointing a remote clears its pause"
```

---

### Task 3: menu separators + grouped repository menu

**Files:**
- Modify: `frontend/src/lib/ui.ts` (`MenuSeparator`, `MenuEntry`, `tidySeparators`, store/openMenu types)
- Modify: `frontend/src/components/ContextMenu.svelte`
- Create: `frontend/src/lib/repoMenu.ts`, `frontend/src/lib/repoMenu.test.ts`
- Test: `frontend/src/lib/ui.test.ts` (create if missing; else add)
- Modify: `frontend/src/components/RepoRow.svelte`
- Modify: `frontend/src/lib/stores.ts` (`repoSettings` store — needed by the menu action)
- Modify: `docs/spec/01-repositories-and-sidebar.md`

**Interfaces:**
- Produces:
  - `ui.ts`: `export interface MenuSeparator { separator: true }`; `export type MenuEntry = MenuItem | MenuSeparator`; `export const SEPARATOR: MenuSeparator = { separator: true }`; `export function isSeparator(e: MenuEntry): e is MenuSeparator`; `export function tidySeparators(entries: MenuEntry[]): MenuEntry[]`; `menu` store and `openMenu`/`openMenuAsync` take/return `MenuEntry[]`.
  - `repoMenu.ts`: `export type RepoMenuId = 'locate' | 'fetch' | 'pull' | 'push' | 'reveal' | 'terminal' | 'settings' | 'move' | 'remove' | 'remove-worktree'`; `export function repoMenuGroups(r: { missing: boolean; worktree: boolean; child: boolean }): RepoMenuId[][]`.
  - `stores.ts`: `export const repoSettings = writable<{ repoID: string } | null>(null)`.

- [ ] **Step 1: Failing tests**

`frontend/src/lib/repoMenu.test.ts`:

```ts
import { describe, expect, it } from 'vitest'
import { repoMenuGroups } from './repoMenu'

describe('repoMenuGroups', () => {
  it('groups a main repository', () => {
    expect(repoMenuGroups({ missing: false, worktree: false, child: false })).toEqual([
      ['fetch', 'pull', 'push'], ['reveal', 'terminal'], ['settings'], ['move', 'remove'],
    ])
  })
  it('puts Locate first when the folder is missing', () => {
    expect(repoMenuGroups({ missing: true, worktree: false, child: false })[0]).toEqual(['locate'])
  })
  it('a nested worktree row cannot move to a group', () => {
    expect(repoMenuGroups({ missing: false, worktree: false, child: true }).at(-1)).toEqual(['remove'])
  })
  it('a linked worktree has no settings and removes the worktree', () => {
    expect(repoMenuGroups({ missing: false, worktree: true, child: true })).toEqual([
      ['fetch', 'pull', 'push'], ['reveal', 'terminal'], ['remove-worktree'],
    ])
  })
})
```

In `frontend/src/lib/ui.test.ts` (check whether it exists; it may need the same mocks other tests use for `ui.ts`'s wailsjs import — copy the `vi.mock` lines from an existing test that imports `./ui`, e.g. `grep -l "from './ui'" src/lib/*.test.ts`):

```ts
import { describe, expect, it } from 'vitest'
import { SEPARATOR, tidySeparators, type MenuEntry } from './ui'

describe('tidySeparators', () => {
  const a = { label: 'A', action: () => {} }
  const b = { label: 'B', action: () => {} }
  it('drops leading, trailing and doubled separators', () => {
    const entries: MenuEntry[] = [SEPARATOR, a, SEPARATOR, SEPARATOR, b, SEPARATOR]
    expect(tidySeparators(entries)).toEqual([a, SEPARATOR, b])
  })
  it('leaves an empty list empty', () => {
    expect(tidySeparators([SEPARATOR])).toEqual([])
  })
})
```

- [ ] **Step 2: Run** — `npx vitest run src/lib/repoMenu.test.ts src/lib/ui.test.ts` → FAIL.

- [ ] **Step 3: Implement**

`ui.ts`:

```ts
export interface MenuSeparator { separator: true }
export type MenuEntry = MenuItem | MenuSeparator
export const SEPARATOR: MenuSeparator = { separator: true }
export const isSeparator = (e: MenuEntry): e is MenuSeparator => 'separator' in e

/** Drops separators at either end and runs of them, so menus can build
 *  their groups conditionally. */
export function tidySeparators(entries: MenuEntry[]): MenuEntry[] {
  const out: MenuEntry[] = []
  for (const e of entries) {
    if (isSeparator(e) && (out.length === 0 || isSeparator(out[out.length - 1]))) continue
    out.push(e)
  }
  if (out.length && isSeparator(out[out.length - 1])) out.pop()
  return out
}
```

Change `menu`'s item type, `openMenu(event, items: MenuEntry[])` and `openMenuAsync(event, build: () => Promise<MenuEntry[]>)` to `MenuEntry[]`, and have both store `tidySeparators(items)`.

`ContextMenu.svelte`: import `isSeparator`, `type MenuEntry`; the clamp's height becomes `menuHeight($menu.items)` where `const menuHeight = (items: MenuEntry[]) => items.reduce((h, e) => h + (isSeparator(e) ? 9 : 30), 0)`; in the loop:

```svelte
    {#each $menu.items as item}
      {#if isSeparator(item)}
        <div class="sep" role="separator"></div>
      {:else}
        <button …existing button…>{item.label}</button>
      {/if}
    {/each}
```

and style `.sep { height: 1px; margin: 4px 6px; background: var(--border); }`. `choose(item: MenuItem)` keeps its type.

`repoMenu.ts`:

```ts
/** The repository row's context menu, as groups the menu separates
 *  (docs/spec/01-repositories-and-sidebar.md). Pure, so the order is tested. */
export type RepoMenuId = 'locate' | 'fetch' | 'pull' | 'push' | 'reveal' | 'terminal' | 'settings' | 'move' | 'remove' | 'remove-worktree'

export function repoMenuGroups(r: { missing: boolean; worktree: boolean; child: boolean }): RepoMenuId[][] {
  const sync: RepoMenuId[] = ['fetch', 'pull', 'push']
  const open: RepoMenuId[] = ['reveal', 'terminal']
  if (r.worktree) return [sync, open, ['remove-worktree']]
  return [
    ...(r.missing ? [['locate'] as RepoMenuId[]] : []),
    sync,
    open,
    ['settings'],
    r.child ? ['remove'] : ['move', 'remove'],
  ]
}
```

`stores.ts`, next to `settingsOpen`:

```ts
/** The repository whose Repository settings dialog is open. */
export const repoSettings = writable<{ repoID: string } | null>(null)
```

`RepoRow.svelte`: build both menus from `repoMenuGroups`. Replace the body of `repoMenu` so that each group's ids map to the existing `MenuItem`s, with `SEPARATOR` between groups:

```ts
  function entries(groups: RepoMenuId[][], items: Partial<Record<RepoMenuId, MenuItem>>): MenuEntry[] {
    return groups.flatMap((g, i) => [...(i ? [SEPARATOR] : []), ...g.flatMap((id) => (items[id] ? [items[id]!] : []))])
  }
```

Items for a main repository (same labels, actions and disabled rules as today, plus settings):

```ts
      locate: { label: 'Locate…', action: () => relocateRepo(repo.id) },
      fetch: { label: 'Fetch', action: () => fetchRemote(repo.id), disabled: repo.missing || !!$busy },
      pull: { label: 'Pull', action: () => pull(repo.id), disabled: repo.missing || !!$busy || !!$mergeState?.merging },
      push: { label: 'Push', action: () => push(repo.id), disabled: repo.missing || !!$busy || !!$mergeState?.merging },
      reveal: { label: revealLabel($platform), action: () => openRepoFolder(repo.id), disabled: repo.missing },
      terminal: { label: 'Open in Terminal', action: () => openRepoTerminal(repo.id), disabled: repo.missing },
      settings: { label: 'Repository settings…', action: () => repoSettings.set({ repoID: repo.id }), disabled: repo.missing, title: "The repository's folder is missing" },
      move: { label: 'Move to group…', action: () => moveRepoToGroup(repo) },
      remove: { label: 'Remove from list…', action: () => removeRepo(repo), danger: true },
```

The worktree branch keeps its async `removeItem` logic and maps `remove-worktree` to it; fetch/pull/push/reveal/terminal there keep today's disabled rules (no `repo.missing`). Call `openMenu(event, entries(repoMenuGroups({ missing: repo.missing, worktree: false, child }), items))` and, in `openMenuAsync`, return `entries(repoMenuGroups({ missing: false, worktree: true, child: true }), {...})`.

- [ ] **Step 4: Run** — `npx vitest run` → PASS; `npm run check` → 0 errors (other `MenuItem[]` callers still type-check because `MenuItem` is a `MenuEntry`).

- [ ] **Step 5: Spec** — `docs/spec/01-repositories-and-sidebar.md`: where the repository row's context menu is described (around line 54 and the worktree menu ~92, nested worktree ~271), add a paragraph:

"A repository row's context menu is grouped, with a line between groups: Locate… (only while the folder is missing); Fetch, Pull, Push; Show in Finder (Show in folder elsewhere), Open in Terminal; Repository settings… (disabled while the folder is missing; see `12-repository-settings.md`); Move to group… and Remove from list…. A linked worktree's menu has Fetch, Pull, Push; the two Open items; and Remove worktree… — no Repository settings…, since its remotes are its main repository's. A worktree shown nested has no Move to group…."

Adjust the existing sentences nearby so nothing contradicts it.

- [ ] **Step 6: Commit**

```bash
/usr/bin/git add frontend/src docs/spec/01-repositories-and-sidebar.md
/usr/bin/git commit -m "feat(sidebar): repository menu in groups with separators, Repository settings… entry"
```

---

### Task 4: Remote form validation (pure)

**Files:**
- Create: `frontend/src/lib/remoteForm.ts`, `frontend/src/lib/remoteForm.test.ts`

**Interfaces:**
- Produces: `export function remoteFormError(name: string, url: string, existing: string[]): string` — '' when the form can be submitted, '-' when it is merely incomplete (button disabled, nothing shown), else the message to show; `export function defaultRemoteName(existing: string[]): string`.

- [ ] **Step 1: Failing test** — `remoteForm.test.ts`:

```ts
import { describe, expect, it } from 'vitest'
import { defaultRemoteName, remoteFormError } from './remoteForm'

describe('remoteFormError', () => {
  it('is incomplete while a field is blank', () => {
    expect(remoteFormError('', 'u', [])).toBe('-')
    expect(remoteFormError('origin', '   ', [])).toBe('-')
  })
  it('refuses whitespace in the name and a duplicate', () => {
    expect(remoteFormError('my remote', 'u', [])).toBe('A remote name has no spaces')
    expect(remoteFormError(' origin ', 'u', ['origin'])).toBe('A remote named origin already exists')
  })
  it('accepts a new name and a URL', () => {
    expect(remoteFormError('upstream', 'git@host:x.git', ['origin'])).toBe('')
  })
})

describe('defaultRemoteName', () => {
  it('suggests origin only for the first remote', () => {
    expect(defaultRemoteName([])).toBe('origin')
    expect(defaultRemoteName(['origin'])).toBe('')
  })
})
```

- [ ] **Step 2: Run** — `npx vitest run src/lib/remoteForm.test.ts` → FAIL.

- [ ] **Step 3: Implement** — `remoteForm.ts`:

```ts
/** The Remotes tab's add form (docs/spec/12-repository-settings.md):
 *  '' — can be added; '-' — incomplete, Add stays disabled with nothing
 *  shown; anything else — the reason, shown under the form. Git checks
 *  the rest when the remote is added. */
export function remoteFormError(name: string, url: string, existing: string[]): string {
  const n = name.trim()
  if (n === '' || url.trim() === '') return '-'
  if (/\s/.test(n)) return 'A remote name has no spaces'
  if (existing.includes(n)) return `A remote named ${n} already exists`
  return ''
}

export const defaultRemoteName = (existing: string[]) => (existing.length === 0 ? 'origin' : '')
```

- [ ] **Step 4: Run** → PASS.

- [ ] **Step 5: Commit**

```bash
/usr/bin/git add frontend/src/lib/remoteForm.ts frontend/src/lib/remoteForm.test.ts
/usr/bin/git commit -m "feat(remotes): add-remote form validation"
```

---

### Task 5: Repository settings dialog

**Files:**
- Create: `frontend/src/components/RepoSettingsDialog.svelte`
- Modify: `frontend/src/lib/api.ts` (five calls)
- Modify: `frontend/src/lib/types.ts` (`Remote`, `RemoteTest`)
- Modify: `frontend/src/App.svelte` (mount before `<DialogHost />`)
- Create: `docs/spec/12-repository-settings.md`; Modify: `docs/spec/README.md` (index line)

**Interfaces:**
- Consumes: `repoSettings` (Task 3), `remoteFormError`/`defaultRemoteName` (Task 4), Go bindings (Task 2), `confirmDialog`, `dialog`, `errorMessage` from `ui.ts`, `busy`, `repos`, `selectedRepoId`, `refreshRepo` from `stores.ts`.
- Produces: `api.listRemotes(id)`, `api.addRemote(id, name, url)`, `api.setRemoteURL(id, name, url)`, `api.removeRemote(id, name)`, `api.testRemote(id, name)`; `types.ts`: `export interface Remote { name: string; fetchURL: string; pushURL: string }`, `export interface RemoteTest { ok: boolean; message: string }`.

- [ ] **Step 1: API + types**

`api.ts` (near `fetch`):

```ts
  listRemotes: (id: string) => call<Remote[]>(Go.ListRemotes(id)),
  addRemote: (id: string, name: string, url: string) => call<void>(Go.AddRemote(id, name, url)),
  setRemoteURL: (id: string, name: string, url: string) => call<void>(Go.SetRemoteURL(id, name, url)),
  removeRemote: (id: string, name: string) => call<void>(Go.RemoveRemote(id, name)),
  testRemote: (id: string, name: string) => call<RemoteTest>(Go.TestRemote(id, name)),
```

(import `Remote`, `RemoteTest` types.)

- [ ] **Step 2: Dialog** — `RepoSettingsDialog.svelte`. Reuse SettingsDialog's layout classes (copy its `.backdrop`, `.dialog`, `.tabs`, `.tab`, `.pane`, `header`, `.content`, `h3` styles; height `min(440px, 86vh)`).

```svelte
<script lang="ts">
  import Icon from './Icon.svelte'
  import { api } from '../lib/api'
  import { defaultRemoteName, remoteFormError } from '../lib/remoteForm'
  import { busy, refreshRepo, repoSettings, repos, selectedRepoId } from '../lib/stores'
  import type { Remote, RemoteTest } from '../lib/types'
  import { confirmDialog, dialog, errorMessage } from '../lib/ui'

  let remotes: Remote[] = []
  let loadError = ''
  let error = ''                 // the last write's git error, shown at the top of the list
  let editing = ''               // remote whose URL is being edited
  let editURL = ''
  let tests: Record<string, RemoteTest | 'running'> = {}
  let adding = false
  let newName = ''
  let newURL = ''

  $: repoID = $repoSettings?.repoID ?? ''
  $: repo = $repos.find((r) => r.id === repoID)
  // The repository left the list (removed, or a worktree that vanished).
  $: if ($repoSettings && !repo) close()
  $: if (repoID) load(repoID)
  $: names = remotes.map((r) => r.name)
  $: formError = remoteFormError(newName, newURL, names)

  async function load(id: string) {
    editing = ''; adding = false; tests = {}; error = ''
    try {
      remotes = await api.listRemotes(id)
      loadError = ''
    } catch (e) {
      remotes = []
      loadError = errorMessage(e)
    }
  }

  async function write(label: string, fn: () => Promise<void>): Promise<boolean> {
    const id = repoID
    busy.set(label)
    error = ''
    try {
      await fn()
      return true
    } catch (e) {
      error = errorMessage(e)
      return false
    } finally {
      busy.set('')
      await load(id)
      if (id === $selectedRepoId) await refreshRepo()
    }
  }

  async function test(name: string) {
    tests = { ...tests, [name]: 'running' }
    let res: RemoteTest
    try {
      res = await api.testRemote(repoID, name)
    } catch (e) {
      res = { ok: false, message: errorMessage(e) }
    }
    tests = { ...tests, [name]: res }
  }

  function startEdit(r: Remote) { editing = r.name; editURL = r.fetchURL; const { [r.name]: _, ...rest } = tests; tests = rest }
  async function saveEdit() {
    const name = editing
    if (await write('Saving remote…', () => api.setRemoteURL(repoID, name, editURL.trim()))) editing = ''
  }
  async function remove(name: string) {
    const ok = await confirmDialog({
      title: `Remove remote ${name}?`,
      message: 'Its remote branches leave the log, and branches that track it lose their upstream.',
      confirmLabel: 'Remove',
      danger: true,
    })
    if (ok) await write('Removing remote…', () => api.removeRemote(repoID, name))
  }
  function startAdd() { adding = true; newName = defaultRemoteName(names); newURL = '' }
  async function add() {
    if (formError !== '') return
    if (await write('Adding remote…', () => api.addRemote(repoID, newName.trim(), newURL.trim()))) adding = false
  }
  function close() { repoSettings.set(null) }
  // Escape closes the confirm dialog on top first; an Escape inside the URL
  // or add fields cancels that edit instead (handled on the inputs).
  function onKey(e: KeyboardEvent) { if ($repoSettings && e.key === 'Escape' && !$dialog && !e.defaultPrevented) close() }
</script>

<svelte:window on:keydown={onKey} />

{#if $repoSettings && repo}
  <div class="backdrop" on:click|self={close} role="presentation">
    <div class="dialog" role="dialog" aria-label="{repo.name} settings">
      <nav class="tabs" aria-label="Repository settings sections">
        <h3 class="ellipsis" title={repo.name}>{repo.name} settings</h3>
        <button class="tab active" aria-current="page">Remotes</button>
      </nav>
      <div class="pane">
        <header>
          <h3>Remotes</h3>
          <button class="icon-btn" title="Close" on:click={close}><Icon name="x" /></button>
        </header>
        <div class="content">
          {#if loadError}<p class="warn">{loadError}</p>{/if}
          {#if error}<p class="warn">{error}</p>{/if}
          {#if remotes.length === 0 && !loadError}<p class="hint">No remotes yet.</p>{/if}
          <ul class="remotes">
            {#each remotes as r (r.name)}
              {@const t = tests[r.name]}
              <li>
                <div class="line">
                  <strong>{r.name}</strong>
                  {#if editing === r.name}
                    <!-- svelte-ignore a11y_autofocus -->
                    <input class="url-input" bind:value={editURL} autofocus
                      on:keydown={(e) => { if (e.key === 'Enter') saveEdit(); if (e.key === 'Escape') { e.preventDefault(); editing = '' } }} />
                    <button class="btn primary" disabled={!!$busy || !editURL.trim() || editURL.trim() === r.fetchURL} on:click={saveEdit}>Save</button>
                    <button class="btn" on:click={() => (editing = '')}>Cancel</button>
                  {:else}
                    <span class="url ellipsis" title={r.fetchURL}>{r.fetchURL}</span>
                    <button class="btn" disabled={t === 'running'} on:click={() => test(r.name)}>{t === 'running' ? 'Testing…' : 'Test'}</button>
                    <button class="btn" disabled={!!$busy} on:click={() => startEdit(r)}>Edit</button>
                    <button class="btn danger-text" disabled={!!$busy} on:click={() => remove(r.name)}>Remove…</button>
                  {/if}
                </div>
                {#if t && t !== 'running'}<p class="result" class:ok={t.ok}>{t.message}</p>{/if}
              </li>
            {/each}
          </ul>
          {#if adding}
            <div class="add">
              <label>Name<input bind:value={newName} on:keydown={(e) => { if (e.key === 'Escape') { e.preventDefault(); adding = false } }} /></label>
              <label class="grow">URL<input bind:value={newURL}
                on:keydown={(e) => { if (e.key === 'Enter') add(); if (e.key === 'Escape') { e.preventDefault(); adding = false } }} /></label>
              <button class="btn primary" disabled={formError !== '' || !!$busy} on:click={add}>Add</button>
              <button class="btn" on:click={() => (adding = false)}>Cancel</button>
            </div>
            {#if formError !== '' && formError !== '-'}<p class="warn">{formError}</p>{/if}
          {:else}
            <div><button class="btn" disabled={!!loadError} on:click={startAdd}>Add remote</button></div>
          {/if}
        </div>
      </div>
    </div>
  </div>
{/if}
```

Styles (in addition to the copied layout ones):

```css
  .remotes { list-style: none; margin: 8px 0 12px; padding: 0; display: flex; flex-direction: column; gap: 6px; }
  .remotes li { padding: 8px 10px; border: 1px solid var(--border); border-radius: 8px; }
  .line { display: flex; align-items: center; gap: 8px; min-width: 0; }
  .line strong { flex: none; }
  .url { flex: 1; min-width: 0; color: var(--muted); font-family: var(--mono); font-size: 12px; }
  .url-input { flex: 1; min-width: 0; font-family: var(--mono); font-size: 12px; }
  .result { margin: 6px 0 0; font-size: 12px; color: var(--danger); }
  .result.ok { color: var(--ok); }
  .add { display: flex; align-items: flex-end; gap: 8px; }
  .add label { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: var(--muted); }
  .add .grow { flex: 1; }
  .warn { margin: 8px 0; font-size: 12px; color: var(--danger); }
  .hint { margin: 8px 0; font-size: 12px; color: var(--muted); }
  .danger-text { color: var(--danger); }
```

Notes for the implementer:
- `on:keydown` on inputs calls `e.preventDefault()` for Escape so the window handler (`!e.defaultPrevented`) leaves the dialog open. Svelte delivers the input's handler before the window's (bubbling), so this works.
- Check `.btn.primary`, `.btn`, `.icon-btn`, `.ellipsis` exist globally in `theme.css` (they do for Settings); `confirmDialog` resolves to a boolean — check its actual return type in `ui.ts` and adapt `if (ok)`.
- `load` must not run on every `$repos` change; it is keyed on `repoID` only (the `$: if (repoID) load(repoID)` statement depends only on `repoID`).

`App.svelte`: import and mount `<RepoSettingsDialog />` **before** `<DialogHost />` (both use z-index 40, so DOM order puts the confirm dialog on top).

- [ ] **Step 3: Check** — `npm run check` → 0 errors; `npx vitest run` → PASS.

- [ ] **Step 4: Spec** — create `docs/spec/12-repository-settings.md`:

```markdown
# Repository settings

A repository's own settings, opened with "Repository settings…" in its row's
context menu (not for a linked worktree, whose remotes are its main
repository's; disabled while the folder is missing). The dialog is titled
"<repository> settings" and has tabs; today only **Remotes**. Escape or the
close button closes it, and it closes by itself if the repository leaves the
list.

## Remotes

One row per remote, by name: its name, its URL (cut short; the full URL in
the tooltip) and three buttons.

- **Test** asks the remote for its branches, never prompting for a password
  and giving up after 30 s. The row then says "Connected", "Authentication
  failed", "Timed out" or the first line of git's error. Test does not lock
  the repository.
- **Edit** turns the URL into a field with Save and Cancel (Enter, Escape).
  Pointing a remote elsewhere ends a background-fetch pause of that remote
  (see `05-remote-and-stash.md`).
- **Remove…** asks "Remove remote <name>?" — its remote branches leave the
  log, and branches that track it lose their upstream — and removes it,
  ending its pause too.

"No remotes yet." when there are none. **Add remote** opens a Name and URL
form (Name is `origin` for a repository's first remote). Add stays disabled
while a field is blank; a name with spaces or one already used says why
under the form; anything else git refuses is shown in the dialog.

Edits, removals and additions are git commands like any other operation:
they show the busy label, wait for other operations on the repository, and
appear in the Commands panel. A failure is shown in the dialog, not as a
toast. Afterwards the sidebar, the log and the toolbar of the selected
repository refresh.
```

Add `12-repository-settings.md` to `docs/spec/README.md`'s list in the same style as the others.

- [ ] **Step 5: Commit**

```bash
/usr/bin/git add frontend/src docs/spec/12-repository-settings.md docs/spec/README.md
/usr/bin/git commit -m "feat(remotes): Repository settings dialog with a Remotes tab"
```

---

### Task 6: Full check, rebuild, reopen

- [ ] `go test ./... -race` → PASS.
- [ ] Frontend: `npx vitest run`, `npm run check`, `npm run build` → PASS.
- [ ] `/usr/bin/git status` clean; `frontend/wailsjs/runtime` unchanged.
- [ ] After merge: `make dev` in the background with a long timeout (node 22) and check the window opens; right-click a repository → grouped menu with lines; Repository settings… opens the dialog listing its remotes.
