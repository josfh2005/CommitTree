# git-ui — Stage, unstage and discard by hunk and by line — Design

Date: 2026-09-29
Status: Approved design, not implemented
Builds on: `2026-09-21-working-tree-design.md` (Changes view, file-level
stage/unstage/discard), which deliberately left hunk-level staging out.

## Goal

Act on part of a changed file from its diff: stage, unstage or discard one
hunk, or a hand-picked set of lines, instead of the whole file. Today the
Changes view only colours the diff; every action works on whole files, so
keeping half of an edit means leaving the app for a terminal or an editor.

## Scope decisions

- **All three actions, by hunk and by line, in one piece of work.** They
  share the whole mechanism (selection in the diff, a partial patch,
  `git apply`); discard is a reverse apply of the same patch.
- **Per section:** a file in *Unstaged* offers Stage and Discard; a file in
  *Staged* offers Unstage only. Discarding from Staged would touch the index
  and the working tree at once; the file-level Discard already covers that.
- **No confirmation for a hunk or line discard; an Undo instead.** The
  file-level Discard keeps its confirmation.
- **Whole-file only** (no hunk or line actions) for: untracked files, binary
  files, submodules, renamed or copied files. Their rows keep the file-level
  actions exactly as today.
- **Out of scope:** the chat's write tools (they stay file-level), the merge
  view's diff, the commit details' diff, editing lines in place, and
  word-level diffs.

## Interaction

- Every `@@` header row of an actionable hunk carries, on its right, the
  buttons for its section: **Stage hunk** and **Discard hunk** (Unstaged), or
  **Unstage hunk** (Staged).
- **Line selection:** a click on a `+` or `-` line selects it (highlighted,
  with a bar in the left gutter), replacing the selection; Shift+click selects
  the range from the last clicked line (only `+`/`-` lines in that range);
  Cmd+click (Ctrl+click on Linux) toggles one line. Clicks on context, header
  or meta lines do nothing. Esc clears the selection. A selection can span
  several hunks.
- While lines are selected, a bar pinned to the top of the diff shows
  "N lines selected" and **Stage lines** / **Discard lines** (Unstaged) or
  **Unstage lines** (Staged), plus a clear (×) control.
- All buttons are disabled while a write is running (the existing `busy`
  store). The selection is cleared after any action, whenever the diff is
  reloaded, and when another file is opened.
- Text selection for copying keeps working: a drag selects text as today;
  only a click without a drag toggles line selection.
- After a hunk or line discard, a toast that does not auto-dismiss reads
  "Discarded 1 hunk in `<file>`" or "Discarded N lines in `<file>`" with an
  **Undo** action.

## Architecture

### Backend (Go)

**`internal/worktree/patch.go`** (new, no git, no I/O):

- `ParseDiff(text string) (FileDiff, error)`: splits one file's unified diff
  into a header (the `diff`/`index`/`---`/`+++` lines) and hunks; each hunk
  keeps its `@@` line numbers and its body lines, each with a kind (context,
  add, del) and a flag for a following `\ No newline at end of file` marker.
- `BuildPatch(d FileDiff, sel Selection) (string, error)`: builds a patch
  containing only the selected changes. `Selection` maps a hunk index to the
  indices of its selected body lines; a hunk given with no line list is taken
  whole. Within a hunk, an unselected `-` line becomes a context line, and an
  unselected `+` line is dropped. Hunks with nothing selected are left out.
  The `@@` header counts are recomputed from the kept lines. An empty result
  is an error ("nothing selected").

**`internal/worktree/hunks.go`** (new):

- `ApplySelection(ctx, dir, path string, staged bool, hash string, sel Selection, action Action) (patch string, err error)`,
  `Action` being `ActionStage`, `ActionUnstage` or `ActionDiscard`.
  1. Refuses a path the current status does not list (the same check
     `FileDiff` makes), an untracked, binary, submodule or renamed path, and
     an action that does not belong to the section (`ActionUnstage` needs
     `staged`, the other two need `!staged`).
  2. Recomputes the file's **full** diff (never the truncated text the pane
     shows) with `FileDiff`, and refuses with `ErrDiffChanged` when its
     SHA-256 differs from `hash`.
  3. Builds the partial patch and applies it with
     `git apply --recount --whitespace=nowarn` plus `--cached` (stage),
     `--cached -R` (unstage) or `-R` (discard), feeding it on stdin.
  4. Returns the applied patch (used for Undo after a discard).
- `Reapply(ctx, dir, patch string) error`: `git apply --recount` of a
  previously discarded patch onto the working tree (Undo). `git apply` is
  atomic, so a patch that no longer fits changes nothing.

**`internal/app/worktree.go`:**

- `GetWorktreeDiff` returns a `WorktreeDiff{Text, Hash, Truncated, Patchable}`
  instead of a string. `Hash` is the SHA-256 of the full diff; `Patchable` is
  false for the whole-file-only cases above.
- `ApplyHunkSelection(id, path string, staged bool, hash string, sel worktree.Selection, action string) error`
  runs `ApplySelection` under the existing `writeWorktree` lock. After a
  successful discard it stores the patch as the repository's last discard
  (in memory, one per repository, replaced by the next discard, lost on
  quit).
- `UndoDiscard(id string) error` reapplies that stored patch under the same
  lock; on success it is cleared, on failure it is kept.

### Frontend

- **`lib/hunks.ts`** (new, pure): parses the visible diff text into rows
  (meta, hunk header, context, add, del) with their hunk and line indices;
  marks the last hunk not actionable when the diff is truncated; selection
  helpers (click, Shift range, Cmd toggle, clear) and the conversion of a
  selection into the `Selection` sent to Go.
- **`components/DiffLines.svelte`** (new): renders the rows, the per-hunk
  buttons, the selection bar and the selection highlight. It replaces the
  inline `{#each}` in `ChangesView.svelte`. `lineClass` stays in `lib/diff.ts`
  for the merge view.
- **`lib/actions.ts`:** `applyHunkSelection(...)` runs through the existing
  `run()` (busy label, error toast, refresh) and, after a discard, shows the
  Undo toast; `undoDiscard(id)`.
- **`ChangesView.svelte`** keeps the `WorktreeDiff` it loads (text, hash,
  flags) and passes it with the file's section to `DiffLines`.

## Data flow

1. Opening a file calls `GetWorktreeDiff` → `{text, hash, truncated, patchable}`.
2. `DiffLines` parses `text`; if `patchable`, actionable hunks get buttons
   and `+`/`-` lines become selectable.
3. A button sends `(path, staged, hash, selection, action)` to
   `ApplyHunkSelection`.
4. Go re-reads the full diff, checks the hash, builds and applies the patch.
5. The usual worktree refresh reloads the status and the open diff; the file
   may move between sections (fully staged, fully unstaged) and the selection
   follows it as today.
6. A discard shows the Undo toast; Undo calls `UndoDiscard`, and the refresh
   shows the lines back.

## Error handling

| Case | Result |
|---|---|
| The diff changed since it was shown (hash mismatch) | Nothing is applied; toast "The file changed since it was shown — reloaded"; the diff reloads. |
| `git apply` refuses the patch | Nothing is applied (git apply is atomic); git's message in an error toast. |
| Undo after the lines changed again | Nothing is applied; toast "Can't undo: the file changed since the discard"; the patch is kept until the next discard. |
| Path not listed, untracked, binary, submodule or renamed | Refused by Go even if the renderer asks. |
| Action not valid for the section | Refused by Go. |
| Empty selection | No buttons are shown; Go refuses it anyway. |
| A write already running | Buttons disabled; Go returns the existing busy error. |

## Testing

- **Go, table tests for `patch.go`** (no git): add-only, delete-only and
  mixed hunks; an unselected `-` becoming context; an unselected `+`
  dropped; `\ No newline at end of file` on either side; several hunks with
  only some selected; header counts recomputed; empty selection refused.
- **Go, integration tests with `testrepo`:** stage one hunk of two; stage
  some lines of a hunk; unstage a hunk of a staged file; discard a hunk and
  some lines, checking both the index and the file on disk; a partially
  staged file; a stale hash refused with nothing changed; undo that works;
  undo refused after the lines changed; refused paths (untracked, renamed,
  submodule); wrong action for the section.
- **Vitest for `hunks.ts`:** parsing, click / Shift / Cmd selection, clicks
  on context lines ignored, the selection sent to Go, the last hunk not
  actionable when truncated.
- **Manual pass** in the app on a demo repository: each action by hunk and
  by line, Undo, the stale-diff message after editing the file in the
  terminal, and copying text from the diff still working.
- `docs/spec/03-working-tree.md` is updated in the same commit as the
  behaviour change.
