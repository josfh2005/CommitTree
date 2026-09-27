# Command log

Every git command the application runs for a repository — because the user
clicked something, because the AI chat ran a tool, or because a view
refreshed — is recorded, with how it ended, and can be read in the Commands
panel. Commands typed in the embedded terminal are not recorded: the
terminal already shows them and their output.

## Opening it

The Commands panel lives in the bottom dock, under the repository view,
next to the terminal (see `08-terminal.md`). It opens and closes with the
repository toolbar's "Commands" toggle, the button in its own header, or
**Cmd+Shift+J** on macOS / **Ctrl+Shift+J** elsewhere (left to the shell
while focus is in the terminal, like Ctrl+J). It cannot open with no
repository selected or a missing one. When both the terminal and Commands
are open they share the dock's width with a draggable divider (each at
least 240 px); alone, either takes the whole width. The dock's height is
shared, draggable, and remembered.

## What it shows

The selected repository's commands, newest first. Each row shows:

- ✓ or ✗ (a timed-out command is a ✗ that says "Timed out" when expanded),
  ⊘ for a cancelled command ("Cancelled"), or a spinner while it runs;
- the command line, `git …`, in monospace, cut with an ellipsis (the full
  line is in the tooltip);
- who asked for it: **You** (a change made from the interface), **AI** (a
  tool call of the chat, including the changes the user approved), or
  **Auto** (a read the application made on its own, such as a refresh —
  reads made because of a click count as Auto too);
- the time it started (HH:MM:SS) and how long it took — while it runs, the
  seconds elapsed so far.

Clicking a row (or Enter on it) expands it: the exit code and what the
command printed, standard output and standard error apart. Each stream is
kept up to 64 KB ("Output truncated" when cut); a repository keeps at most
8 MB of output, beyond which the oldest commands lose their output ("Output
no longer kept") but stay listed. A copy button copies the command line.
↑ and ↓ move between rows.

Reads are hidden unless **Show reads** is ticked (remembered). With nothing
to show the panel says "No git commands yet", or "Only reads so far" with a
Show reads button.

## Running commands and Cancel

A write appears as soon as git starts, with a spinner and its elapsed time
counting every second; when it ends the same row shows how it ended. Reads
appear only when they end, whether or not **Show reads** is ticked. A
running row cannot be expanded (there is no live output) and has a
**Cancel** button: it stops the command as Ctrl+C would in a terminal — git
and any hook or helper it started get an interrupt. Five seconds later, git
itself is killed if it is still alive (on Windows the command is killed at
once); a hook or helper that ignores the interrupt can keep running past
that point on its own. A command that runs past its time limit is
interrupted the same way and shows "Timed out".

The operation that ran the command reports an error, "git command
cancelled" — never the hook output or exit-signal text a cancelled git can
leave on stderr — the way it reports any failure: a toast for an action
from the interface, a failed change for the AI chat. What a cancel leaves
behind depends on the operation:

- **Pull, plain Merge**: left exactly as git stopped it, mid-merge or
  mid-rebase, for the conflict banner to continue or abort — the same
  banner a real conflict would show. (A merge or rebase already in
  progress before a Pull refuses it outright, and could not have been
  caused by this Pull's own cancel.)
- **Rebase, Cherry-pick**: on any stop that is not a real conflict —
  including a cancel — these already run their own `rebase --abort` /
  `cherry-pick --abort`, restoring the branch to where it was, the same as
  any other real failure. Only a genuine conflict is left for the banner.
- **Continuing a conflict resolution** (the banner's Continue, on a merge,
  rebase, cherry-pick, revert or `am`): a cancel can land after the
  resolved step has already committed but while git is still moving on to
  the next commit, so it is reported as cancelled rather than as the
  operation quietly moving on — the sequencer is left wherever git left
  it, which may be mid-way onto the next commit's conflict rather than
  cleanly stopped on it.
- **Stash apply / pop**: the same as any other cancel — reported as
  cancelled and the worktree left as git stopped it. If the cancel happens
  to land after git had already written conflicting entries for the files
  it was merging, that unmerged state looks exactly like a real stash
  conflict, and the app reports it as one — a stash conflict is a normal
  outcome, not a git error, so the cancel is masked in that narrow window.

A helper daemon a git command started without detaching, such as the
credential cache daemon, is stopped by a cancel or a timeout along with
everything else in the process group; it is not gone for good and starts
again the next time a command needs it. **Clear** leaves running commands
in place.

## Retention and privacy

The log is in memory only: the last 500 commands per repository, gone when
the application closes. **Clear** empties the selected repository's log.
Credentials never reach the log: a URL's password or bare token
(`https://user:***@host`, `https://***@host`), the value of a `-c` setting
for an HTTP extra header or any key containing "token", "password" or
"secret", and those same secrets wherever they appear in the output, are
replaced by `***`.
