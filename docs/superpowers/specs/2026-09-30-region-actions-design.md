# Resolving conflict regions by hand — design

Date: 2026-09-30. Status: approved in chat, awaiting review of this document.

## Why

A merge can only be finished inside CommitTree when every conflict can be
settled there. Today a text conflict can only be looked at in the Merge view:
settling it means editing the file outside, or asking the AI. Files without
markers (delete/modify, binary) can be settled with "Take <branch>", but
only from a right-click menu nobody finds.

This is part A of two. Part B — the AI leaving a decision as a card of
concrete options the user picks and the app applies — comes after and
reuses A's region apply unchanged.

Success: the conflict lab (`scripts/conflict-lab`) can be resolved to 11/11
with buttons alone, and the AI resolver and the buttons write files through
the same code.

## What the user gets

In the Merge view's file pane, above each conflicting region (at its
`<<<<<<<` line), a row of buttons named after the real sides:

    Take develop · Take feature/checkout · Both · Edit…

- **Take <side>** replaces the region with that side's lines.
- **Both** replaces it with ours then theirs (for a merge: the checked-out
  branch, then the branch being merged).
- **Edit…** turns the region into a text box pre-filled with Both's text,
  with Apply and Cancel (⌘↵ applies, Esc cancels).
- Hovering a Take button dims the lines it would drop.
- The side names come from the existing `takeLabels` (merge, rebase,
  cherry-pick and stash conflicts already carry their own wording).

After any of them the file reloads in place. When that was the file's last
region it is staged, and a toast says so ("config/settings.json resolved
and staged").

The file pane header gets **Restart file** for any file of the operation
that was in conflict (still conflicted, or staged since): after a
confirmation — it discards what was resolved, by hand or by the AI — it
puts the file back as git first wrote it, markers included, and unstaged.
This is the undo; there is no per-region undo.

Selecting a file without markers (the `!` rows) shows **Take <ours>** and
**Take <theirs>** as buttons at the top of the pane, with the confirmation
they already have. The right-click menu stays.

All of these are disabled while another operation runs (`busy`) or while an
AI run is in progress for the repository.

## Design

Approach chosen: the app applies a region by its id (not the frontend
writing whole files). One parser, one write path, under the repository's
write lock that the AI tools and Take already use.

### `internal/merge`

- `Hunk` gains `ID string`, filled by `Parse`: 8 hex chars of SHA-1 over
  ours, base and theirs; a repeat of the same region in a file gets `-2`,
  `-3`. It moves here from `internal/ai/mergetools` (`regionIDs`), unchanged,
  so ids the AI already sees stay the same.
- `Hunk` also exposes its marker block's line span (`Start`, `End`, 0-based,
  End exclusive) — today the unexported `start`/`end`.
- `RegionText(h Hunk, choice string) (string, error)`: `"ours"` → `h.Ours`,
  `"theirs"` → `h.Theirs`, `"both"` → `h.Ours + h.Theirs`; anything else an
  error.
- `ResolveRegion(ctx, dir, path, id, content string) (left int, err error)`:
  refuses a path that is not one of the operation's text conflicts
  (`ErrNotInMerge`) or is a symlink; parses; finds the region by id
  (`ErrNoSuchRegion` when absent — nothing written); refuses content with
  conflict markers (`ErrMarkersLeft`); splices; writes atomically (temp file
  in the same directory, same mode, rename); returns the regions left.
  This is the write code now inside `mergetools.resolveHunk`, moved.
- `Restart(ctx, dir, path) error`: `git checkout -m -- <path>` for a path of
  the operation that was conflicted, which recreates the conflict from the
  index (and from git's resolve-undo record once the file was staged). The
  implementation verifies with a test that a staged file comes back.

### `internal/ai/mergetools`

`resolve_hunk` keeps its behaviour and messages (context/bracket guard,
region id or number) but writes through `merge.ResolveRegion`; its region
ids come from `Hunk.ID`.

### `internal/app`

- `ResolveMergeRegion(id, path, region, choice, text string) error`: under
  `writeMerge`. Content is `text` when choice is `"text"`, else
  `merge.RegionText`. When `left == 0` it stages the file (`merge.Stage`).
- `RestartConflictFile(id, path string) error`: under `writeMerge`.
- `ConflictFile` gains `Regions []Region` — `{ID, Start, End}` per region of
  the file's current text — so the frontend places the buttons without
  parsing markers.
- Refused while an AI run holds the repository (same check the resolver
  uses, `ErrChatBusy`).

### Frontend

- `lib/merge.ts`: a pure `regionAt(regions, line)` / layout helper that
  says which region (if any) starts at a text line, and which lines belong
  to which side of it (for the hover dimming and for Edit…).
- `MergeView.svelte`: the toolbar row per region, the inline editor, the
  header's Restart file, the Take buttons for `!` files.
- `lib/actions.ts`: `resolveMergeRegion`, `restartConflictFile` (with its
  confirmation), following `takeMergeSide`.
- `lib/api.ts` and the Wails bindings for the two new methods.

### Errors

- A stale region id (the AI or another window resolved it meanwhile): a
  toast "That region changed; reloaded." and the file reloads.
- A file no longer in conflict: the toast carries git's refusal.
- Everything else surfaces as the existing error toast.

## Testing

- Go, `merge`: ResolveRegion by id, stale id writes nothing, mode kept,
  non-conflicted path and symlink refused, markers in content refused;
  RegionText; Restart brings back a staged file with its markers; ids stable
  across a resolve of another region.
- Go, `app`: ResolveMergeRegion stages on the last region; refused while an
  AI run holds the repository.
- Go, `mergetools`: existing tests pass unchanged on the shared path.
- Frontend (vitest): the region layout helper; action wiring as the
  existing merge actions are tested.
- Manual: the conflict lab resolved with buttons only, then graded by
  `check.py` (Take for 7 and 9, Edit… or Both where a combination is
  needed).

## Out of scope

- Part B (AI decision cards) — its own design after A.
- Per-region undo (Restart file covers it).
- Syntax highlighting or a three-pane merge editor.

## Docs

`docs/spec/04-conflicts.md` describes the region buttons, Edit…, Restart
file and the visible Take buttons, in the same commit as the behaviour.
