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
   a commit, detached HEAD for push/pull) return `error: …` to the model
   straight away — the user is never asked about something that cannot run.
2. **Ask.** Emit `chat:confirm` with a new `confirmID` and the proposal, and
   block on a channel registered under that id.
3. **Decide.** `ConfirmChatAction(repoID, confirmID, approve)` (new bound
   method) delivers the answer. `StopChat` cancels the run's context, which
   counts as a rejection. There is no timeout.
4. **Re-check.** On approval, compare the precondition with the repository
   now: the refs/HEAD fingerprint (`refs.Fingerprint`) for every tool, and
   additionally the set of staged paths for `commit`. If anything changed,
   do nothing and return `error: the repository changed since this was
   proposed; check its state and propose again`.
5. **Execute** through the App method, under the write lock. `ErrBusy` becomes
   `error: another operation is running in this repository; try again when
   it finishes`.
6. **Report.** Return a one-line result to the model (`done: pushed main to
   origin/main (3 commits)`, `rejected by the user`, or the error), emit the
   usual `chat:tool_result`, and emit `repo:changed` after anything that ran.

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
| `push` | — | `branch → remote/branch` and the commits ahead; "publishes a new upstream" when there is none | `Push` |
| `pull` | — | `remote/branch → branch`, commits behind, the configured strategy | `Pull` |
| `merge_branch` | `branch` | `branch → current`, commits it brings; "a merge commit is always created" | `MergeBranch` |

Paths for `stage_files`/`unstage_files` must each appear in the current
`worktree.Status`, as the UI already requires; the App methods keep their own
`--literal-pathspecs` validation. A multi-path stage that fails part-way
reports which paths were staged.

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
- After any executed write, emit `repo:changed {repoID}`.

### Frontend

- `lib/chat.ts`: `chat:confirm` is added to `CHAT_EVENTS`; the reducer
  attaches the proposal to the pending tool entry of the current answer
  (`{ confirmID, title, details, state: 'pending' }`). The matching
  `chat:tool_result` sets `state` from its summary: `done`, `rejected` or
  `failed`.
- `ChatPanel.svelte`: a pending tool renders a card — title, detail lines,
  Reject and Approve — calling `api.confirmChatAction`. Buttons disable after
  the first click. A decided card shows its state in one line. On mount and
  on repository switch the panel calls `getChatConfirm` to restore a pending
  card.
- `App.svelte`: listens for `repo:changed` and runs `refreshRepo()` when it
  is for the selected repository, next to the existing `worktree:changed`
  listener.

### Prompt

`internal/ai/prompts/defaults/chat.md` gains a short section: which write
tools exist, that each one is shown to the user for approval before it runs,
to propose one step at a time, to never claim a change happened until the
tool result says `done`, and that destructive operations are not available
and must be done by the user. A user who has overridden `chat.md` keeps
their file and does not get this text; the Settings prompt list already
offers "Reset" for that.

## Error handling

- Invalid arguments → `error:` to the model, no card.
- Repository changed between proposal and approval → refused, told to the
  model; the card shows `failed` with that reason.
- Write lock busy → `error:` to the model; card `failed`.
- The App method's own error (push rejected as non-fast-forward, checkout
  blocked by local changes, …) → its message to the model; card `failed`.
- Stop while waiting → rejection; the run ends as any stopped run does.
- App quit while waiting → the run's context is cancelled; nothing ran.

## Testing

- `writetools` unit tests against real repositories from `internal/testrepo`:
  each tool's argument validation and proposal text (ahead counts, new
  upstream wording, staged files for commit, remote branch checkout).
- `internal/app` tests with a fake provider and a recording emitter:
  approve runs the operation and emits `repo:changed`; reject runs nothing;
  a ref moved between proposal and approval refuses; staged set changed
  refuses a commit; a held write lock returns the busy error; `StopChat`
  while waiting unblocks the tool as a rejection; `ConfirmChatAction` with an
  unknown id errors; `GetChatConfirm` returns the pending card; the chat's
  tool list includes both the read and the write tools.
- vitest: the reducer attaches `chat:confirm` to the right tool and resolves
  its state from the tool result; `CHAT_EVENTS` contains `chat:confirm`.
- Manual: ask the chat to commit staged work, push, create and switch
  branch, pull with conflicts (conflict view opens), reject a push, Stop
  while a card is pending, switch repository and back with a card pending.

## Out of scope

Destructive operations, auto-approval, proactive (unasked) writes, write
tools in "Explain", hunk-level staging, choosing a remote other than the
branch's upstream/`origin`, and any change to the conflict-resolution agent.
