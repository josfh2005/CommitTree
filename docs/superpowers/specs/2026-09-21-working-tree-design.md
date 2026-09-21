# git-ui — Sub-project 2a: Working tree — changes, staging and commit — Design

Date: 2026-09-21
Status: Implemented (manual pass pending)
Builds on: `2026-09-16-git-ui-design.md` (core viewer),
`2026-09-17-merge-agent-design.md` (merge view, whose file list this reuses) and
`2026-09-20-ai-providers-design.md` (task provider), all merged to `main`.

## Goal

See what has changed in the working tree, stage and unstage by file, discard
changes, and commit — with the commit message written by the task model. This
is what the app still cannot do: today it can merge and resolve conflicts, but
an ordinary edit has to be committed in a terminal.

## Scope decisions

- **Split from sub-project 2.** This is 2a. Push, pull with upstream handling
  and stash are 2b, designed separately.
- **File-level staging only.** No hunk-level staging in 2a; it is the single
  largest piece of the original scope and the file level makes the app usable
  on its own.
- **A third mode in the main pane**, beside the log and the merge view, rather
  than a permanent bottom panel or a synthetic row at the top of the log.
- **Amend is included**, because it is what people reach for a minute after
  committing. Anything that rewrites more than the last commit is not.
- **Discarding an untracked file deletes it**, after a confirmation that says
  it cannot be undone. No trash integration.
- **Out of scope:** hunks, push, stash, rebase, cherry-pick, commit signing,
  and committing while a merge is in progress (the merge view owns that).

## Architecture

### `internal/worktree` (new)

A sibling of `internal/merge`, which already has this shape for merges.

```go
type FileStatus struct {
    Path    string `json:"path"`
    OldPath string `json:"oldPath,omitempty"` // rename source
    Status  string `json:"status"`            // M, A, D, R, T, ?
}

type State struct {
    Staged    []FileStatus `json:"staged"`
    Unstaged  []FileStatus `json:"unstaged"`
    Untracked []FileStatus `json:"untracked"`
    Merging   bool         `json:"merging"` // the merge view owns this repo
}

func Status(ctx context.Context, dir string) (State, error)
func Stage(ctx context.Context, dir, path string) error
func Unstage(ctx context.Context, dir, path string) error
func Discard(ctx context.Context, dir, path string) error
func Commit(ctx context.Context, dir, message string, amend bool) error
func Preview(ctx context.Context, dir string) (CommitInfo, error)
```

`Status` reads `git status --porcelain=v2 -z --untracked-files=all`. The v2
format gives the staged and unstaged state of each path in one call, names
rename sources explicitly, and `-z` survives spaces, newlines and non-ASCII
names — all three of which the merge work had to handle the hard way.

A file that is both staged and modified appears in **both** lists, because
that is what git means and hiding it would lose the distinction between what
will be committed and what will not.

### Paths from the renderer

Every mutating call takes a path that must exactly equal one `Status` just
returned, and runs its git command with `--literal-pathspecs`. This is the
rule the merge review settled after finding that a bare path is a pathspec, so
`:(glob)*` or `*` could reach files the user never selected. `Discard` also
refuses a path that is a directory prefix of another listed path, for the
directory/file swap the same review found.

### Operations

| Operation | Command |
|---|---|
| Stage tracked or untracked | `git add -- <path>` |
| Unstage | `git restore --staged -- <path>` |
| Discard unstaged, tracked | `git restore -- <path>` |
| Discard staged + modified | `git restore --source=HEAD --staged --worktree -- <path>` |
| Discard untracked | delete the file |
| Commit | `git commit -F <tmpfile>` (`--amend` when amending) |

The message goes to git through a temporary file, never on the command line:
a long message with quotes or newlines is otherwise at the mercy of quoting
and of ARG_MAX. The file is written inside the repository's `.git` directory
and removed afterwards.

`Discard` on an untracked path uses `os.Lstat` and refuses anything that is
not a regular file or a symlink — never following a link and never recursing
into a directory, so a planted link cannot make the app delete outside the
repository.

### `CommitInfo`

```go
type CommitInfo struct {
    StagedCount int    `json:"stagedCount"`
    CanAmend    bool   `json:"canAmend"`    // false in an empty repository
    LastMessage string `json:"lastMessage"` // loaded when Amend is ticked
    Pushed      bool   `json:"pushed"`      // the commit to amend is on the upstream
    Upstream    string `json:"upstream"`
}
```

`Pushed` drives the amend warning, the same way `ops.ResetPreview` drives the
reset dialog's.

### App layer

- `GetWorktreeState(id)`, `StageFile(id, path)`, `UnstageFile(id, path)`,
  `DiscardFile(id, path)`, `CommitChanges(id, message, amend)`,
  `GetCommitPreview(id)`, `GenerateCommitMessage(id, runID)`.
- Every mutation runs under the existing per-repo write lock, so it cannot
  interleave with a merge action or an agent tool call, and emits
  `worktree:changed` afterwards.
- A commit invalidates the log: it emits the existing log-refresh path too, so
  the graph shows the new commit without a manual reload.

## The commit message

- A new prompt `commit-message` beside `explain-commit`, with a default in
  `internal/ai/prompts/defaults` and editable in Settings like the others.
- It receives the **staged** diff only — the message must describe what is
  being committed — truncated to `tasks.OllamaDiffBudget` with an explicit
  "truncated" line so the model does not invent what it could not see, plus
  the repository name and branch.
- It runs on the **task** provider and model, not the chat one, through the
  existing `responderFor`, and streams into the textarea as `commit:delta`
  events with a `commit:done`, mirroring how explain already streams.
- A new setting controls when it runs:

```go
CommitMessage string `json:"commitMessage"` // auto-local | auto | manual
```

  `auto-local` is the default: generate automatically when the task provider
  is Ollama (local and free), otherwise offer the button. `auto` always
  generates; `manual` never does. Automatic generation fires when the staged
  set changes **and** the textarea is untouched; once the user types, it stops
  until the box is cleared or the button is pressed.

- The button is present in every mode, so the message can be regenerated after
  staging one more file.

## Frontend

- **`FileList.svelte` (new, shared):** the merge view's file list — section
  headers with counts, a status glyph per row, a hover action — extracted so
  both views use one component. `MergeView.svelte` is refactored onto it in
  the same task, which is the only change to existing merge behaviour.
- **`ChangesView.svelte` (new):** `FileList` on the left with Staged, Unstaged
  and Untracked; the diff on the right; the commit box below the list.
- **The sidebar** gains a "Changes" row above the branches with the number of
  changed files; clicking it selects the Changes mode, and clicking a commit
  in the log leaves it. While a merge is in progress the merge view wins.
- **The diff pane** shows the staged diff against HEAD, the unstaged diff
  against the index, and for an untracked file its contents, capped, with a
  note when the file is binary.
- **The commit box:** textarea, "Write with AI" (with a stop while streaming),
  an Amend checkbox that loads the previous message, and Commit — disabled
  while nothing is staged. Amending a commit already on the upstream asks for
  confirmation first.

## Errors

| Case | Behaviour |
|---|---|
| Nothing staged | Commit and the AI button are disabled |
| Path no longer in status | The action is refused; the view refreshes |
| git refuses the commit (identity, hook, empty message) | git's own message in a toast, verbatim |
| Amend in an empty repository | The checkbox is disabled |
| Provider error while generating | Toast; the textarea keeps whatever arrived |
| Repository is mid-merge | The Changes row routes to the merge view instead |

## Testing

- **Go, against real temp repositories** as `internal/merge` does: staging a
  rename, a deletion and an untracked file; a path with a space and a
  non-ASCII path; discard for each of the three cases; a commit whose message
  spans lines and contains quotes; amend, and amend refused in an empty
  repository; pathspec magic refused at every entry point; and a
  directory/file swap refused by `Discard`.
- **Vitest, on the pure parts:** grouping a status into sections, when Commit
  and the AI button are enabled, and the auto-generate rule — fires on a
  staged change, never overwrites what the user typed.
- **Manual, added to the pending list:** the AI message against a real
  provider.

## Risks

- **Discarding deletes an untracked file for good.** Mitigated by a
  confirmation naming the file and saying it cannot be undone, and by refusing
  anything that is not a regular file or symlink.
- **Extracting `FileList` touches working merge code.** Mitigated by doing it
  in one task with the merge view's existing tests as the check.
- **`status --porcelain=v2` parsing is fiddly.** Mitigated by tests over the
  awkward names, and by never interpreting a path the renderer sends.
- **A slow AI message on a large staged diff.** Mitigated by the budget, by
  streaming, and by a stop button.

## Out of scope / later (2b and beyond)

- Push, pull with upstream handling, stash.
- Hunk-level staging.
- Commit signing, and anything rewriting more than the last commit.
