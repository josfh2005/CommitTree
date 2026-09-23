# git-ui — Write tools for the AI chat — Design

Date: 2026-09-23
Status: Implemented (manual pass pending)
Builds on: `2026-09-17-ai-foundation-design.md` (the chat agent and its
read-only tools), `2026-09-21-working-tree-design.md` (2a: stage, commit) and
`2026-09-21-push-pull-stash-design.md` (2b: fetch, push, pull, stash).

## Goal

Let the user tell the repository chat "commit this", "push it", "create a
branch called x and switch to it" — and have it happen, with the user
approving every change before it runs. Today the chat's tools are strictly
read-only (`internal/ai/tools`); the only write-capable agent tools
(`internal/ai/mergetools`) exist only inside a conflict-resolution run.

## Decisions

- **Scope — local and remote, nothing destructive.** Stage/unstage, commit
  (no amend), create branch, check out a branch, stash push, fetch, push
  (never force), pull (configured strategy), merge a branch into the current
  one. Out: reset, discard, delete branch, amend, stash drop/pop/apply,
  force-push, rebase, anything that rewrites or throws away work.
- **Every write is confirmed.** Each write tool call pauses the answer and
  shows a card in the chat with the exact operation. Approve runs it; Reject
  tells the model the user declined. No auto-approve setting.
- **Always available in the chat.** The regular chat run gets the write tools
  alongside the read tools; the confirmation card is the guard. "Explain in
  chat" (started from the log) stays read-only.
- **Same path as the UI.** An approved write runs through the App methods the
  toolbar and views already use — the per-repo write lock (`a.write` /
  `writeWorktree`), their validation, their events. The agent gets no second
  way to mutate a repository.

## Architecture

### Confirmation: the tool call waits

When the model calls a write tool, `RunTool` (inside `agent.Execute`) does
not return until the user decides:

1. **Prepare.** Validate the arguments and read the repository to build a
   *proposal*: a title and a few detail lines describing exactly what will
   happen, plus the *precondition* it was built against. Invalid arguments
   (unknown branch, a path not in the working tree status, nothing staged for
   a commit, detached HEAD for push/pull/merge, a branch argument that fails
   `git check-ref-format --branch` such as a revision suffix like `main~2` or
   `main@{u}`) return `error: …` to the model straight away — the user is
   never asked about something that cannot run. The pull strategy it reads is
   whatever is configured *at this moment* (Prepare runs again for every
   write call, not once for the whole chat run), so a setting changed mid-run
   is picked up by the next `pull` proposal.
2. **Ask.** Emit `chat:confirm` with a new `confirmID` and the proposal, and
   block on a channel registered under that id.
3. **Decide.** `ConfirmChatAction(repoID, confirmID, approve)` (new bound
   method) delivers the answer. `StopChat` cancels the run's context, which
   counts as a rejection. There is no timeout.
4. **Re-check.** On approval, compare the precondition with the repository
   now: the refs/HEAD fingerprint (`refs.Fingerprint`) for every tool, and
   additionally the set of staged paths for `commit`. If anything changed,
   do nothing and return `error: the repository changed since this was
   proposed; check its state and propose again`. This runs just before the
   App method takes the per-repo write lock, a millisecond window that is
   accepted.
5. **Execute** through the App method, under the write lock. `ErrBusy` becomes
   `error: another operation is running in this repository; try again when
   it finishes`, and nothing else happens (no `repo:changed`). Any other
   outcome — success or a real execution error — means the App method was
   attempted, so `repo:changed {repoID}` is always emitted after it, since a
   failure can still be partial: `create_branch` with `checkout: true` can
   create the branch and then fail to switch to it, in which case the result
   says so explicitly: `error: created branch <name> but could not switch to
   it: <err>`, rather than a bare switch error that would read as if nothing
   happened. `create_branch`'s proposal carries the *resolved commit hash*
   for `start` (what the title's short hash already names), not the model's
   original argument, so execution creates the branch at exactly what was
   shown and approved even if that argument was time-relative.
6. **Report.** Return a one-line result to the model (`done: pushed main to
   origin/main (3 commits)`, `rejected by the user`, or the error), emit the
   usual `chat:tool_result`.
7. **Confirmation order within one answer.** The model can ask for several
   write calls in a single response (rare, but not disallowed). Once one of
   them returns a result that doesn't start with `done:` (rejected, failed,
   or already skipped), every later write call in that same response is
   refused immediately — `error: skipped because the previous change was not
   approved or failed` — without Prepare and without a card; read tools are
   unaffected. A later model response (a new round-trip) is unaffected. The
   chat prompt also asks the model to stop and ask before proposing another
   change when one is rejected.

The per-repo write lock is **not** held while waiting for the user, so the
UI, the terminal and other operations stay usable; the re-check in step 4 is
what makes that safe. The chat's own one-run-per-repo lock (`a.ai.runs`) is
held throughout, as for any run, so the composer stays disabled.

Rejected alternatives: ending the run with a pending proposal and starting a
new run on approval (complicates the history and the run lock), and having
the frontend execute the approved action through its toolbar actions
(duplicates their own confirm dialogs, and the backend would not know what
actually ran).

### Tools — `internal/ai/writetools` (new)

`writetools` is a sibling of `tools`: `Specs() []ai.ToolSpec`, and
`Prepare(ctx, dir string, call ai.ToolCall) (Proposal, error)`. It cannot
import `internal/app`, so execution lives in the app (below).

```go
type Proposal struct {
    Title   string   `json:"title"`   // "Push main to origin/main"
    Details []string `json:"details"` // "3 commits: a1b2c3d Fix…", staged files, commit message…
    Fingerprint string   `json:"-"`
    Staged      []string `json:"-"` // commit only
}
```

| Tool | Arguments | Proposal shows | Runs (App method) |
|---|---|---|---|
| `stage_files` | `paths` (array) | the paths | `StageFile` per path |
| `unstage_files` | `paths` | the paths | `UnstageFile` per path |
| `commit` | `message` | the message and the staged files | `CommitChanges(id, message, false)` |
| `create_branch` | `name`, `start` (optional, default HEAD), `checkout` (bool) | name, start commit, whether it switches | `CreateBranch` |
| `checkout_branch` | `name` (local, or `remote/name` for a remote branch) | from → to | `Checkout` / `CheckoutRemote` |
| `stash_push` | `message` (optional), `include_untracked` (bool) | message, number of files | `StashPush` |
| `fetch` | — | the remotes | `Fetch` |
| `push` | — | `branch → @{push}` (falling back to `@{upstream}` when `@{push}` doesn't resolve) and the commits ahead; "publishes a new upstream" when there is none | `Push` |
| `pull` | — | `remote/branch → branch`, commits behind, the configured strategy | `Pull` |
| `merge_branch` | `branch` | `branch → current`, commits it brings; "a merge commit is always created" | `MergeBranch` |

Paths for `stage_files`/`unstage_files` must each appear in the current
`worktree.Status`, as the UI already requires; the App methods keep their own
`--literal-pathspecs` validation. A multi-path stage that fails part-way
reports which paths were staged.

`checkout_branch`/`merge_branch` arguments are validated with
`git check-ref-format --branch` (after rejecting a leading `-` outright) so a
revision expression such as `main~2` or `main@{u}` is refused as `no branch
"<x>"` instead of being silently resolved through `refs/heads/<x>` (git reads
the suffix even with that prefix, so it can resolve to a real commit despite
no branch actually being named that).

`checkout_branch` of a remote branch (`<remote>/<name>`) resolves `<remote>`
as the *longest* configured remote name (`git remote`) that prefixes the
argument, not a first-`/` split — so a remote whose own name contains a slash
is handled correctly, and an unconfigured prefix is refused as `no branch
"<x>"`. When a local branch already called `<name>` exists, the proposal
switches to it (`switches to existing local branch <name> (A ahead, B behind
<remote>/<name>)`) instead of claiming it "creates" a branch that is already
there — a plain `Checkout`, not `CheckoutRemote`.

`push`'s target, when the branch has an upstream, is `@{push}` — what
`git push` with no refspec actually pushes to, which can differ from
`@{upstream}` in a triangular setup (`branch.<b>.pushRemote`, or
`push.default=current`) — falling back to `@{upstream}` when `@{push}`
doesn't resolve. When `push.default` is `matching`, the proposal refuses
outright (`push.default is "matching", which would push other branches too;
push from the toolbar instead`), since a plain `git push` in that mode is not
limited to the current branch.

### App — `internal/app/ai.go` and a new `internal/app/chatwrite.go`

- `SendChat` passes `tools.Specs()` plus `writetools.Specs()`, and a
  `RunTool` that routes write tool names to `a.runWriteTool(ctx, repoID,
  runID, dir, call)`. `ExplainInChat` is unchanged (it runs on a task responder with no tools).
- `runWriteTool` implements steps 1–6 above. Pending confirmations live in
  `a.ai.confirms map[string]pendingConfirm` (id → channel, repoID, the
  emitted event), guarded by `a.ai.mu`, and are removed when decided or
  cancelled.
- `ConfirmChatAction(repoID, confirmID string, approve bool) error` — unknown
  or already-decided ids return an error (a double click is harmless).
- `GetChatConfirm(repoID string) *ConfirmEvent` — the pending card for a
  repository, if any, so a chat panel that re-mounts (switching repositories
  and back) can show it again instead of leaving an answer stuck.
- Pull and merge that end in conflicts return `done: conflicts in N files;
  the conflict view is open — resolve them there`. The conflict view takes
  over exactly as when the user pulls or merges from the UI.
- `repo:changed {repoID}` is emitted whenever `executeWrite` was attempted —
  on success and on a real execution error alike — since even a failure can
  be partial. It is **not** emitted for `ErrBusy`, nor for a rejection or a
  `Recheck` refusal, since nothing was attempted in either of those.
- Write calls in the same model response are ordered: once one doesn't
  return a `done:` result, every later write call in that response is
  refused as `error: skipped because the previous change was not approved or
  failed`, with no Prepare and no card — tracked per model round-trip (a
  `step int` `agent.Run.RunTool` now receives, alongside `ctx` and `call`),
  reset for the next response.

### Frontend

- `lib/chat.ts`: `chat:confirm` is added to `CHAT_EVENTS`, and handled
  *before* the reducer's runID guard in `applyEvent` — `withPendingConfirm`
  already checks `repoID` itself and adopts `ev.runID`, and a confirmation
  can arrive after the panel re-mounted and reloaded history with no
  in-flight run (`runID` null); dropping it at the guard would strand the
  run with no card and no Stop. The reducer attaches the proposal to the
  pending tool entry of the current answer (`{ confirmID, title, details,
  state: 'pending' }`). `withConfirmDecision(state, confirmID, next)` sets a
  transitional `'approved'`/`'rejecting'` state the instant the user clicks,
  before `ConfirmChatAction` returns (or reverts to `'pending'` when that
  call itself failed). The matching `chat:tool_result` then sets `state`
  from its summary: `done`, `rejected` or `failed`. `confirmResultText(tool)`
  formats a decided card's one line, stripping the tool summary's own
  `done: `/`error: ` prefix so the outcome isn't stated twice.
- `ChatPanel.svelte`: a pending tool renders a card — title, detail lines,
  Reject and Approve — calling `api.confirmChatAction`, which immediately
  applies `withConfirmDecision` so the card shows "Running…"/"Rejecting…"
  and can't be double-clicked; only a failed `confirmChatAction` call itself
  re-enables the buttons (with a toast) — `ConfirmChatAction` returning is
  not the same as the write finishing, so success alone does not re-enable
  them. A decided card shows `confirmResultText`. On mount and on repository
  switch the panel calls `getChatConfirm` to restore a pending card.
- `App.svelte`: listens for `repo:changed` and runs `refreshRepo()` when it
  is for the selected repository, next to the existing `worktree:changed`
  listener.

### Prompt

`internal/ai/prompts/defaults/chat.md` gains a short section: which write
tools exist, that each one is shown to the user for approval before it runs,
to propose one step at a time, to never claim a change happened until the
tool result says `done`, that a rejected change means stop and ask before
proposing another, and that destructive operations are not available and
must be done by the user. A user who has overridden `chat.md` keeps their
file and does not get this text; the Settings prompt list already offers
"Reset" for that.

## Error handling

- Invalid arguments → `error:` to the model, no card.
- Repository changed between proposal and approval → refused, told to the
  model; the card shows `failed` with that reason.
- Write lock busy → `error:` to the model; card `failed`; no `repo:changed`.
- The App method's own error (push rejected as non-fast-forward, checkout
  blocked by local changes, …) → its message to the model; card `failed`;
  `repo:changed` still goes out, since execution was attempted.
- A write call in the same model response as an earlier rejected/failed one →
  `error: skipped because the previous change was not approved or failed`,
  no Prepare, no card.
- Stop while waiting → rejection; the run ends as any stopped run does.
- App quit while waiting → the run's context is cancelled; nothing ran.

## Testing

- `writetools` unit tests against real repositories from `internal/testrepo`:
  each tool's argument validation and proposal text (ahead counts, new
  upstream wording, staged files for commit, remote branch checkout); a
  triangular push setup names the `@{push}` remote in the title;
  `push.default=matching` refuses; a remote branch checkout with an existing
  local branch of the same name shows "switches to", not "creates", with
  ahead/behind counts, and resolves the remote by its longest configured
  name; `checkout_branch`/`merge_branch` reject revision-suffix arguments
  (`main~2`, `main@{u}`) as `no branch`; `merge_branch` on a detached HEAD
  refuses.
- `internal/app` tests with a fake provider and a recording emitter:
  approve runs the operation and emits `repo:changed`; reject runs nothing
  (no `repo:changed` either); a ref moved between proposal and approval
  refuses; staged set changed refuses a commit; a held write lock returns
  the busy error with no `repo:changed`; `StopChat` while waiting unblocks
  the tool as a rejection; `ConfirmChatAction` with an unknown id errors;
  `GetChatConfirm` returns the pending card; the chat's tool list includes
  both the read and the write tools; two write calls in one model response
  where the first is rejected — the second is skipped, gets no card, and
  neither branch is created; an attempted-but-failed write still emits
  `repo:changed`; a `create_branch`+checkout whose switch fails reports the
  branch was created; the pull strategy read for a second write call in the
  same run reflects a setting saved after the first call's proposal, not the
  one read before it.
- vitest: the reducer attaches `chat:confirm` to the right tool and resolves
  its state from the tool result, including when it arrives with no
  in-flight run (`runID` null, as after a re-mount); `CHAT_EVENTS` contains
  `chat:confirm`; `withConfirmDecision` sets and reverts the transitional
  states; `confirmResultText` formats each decided state without repeating
  the tool summary's own prefix.
- Manual: ask the chat to commit staged work, push, create and switch
  branch, pull with conflicts (conflict view opens), reject a push, Stop
  while a card is pending, switch repository and back with a card pending.

## Out of scope

Destructive operations, auto-approval, proactive (unasked) writes, write
tools in "Explain", hunk-level staging, choosing a remote other than the
branch's upstream/`origin`, and any change to the conflict-resolution agent.
