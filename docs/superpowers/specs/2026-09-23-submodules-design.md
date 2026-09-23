# git-ui — Git submodules — Design

Date: 2026-09-23
Status: Approved 2026-09-23 (all [proposed] decisions accepted as written)
Builds on: `docs/spec/01-repositories-and-sidebar.md`, `docs/spec/02-log-and-history.md`,
`docs/spec/03-working-tree.md`, the worktrees design
(`2026-09-23-worktrees-and-sidebar-order-design.md`).

## Goal

Today git-ui does not know submodules exist. A repository with submodules
works, but a submodule is an opaque path: the Changes view lists it as a
modified "file" whose diff is one `Subproject commit <hash>` line, a commit's
details show the same, and there is no way to see which submodules exist,
whether they are initialised, or to open one.

This design adds, SmartGit style, a **Submodules** section to an expanded
repository, lets the user open a submodule like a repository, and renders a
submodule pointer change (a gitlink, mode `160000`) as what it is: "moved
from commit A to commit B", with the commits in between.

## Decisions

Already decided by the owner:

- **A "Submodules" section** inside the expanded repository, a sibling of
  Branches / Remotes / Tags / Stash.
- **A flat list**: every submodule is one row showing its path, whatever
  folder it lives in — no folder tree.
- **Not child rows** like worktrees: submodules never appear as rows of the
  repository list.

Proposed here:

1. **[proposed] Position and visibility.** The section comes last, after
   Stash: Changes, Branches, Remotes, Tags, Stash, **Submodules**. It is
   present only when the repository has at least one submodule (a `.gitmodules`
   entry or a gitlink in the index); a repository without submodules shows no
   empty section. Like Tags and Stash, its header always shows a count, it
   starts collapsed, and its expanded state is remembered per repository.

2. **[proposed] Recursive, still flat.** Nested submodules (a submodule's own
   submodules) are listed in the same flat list, by their path relative to
   the top repository (`vendor/lib/deps/zlib`), sorted by path. Only the
   top-level repository in the sidebar has this section; an opened submodule
   shows its own sections except Submodules, since its descendants are
   already in the top repository's list. (Consequence: the list of an opened
   submodule is always reached from the top repository.)

3. **[proposed] What a row shows.** The path (the last segment in normal
   weight, the leading folders dimmed, so `vendor/lib/` **zlib**), and one
   state marker on the right:

   | State | Marker | Meaning (git's `submodule status` prefix / status flags) |
   |---|---|---|
   | In sync | none | checked out at the commit the parent records |
   | Moved | `↕` + short hash | checked out at a different commit than recorded (`+`) |
   | Modified content | `●` | tracked changes inside the submodule (`S.M.`) |
   | Untracked content | `○` | only untracked files inside (`S..U`) |
   | Not initialised | row dimmed, label "not initialised" | no working tree yet (`-`) |
   | Conflict | `!` | the gitlink is unmerged (`U`) |

   "Moved" and "modified content" can combine (both markers). The tooltip
   gives the full picture: recorded commit, checked-out commit and its
   branch if any, the remote URL from `.gitmodules`, and the submodule's name
   when it differs from its path.

4. **[proposed] Opening a submodule.** Clicking an initialised row opens
   the submodule **as a repository**, exactly as a detected worktree is
   opened: its own log, Changes, Branches/Remotes/Tags/Stash, chat
   conversation, terminal tabs and write lock. It is *detected, not stored*:
   nothing goes into `repos.json`, and its identifier is `repos.IDFor(path)`,
   so its chat and terminal state survive restarts.

   Because a submodule has no row of its own, the selection is shown by
   highlighting its row in the parent's Submodules section (the parent
   repository stays expanded), and the log header shows a breadcrumb
   `parent › vendor/lib/zlib`; clicking `parent` selects the parent again.
   Clicking the highlighted row again returns to the parent (the same
   click-to-deselect idea as the log). An uninitialised row cannot be opened;
   a click on it does nothing and its menu offers Initialise.

5. **[proposed] Actions** — context menu of a row:
   - **Open** (same as click; absent when not initialised)
   - **Initialise** — only when not initialised:
     `git submodule update --init -- <path>` (clones it and checks out the
     recorded commit).
   - **Update to recorded commit** — only when initialised and moved:
     `git submodule update -- <path>`. Confirmed first, because it leaves the
     submodule on a detached HEAD at the recorded commit; the dialog names the
     commit it will leave and, when the submodule is on a branch, that the
     branch itself is not changed. Refused (not forced) when the submodule has
     modified content that the checkout would overwrite — git's message is
     shown in plain words.
   - **Sync URL** — `git submodule sync -- <path>`, copies the URL from
     `.gitmodules` into the submodule's config (for when upstream changed it).
   - **Show in Finder** (platform equivalent) and **Open terminal here**.

   The section header's menu has **Initialise all** (`update --init
   --recursive`) and **Update all** (`update --recursive`), both confirmed
   with the list of submodules they will touch.

   These run with the same environment, timeout and credential handling as
   Fetch (they may need the network), and take the parent's write lock **and**
   the lock of every affected submodule id, so nothing else can write into a
   submodule while it is being updated.

   Out of scope: adding, removing (`deinit`), moving a submodule, changing
   its URL or branch in `.gitmodules`.

6. **[proposed] Changes view.** A gitlink path is listed as today, but its row
   carries a submodule icon instead of a plain file glyph and the letter keeps
   git's meaning (`M` pointer moved, `A` submodule added, `D` removed).
   Selecting it shows, instead of the raw `Subproject commit` diff:

   > **Submodule `vendor/lib/zlib`** — `a1b2c3d` → `e4f5g6h`
   > \> e4f5g6h Fix overflow in inflate
   > \> 9c8d7e6 Bump version
   > (from `git diff --submodule=log`; `<` lines when it moved backwards)

   (git's `--submodule=log` lists each commit by subject only, without its
   hash, so the rows show `> subject`.)

   plus a note "Contains modified content / untracked content — **Open
   submodule**" when that applies, with the link opening it. When the
   submodule's objects are not available (not initialised, not fetched),
   only the two hashes are shown.

   - A submodule whose **pointer did not move** but which has changes inside
     is still listed under Unstaged (git reports it), with the `●`/`○`
     marker and no stage checkbox: there is nothing to stage in the parent;
     the pane says "Commit inside the submodule first" with the Open link.
   - **Stage / Unstage** of a moved pointer work as for any path (they
     record or un-record the new commit).
   - **Discard** on a submodule row is replaced by **Update to recorded
     commit** (point 5), with the same confirmation; a plain `restore` does
     nothing to a submodule, so offering "Discard" would mislead.

7. **[proposed] Commit details.** In the details pane's file list, a gitlink
   change gets the same icon, and its diff is rendered the same way
   (`git diff --submodule=log <parent> <commit> -- <path>`). This is where
   "submodule moved a..b" lives. **The log itself is unchanged**: no extra
   rows or badges for commits that move a submodule — a filter or badge can
   come later if it proves useful.

8. **[proposed] After checkout, pull or merge.** git does not move
   submodules along with the parent unless the user set `submodule.recurse`
   (git-ui honours that config because git does, and does not set it). When
   a write in the parent leaves any submodule "moved", a toast says
   "N submodules are not at the recorded commit" with an **Update all**
   action. No automatic update.

9. **[proposed] AI.** No new tools. The chat's read tools that return
   diffs (for the Changes view and for commits) get the `--submodule=log`
   rendering, so the model sees "moved a..b" with subjects instead of a
   hash line. The chat of an opened submodule is that submodule's own
   conversation, as for worktrees.

## Architecture

### Reading submodules — `internal/submodules`

```go
type Submodule struct {
    Name        string // from .gitmodules (submodule.<name>.path)
    Path        string // relative to the top repository, slash-separated
    URL         string // from .gitmodules
    Recorded    string // commit the parent's index records ("" when absent)
    CheckedOut  string // commit checked out, "" when not initialised
    Branch      string // submodule's current branch, "" when detached
    Initialised bool
    Moved       bool   // CheckedOut != Recorded
    Modified    bool   // tracked changes inside
    Untracked   bool   // untracked files inside
    Conflict    bool
}

func List(ctx context.Context, dir string) ([]Submodule, error)
```

- `git submodule status --recursive` gives, per submodule, the prefix
  (`' '`, `+`, `-`, `U`), the checked-out (or recorded, for `-`) hash and
  the path relative to `dir`. Paths are unquoted by git only when safe; the
  parser handles C-quoted paths.
- Recorded commits: `git ls-files -s -z` filtered to mode `160000`, run in
  each parent (top repository and each initialised submodule that has
  submodules).
- Names and URLs: `git config -f .gitmodules -z --get-regexp '^submodule\..*\.(path|url)$'`
  per parent.
- Modified / untracked: the `sub` field of `git status --porcelain=v2`
  (`S<c><m><u>`), read in the direct parent. For nested submodules this is
  one extra `status` per initialised submodule that has submodules — acceptable
  because only repositories with nested submodules pay it.
- Branch: `git -C <path> symbolic-ref --short -q HEAD` per initialised
  submodule, only computed when the section is expanded (it is only needed
  for the tooltip and the Update dialog).

`internal/worktree.Status` keeps its records but also reports, per path,
`Submodule bool` and `SubModified / SubUntracked bool` from the same
porcelain `sub` field (field 3: `N...` for a normal path).

### App

- `GetSubmodules(id) ([]SubmoduleItem, error)` — read when the section is
  expanded and on the same refresh triggers as refs (focus, after writes).
  `RepoItem`/refs gain `SubmoduleCount int` so the section can appear with
  its count while collapsed (from the cheap `ls-files -s` + `.gitmodules`
  read only).
- Detected ids: every `GetSubmodules` result for an initialised submodule is
  remembered in memory (`a.submodules map[id]struct{path, parentID}`, replaced
  per parent on each read), and `a.dir(id)` resolves stored repositories,
  then detected worktrees, then detected submodules. List-entry actions on a
  submodule id are `ErrUnknownRepo`, as for worktrees.
- `InitSubmodule(id, path)`, `UpdateSubmodule(id, path)`, `SyncSubmodule(id,
  path)`, `InitAllSubmodules(id)`, `UpdateAllSubmodules(id)` — `path` must be
  one the last `List` returned (a stale or invented path is refused, the same
  rule as diffs). Locks as in decision 5.
- Diffs: `gitlog.Diff` and the working-tree diff pass `--submodule=log` (it
  only changes gitlink hunks; normal files are unaffected). The frontend
  recognises the `Submodule <path> <a>..<b>:` header and renders it.

### Frontend

- `RepoRefs`: the Submodules section, rows, markers, tooltips, menus,
  highlighted row when a submodule is selected.
- Selection: `selectedRepo` may be a submodule id; the log header shows the
  breadcrumb when the selected id is a detected submodule (`parentId` known).
- `ChangesView` / `FileList` / `CommitDetails`: submodule icon, the rendered
  pointer diff, disabled stage checkbox for content-only changes, Update in
  place of Discard.
- `lib/submodules.ts`: marker derivation, header-line parsing of the
  `--submodule=log` output (unit-tested).

## Error handling

- `git submodule status` failing (e.g. a broken `.gitmodules`) shows the
  section with an error row "Could not read submodules: <reason>" instead of
  hiding the repository's other sections.
- A `.gitmodules` entry with no gitlink, or a gitlink with no `.gitmodules`
  entry, is listed with a "not configured" label and no actions except Show
  in Finder (git itself cannot init or update it).
- Initialise / Update failing on the network or on credentials: the same
  error presentation as Fetch.
- A submodule deinitialised or removed while selected: the next read drops
  its id; the selection moves to the parent (not cleared, since the user was
  "inside" the parent).

## Testing

- Go (`internal/submodules`): real repositories built with
  `git -c protocol.file.allow=always submodule add` — in sync, moved,
  uninitialised, modified content, untracked content, nested, path with a
  space, `.gitmodules` without a gitlink; `List` returns the flat recursive
  list with correct flags.
- Go (`internal/worktree`): `Status` flags a gitlink and its content state.
- Go (`internal/app`): `a.dir` resolves a submodule id; actions refuse
  unknown paths; Update refuses over modified content; locks taken on both
  ids; diffs contain the `Submodule … a..b:` header.
- vitest: marker derivation, header parsing (forward, backward, new, removed,
  objects missing).
- Manual: a repository with two submodules (one nested) — section, markers,
  open/breadcrumb/back, Initialise, Update after checking out an older parent
  commit, the Changes and commit-details rendering, the toast after checkout.

## Docs

In the same commit as each behaviour: `docs/spec/01-repositories-and-sidebar.md`
(the section, rows, opening, actions), `docs/spec/02-log-and-history.md`
(commit details rendering), `docs/spec/03-working-tree.md` (submodule rows,
diff, no stage for content-only, Update instead of Discard),
`docs/spec/06-ai.md` (diff rendering seen by the tools),
`docs/spec/07-conventions-and-constraints.md` (submodule ids are detected,
the two-lock rule).

## Out of scope

Adding, removing, deinitialising or moving submodules; editing
`.gitmodules`; foreach commands; a log filter or badge for commits that move
a submodule; setting `submodule.recurse` from the app; submodules as sidebar
rows.
