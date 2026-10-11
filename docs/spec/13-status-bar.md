# Status bar

A strip along the bottom of the window shows what the application is doing
right now and, when it is doing nothing, how the last thing the user or the
AI asked for ended. Without it, a push, a clone continued in the background
or a model download is visible only where it started — the toolbar's busy
label of the selected repository, the clone dialog, Settings → Providers,
the Commands panel — and nowhere once that place is closed or another
repository is selected.

## Layout

The bar is about 22 px high and spans the whole window, under the sidebar,
the repository view with its bottom dock, and the chat column. It is always
there: it cannot be hidden and has no setting. It is drawn in the theme's
colours, so it follows Light, Dark and High contrast.

From left to right:

1. **The shown task**, or the last result when nothing runs (see below).
   For a task: a spinner; its label; the repository's name in muted text
   (none for a clone or a model download); its progress when it has one —
   a clone shows git's phase and percentage ("Receiving 42 %"), a model
   download its percentage; and the elapsed time ("12 s"), which appears
   once the task has run for a second and then counts every second. The
   label is cut with an ellipsis when the window is narrow; the full text
   is in the tooltip.
2. **Cancel**, when the task can be cancelled at that moment. After a click
   it reads "Cancelling…" and is disabled until the task ends. A cancel
   that reaches a command which has just ended on its own ("command is not
   running") cancelled nothing: it is ignored silently and the button goes
   back to "Cancel" (the operation may already be running its next
   command). Any other failure to cancel is an error toast and the button
   is usable again too. "Cancelling…" disappears with the task.
3. At the right edge, **"+N"** when N more tasks run ("+2" for two more).

## What counts as a task

- **An operation**: anything that sets the toolbar's busy label ("Pushing…",
  "Committing…", "Deleting branch…", and so on), for the repository it acts
  on. Started by the user, so a You task. The label is the busy label.
- **A clone**: "Cloning <name>", with git's phase and percentage, from the
  moment it is started — whether the clone dialog is open or the clone
  continues in the background. A You task. It has no repository name.
- **A model download**: "Downloading <model>", with its percentage, started
  from Settings → Providers (it keeps running when Settings is closed). A
  You task. It has no repository name.
- **A background fetch**: each repository's automatic fetch round
  (`05-remote-and-stash.md`) shows as "Fetching" with the repository's name,
  while it runs. An Auto task.
- **A git write** that belongs to none of the above: a change the AI chat ran
  after the user approved it, for example, shows as its command line
  (`git commit -m …`) with the repository's name. Its task is of the
  command's own origin: You, AI or Auto (`09-command-log.md`). Writes of a
  repository that has an operation or a background fetch running are part of
  that task and are not shown on their own; they are what its Cancel stops.
  Reads are never tasks.

Not tasks: the AI chat thinking, the suggested replies, and the generation
of a commit message. They have their own indicators in the places they
happen.

## Which task is shown

Tasks started by the user (You) or by the AI come before automatic ones
(Auto); among tasks of the same rank, the one that started last is shown.
The others are counted in "+N".

## Cancel

What Cancel stops depends on the task:

- a clone: the clone, as the dialog's Cancel does (its "Cancelling…" is the
  same in both places);
- a model download: the download;
- a git write shown on its own: that command;
- an operation or a background fetch: the newest git write its repository is
  running at that moment. When it runs none (the operation is between two
  commands, or is waiting for the user), there is no Cancel button for it.

A cancelled command ends its task like any other; the effect on the
repository is the one described in `09-command-log.md` for that operation.

## The "+N" list

"+N" opens a list upward, above the bar, with every running task — the
shown one included — one row each: spinner, label, repository, progress,
elapsed time and the row's own Cancel. Clicking a row does what clicking the
bar does for that task. When the list opens, focus moves to its first row;
↑ and ↓ move between rows and Enter opens one. Esc, a click outside, or the
"+N" chip again closes it, and focus goes back to the chip. The list closes
by itself when no more than one task is left.

## At rest: the last result

When nothing runs, the bar shows, in muted text, how the last task started
by the user or the AI ended:

- `✓ Pushed · git-ui · 2 min ago`
- `✗ Pulling failed · git-ui · just now`, in the error colour; the error
  message is in the tooltip;
- `⊘ Pushing cancelled · git-ui · 5 min ago`.

The time is "just now" under a minute, then "N min ago", then "N h ago",
kept current while the app stays open. The result stays until another task
of the user or the AI ends. Only those tasks set it: an automatic one —
a background fetch, even a failed one — never does (a failed background
fetch has the toolbar's amber dot and its notifications), so a background
fetch never replaces the last result. A git write shown on its own that is
Auto does not either. At start-up, with nothing finished yet, the bar is
empty, and nothing is kept across restarts.

The text of the result comes from the task's label:

- an operation reads in the past tense of its busy label — "Pushing…"
  becomes "Pushed", "Deleting branch…" "Deleted branch", "Cherry-picking…"
  "Cherry-picked", "Committing merge…" "Committed merge", and so on for
  every label. A label with no past tense of its own reads "<label without
  its ellipsis> — done";
- a failure reads "<label without its ellipsis> failed" and a cancel
  "<label without its ellipsis> cancelled", for every kind of task
  ("Pulling failed", "Cloning foo failed", "Downloading llama3 cancelled");
- a clone that succeeded reads "Cloned <name>", a model download
  "Downloaded <model>";
- a git write shown on its own reads as its command line, with ✓, ✗ or ⊘
  for how it ended (the failure's message in the tooltip).

An operation's result is its own: the git writes it ran do not add results
of their own.

## Clicking

Clicking the shown task, a row of the "+N" list or the last result opens
where the detail is:

- a clone, while it runs: the clone dialog's progress view, as **Clone…**
  does during a clone; a finished clone: nothing;
- a model download, running or finished: Settings, on the Providers tab;
- an operation, a background fetch or a git write, running or finished: the
  Commands panel of its repository, opened if closed; when that repository
  is not the selected one it is selected first. A repository that is no
  longer in the list, or is missing: nothing.

## Accessibility

The bar has one live region (polite), visually hidden and outside the
buttons, so it stays in place while the visible text changes. It says the
shown task ("Pushing… git-ui", the label and the repository's name) when it
starts, and the last result ("✓ Pushed git-ui") when it ends. Progress, the
elapsed time and "N min ago" are not in it, so they are not read as they
tick. An Auto task (a background fetch) is left out: while only Auto tasks
run, the region keeps the last result. The bar's buttons are reachable with Tab; "+N" says that it
opens a popup and whether it is open; the list is a labelled list ("Running
tasks"). Animations stop when the system asks for reduced motion.

## Edge cases

- A repository removed while its task runs: the task keeps the name it
  started with; clicking it does nothing.
- Several operations in different repositories at once: each is its own
  task; the toolbar's busy label reflects only the last one, the bar does
  not depend on it.
- Closing the window or reloading it forgets the tasks; a git write that
  started before a reload and ends after it is not shown.
