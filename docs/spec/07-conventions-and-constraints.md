# Conventions and constraints

This document holds what is true everywhere in CommitTree rather than in one
screen: the vocabulary the interface uses for busy states, errors and
confirmations; the constraints that follow from driving the git command line
instead of a library; and, in the appendix, the git behaviour a rebuilder
would otherwise discover the hard way.

## Concepts

- **Write lock**: a per-repository guard that lets only one mutating
  operation run at a time against that repository.
- **Busy state**: a single, application-wide indicator that a mutating
  operation is in flight, shown as a short label.
- **Toast**: a small, self-dismissing message reporting the outcome of an
  action.
- **Confirmation dialog**: a modal that asks the user to approve an action
  before it runs, styled as ordinary or as dangerous.
- **Prompt dialog**: a modal that collects one or two text values (and,
  optionally, a checkbox) before an action runs.
- **Choice dialog**: a confirmation whose message and confirm button change
  with a choice the user makes inside it.
- **Context menu**: a list of actions attached to a row or item, opened by a
  right-click.
- **Hover actions**: buttons on a list row that are present but invisible
  until the row is hovered, focused, or selected.
- **Fingerprint**: a short value summarising a repository's refs and HEAD,
  used to detect that something changed outside the application.

## Interaction conventions

### Busy state and blocking

A mutating operation sets a single, application-wide busy label (for
example "Committing…", "Deleting branch…") for its duration. This is not a
full-screen modal overlay: the rest of the interface stays visible and the
label is shown as a small note in the sidebar. Its effect is to disable
every button in the interface that would start another mutation — across
every repository, not only the one the running operation targets. The
frontend's busy state is therefore coarser than the backend's guarantee: the
backend serialises writes per repository (see below) and would happily run
a write against a second repository while the first is busy, but the
interface does not offer that at all. Read views keep working while an
operation is busy: they are not gated by it.

An operation clears the busy label when it finishes, whether it succeeded
or failed, and then refreshes the affected views (refs, log, working tree)
so the interface reflects the repository's new state rather than trusting
its own prediction of it.

### Errors

A failed operation shows a toast styled as an error; it does not open a
dialog and does not block further action. An error toast stays until
dismissed by the user. An informational toast (for example "Copied")
dismisses itself after a few seconds. Toasts carry the failure's message
text as reported by the backend — typically git's own stderr — rather than
a generic message, so the user sees the same words git produced.

### Confirmation dialogs

A confirmation dialog states what will happen and offers Cancel and one
labelled confirm button. The confirm button is styled as dangerous (a
different colour from the ordinary, primary style) or not, and this styling
is decided per call, not per action name.

**The rule the styling follows is reversibility, not deletion.** Verified
against the code: an action that deletes something is not automatically
styled dangerous, and an action that deletes nothing can be. Examples:

- Removing a repository from the sidebar list deletes an entry from the
  list, but its confirmation is not styled dangerous — the files on disk
  are untouched and re-adding the repository restores it exactly. The
  dialog's own message says as much.
- Resetting the current branch is offered as a choice of soft, mixed or
  hard. Only the hard option's confirmation is styled dangerous, because
  only hard discards uncommitted changes; soft and mixed move the branch
  pointer without losing anything.
- Merging a branch, checking out a commit (which detaches HEAD), and
  applying or popping a stash entry are not styled dangerous: each is a
  state that can be undone by another ordinary action in the interface.
- Deleting a branch or a tag, dropping a stash entry, taking one side of a
  conflicted file, discarding an uncommitted change, and aborting a
  conflict resolution in progress are all styled dangerous: each throws
  away something the interface has no other way to bring back.

A context menu may separately colour one of its own entries as if it were
dangerous (red text on the menu item) based on the action's name looking
destructive — "Remove from list" is coloured this way in its menu even
though the confirmation dialog it opens is not styled dangerous. The two
"dangerous" signals are decided independently; only the confirmation
dialog's styling follows the reversibility rule above.

The AI chat's approval card for a proposed write (see AI) is not this
confirmation dialog: it lives inline in the chat conversation rather than as
a modal, it names an operation the model chose rather than one the user
already started, and it offers Reject and Approve rather than Cancel and a
single confirm button. It plays the same role a confirmation dialog would —
nothing the model proposes runs until the user approves that card — but is
its own mechanism, scoped to the chat panel.

A confirmation dialog may also carry a checkbox alongside its message, used
when a choice needs to be remembered as part of confirming (for example,
whether to also apply an action to a related item). The checkbox's state is
only meaningful when the user confirmed; cancelling discards it.

### Prompt and choice dialogs

A prompt dialog collects one required text value and, optionally, a second
labelled value and a checkbox, then returns them together (or null if
cancelled) — used for naming a new branch or tag, or renaming a group. Its
submit button is never styled dangerous.

A choice dialog is a confirmation built around a set of mutually exclusive
options (for example the three reset modes). Choosing an option updates the
dialog's message and confirm-button label live, and can change whether the
confirm button is styled dangerous, without closing and reopening the
dialog.

Every dialog closes on Escape (as a cancel) and on a click outside it (also
a cancel); a prompt's or choice's primary input is focused automatically
when it opens.

### Context menus

A context menu opens at the pointer, positioned so it stays inside the
window rather than being clipped at an edge. It closes on an outside click,
on the window losing focus, on resize, or on Escape. Its items can be
individually disabled (shown but not clickable) — used, for example, to
grey out an action that the current busy state or repository state makes
unavailable rather than removing it from the menu.

### Row hover actions

A list row (a changed file, a merge file) can carry one or more small
action buttons pinned to its right edge. These buttons exist in the layout
at all times but are invisible until the row is hovered, until the row is
the current selection, or until one of the buttons itself receives keyboard
focus — the last case is what makes the actions reachable without a mouse.
Making a button visible only, rather than removing it from layout, keeps
the row's width and the position of other elements stable as the pointer
moves. A hover action can itself be styled as dangerous and can be
disabled while the application is busy.

### Reacting to the window regaining focus

The application does not poll the filesystem or the git repository while
idle. Instead, when the window regains focus:

- The merge/conflict state for the selected repository is always reloaded,
  because git state such as a conflict abort performed outside the
  application (in a terminal) is not something a lighter check could catch.
- The working tree state is always reloaded.
- The selected repository's fingerprint (a summary of its refs and HEAD) is
  compared against the value recorded the last time it was known; if it
  changed, the fuller refresh (refs and log) also runs. This avoids a full
  reload on every focus when nothing changed, while still catching a branch
  switch, commit, or fetch made outside the application. The fingerprint is
  re-recorded whenever the selected repository changes or the log is
  reloaded, not only on focus.

No other event triggers a refresh: switching to the application from
another window is the only moment external changes are checked for.

### Application menu and opening Settings

The sidebar's Settings row shows the app's version (for example v0.2.0) on
its right, muted; the version comes from `wails.json` (`info.productVersion`),
which also stamps the macOS bundle.

Settings opens from the sidebar's Settings row and, following each system's
convention, from the keyboard. On macOS the menu bar's CommitTree menu has
Settings… (⌘,), Hide CommitTree (⌘H) and Quit CommitTree (⌘Q), followed by
the standard Edit and Window menus. The system's usual Hide Others and Show
All entries are not offered. On Linux there is no menu bar, and Ctrl+, opens
Settings.

The Settings dialog has a fixed size, with tabs in a column on the left:

- **General**: Appearance, the Git pull strategy and what Push does (ask, current branch, all branches).
- **Providers**: Anthropic and OpenAI, each with its API key (Save, or the
  stored key's hint and Remove), and Ollama with its status, URL (Test), the
  warning when the URL is not this machine, the installed models (name, size,
  and a "chat" or "explain commit" badge on the ones in use), and model
  downloads (the chat
  model when Ollama is the chat provider and it is not installed, or any
  other model by name, with progress and Cancel).
- **AI models**: the chat and explain-commit providers and models, the
  commit message mode and suggested replies.
- **Prompts**

It opens on the tab last used; a remembered API keys tab, from before
Providers replaced it, opens on Providers. Only the tab's own content
scrolls. The note on where AI requests are processed shows on the Providers
and AI models tabs.
If the AI settings cannot be loaded, the General tab still works and the AI
tabs show the error.

### Appearance

Settings has an Appearance section. Its Theme control offers Auto, Light and
Dark: Auto (the default) follows the system's light or dark appearance and
switches with it while the application is open; Light and Dark keep that
palette whatever the system says. Native controls (selects, date fields,
scrollbars) and the embedded terminal follow the chosen theme too. The
window's own background, seen for an instant at launch before the page
paints, stays light.

Below it, a High contrast checkbox works with every theme. It
changes only text colours: text becomes black (or white in dark mode), and
secondary text such as dates, authors and hashes becomes darker (or
lighter). Backgrounds, borders, hover, selection and the accent, graph lane,
ref badge and diff colours stay as they are. Every kind of text keeps a contrast of at
least 4.5:1 against the backgrounds it appears on. Both choices apply at once
and are remembered on this computer.

## Architectural constraints that shape behaviour

### The old name stays on disk

The application was first called git-ui. It is now CommitTree, but
everything that identifies stored data keeps the old name so that an
existing install keeps its repositories, settings, chats and API keys:
the settings folder (`~/Library/Application Support/git-ui/` on macOS),
the secret-store service name (`git-ui`), and the prefixes of temporary
files. Only what the user reads says CommitTree.

### The application drives the git command line

CommitTree has no embedded git implementation; every operation shells out to
the system's `git` binary. This is not an incidental detail — several
behaviours exist only because of it:

- **No interactive prompts.** Every git invocation runs with
  `GIT_TERMINAL_PROMPT=0`, so a command that would otherwise pause for
  credentials or a yes/no answer fails immediately instead of hanging the
  application. There is no way for git to ask the user anything through a
  terminal; anything git needs must already be configured (a credential
  helper, SSH agent, and so on) outside the application.
- **A fixed locale for output.** Every invocation runs with `LC_ALL=C`, so
  git's messages are always in English and in their default wording. This
  is what lets the application match specific strings in git's output (for
  example, to detect a no-op) reliably rather than guessing at a
  locale-dependent phrase.
- **Different timeouts for different kinds of command.** An ordinary read
  (status, log, diff) is capped at ten seconds. A command that talks to a
  remote (fetch, pull, push) is allowed up to five minutes. A command that
  may run one of the repository's own hooks (a `--continue` step, which can
  trigger a commit hook) is allowed up to two minutes — long enough for a
  slow hook, short enough that a hook stuck waiting on input is eventually
  killed. A command that times out is reported as a distinct timeout error,
  not folded into a generic failure, so the interface can say so. If a
  killed git process leaves a child (such as a credential helper) holding
  its output pipe open, the application does not wait indefinitely for that
  child either — it gives it a short grace period before moving on.
- **No editor ever opens.** Any git subcommand that might otherwise launch
  an editor (committing without a message, continuing a rebase, cherry-pick
  or revert, applying a mailbox patch) is either given its content directly
  on the command line or told not to invoke one. Where a command has to be
  stopped from invoking an editor rather than being given content, the
  application does so by setting the `GIT_EDITOR` environment variable for
  that invocation, not by passing a git configuration override — an
  environment variable git will always read before its own configuration,
  so a value the user has set in their own shell profile (which would
  arrive as an inherited environment variable of the same name) cannot
  override it.

### One write lock per repository

Every operation that changes a repository's state acquires that
repository's own lock before running and releases it when the operation
completes, whether it succeeded or failed. A second mutating operation
that arrives for the same repository while one is already running is
refused outright, immediately, with a distinct "busy" error — it does not
queue, and it does not wait. A mutating operation on a different repository
is not affected by another repository's lock; the two run independently.
(The interface layered on top of this is more conservative: see "Busy
state and blocking" above.) Read operations are never blocked by the lock.

A background fetch (see Remote and stash) takes the same lock, but only
when it is free, and gives way: a user write that finds it holding the lock
cancels it and gets the lock as soon as git has stopped, before any other
write. A second write arriving meanwhile is busy, as usual.

The embedded terminal (see Terminal) is the one deliberate exception: a
shell typed into by the user neither takes nor waits on this lock, the same
way an external terminal open on the same repository never has. Requiring
it to would let a pager or editor left open in a shell hold the lock
indefinitely, and pausing keystrokes while an app operation runs would
freeze Ctrl-C during a long push.

A write proposed by the AI chat (see AI) does not take this lock while it
waits for the user to approve or reject it — only the eventual, approved
operation does, exactly as if the user had triggered it from the toolbar.
The wait can be arbitrarily long, and taking the lock for its duration would
let one unanswered chat card block every other write against that
repository; instead, the repository is re-checked for changes at the moment
of approval, immediately before the lock is taken, and the write is refused
if anything moved in the meantime.

A detected git worktree (see Repositories and sidebar) is a repository for
this purpose: it has its own lock, independent of its main repository's and
of its sibling worktrees'. They share one object database and one set of
refs, so two of them can be written at the same moment; git's own ref
locking still serialises the ref updates themselves, and a branch can only
be checked out in one of them at a time (git refuses the second).

A submodule write (Initialise, Update, Sync, and their header "all"
counterparts) is the one operation that holds more than one lock: the top
repository's, and the lock of every submodule id it touches (one for a
single-submodule action, every submodule under the repository for
Initialise all / Update all). A single-submodule action on a *nested*
submodule (one that is itself inside another submodule, not the top
repository) takes a third lock in between: its direct parent's — the
command also runs there, with the submodule's path relative to it, rather
than in the top repository with a top-relative path, which git refuses.
Every one of those locks is acquired with the
same non-waiting `TryLock` as an ordinary write, in order, before the
command runs; if any of them is already held, the whole operation is
refused with the same "busy" error and every lock it had already acquired
is released, so a refused write never leaves one behind. These run with the
same environment, timeout and credential handling as Fetch, since they may
need the network.

### Repository identity and detected worktrees and submodules

Every per-repository operation names its repository by an identifier. A
stored repository's identifier is derived from its path when it is first
added and never changes. A linked worktree detected under a listed
repository gets an identifier derived from its path the same way, every
time it is listed, so its conversation and remembered UI state survive
restarts and return if a worktree is recreated at the same path (terminal
shells never survive a restart, for any repository). An initialised
submodule detected under a repository gets an identifier derived from its
absolute path the same way, every time it is listed, whether that parent is
a stored repository or a detected worktree. An identifier is resolved
against the stored repositories first, then against the worktrees, then
against the submodules the latest read of the repository list detected; a
worktree or a submodule that has since disappeared (removed, deinitialised,
or its parent gone) no longer resolves. Detected worktrees and submodules
are not list entries: removing, grouping or relocating one is refused as an
unknown repository.

### State kept in a handful of JSON files, not a database

Everything the application needs to remember between runs is stored as
plain JSON files: the list of known repositories (with each one's display
name and its assigned sidebar group), per-repository git preferences,
AI/assistant settings, and a per-repository store of chat history. Each
file is written by producing the new contents in full, saving it to a
temporary file beside the real one, and only then replacing the real file
with it — so a write that is interrupted partway (a crash, a forced quit)
leaves the previous, still-valid file in place rather than a truncated or
corrupted one. There is no database, no schema migration system, and no
partial-record update: every save rewrites the whole file.

One category of stored value is deliberately kept out of these files: any
API key for an AI provider is kept in the operating system's own secret
store (its keychain or equivalent) rather than in a settings file, and
there is no plain-text fallback if that store is unavailable — saving the
key simply fails.

Some state is deliberately held only in memory and is lost when the
application restarts:

- The per-repository write lock itself, and the record of which repository
  currently has an operation running.
- The graph layout computed for the commit log's currently loaded page, so
  a further page can be laid out consistently with what came before. Losing
  it on restart only costs recomputation; the log itself is read fresh from
  git each time regardless.
- Which chat or model-download run is currently in progress, and the means
  to cancel it. A restart cannot leave an orphaned background run because
  the run does not survive the restart either.
- A reminder that a stash entry is still owed a drop after a conflicted
  stash pop was resolved through the merge view rather than through the
  stash view directly (git itself leaves the stash entry in place when a
  pop conflicts, meaning to remove it once the conflict is resolved). If
  the application restarts before that resolution happens, the reminder is
  lost: the stash entry is not dropped automatically, and it is left for
  the user to drop by hand — the loss is a missed convenience, not a loss
  of data, since the entry itself is still exactly where git left it.

### No server and no telemetry

The application makes no network connection of its own beyond running git
commands that themselves talk to a remote (fetch, pull, push), and beyond
calls the user's own configured AI provider requires when the assistant
features are used. It exposes no local server or listening port, and sends
no usage data or analytics anywhere. Everything it knows about the user's
repositories and preferences stays in the JSON files and the secret store
described above.

### macOS and Linux only

The embedded terminal (see Terminal) is built on a real pseudo-terminal on
the backend and a terminal emulator component in the frontend, and is only
available on macOS and Linux; there is no Windows console (ConPTY)
implementation. Every other feature is unaffected by this.

### How the backend tells the frontend that something changed

The backend does not poll and does not push a general "state changed"
signal. Instead, after a mutating call completes, the backend emits one of a
small number of named events (that the working tree changed, that the merge
state changed, that a repository changed through some other means, plus a
pair of events used only to stream a generated commit message into the box
as it is produced and to mark that stream done). The frontend listens for
these named events and re-reads whatever they concern from the backend
rather than the event itself carrying the new state — the event is a signal
to re-fetch, not a payload to render directly. Nothing is pushed to the
frontend that was not caused by a call the frontend itself made, except for
the window-focus checks described above, which the frontend initiates on
its own.

A write proposed and approved through the AI chat emits the repository-
changed event on completion regardless of whether it succeeded or failed,
as long as it was actually attempted (the write lock was taken) — a failure
partway through, such as a branch created but not checked out, can still
have changed the repository, so the frontend still needs to refresh. It is
not emitted when nothing was attempted at all: the write was rejected, or
refused because the repository had changed since it was proposed, or
refused because the repository was already busy.

## Appendix: git command-line behaviour worth knowing

These are traps this project hit, verified against the code's own comments
and tests rather than assumed.

- **`git stash list` cannot be trusted for structured output.** Verified
  against git 2.54: `git stash list --format=…` and `-z` are silently
  ignored — `git stash list --format=%gd%x00%s%x00%H` still prints the
  plain default text, with no NUL separators and no commit hash at all.
  The fix used here is to read `git reflog show --format=… refs/stash`
  instead: the stash is itself a reflog, and `reflog show` honours the same
  format placeholders `git log` does, so it is the only reliable way to get
  each stash entry's hash alongside its message. A repository that has
  never had a stash has no `refs/stash` at all, and `reflog show` on a
  ref that does not exist fails with exit code 128 — that ref's existence
  has to be checked first, rather than trying to read the reflog and
  treating any failure as "no stashes", since a real failure would look
  the same.

- **`git stash push` reports "nothing to do" with a successful exit code.**
  When there is nothing to stash, `git stash push` prints "No local changes
  to save" to stdout and exits 0 — it is not an error at the process level,
  only a message a caller must notice. Treating a zero exit as success
  outright would make the application believe a stash was created when
  none was.

- **Forcing off an editor must go through the environment, not
  configuration.** Git resolves the `GIT_EDITOR` environment variable
  before it consults `core.editor` from any configuration source. A
  `-c core.editor=true` override on the command line would still lose to a
  `GIT_EDITOR` the user has exported in their own shell profile, which the
  application's child process inherits. Setting `GIT_EDITOR` itself (to a
  no-op command) is the only override that is guaranteed to win, which
  matters for any `--continue` step that might otherwise try to open a
  real, uncloseable editor inside a GUI application with no terminal.

- **A conflicted cherry-pick or revert must be told apart from a
  conflicted stash, a rebase and an in-progress merge, and the order of
  the checks matters.** All of these can leave the index with unmerged
  entries and nothing else in common, so the check for "unmerged entries
  and none of the other markers" cannot come first — it is also exactly
  what a conflicted cherry-pick or revert looks like before their own
  markers are checked. The safe order is: look for the sequencer's own
  pseudo-refs first (`CHERRY_PICK_HEAD`, `REVERT_HEAD`), then for
  `MERGE_HEAD`, then for a rebase bookkeeping directory (told apart from a
  `git am` in progress, which uses the same `rebase-apply` directory as the
  older rebase backend, by the presence of an `applying` file inside it),
  and only once none of those match, fall back to treating plain unmerged
  entries as a conflicted stash. Getting this order wrong is destructive in
  a specific way: a rebase's abort is `git rebase --abort`, but running
  that against what is actually an in-progress `git am` throws away the
  mailbox patch being applied, because `rebase --abort` and `am --abort`
  clean up the same `rebase-apply` directory differently.

- **A rename or copy line in a `-z --name-status` listing has one more
  field than an ordinary line.** An ordinary change is two
  NUL-terminated fields: a status letter and the path. A rename or copy
  (status starting `R` or `C`) is three: the status, the old path, and the
  new path. Code that walks this output has to branch on the status
  character to know whether to advance by two fields or three, or it reads
  the next entry's status letter as if it were a path.

## Known divergences

None found: no earlier design document for these conventions and
constraints was available to compare against.
