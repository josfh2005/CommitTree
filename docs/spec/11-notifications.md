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
| Operations finished | Chat answer finished | the answer took 10 s or longer and showed no decision card |
| The AI needs you | A write card waits for confirmation | always |
| | A decision card shown to you | always; not when the card was refused and the AI retries |
| Problems | A chat answer ended with an error | always |
| New commits on the remote | A background fetch brought commits to the checked-out branch's upstream that the branch does not have — "3 new commits on origin/main" | always (while Fetch in the background is on) |

One user operation is one event, however many git commands it runs. An
operation notifies once: conflicts or a failure replace "finished". A push of
the main branches in which any branch was not pushed is a failed Push: "Push
failed: N of M branches were not pushed".
Staging, discarding, reads, terminal commands, creating branches or tags
never notify.

Only commits that arrived in that background fetch count: commits a manual
Fetch already brought, or that the branch already contains, are never
announced, and a restart does not repeat a notice. Other branches, new
remote branches and background fetch failures never notify (see
`05-remote-and-stash.md`).

The title is the repository name; the body one sentence — "Push finished ·
14 s", "Pull failed: <first line of the error, at most 120 characters>",
"Conflicts in 3 files after the merge".

## Where it appears

1. Window not focused → an OS notification. Clicking it brings the window
   forward on that repository (with the chat open for chat events).
2. Window focused, event in the selected repository — and, for chat
   events, the chat panel open → nothing new; the screen already shows it.
3. Window focused, another repository, or the chat closed → a toast
   "<repo>: <body>" with **View**, which selects that repository (and opens
   the chat for chat events).

A failed operation keeps its error toast, never a second one; when the
repository is not the selected one, that toast starts with its name and
has **View**.

Each repository and category shows at most one OS notification: a newer
one replaces the older (macOS, Linux; Windows stacks them). When the OS
notification cannot be shown — not allowed, or not available (an unbundled
development build on macOS) — the toast is shown instead.

On Linux a notification has an **Open** button, which is what opens it;
clicking its text or closing it does nothing, because the system reports
both the same way.

Permission is asked the first time an OS notification is due, never at
startup, and at most once per run.

A stopped chat answer ends like a finished one; it was stopped from the
focused window, so it shows nothing.

## Settings

Settings → General → Notifications: **Show notifications** turns them all
off; **Finished operations (10 s or longer)**, **The AI needs you**,
**Problems — failures and conflicts** and **New commits on the remote** turn
off one category each. All on by default; kept on this computer, not per
repository. **New commits on the remote** is disabled, with the hint "Turn on
Fetch in the background first", while **Fetch in the background** — just
above, under Fetch: Off, Every 5, 15 (default), 30 or 60 min — is Off. Below them a line says
whether system notifications are allowed, not allowed yet, or unavailable
(and why) — in the last two cases toasts are used instead.

A failed operation's error toast does not depend on these settings: it is
shown as it always was.
