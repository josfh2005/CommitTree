# Push dialog — design

Date: 2026-10-28. Status: draft for the owner's approval.

## Why

The owner's words: "vamos a mejorar el push a una ventana como esta, que
permita incluso crear ramas en remote con el mismo nombre de la rama local si
se selecciona el checkbox y se le hace push a todas las seleccionadas, que por
defecto ya viene la rama actual cuando di push".

Today Push is one of three fixed things (Settings → General → Push: *Ask each
time* / *Current branch* / *Main branches*), and publishing a branch that has
no upstream only happens for the checked-out one (to `origin`). The owner
works in repositories with many hotfix/feature branches and wants SourceTree's
"Push to repository" window: pick a remote, tick the branches to push, name
or create their remote branches, choose whether they track, optionally push
tags or force, then OK pushes every ticked branch in one go.

## Goals

- **Push always opens the dialog** (toolbar Push). The current branch comes
  ticked; nothing else does.
- A local branch with no remote branch, once ticked, is pushed to a remote
  branch **of the same name**, which creates it.
- OK pushes every ticked branch to the chosen remote.
- Keep everything that already hangs off a push working: the hosting
  pipeline watch for every branch whose remote branch moved
  (`App.watchPushed`), in-app credential prompts (`a.prompting`), the
  command log, the write lock, the results toast / results dialog.

## Non-goals

- No keyboard shortcut for Push (none exists today; none added).
- No filter/search field in the branch table.
- No change to the chat's `push` tool: it keeps pushing the current branch
  without the dialog, behind its own confirmation card (`App.Push`).
- No pushing to several remotes in one OK: one dialog = one remote.
- No deleting remote branches from the dialog.

## Anti-goals

- Force must never be plain `--force`.
- The dialog must not tick branches by itself beyond what the entry point
  asked for (owner: only the current branch on toolbar Push).

## The dialog (what the owner showed)

Modeled on SourceTree's window (owner's screenshots), same layout:

```
Push to repository: [origin ▾]   git@bitbucket.org:team/repo.git   (URL, read-only, dim)
┌ Branches to push ─────────────────────────────────────────────┐
│ Push? │ Local branch        │ Remote branch        │ Track?   │
│ [ ]   │ develop             │                      │  [–]     │
│ [x]   │ feature/NEXO-1682   │ feature/NEXO-1682 ▾  │  [x]     │
│ [ ]   │ master              │ master            ▾  │  [–]     │
│ [ ] Select All                                                │
└───────────────────────────────────────────────────────────────┘
[ ] Push all tags                                  [Cancel] [OK]
[ ] Force push
```

Row behaviour, as SourceTree does it (owner's screenshots):

- **Local branch**: every local branch of the repository, by name. The table
  scrolls when there are many.
- **Remote branch**: while a row is unticked it shows the branch's remote
  counterpart on the selected remote — its upstream's name when the upstream
  is on that remote, otherwise a remote branch of the same name if one
  exists — and is blank when there is none. Ticking a row with a blank
  remote branch fills it with **the local branch's own name**. It is a
  combo box: a dropdown of the selected remote's branches, and the user can
  also type a new name.
- **Track?**: unticked rows show it greyed out with a dash (indeterminate);
  ticking Push? ticks Track?. The user can untick it. Ticked = after the
  push the local branch's upstream is that remote branch (`git push -u`
  semantics). A branch that already tracks exactly that remote branch is
  unaffected either way.
- **Select All** ticks/unticks every row (rows filled in as above).
- **Push to repository**: a dropdown of the repository's remotes; the URL of
  the selected one is shown beside it. Default: the current branch's
  upstream remote, else `origin`, else the first remote. Changing it
  recomputes each row's Remote branch (a value the user typed is kept — my
  call).
- **Push all tags**: also pushes every local tag to that remote.
- **Force push**: `--force-with-lease` (owner's decision), applied to every
  ticked branch. Unticked by default.
- **OK** is enabled when at least one branch is ticked or *Push all tags* is
  ticked; it pushes, closes the dialog and runs like today's push (busy
  label, command log). **Cancel** / Esc pushes nothing.

## Entry points (owner approved)

| Entry point | Opens the dialog with |
|---|---|
| Toolbar **Push** | the current branch ticked (nothing ticked with a detached HEAD) |
| Branch menu (sidebar) **Push** on branch X | X ticked (not the current branch, unless X is it) |
| Repository row menu **Push main branches** | the same branches today's Push all picks ticked: the current branch when it is a main branch, plus every other main branch ahead of a live upstream |
| Chat `push` tool | no dialog — unchanged |

The setting **Settings → General → Push** is removed, since the dialog now
covers its three choices (owner: "Siempre"). A stored `pushScope` value is
ignored and dropped on the next save. The toolbar tooltip becomes "Push…"
style wording that says it opens the push window (exact text: builder's
call); its badge still counts the current branch. The *Ask each time* choice
dialog goes away.

### Settled after the builder check (my calls, for the owner to check)

- **Which remote it opens on.** The remote of the first pre-ticked
  branch's upstream (toolbar: the current branch; branch menu: X), else
  `origin`, else the first remote. For *Push main branches*: the remote most
  of the pre-selected main branches track (ties → `origin`), and only the
  main branches whose upstream is on that remote come pre-ticked. A ticked
  branch whose upstream is on another remote is pushed to the chosen remote
  under the name shown in its row (same name when blank) — the user picked
  that remote.
- **Remote branches list** is what the last fetch knows (the refs store; the
  background fetch keeps it fresh). Opening the dialog or changing the remote
  does not fetch. A branch someone else created meanwhile is rejected by the
  remote and reported as "pull first", as today.
- **Force applies to branches only.** Tags are pushed without force; a tag
  that differs on the remote is reported under "Tags" with git's reason.
- **Track? off** leaves the branch's current upstream alone (whatever it
  is). **Track? on** sets the upstream to the pushed remote branch, repointing
  it if it tracked something else (`-u` semantics, as SourceTree does).
- **A differing remote name is flagged, not changed** (owner ruling "(a)
  Avisar", 2026-10-28). A Remote branch (pre-filled from an upstream with
  another name, or typed) that differs from the Local name stays as it is;
  the cell gets a warning tint and the tooltip "Differs from the local branch
  name" (`namesDiffer`).
- **One credential prompt per OK.** When an invocation fails with a cancelled
  or refused prompt and printed no ref line, the remaining invocations (and
  the Tags row) are not started and fail with the same reason.
- **Names are checked before any push** (`git check-ref-format --branch`),
  and two branches may not push to the same remote branch.
- **Rows**: every local branch, including ones tracking a local branch
  (remote `.`; their Remote branch defaults blank → same name when ticked)
  and ones checked out in other worktrees. git decides what it accepts.

## Outcome and results

Results reuse today's Push-all reporting (spec 05): a toast when nothing
failed ("Pushed `<branch>`", "Pushed N branches", ", M already up to date",
"Everything up to date"), and the "Push results — `<repository>`" dialog
listing every branch when any failed, with the same reasons (remote moved on
→ "pull first"; a lease that no longer holds reads as a rejection with
git's reason). Tags, when pushed, add one line/row ("Tags") to that report
(my call). The push is not atomic: one rejected branch does not stop the
others. Every branch whose remote branch moved starts a pipeline watch
(`watchPushed`).

## How (decided parts)

- Backend: one new App method replacing PushBranch/PushAll from the UI, e.g.
  `App.PushBranches(id, PushRequest)` with `{remote, branches:[{local, remote,
  track}], tags, force}` → `[]ops.BranchPushResult` (shape: builder's call).
  It goes through `a.write` + `a.prompting` like the others and calls
  `watchPushed` for every result with status pushed, even when the call
  errors (as PushAll does).
- `git push --porcelain` with explicit refspecs `refs/heads/<local>:refs/heads/<remote>`
  (reuse `pushGroup`/`parsePorcelain`/`classifyPushLine`). `-u` applies to a
  whole `git push` invocation, so tracked and untracked rows go in separate
  invocations (or set upstreams afterwards) — builder's call, but untracked
  rows must not get an upstream.
- `--force-with-lease` with an explicit expectation per ref is preferred
  over the bare flag, so a renamed/new remote branch behaves predictably
  (builder's call how).
- Tags: `--tags` (all local tags) on the same remote.
- The data the dialog needs (remotes + URLs, local branches with their
  upstream remote/name, the remote's branches) comes from the existing refs
  store / remotes API where possible; a small new read API is fine.
- Dead code goes: the push-scope setting, `pushDecision`/`pushChoiceOptions`,
  `App.PushBranch`/`App.PushAll` if nothing else uses them (the chat tool
  uses `App.Push`, which stays).
- Docs: spec 05 (Push section rewritten), plus every other spec that mentions
  the push setting, the branch-menu Push, Push main branches or the toolbar
  tooltip (01, 12/13 if they do), updated in the same commit as the
  behaviour.

## Left to the builder

Exact method and type names, how the dialog component is built (a new
`PushDialog.svelte` hosted like the other dialogs), the toolbar tooltip
text, sort details, tests (Go ops tests against `testrepo`, frontend unit
tests for the pure row logic: defaults, tick → fill, remote change).
