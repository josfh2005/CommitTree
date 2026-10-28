# Push Dialog Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace CommitTree's fixed Push behaviours with a SourceTree-style "Push to repository" dialog: pick a remote, tick branches (creating same-name remote branches), Track?, Push all tags, Force push (`--force-with-lease`), OK pushes every ticked branch.

**Architecture:** One new Go op `ops.PushBranches` (explicit refspecs, one `git push --porcelain` per track group, lease per ref, optional tags) behind `App.PushBranches`, which keeps the write lock, credential prompts and `watchPushed`. The frontend gets a pure module `pushDialog.ts` (row defaults, ticking, remote change, request building, main-branch preselection) and a `PushDialog.svelte` component opened from toolbar Push, the repo-row menu (Push, Push main branches) and the branch menu. The push-scope setting and the old PushAll/PushBranch paths are removed.

**Tech Stack:** Go 1.x + Wails v2, Svelte 5 (legacy `$:` syntax as in the existing components), TypeScript, vitest.

**Spec:** `docs/superpowers/specs/2026-10-28-push-dialog-design.md` (read it first; it carries the owner's decisions and the "my calls" list).

## Global Constraints

- git is `/usr/bin/git` for any manual git command; commits never carry `Co-Authored-By`.
- Every behaviour change updates `docs/spec/` in the same commit (spec 05 Push section is the main one; also 01, 07, 11, 12, 14 where they mention the push setting, Push main branches, branch-menu Push or the Push tooltip).
- Force is `--force-with-lease` with an explicit expectation per ref — never plain `--force`, never on tags.
- Toolbar Push pre-ticks only the current branch (nothing with a detached HEAD).
- The chat `push` tool (`internal/ai/writetools`, `App.Push`) is unchanged.
- Go tests: `go test ./...`. Frontend: `cd frontend && npm test` and `npm run check` (Node 22: `source ~/.nvm/nvm.sh && nvm use 22`).
- Wails bindings in `frontend/wailsjs/go` are committed; regenerate with `source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && ~/go/bin/wails generate module` after changing App methods/types, and commit the result.

## Review Focus

1. A ticked branch whose remote branch name was typed by hand and does not exist yet → created under that name, upstream set only if Track? is ticked.
2. Force push on a branch whose remote moved since the last fetch → rejected by the lease, reported with git's reason, other branches still pushed.
3. Ticked branches split across tracked and untracked rows on one remote → untracked rows get no upstream; an untracked row with an existing upstream keeps it.
4. Push all tags with no branch ticked → only tags pushed; one "Tags" row/line in the outcome; `watchPushed` never sees it.
5. Changing the remote in the dialog after typing a remote-branch name → typed name kept, other rows recomputed against the new remote.

---

### Task 1: `ops.PushBranches`

**Files:**
- Create: `internal/ops/pushbranches.go`
- Modify: `internal/ops/pushall.go` (add `Tags` to `BranchPushResult`)
- Test: `internal/ops/pushbranches_test.go` (use `testrepo` like `pushall_test.go`; reuse its `aheadClone`/`remoteHash` helpers)

**Interfaces:**
- Produces:
  ```go
  type PushSpec struct {
      Local  string `json:"local"`  // local branch name
      Remote string `json:"remote"` // remote branch name; "" means Local
      Track  bool   `json:"track"`
  }
  type PushRequest struct {
      Remote   string     `json:"remote"`
      Branches []PushSpec `json:"branches"`
      Tags     bool       `json:"tags"`
      Force    bool       `json:"force"`
  }
  func PushBranches(ctx context.Context, dir string, req PushRequest) ([]BranchPushResult, error)
  // BranchPushResult gains: Tags bool `json:"tags,omitempty"` — the one result row for "Push all tags"
  ```
- Results: one per spec in request order (`Branch` = Local, `Target` = `<remote>/<remote branch>`), then the tags row (`Branch: "Tags"`, `Target: <remote>`, `Tags: true`) when `req.Tags`.
- Errors (no push run): empty `req.Remote` or a remote that `git remote get-url` doesn't know; neither branches nor tags requested; a `Local` that is not a local branch.

- [ ] **Step 1: Write the failing tests**
  - `TestPushBranchesCreatesASameNameRemoteBranchAndTracksIt`: local `feature/x` with no upstream, spec `{Local:"feature/x", Track:true}` → result `{Branch:"feature/x", Target:"origin/feature/x", Status:pushed}`; remote has `feature/x` at the local hash; `git rev-parse --abbrev-ref feature/x@{u}` = `origin/feature/x`.
  - `TestPushBranchesUntrackedGetsNoUpstreamAndKeepsAnExistingOne`: `topic` (no upstream) with `Track:false` → pushed, `topic@{u}` fails; `develop` tracking `origin/develop`, pushed with `{Remote:"other", Track:false}` → upstream still `origin/develop`.
  - `TestPushBranchesTrackRepointsTheUpstream`: `develop` tracking `origin/develop`, spec `{Remote:"dev2", Track:true}` → `develop@{u}` = `origin/dev2`.
  - `TestPushBranchesForceWithLeaseOverwritesARewrittenBranch`: amend local `main` after it was pushed → without Force: rejected with "pull main first"; with Force: pushed, remote `main` = local hash.
  - `TestPushBranchesForceRefusesWhenTheRemoteMovedSinceTheLastFetch`: another clone pushes to `main`; local (not fetched) rewrites `main`, Force → `Status: rejected`, reason non-empty, remote `main` unchanged; a second spec `develop` in the same request still `pushed`.
  - `TestPushBranchesPushesTagsAsOneRow`: two local tags, `Tags:true`, no branches → `[{Branch:"Tags", Target:"origin", Status:pushed, Tags:true}]`, both tags on the remote; again → `upToDate`.
  - `TestPushBranchesRefusesBadRequests`: unknown remote, empty request, missing local branch → error, nothing pushed.

- [ ] **Step 2: Run** `go test ./internal/ops -run TestPushBranches -v` — Expected: FAIL (undefined: ops.PushBranches).

- [ ] **Step 3: Implement `PushBranches` in `internal/ops/pushbranches.go`**
  - Up to two `git push --porcelain` invocations on `req.Remote`: tracked specs with `-u`, untracked without (skip an empty group). Reuse `parsePorcelain`, `classifyPushLine`, `pushErrorText`, `gitcmd.NetworkTimeout` exactly as `pushGroup` does.
  - Refspec `refs/heads/<local>:refs/heads/<remote branch>`.
  - Force: per spec add `--force-with-lease=refs/heads/<remote branch>:<expected>` where expected is `git rev-parse --verify --quiet refs/remotes/<remote>/<remote branch>` or empty (= must not exist) when there is no remote-tracking ref.
  - Tags: a separate `git push --porcelain --tags <remote>` (never forced). Row status: any `!` line → rejected (first `!` summary as reason), else any pushed line → pushed, else up to date; run error with no lines → failed with `pushErrorText`.
  - Doc comment points at `docs/spec/05-remote-and-stash.md`.

- [ ] **Step 4: Run** `go test ./internal/ops -v -run 'TestPushBranches|TestPushAll|TestParsePorcelain'` — Expected: PASS.

- [ ] **Step 5: Commit** `feat(push): ops.PushBranches pushes chosen branches to one remote with track, lease and tags`

### Task 2: `App.PushBranches` + bindings

**Files:**
- Modify: `internal/app/remote.go`, `frontend/wailsjs/go/**` (regenerated), `frontend/src/lib/api.ts`, `frontend/src/lib/types.ts`
- Test: `internal/app/remote_test.go` (or the existing app test file that covers `pushedBranches`)

**Interfaces:**
- Consumes: `ops.PushBranches`, `ops.PushRequest` (Task 1).
- Produces:
  ```go
  func (a *App) PushBranches(id string, req ops.PushRequest) ([]ops.BranchPushResult, error)
  ```
  ```ts
  // types.ts
  export interface PushSpec { local: string; remote: string; track: boolean }
  export interface PushRequest { remote: string; branches: PushSpec[]; tags: boolean; force: boolean }
  // BranchPushResult gains: tags?: boolean
  // api.ts
  pushBranches: (id: string, req: PushRequest) => call<BranchPushResult[]>(Go.PushBranches(id, req))
  ```

- [ ] **Step 1: Write the failing test** `TestPushedBranchesSkipsTheTagsRow`: `pushedBranches([{Branch:"a",Status:pushed},{Branch:"Tags",Status:pushed,Tags:true},{Branch:"b",Status:upToDate}])` = `["a"]`.
- [ ] **Step 2: Run** `go test ./internal/app -run TestPushedBranches -v` — Expected: FAIL.
- [ ] **Step 3: Implement** `App.PushBranches` like `App.PushAll`: `a.write` + `a.prompting(ctx, id, dir, "Push")`, then `a.watchPushed(id, pushedBranches(results))` even when err != nil; `pushedBranches` skips `Tags` rows. Regenerate bindings; add the TS types and `api.pushBranches`.
- [ ] **Step 4: Run** `go test ./internal/app ./internal/ops` and `cd frontend && npm run check` — Expected: PASS.
- [ ] **Step 5: Commit** `feat(push): App.PushBranches with pipeline watch for each pushed branch`

### Task 3: Pure dialog logic `pushDialog.ts`

**Files:**
- Create: `frontend/src/lib/pushDialog.ts`, `frontend/src/lib/pushDialog.test.ts`

**Interfaces:**
- Consumes: `Refs`, `Branch`, `PushRequest` (types.ts); `othersAhead` (push.ts).
- Produces:
  ```ts
  export interface PushRow { local: string; remote: string; push: boolean; track: boolean; typed: boolean }
  export function splitUpstream(upstream: string, remotes: string[]): { remote: string; branch: string } | null // longest remote-name prefix + '/'
  export function counterpart(b: Branch, remote: string, refs: Refs): string   // upstream branch when on `remote`, else same-name branch on `remote`, else ''
  export function defaultRemote(refs: Refs, ticked: string[]): string           // first ticked branch's upstream remote, else 'origin', else first remote, else ''
  export function initialRows(refs: Refs, remote: string, ticked: string[]): PushRow[] // every local branch by name; ticked rows: remote = counterpart || local, track true
  export function setTicked(row: PushRow, on: boolean): PushRow                 // on: fill blank remote with local, track true; off: track false, remote kept
  export function selectAll(rows: PushRow[], on: boolean): PushRow[]
  export function changeRemote(rows: PushRow[], refs: Refs, remote: string): PushRow[] // typed rows keep their name; others = counterpart, or local when ticked and blank
  export function editRemote(row: PushRow, name: string): PushRow               // typed = true
  export function pushRequest(remote: string, rows: PushRow[], tags: boolean, force: boolean): PushRequest // ticked rows only, in row order
  export function canPush(rows: PushRow[], tags: boolean): boolean
  export function mainBranchesSelection(refs: Refs): { remote: string; ticked: string[] } // current (official, not upstreamLocal, not detached) + othersAhead; remote = the one most of them track (tie → 'origin', then name order); ticked = those on that remote, plus a current with no upstream
  ```

- [ ] **Step 1: Write failing tests** in `pushDialog.test.ts`, one `it` per function above with a small `Refs` fixture (`origin` with `main`, `develop`; `backup` with `release/1.0`; locals `main` (current, upstream `origin/main`), `develop` (upstream `origin/develop`, ahead 1, official), `feature/x` (no upstream), `release/1.0` (upstream `backup/release/1.0`, ahead 2, official)). Pin at least:
  - `initialRows(refs,'origin',['main'])` → `main` row `{push:true, remote:'main', track:true}`; `feature/x` row `{push:false, remote:'', track:false}`; `develop` row `{push:false, remote:'develop'}`.
  - `setTicked(featureRow,true)` → `{remote:'feature/x', track:true}`.
  - `changeRemote` to `backup`: `develop` → `''` (unticked), a typed `'mine'` kept.
  - `splitUpstream('origin/feature/x',['origin','or'])` → `{remote:'origin', branch:'feature/x'}`.
  - `defaultRemote(refs,['release/1.0'])` → `'backup'`; detached / no ticked → `'origin'`.
  - `mainBranchesSelection(refs)` → `{remote:'origin', ticked:['main','develop']}`.
  - `pushRequest` drops unticked rows, `canPush([], true)` true, `canPush(noneTicked,false)` false.
- [ ] **Step 2: Run** `cd frontend && npx vitest run src/lib/pushDialog.test.ts` — Expected: FAIL.
- [ ] **Step 3: Implement** `pushDialog.ts` (pure, no stores).
- [ ] **Step 4: Run** the same — Expected: PASS.
- [ ] **Step 5: Commit** `feat(push): pure row logic for the push dialog`

### Task 4: `PushDialog.svelte` and every entry point

**Files:**
- Create: `frontend/src/components/PushDialog.svelte`
- Modify: `frontend/src/App.svelte` (mount it like `<CloneDialog />`), `frontend/src/lib/actions.ts`, `frontend/src/lib/push.ts`, `frontend/src/components/RepoRow.svelte`, `frontend/src/components/RepoRefs.svelte`, `frontend/src/lib/actions.test.ts`, `docs/spec/05-remote-and-stash.md`, `docs/spec/01-repositories-and-sidebar.md`, `docs/spec/11-notifications.md`, `docs/spec/14-hosting.md`
- Test: `frontend/src/lib/actions.test.ts`, `frontend/src/lib/push.test.ts`

**Interfaces:**
- Consumes: Task 3 functions, `api.pushBranches`, `api.listRemotes` (`RemoteConfig.pushURL || fetchURL` is the URL shown).
- Produces (actions.ts):
  ```ts
  export const pushDialogState: Writable<{ id: string; repo: string; refs: Refs; remotes: RemoteConfig[]; remote: string; rows: PushRow[] } | null>
  export async function push(id: string): Promise<void>                     // toolbar + repo-row Push: current branch ticked
  export async function pushBranch(id: string, branch: Branch): Promise<void> // branch menu: that branch ticked
  export async function pushMainBranches(id: string): Promise<void>         // repo-row "Push main branches": mainBranchesSelection
  export async function runPush(id: string, req: PushRequest): Promise<boolean> // today's pushAll reporting, with req
  ```
- Dialog layout and copy exactly as the spec's sketch: title row "Push to repository:" + remote `<select>` + dim read-only URL; group "Branches to push" with table columns "Push?", "Local branch", "Remote branch" (input with `<datalist>` of the selected remote's branches), "Track?" (disabled + `indeterminate` when the row is unticked); "Select All"; "Push all tags"; "Force push"; Cancel / OK (OK disabled unless `canPush`). Esc = Cancel. A repo with no remotes opens nothing and toasts "No remote to push to — add one in Repository settings".

- [ ] **Step 1: Write failing tests** in `actions.test.ts` (mock `api` as the file already does):
  - `push(id)` sets `pushDialogState` with `remote:'origin'` and only the current branch ticked.
  - `pushBranch(id, develop)` ticks only `develop`.
  - `runPush` toasts "Pushed feature/x" when all pushed; opens the results dialog (`Push results — <repo>`) when one is rejected; a tags row reads "Tags → origin".
  - push.ts: a lone tags row reads `pushedMessage([{branch:'Tags',tags:true,status:'pushed',target:'origin'}])` = `'Pushed tags'`; with branches, the branches are counted without the tags row and `' and tags'` is appended when the tags pushed: `[feature/x pushed, tags pushed]` → `'Pushed feature/x and tags'`.
- [ ] **Step 2: Run** `cd frontend && npx vitest run src/lib/actions.test.ts src/lib/push.test.ts` — Expected: FAIL.
- [ ] **Step 3: Implement** the component and actions; rename `pushAll` → `runPush(id, req)` (busy label "Pushing…" for one branch, "Pushing branches…" for more, command-log kind `'push'`); wire Toolbar (`push(repoId)` already), RepoRow (`push`, `'push-all'` → `pushMainBranches`), RepoRefs (`pushBranch`). Rewrite spec 05 → Push as the dialog (entry points table, row rules, Track?, Force = `--force-with-lease` per ref, tags, results unchanged); fix 01 (menus), 11 (failed-push wording), 14 (which pushes start a pipeline watch).
- [ ] **Step 4: Run** `cd frontend && npm test && npm run check` — Expected: PASS.
- [ ] **Step 5: Commit** `feat(push): push dialog for toolbar, repo and branch menus; spec 05`

### Task 5: Remove the push-scope setting and the old push paths

**Files:**
- Modify: `internal/gitsettings/gitsettings.go` (+ `_test.go`), `frontend/src/components/SettingsDialog.svelte`, `frontend/src/lib/types.ts`, `frontend/src/lib/push.ts` (+ test), `frontend/src/lib/toolbar.ts` (+ test), `frontend/src/components/Toolbar.svelte`, `internal/app/remote.go`, `internal/ops/pushall.go`, `internal/ops/branchremote.go` (+ tests), `frontend/src/lib/api.ts`, `frontend/wailsjs/go/**`, `docs/spec/05-remote-and-stash.md`, `docs/spec/07-conventions-and-constraints.md`, `docs/spec/12-repository-settings.md`

**Interfaces:**
- Produces: `pushTitle(refs: Refs | null): string` = `'Push — choose branches to push'`; `GitSettings` without `pushScope`; `gitsettings.Settings` without `PushScope` (an old `pushScope` key in the file loads fine and is dropped on save).

- [ ] **Step 1: Write failing tests**: `TestLoadIgnoresAnOldPushScope` (file `{"pullStrategy":"merge","pushScope":"all"}` loads with `PullStrategy: merge`, no error; Save writes no `pushScope`); toolbar test pins the new tooltip.
- [ ] **Step 2: Run** `go test ./internal/gitsettings` and `cd frontend && npx vitest run src/lib/toolbar.test.ts` — Expected: FAIL.
- [ ] **Step 3: Implement**: delete `PushScope` + its constants/validation/tests; the Settings → General → Push row and its hint; `PushScope`, `pushDecision`, `pushChoiceOptions`, `pushCount` (if unused after Task 3/4); `App.PushAll`, `App.PushBranch`, `ops.PushAll`/`pushTargets`, `ops.PushBranch` and their tests (keep anything `PushBranches`, `FastForwardBranch` or the chat tool still uses — check with grep); `api.pushAll`/`api.pushBranch`; regenerate bindings. Update specs 05 (setting gone, tooltip), 07 (General tab contents), 12 (git-flow names now decide what "Push main branches" pre-ticks).
- [ ] **Step 4: Run** `go vet ./... && go test ./...` and `cd frontend && npm test && npm run check` — Expected: PASS, no references left (`grep -rn "pushScope\|PushAll\|pushAll\|ops.PushBranch\b" internal frontend/src docs/spec` empty except history notes).
- [ ] **Step 5: Commit** `refactor(push): drop the push-scope setting and the old push-all/branch paths; specs 05, 07, 12`

### Task 6: Build and hand to the owner

- [ ] **Step 1:** `make build` (Node 22 via the Makefile) — Expected: `build/bin/CommitTree.app` built and signed.
- [ ] **Step 2:** Quit any running CommitTree and open the new build (`open build/bin/CommitTree.app`); smoke: toolbar Push opens the dialog with the current branch ticked in `/tmp/git-ui-demo` (or a scratch clone).
- [ ] **Step 3:** No commit unless the build changed tracked files (`git status`; `make build` restores `frontend/wailsjs/runtime`).
