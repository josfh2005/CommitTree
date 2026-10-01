# Notifications

CommitTree tells the user about the events they were waiting for or that
need them, and nothing else: an OS notification when the window is not
focused, an in-app toast when it is focused but the event is out of sight.

## Events

| Category | Event | Notifies when |
|---|---|---|
| Operations finished | Fetch, Pull, Push, Merge, Rebase, Cherry-pick, Initialise/Update all submodules, git-flow init/start/finish | succeeded and took 10 s or longer |
| Problems | One of those operations failed | always |
| | Conflicts left after Merge, Pull, Rebase, Cherry-pick, git-flow finish, Stash apply/pop | always |

One user operation is one event, however many git commands it runs. An
operation notifies once: conflicts or a failure replace "finished".
Staging, discarding, reads, terminal commands, creating branches or tags
never notify.

The title is the repository name; the body one sentence — "Push finished ·
14 s", "Pull failed: <first line of the error, at most 120 characters>",
"Conflicts in 3 files after the merge".

## Where it appears

1. Window not focused → an OS notification. Clicking it brings the window
   forward on that repository.
2. Window focused, event in the selected repository → nothing new; the
   screen already shows it.
3. Window focused, another repository → a toast "<repo>: <body>" with
   **View**, which selects that repository.

A failed operation keeps its error toast, never a second one; when the
repository is not the selected one, that toast starts with its name and
has **View**.

Each repository and category shows at most one OS notification: a newer
one replaces the older (macOS, Linux; Windows stacks them). When the OS
notification cannot be shown — not allowed, or not available (an unbundled
development build on macOS) — the toast is shown instead.

Permission is asked the first time an OS notification is due, never at
startup, and at most once per run.
