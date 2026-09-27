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

- ✓ or ✗ (a timed-out command is a ✗ that says "Timed out" when expanded);
- the command line, `git …`, in monospace, cut with an ellipsis (the full
  line is in the tooltip);
- who asked for it: **You** (a change made from the interface), **AI** (a
  tool call of the chat, including the changes the user approved), or
  **Auto** (a read the application made on its own, such as a refresh —
  reads made because of a click count as Auto too);
- the time it started (HH:MM:SS) and how long it took.

Clicking a row (or Enter on it) expands it: the exit code and what the
command printed, standard output and standard error apart. Each stream is
kept up to 64 KB ("Output truncated" when cut); a repository keeps at most
8 MB of output, beyond which the oldest commands lose their output ("Output
no longer kept") but stay listed. A copy button copies the command line.
↑ and ↓ move between rows.

Reads are hidden unless **Show reads** is ticked (remembered). With nothing
to show the panel says "No git commands yet", or "Only reads so far" with a
Show reads button.

## Retention and privacy

The log is in memory only: the last 500 commands per repository, gone when
the application closes. **Clear** empties the selected repository's log.
Credentials never reach the log: a URL's password or bare token
(`https://user:***@host`, `https://***@host`), the value of a `-c` setting
for an HTTP extra header or any key containing "token", "password" or
"secret", and those same secrets wherever they appear in the output, are
replaced by `***`.
