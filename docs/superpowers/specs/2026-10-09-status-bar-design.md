# Status bar — design

Date: 2026-10-09. Status: approved in chat, awaiting written-spec review.

## Goal

A bar along the bottom of the window shows one of the tasks running right
now — a push, a clone continued in the background, a background fetch, a
model download, a git write the AI chat started — and, when nothing runs,
the last thing the user or the AI finished. Today each of these is only
visible where it started (the toolbar's busy label for the selected
repository, the clone dialog, Settings → Providers, the Commands panel's
running rows), and invisible once that place is closed or another
repository is selected.

## Decisions (from the brainstorm)

- **Tasks are operations the user recognises, plus clone and model
  download** — not raw git commands. "Pushing… · git-ui", "Cloning foo ·
  Receiving 42 %". A running git write with no named operation around it
  (an approved AI write, for instance) still shows, as its command line.
  The AI chat thinking and commit-message generation are **not** tasks.
- **Always visible.** At rest it shows the last result of a task started by
  the user or the AI; automatic tasks (background fetch) never replace it.
- **One task at a time, plus "+N".** The chip opens a list of every running
  task, each with its own Cancel.
- **Frontend task registry** (`lib/tasks.ts`). The sources already live in
  the frontend (busy labels, clone state, model progress) or reach it as
  events (git commands); a backend registry would duplicate the labels. The
  only backend change is a repository id on command-log entries.

## Behaviour

### Layout

A strip about 22 px high across the whole window, under the sidebar, the
repository view (and its bottom dock) and the chat column. It uses the
theme tokens, so it follows Light/Dark and High contrast. It never hides and
has no setting.

Left to right:

1. **The shown task** (or the last result, see below): a spinner, the
   task's label, the repository name in muted text (none for a clone or a
   model download), then the progress when the task has one (clone: phase
   and percentage, "Receiving 42 %"; model download: percentage), then the
   elapsed time ("12 s") once the task has run for a second, ticking every
   second. The label is cut with an ellipsis; the full text is in the
   tooltip.
2. **Cancel**, when the task can be cancelled at that moment. After a click
   it reads "Cancelling…" and is disabled until the task ends. A cancel that
   arrives after the command already ended ("command is not running") is
   ignored silently.
3. At the right edge, **"+N"** when N more tasks run. It opens a list
   upward, above the bar: every running task (the shown one included), one
   row each with spinner, label, repository, progress, elapsed time and its
   own Cancel. Clicking a row does what clicking the bar does for that task.
   Esc, a click outside, or the chip again closes it; the list closes by
   itself when no more than one task is left. ↑/↓ move between rows,
   Enter opens one.

### Which task is shown

Tasks started by the user (origin You) or the AI (origin AI) come before
automatic ones (Auto); within the same rank, the one that started last.

### At rest: the last result

When nothing runs the bar shows how the last You/AI task ended, muted:

- `✓ Pushed · git-ui · 2 min ago`
- `✗ Pulling failed · git-ui` in the error colour, the error message in the
  tooltip;
- `⊘ Pushing cancelled · git-ui`.

The relative time is "just now" under a minute, then "N min ago", "N h
ago", refreshed every 30 s. Only You/AI tasks set it: a background fetch,
even a failed one, does not (its own failures already have the toolbar's
amber dot and notifications). At start-up, with nothing finished yet, the
bar is empty. Nothing is kept across restarts. The result stays until
another You/AI task ends.

The result text comes from the operation's busy label: a table maps each
label to its past tense ("Pushing…" → "Pushed", "Deleting branch…" →
"Deleted branch", "Cherry-picking…" → "Cherry-picked", "Committing merge…"
→ "Committed merge", …); a label the table lacks reads "<label without the
ellipsis> — done". A failure or a cancel always reads "<label without the
ellipsis> failed" / "cancelled". A clone reads "Cloned <name>" / "Cloning
<name> failed"; a model download "Downloaded <model>" / "Downloading
<model> failed"; a git fallback task its command line with ✓/✗/⊘.

### Clicking

Clicking the shown task, a row of the "+N" list, or the last result opens
where its detail is:

- a clone → the clone dialog's progress view (as **Clone…** does during a
  clone), or for a finished clone, nothing;
- a model download → Settings → Providers;
- an operation, background fetch or git command → the Commands panel; if
  the task's repository is not the selected one it is selected first. A
  repository no longer in the list → nothing.

### Accessibility

The label area is a live region (`role="status"`, polite): screen readers
hear a task start and end, not each elapsed-time tick (the elapsed time is
outside the live region). The bar's buttons are reachable with Tab; the
list is a proper list with focus moving into it when it opens and back to
the chip when it closes.

## Task sources

Each task carries `{ id, kind, origin, label, repoId?, repoName, progress?,
start, cancel? }`, `kind` one of `op | clone | fetch | model | git`.

| Source | Kind / origin | Label | Cancel |
|---|---|---|---|
| Every operation that sets the busy label (`run`, `runOp` and the direct `busy.set` sites in `actions.ts`) | op / You | the busy label | the newest running git write in that repository (see below) |
| The clone (`clone.ts` stores) | clone / You | "Cloning <name>" + phase/% | `CancelClone` |
| A background fetch of one repository (`autoFetch.ts` `runRound`) | fetch / Auto | "Fetching" | the running git write in that repository |
| A model download (`PullModel`, `model:progress`/`model:done`) | model / You | "Downloading <model>" + % | `CancelPull` |
| A running git write in a repository with no op/fetch task running | git / the entry's origin | the command line (`commandLine(args)`) | `CancelCommand(repoId, entryId)` |

- **Busy label stays as it is** for the toolbar. Operations get a small
  helper that sets the busy label and registers the task in one call, and
  ends both in one call with the outcome (ok, failed + message, cancelled
  when the error is the cancel error), replacing each `busy.set(label)` /
  `busy.set('')` pair. The task's repository is the one the operation acts
  on (the selected repository at its start, or the id the operation
  already receives).
- **Running git writes.** A module-level listener on `cmdlog:entry`,
  started once at app start, keeps a map of running writes (all
  repositories) by entry id: an entry with outcome `running` adds, any
  other outcome for the same id removes. The Commands panel keeps its own
  listener unchanged. Writes in a repository that has an op or fetch task
  running are folded into that task (they give it its Cancel) rather than
  shown on their own. A cancel targets the newest running write of that
  repository; with none running at that moment, there is no Cancel.
- **Model download** state moves out of `SettingsDialog.svelte` into a
  store in a lib module, so it outlives the dialog; the dialog reads the
  same store.
- **Origin of a git fallback task** is the entry's origin (You, AI or
  Auto). An AI write approved in the chat is therefore an AI task and can
  set the last result.

### Backend change

`cmdlog:entry` events and the entries `CommandLog(id)` returns gain
`repoId`: the id of the sidebar repository whose folder the command ran in,
or empty when it ran outside every listed repository (a clone into a new
folder, for instance). `repo` (the backend's own key) is unchanged and the
Commands panel keeps filtering by it. The frontend cannot derive the id
itself: `repo` is a cleaned path that need not equal `Repo.path`.

## Edge cases

- A repository removed while its task runs: the task keeps the name it was
  registered with; clicking it does nothing.
- Several operations in different repositories at once: each is its own
  task (the busy label only reflects the last; the bar does not depend on
  it).
- The window reloads (dev only): running tasks registered before are lost;
  running git writes started before the reload are not recovered (their
  end events find no start and are ignored). Acceptable.
- Two clicks on Cancel: the second does nothing (button disabled).

## Out of scope

- Tasks for the AI chat thinking, suggested replies or commit-message
  generation.
- A history of results beyond the last one (the Commands panel is that).
- Hiding the bar, or moving it.
- Live output in the bar.

## Spec and docs

- New `docs/spec/13-status-bar.md` with the behaviour above, added to the
  table in `docs/spec/README.md`.
- `docs/spec/09-command-log.md`: running writes and Cancel are also reachable
  from the status bar.
- `docs/spec/01-repositories-and-sidebar.md` (clone): a clone continued in
  the background shows in the status bar; clicking it reopens the progress.
- `docs/spec/05-remote-and-stash.md` (Background fetch): shows in the
  status bar as an automatic task, never as the last result.
- `docs/spec/06-ai.md` (model download): shows in the status bar with its
  percentage and Cancel.
- `docs/spec/07-conventions-and-constraints.md`, where it describes how long
  operations announce themselves: the status bar alongside the busy label.

## Testing

- Vitest for `lib/tasks.ts`: which task is shown (rank, then recency);
  past-tense table and fallback; failed/cancelled texts; last result set by
  You/AI only, never by Auto; running-write map (add on running, remove on
  end, an end before its start is ignored); folding writes into an op/fetch
  task of the same repository; git fallback task otherwise; Cancel target is
  the newest running write; relative time formatting.
- Vitest for the busy helper: busy label and task set and cleared together,
  outcome passed through, cancel error → cancelled.
- Go test: entries carry `repoId` for a listed repository and empty
  otherwise, both in the event and in `CommandLog`.
- Manual pass (owner): a push with the panel closed; a clone continued in
  the background while pushing in another repository ("+1", list, Cancel);
  a model download with Settings closed; an AI write from the chat; a
  background fetch never replacing the last result; themes and high
  contrast; a narrow window.
