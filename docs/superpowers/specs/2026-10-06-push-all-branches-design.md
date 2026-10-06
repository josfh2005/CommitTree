# Push all branches, and ahead/behind badges on branches

Date: 2026-10-06. Requested by the owner on 2026-10-06; designed with them the
same day. Behaviour goes to `docs/spec/05-remote-and-stash.md` (Push, the
setting), `docs/spec/01-repositories-and-sidebar.md` (branch badges, the repo
row menu), `docs/spec/07-conventions-and-constraints.md` (Settings → General)
and `docs/spec/11-notifications.md` (a partly failed push).

## Goal

Pushing a repository can publish every branch that has something to publish,
not only the checked-out one, or ask which of the two to do. And the sidebar
shows, on each local branch, how many commits are waiting to be pushed and
how many are waiting to be pulled, so the owner can see at a glance what
"all branches" will push.

Out of scope: pushing tags, force pushing (with or without lease), publishing
branches that have no upstream (other than the current branch, as today), an
AI chat tool for pushing all branches, per-repository push settings, counts
on remote-tracking branches or on collapsed branch folders.

## Decisions taken with the owner

| Question | Decision |
|---|---|
| Ask every time, or a setting? | A setting with three values; the default is **Ask each time**. |
| What does "all branches" mean? | The current branch, plus every other local branch with an upstream that is ahead of it. |
| Tags? | Not pushed. |
| How are per-branch outcomes shown? | A toast when every branch went through; a results dialog, one row per branch, when any did not. Not atomic: the branches that can be pushed are pushed. |
| Rejected branches / force? | Never forced. A rejection is reported with its reason and the advice to pull first. |
| AI chat? | Unchanged: its `push` tool still pushes the current branch only. |
| Where are the badges? | On local branch rows in the sidebar: ahead in red, behind in blue. The toolbar's badges stay as they are. |

## Ahead/behind badges on local branches

`refs.List` already runs one `for-each-ref`; its format gains
`%(upstream:track,nobracket)`, which git fills from the last fetch with
`ahead N`, `behind M`, `ahead N, behind M`, `gone` or nothing. `refs.Branch`
gains `Ahead int`, `Behind int` and `UpstreamGone bool` (JSON `ahead`,
`behind`, `upstreamGone`, omitted when zero/false). No extra git command runs,
so the counts refresh whenever the refs do: after a fetch (toolbar or
background), a pull, a push, a commit, a checkout, and so on.

`BranchRow` shows, at the right of a local branch row and before the
"worktree" tag and the filter icon:

- `↑N` in red (`--ahead` token) when the branch has N commits its upstream
  does not have;
- `↓M` in blue (`--behind` token) when the upstream has M commits the branch
  does not have;
- both when the branch has diverged; nothing when both counts are zero, the
  branch has no upstream, or the upstream is gone.

The two tokens are defined for light, dark and both high-contrast palettes,
with text contrast of at least 4.5:1 on the sidebar background. The badges
are small text (11 px, tabular numbers, no pill background) so a row of
branches stays calm. The tooltip on either badge reads "N commits to push to
`<upstream>` · M commits to pull, as of the last fetch", leaving out a zero
part. The badges appear in every expanded repository (each shows its own
refs), not only the selected one. Remote-tracking rows and collapsed folder
rows show none.

## The push scope setting

`gitsettings.Settings` (which already holds `pullStrategy`) gains
`pushScope`: `ask` (the default, also used when the field is missing),
`current` or `all`; anything else is rejected as invalid, like an unknown
pull strategy. Settings → General shows it under the pull strategy as
"Push": *Ask each time* / *Current branch only* / *All branches*.

## What "all branches" pushes

The set is computed in Go when the push runs, from a fresh `for-each-ref`:

1. **The current branch**, always (when HEAD is not detached), exactly as a
   plain Push treats it: with no upstream it is published to `origin` under
   its own name with `-u`. It is included even when it is not ahead, so its
   outcome can read "up to date".
2. **Every other local branch** whose upstream is set, is not gone, lives on
   a remote (`%(upstream:remotename)` is not `.`, i.e. it does not track
   another local branch), and is ahead of it according to the last fetch.

Each branch is pushed to its upstream as an explicit refspec,
`refs/heads/<branch>:<%(upstream:remoteref)>`, on `%(upstream:remotename)`.
`branch.<name>.pushRemote` and `push.default` are not consulted in this mode;
the spec says so. Branches checked out in another worktree are pushed like
any other. With a detached HEAD only step 2 applies, and Push All with an
empty set reports "Nothing to push" without running git.

Branches are grouped by remote and each remote gets one command:

    git push --porcelain [-u] <remote> <refspec>...

(`-u` only on the `origin` group that carries a current branch with no
upstream — it sets the upstream only for refs that went through, and the
other refspecs on that remote already have one). Remotes run one after
another, in name order. Without `--atomic`, git updates each ref on its own,
so one rejection does not stop the others.

### Reading the outcome

`--porcelain` prints a `To <url>` line, then one line per ref,
`<flag>\t<src>:<dst>\t<summary>`, then `Done`. The flags map to:

| Flag | Status | Reason shown |
|---|---|---|
| ` ` `*` `+` | `pushed` | — |
| `=` | `upToDate` | — |
| `!` with `(non-fast-forward)` or `(fetch first)` | `rejected` | "The remote has commits you don't have — pull `<branch>` first" |
| `!` with anything else (`[remote rejected] (…)`, hook output) | `rejected` | git's summary, as is |

A remote whose command fails without printing any ref line (network,
authentication, unknown remote, timeout) gives every branch of that group
status `failed` with the command's error message. A ref line missing for a
branch the command named (should not happen) gives `failed` with "No result
from git".

The Go call returns `[]BranchPushResult{Branch, Target, Status, Reason}`,
`Target` being `<remote>/<remote branch>`, in the order the branches were
pushed. It returns an error only when nothing could start: the repository is
missing, another write holds it, a conflict owns it.

## The Push flow

`App.Push(id)` stays as it is (the current branch). A new
`App.PushAll(id) ([]BranchPushResult, error)` takes the same write lock and
the same conflict guard. The frontend decides which one to call:

- Toolbar Push and the repo row menu's **Push** follow the setting:
  - *Current branch only*: `push`, unchanged.
  - *All branches*: `pushAll`.
  - *Ask each time*: if no local branch other than the current one is ahead
    of an upstream (counted from the refs store, i.e. the last fetch), it
    pushes the current branch without asking. Otherwise a choice dialog,
    "Push `<repo>`", offers *Current branch (`<name>`)* (selected) and *All
    branches (N)*, where N counts the current branch plus the other branches
    ahead; the message under the options reads "Change the default in
    Settings → General." Confirm reads "Push" or "Push N branches". Cancel
    pushes nothing. With a detached HEAD the dialog still opens when other
    branches are ahead, offering only *All branches (N)*; if none are, Push
    refuses as today.
- The repo row menu also gets **Push all branches**, right under Push, which
  calls `pushAll` whatever the setting says. It is disabled when Push is.
- The toolbar Push tooltip says what a click will do: "Push `<branch>`",
  "Push all branches" or "Push — asks current or all branches". Its badge
  keeps counting the current branch only.
- While it runs the busy label is "Pushing branches…".

### Showing the result

- Every branch `pushed` or `upToDate`: a toast, "Pushed N branches" (or
  "Pushed `<branch>`" for one), with "M already up to date" appended when
  some were; "Everything up to date" when none moved.
- Any branch `rejected` or `failed`: a results dialog, "Push results —
  `<repo>`", one row per branch: a mark (✓ pushed, — up to date, ✗ rejected
  or failed), the branch, its target and, for ✗, the reason. A single OK
  closes it. Rows keep the push order.

For notifications (`11-notifications.md`) a push of all branches with any
`rejected` or `failed` row counts as a failed push; its text reads "Push:
N of M branches failed in `<repo>`". The 10 s rule is unchanged. The
Commands panel shows each `git push` as it does today.

After the push the refs reload, so the sidebar badges and the toolbar's
counts show the new state.

## Error handling summary

| Situation | Outcome |
|---|---|
| Another write running, conflict in progress, repository missing | Refused before anything runs (error toast), as Push today |
| Detached HEAD, no other branch ahead | "Nothing to push" (Push All) / refusal as today (Push) |
| One branch rejected, others fine | Others pushed; results dialog; failed notification |
| Remote unreachable / auth failed | Its branches `failed` with git's message; other remotes still pushed |
| Upstream gone | Not in the set, no badge |
| Branch tracking a local branch | Not in the set; its badge still shows counts against that branch |

## Testing

Go (`internal/ops`, against `testrepo` repositories with local bare remotes):

- the set: current without upstream, current up to date, other branch ahead,
  other branch not ahead, upstream gone, tracking a local branch, a second
  remote, detached HEAD, upstream with a different remote branch name;
- push: everything pushed; one branch rejected (remote moved on) while the
  others go through; a remote that does not exist fails only its group;
  `-u` sets the upstream of a newly published current branch;
- porcelain parsing as a pure function over captured outputs, every flag
  and the "no ref lines" case;
- `refs.List` track parsing: ahead, behind, both, gone, none;
- `gitsettings`: default, round trip, invalid value.

Vitest:

- the decision: setting × other branches ahead × detached HEAD → push,
  pushAll, dialog (and the dialog's options and N);
- the result summary: toast text vs dialog, notification text;
- the badge text and tooltip, including zero parts left out.

App-level (`internal/app`): `PushAll` honours the write lock and the
conflict guard.

Manual pass: a demo repository with three branches ahead, one diverged and
one without upstream; check the badges, each setting value, the dialog, the
results dialog and the toast, in light, dark and high contrast.
