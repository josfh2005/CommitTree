# git-ui — Sub-project 4a: Branch merge with an AI conflict agent — Design

Date: 2026-09-17
Status: Draft for review
Builds on: `2026-09-16-git-ui-design.md` (core viewer) and
`2026-09-17-ai-foundation-design.md` (AI foundation), both merged to `main`.

## Goal

Merge a branch into the current one from the sidebar, show the conflicted
state inside the app, and let an AI agent resolve the conflicts hunk by hunk
while the user watches. The agent stops before the commit; the user reviews
the staged result and closes the merge with one action.

## Scope decisions

- The merge always runs `--no-ff`, so an integrated branch is visible in the
  graph as its own merge commit.
- A conflicted merge is **left in place**. The app shows the `MERGING` state
  and offers "Abort merge"; it never aborts on its own.
- The agent runs **autonomously** — it resolves every conflict it can without
  asking — and **stops before committing**. The user reviews and commits.
- The agent writes **only through a hunk-level tool**. It never sees or
  rewrites a whole file, so untouched code cannot be lost to a hallucination.
- While the repo is merging, the bottom panel of the log column shows the
  merge instead of commit details. The agent narrates in the existing chat
  panel.
- The agent needs tool calling, so it is **Ollama only**. Apple Intelligence
  is one-shot and stays out of this flow.

## Non-goals

Rebase, cherry-pick and revert. A staging UI or partial staging (sub-project
2). Editing a conflicted file inside the app. A three-way merge editor.
Resolving conflicts that arise from rebase. Octopus merges, submodule
conflicts, and binary conflicts — those are listed and handed back to the
user. Repo groups and the stashes section, which are separate changes.

## Architecture

### `internal/merge` (new)

Owns the git side of merging and, separately, the text surgery.

`merge.go` — driving git:

```go
type Outcome int // Merged, Conflicted, UpToDate

type Result struct {
    Outcome   Outcome
    Conflicts []string
}

func Start(ctx context.Context, dir, branch string) (Result, error)
func Status(ctx context.Context, dir string) (State, error)
func Abort(ctx context.Context, dir string) error
func Commit(ctx context.Context, dir string) error
```

`Start` runs `git -c merge.conflictStyle=zdiff3 merge --no-ff --no-edit
<branch>`. The `-c` matters: `zdiff3` writes the common ancestor into the
markers, and knowing what the code looked like *before* both sides changed it
is most of what makes a small model's answer usable. It is set per command,
so the user's own config is untouched.

Exit status alone does not say what happened, so `Start` classifies: a clean
exit with "Already up to date" is `UpToDate`, a failure whose unmerged paths
are non-empty is `Conflicted`, and anything else is a real error returned as
`*gitcmd.Error` (a dirty worktree lands here).

`state.go` — reading the state:

```go
type State struct {
    Merging   bool     `json:"merging"`
    From      string   `json:"from"`      // branch being merged in
    Into      string   `json:"into"`      // current branch
    Conflicts []string `json:"conflicts"` // unmerged and carrying markers
    Manual    []string `json:"manual"`    // unmerged with no markers to splice
}
```

`Merging` is `.git/MERGE_HEAD` existing. `From` comes from
`.git/MERGE_MSG`'s first line. Unmerged paths come from `git diff
--name-only --diff-filter=U`, and each is then classified by reading it:
a file whose content has no conflict markers goes to `Manual`. That single
test covers binaries, delete/modify and add/add together, and it asks
exactly the question the tools care about — is there a marker block to
splice — instead of inferring it from stage modes.

There is no `Resolved` list. What the user resolved during this merge is the
difference between the conflicts reported when the merge started and the ones
still unmerged, which the frontend already has; deriving it in git would mean
listing every auto-merged file as though the agent had touched it.

`conflict.go` — the text surgery, pure functions over strings, no git:

```go
type Hunk struct {
    Index  int    // position in the file, 0-based
    Ours   string
    Theirs string
    Base   string // "" when the file was written in 2-way style
    Before string // up to ContextLines above the marker
    After  string // up to ContextLines below
}

func Parse(content string) ([]Hunk, error)
func Splice(content string, index int, resolved string) (string, error)
```

`Parse` walks the file recognising `<<<<<<<`, `|||||||`, `=======` and
`>>>>>>>` at the start of a line, and tolerates both marker styles so a file
written before this feature existed still parses. Malformed nesting is an
error rather than a guess.

`Splice` is where the safety guarantee lives, and it is worth stating
precisely: it re-parses `content`, replaces exactly the marker block of hunk
`index` with `resolved`, and leaves every other byte alone — including line
endings and a missing final newline. It refuses when the hunk index is out of
range, and when `resolved` itself contains conflict markers. Because it works
from a fresh parse each time, the agent resolving hunk 0 and then hunk 1 of
the same file is correct without the caller tracking offsets.

### `internal/ai/mergetools` (new)

The agent's tools, in the shape `internal/ai/tools` already established
(`Specs() []ai.ToolSpec` plus `Run(ctx, dir, call) string`, every result a
string the model reads):

| Tool | Arguments | Returns |
|---|---|---|
| `list_conflicts` | — | each unresolved file with its hunk count, and the `Manual` list marked as "resolve by hand" |
| `read_conflict` | `path`, `hunk` | base, ours and theirs for that hunk, with surrounding context, truncated to 8 KB |
| `resolve_hunk` | `path`, `hunk`, `resolved` | splices and reports how many hunks remain in the file |
| `stage_file` | `path` | `git add -- <path>`; refuses while markers remain |

The read-only tools from `internal/ai/tools` are passed in alongside, so the
agent can run `file_history` or `show_commit` to see why each side changed
the code before choosing.

Every write goes through `resolve_hunk` and `stage_file`. There is no tool to
create a file, delete one, touch a file that has no conflict, or commit. That
is the whole containment story, and it is a property of the tool list rather
than of the prompt.

### Agent run

`internal/app/merge.go` adds `ResolveConflicts(repoID, runID string) error`.
It takes the repo's existing chat slot — the same one `SendChat` and
`ExplainInChat` share — so a resolve run and a chat cannot interleave, and
`ErrChatBusy` is returned when one is already going. It emits
`agent.EventStart` with the text "Resolve the conflicts from merging <from>
into <into>", streams deltas into the chat, and saves the exchange in the
repo's conversation like any other run.

Two changes to `internal/ai/agent`:

- `Run` gains `MaxSteps int`, where 0 keeps today's default of 8. A merge
  with six conflicted files needs far more than eight tool calls, so this run
  asks for 30. Trimming (`HistoryLimit`, `KeepToolResults`) is unchanged and
  keeps the context bounded regardless of step count.
- After a tool call that changed the working tree, the run emits
  `merge:changed` with the repo ID, so the merge panel refreshes as the agent
  works instead of only at the end.

A new prompt `internal/ai/prompts/defaults/resolve-conflicts.md`, overridable
like the others, tells the agent to work one hunk at a time, to prefer
keeping both sides' intent over picking a side, to stage a file only once it
is clean, to leave anything it is unsure about unresolved and say so, and
never to invent code that was in neither side. Variables available:
`{{repo}}`, `{{branch}}`, `{{date}}`.

### App API

```go
func (a *App) MergeBranch(id, branch string) (merge.Result, error)
func (a *App) GetMergeState(id string) (merge.State, error)
func (a *App) AbortMerge(id string) error
func (a *App) CommitMerge(id string) error
func (a *App) ResolveConflicts(id, runID string) error
func (a *App) GetConflictFile(id, path string) (ConflictFile, error)
```

The four write operations go through the existing `a.write` per-repo lock, so
a merge cannot race a checkout. `ResolveConflicts` uses the chat slot
instead, and is cancelled by the chat's existing stop button.

`ConflictFile` is `{ resolved bool, text string }`: while a file is still
conflicted, `text` is its content with markers, which is what the user needs
to see; once it is staged, `text` is `git diff --cached -- <path>`, so review
before committing is an ordinary diff.

### Frontend

- `lib/stores.ts`: a `mergeState` store, reloaded by `refreshRepo` and on the
  `merge:changed` event.
- `lib/actions.ts`: `mergeBranch`, `abortMerge`, `commitMerge`,
  `resolveConflicts`, following the existing `run()` wrapper for busy state,
  toasts and refresh.
- `RepoRefs.svelte`: a "Merge <label> into <current>" item in the branch
  context menu, after "Check out". Disabled when the branch is the current
  one, when HEAD is detached, while another operation runs, and while a merge
  is already in progress. Confirmation dialog naming both branches.
- `MergeView.svelte` (new): replaces `CommitDetails.svelte` in the bottom
  panel while `mergeState.merging`. Same two-column shape — file list left,
  content right — listing conflicted files first, then resolved, then the
  manual ones with a note. A header strip carries "Merging <from> into
  <into>", "Resolve with AI", "Abort merge" and "Commit merge", the last
  enabled only when nothing is unresolved.
- The sidebar repo row shows a small "merging" badge, so a repo left mid-merge
  is visible without selecting it.

## Error handling

- **Dirty worktree.** `git merge` refuses when local changes would be
  overwritten. The app says so and explains that the changes must be
  committed or stashed outside the app, since there is no staging UI yet.
- **Already up to date.** A toast, no state change.
- **Ollama unreachable, or a model without tool support.** The existing
  classified errors (`ErrUnreachable`, `ErrModelNotFound`, `ErrNoToolSupport`)
  surface on the "Resolve with AI" button and point at Settings. The merge
  state is unaffected — resolving by hand stays available.
- **Agent hits the step limit.** `Step limit reached.` lands in the chat, the
  files it did resolve stay resolved and staged, and the rest are still
  listed. Running it again continues from there.
- **A tool refuses** (markers left in `resolved`, bad hunk index, staging an
  unclean file). The refusal goes back to the model as the tool result, which
  is how the agent learns to retry, and the working tree is untouched.
- **The user edits a conflicted file in their editor mid-run.** `Splice`
  re-parses on every call, so an edit that keeps the markers is handled; one
  that removes them makes the next `resolve_hunk` fail with an out-of-range
  index, which the agent reports rather than corrupting the file.

## Testing

- `internal/merge` conflict parsing and splicing, table-driven over fixtures:
  2-way and 3-way markers, several hunks in one file, CRLF, no trailing
  newline, marker-looking text inside a string literal, malformed nesting.
  The core assertion for `Splice` is that everything outside the replaced
  block is byte-identical.
- `internal/merge` against real repositories built with `internal/testrepo`:
  clean merge, conflicted merge, already-up-to-date, dirty worktree, abort
  restores the previous state, commit produces a merge commit with two
  parents.
- `internal/ai/mergetools`: each tool against a conflicted repo, including
  `resolve_hunk` rejecting a resolution that still has markers and
  `stage_file` refusing an unclean file.
- `internal/app`: `ResolveConflicts` is busy while a chat runs, its event
  order, and `GetMergeState` before, during and after a merge.
- Frontend: the merge-state store, the branch menu label and its disabled
  cases, and `MergeView` rendering each file category.

## Risks

The honest one is model quality. A 7B model resolves import and version
conflicts well and reasons poorly about conflicting logic. The design answers
this with containment rather than optimism — hunk-level writes, a diff to
review, staging as an explicit step, and `git merge --abort` always one click
away — but the feature will still produce resolutions that a careful reader
must reject. The prompt therefore asks the agent to leave anything doubtful
unresolved, and the UI treats an unfinished run as a normal outcome instead
of a failure.
