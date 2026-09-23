# Terminal

This area covers the embedded shell: a real interactive terminal, tied to a
repository, that lives below the chat panel. It exists for the things the
rest of the application does not cover — anything from `npm install` to a
one-off git incantation — without leaving the window.

## Concepts

- **Tab**: one running shell, opened at a repository's root. A repository
  can have any number of tabs; tabs of repositories other than the selected
  one keep running in the background rather than being closed.
- **Settling**: the point, after the user presses Enter, at which the shell's
  output has stopped for a short stretch — the signal the application uses
  to treat a typed command as "probably done" and check whether anything
  about the repository changed.

## Tabs

Each repository has its own set of tabs, independent of every other
repository's. Opening the terminal on a repository with no tabs yet opens
one automatically, started at the repository's root. A tab's label is the
shell's own name plus a number scoped to that repository — `zsh 1`,
`zsh 2`, and so on — where the number is handed out once and never reused
for that repository while the application keeps running, even after the
tab it named is closed. A `+` button beside the tabs opens another shell for
the same repository.

A shell that exits on its own — typed `exit`, or the process behind it
dying — does not close its tab. The tab stays, its label gains
` — exited (<code>)`, and its scrollback remains visible until the user
closes it explicitly.

Closing a tab, whether it is still running or already exited, never asks
for confirmation. Closing sends SIGHUP to the shell's own process group and
to the terminal's current foreground process group (so a job-control shell's
running job is hung up too, the same as closing a real terminal window
closes its jobs), then waits up to two seconds; if the shell itself has not
exited by then, SIGKILL is sent to both groups. A background job the shell has disowned
(`nohup`, `disown`) survives the shell's own SIGHUP the same way it would
survive closing an external terminal.

Closing the last tab of a repository does not close the panel: it leaves an
"Open shell" button in its place, and the panel stays visible until the user
hides it or opens a new tab.

## Layout and visibility

The terminal lives in the same column as the chat panel, below it, separated
by a draggable divider; its height is remembered between sessions. The chat
and the terminal open and close independently of each other — closing one
leaves the other filling the whole column. Two toggles open the terminal: a
button labelled "Terminal" (icon and text, so it is found without hunting)
in the log view's header, disabled when no repository is selected or the
selected one is missing, whose tooltip names the shortcuts; and the same
button in the terminal panel's own header to hide it again. Two shortcuts
toggle it from anywhere in the window, both matched on the physical key
rather than the character it produces:

- **Cmd+J** on macOS, **Ctrl+J** elsewhere — the one that works on every
  keyboard layout. Ctrl+J is a line feed to a shell, so on Windows and Linux
  it is left to the shell while focus is inside the terminal itself; Cmd+J
  never reaches the shell and works there too.
- **Ctrl+`**, kept for layouts where the backquote key is easy to reach. It
  is matched on the physical key so it still fires where backtick is a dead
  key, but on a Spanish ISO Mac keyboard it is awkward enough that Cmd+J is
  the one to use.

Switching the selected repository does not tear down any tab's shell: every
tab, across every repository, keeps a live terminal instance with its own
scrollback (5 000 lines); only the selected repository's tabs are shown, the
rest sit hidden. A long-running program such as `top`, left running in a
background repository's tab, keeps running and is exactly where it was left
when that repository is selected again. Hiding the terminal panel itself —
with either toggle or a shortcut — behaves the same way: every tab's shell keeps
running and its scrollback is kept exactly as it was, since hiding only
changes what is drawn, not what is mounted; nothing is torn down until a tab
is closed explicitly or the application restarts. Tabs are not restored
across an application restart — every tab, running or exited, is gone once
the application closes.

Resizing the panel (dragging either divider) resizes the visible terminal to
match, the same as resizing a real terminal window would.

## Staying fresh

A shell run from the terminal can change the repository — a commit, a
checkout, a branch delete — without the application knowing, since it does
not go through any application code. To catch up, the application treats a
command finishing in the terminal the same way it treats the window
regaining focus: once the terminal detects that a command has "settled"
(the user pressed Enter, and then 400 ms passed with no further output), it
runs the same check the window-focus handler runs. Output with no Enter in
it — a program that only prints, or one still running without having been
sent a newline — never triggers the check; settling requires both.

What gets refreshed depends on whether the tab belongs to the currently
selected repository:

| Tab's repository | What refreshes |
|---|---|
| The selected repository | The full check: merge and working-tree state reloaded, the fingerprint compared, and the repository refreshed if anything changed — identical to a window-focus refresh. |
| Any other repository | Only the repository list, so that repository's sidebar entry (its branch label, in particular) stays correct. Its log, working tree and other selected-repository state are not reloaded, since nothing is currently showing them. |

## Busy notice

While an application operation (a commit, a pull, a merge, and so on) is
running, the terminal's tab bar shows "Operation running: `<label>`" using
the same label the rest of the interface already shows for that operation —
`busy` is a single, application-wide label (see Conventions and
constraints), not one scoped to the selected repository, so the notice shows
regardless of which repository the operation targets. This is informational
only — see Safety below for why it does not stop the user from typing.

## Safety

- **Not sandboxed.** The shell runs as the user, with their own login-shell
  profile and the application's own environment, including its `PATH`
  repair, plus `TERM=xterm-256color` and `COLORTERM=truecolor`. If none of
  `LC_ALL`, `LC_CTYPE` or `LANG` is already set in that inherited
  environment — as happens when the application is launched from Finder
  rather than a shell — `LANG=en_US.UTF-8` is added too, so shell output
  that relies on a UTF-8 locale still renders correctly. This is not the
  same environment a git invocation gets: `GIT_TERMINAL_PROMPT=0` and
  `LC_ALL=C` are added only to the application's own git commands, not to
  the shell. The shell can do anything a terminal application on the same
  machine can do.
- **Bypasses the write lock, by design.** Every mutating action the rest of
  the application performs takes that repository's write lock first (see
  Conventions and constraints); the embedded terminal does not, and nothing
  it runs waits for one either. Requiring it to would let a pager or an
  editor left open in a shell hold the lock indefinitely, and pausing
  keystrokes while an application operation runs would freeze something like
  Ctrl-C during a long push. Git already protects its own on-disk state with
  its own locks (`index.lock`, ref locks); the realistic worst case of the
  resulting race is a git error such as "index.lock exists," not a
  corrupted repository — the same exposure an external terminal open on the
  same repository already has today. This is unchanged even while the busy
  notice above is showing.
- **No AI access.** The assistant never reads a terminal tab's output and
  has no tool that can type into one (see AI). Scrollback can hold secrets —
  an access token, the contents of a `.env` file someone `cat`s — which is
  the reason access is withheld rather than merely unimplemented.

## Rules

1. Tabs belong to a repository; only the selected repository's tabs are
   shown, and switching repositories never stops or restarts a hidden tab's
   shell. Hiding the terminal panel itself has the same effect: every tab's
   shell and scrollback survive until the tab is closed or the application
   restarts.
2. A tab's number is unique per repository for the life of the running
   application and is never reused, even after the tab it named is closed.
3. An exited shell keeps its tab, labelled with its exit code, until closed
   by hand.
4. Closing a tab never asks for confirmation, regardless of whether the
   shell has already exited.
5. Opening the terminal on a repository with no tabs opens one
   automatically; closing the last tab leaves an "Open shell" state rather
   than closing the panel.
6. Tabs, running or exited, do not survive an application restart.
7. A settle (Enter, then 400 ms of quiet) refreshes the full state for the
   selected repository, and only the repository list for any other.
8. The embedded terminal never takes or waits on the per-repository write
   lock, and the application never sandboxes or filters what runs in it.
9. The assistant has no access to any terminal tab, in either direction.

## Known divergences

None.
