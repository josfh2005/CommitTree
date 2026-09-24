# git-ui — Blame — Design

Date: 2026-09-24
Status: Draft — design approved in conversation 2026-09-24, awaiting spec review
Builds on: `docs/spec/02-log-and-history.md`, `docs/spec/03-working-tree.md`,
`docs/spec/06-ai.md`, the AI foundation design (`2026-09-17-ai-foundation-design.md`).

## Goal

Answer "who changed this line, in which commit, and why?" without leaving
CommitTree. Open any file listed in a commit or in the Changes view, see the
commit that last touched each line, jump to that commit in the log, walk back
through older revisions, and ask the AI to explain a range of lines.

This design adds:

1. **`GetBlame`** in Go: one `git blame --porcelain` call, parsed into
   blocks of consecutive lines from the same commit.
2. A **Blame view** in the main pane (`mainView = 'blame'`), opened from a
   file's context menu in Commit details and in the Changes view.
3. **Blame previous revision** and **Ignore whitespace** in that view.
4. **✨ Explain these lines in chat**, a one-shot explanation in the style of
   the log's "✨ Explain in chat", with its own editable prompt.
5. A read-only **`blame_file`** chat tool.

## Decisions

Decided by the owner during brainstorming:

- **Entry points:** a file in Commit details (the file at that commit) and a
  file in the Changes view (the working-tree file, uncommitted lines marked).
  No "Blame file…" picker for arbitrary files.
- **Own view in the main pane**, like Changes and Stash, with a Back button;
  not a Diff/Blame toggle inside the diff pane.
- **One-shot blame of the whole file** (approach 1). No `--incremental`
  streaming, no lazy `-L` ranges on scroll.
- **Extras in scope:** Blame previous revision, Ignore whitespace (`-w`).
- **AI:** "Explain these lines in chat" works like "Explain in chat" for a
  commit (task provider, one call, no tool loop, answer streamed into the
  repo's chat). Plus a `blame_file` read tool for the normal chat.

Out of scope: move/copy detection (`-M`/`-C`), syntax highlighting, a picker
for files not shown in any list, opening the file in an external editor.

## Backend

### `internal/gitlog/blame.go`

```go
type BlameOptions struct {
    IgnoreWhitespace bool
    Start, End       int // 1-based inclusive line range; 0, 0 = whole file
}

type BlameBlock struct {
    Hash        string    `json:"hash"`
    Short       string    `json:"short"`
    Author      string    `json:"author"`
    Email       string    `json:"email"`
    Date        time.Time `json:"date"`     // author time
    Summary     string    `json:"summary"`
    Start       int       `json:"start"`    // first final line number, 1-based
    Count       int       `json:"count"`
    Previous    string    `json:"previous,omitempty"`  // porcelain "previous" commit
    PrevPath    string    `json:"prevPath,omitempty"`  // path in Previous (follows renames)
    Boundary    bool      `json:"boundary,omitempty"`  // shallow/root boundary commit
    Uncommitted bool      `json:"uncommitted,omitempty"` // hash is all zeros
}

type Blame struct {
    Path      string       `json:"path"`
    Rev       string       `json:"rev"`   // "" = working tree
    Lines     []string     `json:"lines"` // file text, one entry per line
    Blocks    []BlameBlock `json:"blocks"` // ordered, cover Lines exactly
    Truncated bool         `json:"truncated"`
}

func GetBlame(ctx context.Context, dir, rev, path string, opts BlameOptions) (Blame, error)
```

- Runs `git blame --porcelain [-w] [-L s,e] [<rev>] --end-of-options -- <path>`
  (no `<rev>` for the working tree) through `gitcmd` with a new
  `gitcmd.BlameTimeout = 30 * time.Second`; `ReadTimeout` (10 s) is too short
  for long-history files.
- **Parser** (`parsePorcelain`, pure, table-tested): the porcelain header
  gives a commit's metadata only on its first appearance, so metadata is kept
  per hash; each content line (`\t`-prefixed) is appended to `Lines`. A block
  is a maximal run of consecutive final lines with the same hash. Header
  fields used: `author`, `author-mail` (angle brackets stripped),
  `author-time` + `author-tz`, `summary`, `previous <hash> <path>`,
  `boundary`.
- **Uncommitted:** hash `0000000000000000000000000000000000000000` sets
  `Uncommitted` and blanks author/summary (git reports "Not Committed Yet").
- **Cap:** `maxBlameLines = 20000` (a package variable so tests can lower
  it). Past the cap, parsing stops, `Truncated = true`, and the last block is
  cut to end at the cap.
- **Binary files:** if any content line contains a NUL byte, return
  `ErrBinaryBlame` ("binary file — blame not available").
- Other git errors (path not in that revision, bad rev) are returned as the
  git message.

### `internal/app/blame.go`

```go
func (a *App) GetBlame(id, rev, path string, ignoreWhitespace bool) (gitlog.Blame, error)
```

Resolves `dir` like `GetDetails`; no write lock (read-only).

### Explain these lines in chat

```go
func (a *App) ExplainLinesInChat(repoID, rev, path string, start, end int, provider, runID string) error
```

- Same shape as `ExplainInChat`: requires AI enabled, takes the repo's chat
  slot (`ErrChatBusy` when taken), uses the task provider unless `provider`
  is given, 3-minute timeout, stores the question and the answer in the
  chat history, emits the usual start/delta/done/error chat events. The
  shared skeleton (slot, history, events, finish) is extracted into one
  helper used by both methods, so the two cannot drift.
- **Question stored in history:**
  `Explain lines 40–58 of internal/foo.go (at a1b2c3d)`, or
  `(working tree)` when `rev` is empty.
- **Context** built by a new `tasks.ExplainLines` (next to `tasks.Explain`),
  within the same budget (`tasks.OllamaDiffBudget`) so it works on Ollama:
  1. the selected lines with line numbers;
  2. the blame of that range (`GetBlame` with `Start`/`End`), one line per
     block;
  3. for each distinct commit in the range, newest first, **at most 5**: its
     full message and the diff of that file in that commit (compared with
     its first parent). Uncommitted lines contribute the working-tree diff
     of the file instead. Diffs are trimmed to fit the budget, newest commit
     first.
- **Prompt:** new editable prompt `explain-lines`
  (`prompts.ExplainLines`, default in `prompts/defaults/explain-lines.md`).
  Settings' Prompts section lists every embedded default, so it appears there
  without UI changes; it runs on the task provider chosen under
  "Explain commit".

### `blame_file` chat tool

Added to `internal/ai/tools` (read tools):

- Parameters: `path` (required), `rev` (default `HEAD`), `start_line`,
  `end_line`.
- Output: one line per block, no file text, to save tokens:
  `L12-18  a1b2c3d  2026-09-01  Jose Fidalgo  <summary>`;
  uncommitted blocks read `L3-4  (not committed yet)`.
- Without a range, at most 200 blocks; past that the output ends with
  `… truncated; pass start_line/end_line to narrow`.
- Uses `gitlog.GetBlame`; errors come back as `error: …` like the other
  tools.

## Frontend

### State (`lib/stores.ts`, `lib/blame.ts`)

- `mainView` gains `'blame'`.
- `blameTarget: writable<BlameTarget | null>` with
  `BlameTarget = { path: string; rev: string; from: 'log' | 'changes' }`
  (`rev` empty = working tree; `from` = where Back returns to).
- `blameStack: BlameTarget[]` for Blame previous revision; kept in
  `lib/blame.ts` as pure helpers (`pushRevision`, `back`) returning the next
  target or `null` (= leave the view).
- `openBlame(path, rev, from)` resets the stack, sets the target and
  `mainView = 'blame'`. Changing or deselecting the repository clears
  `blameTarget` and returns to the log.
- `blameIgnoreWhitespace = persisted('blameIgnoreWhitespace', false)`.
- App.svelte shows `<BlameView>` when `mainView === 'blame'`, the repo is
  present, and no conflict owns the screen (`conflictWins` still wins).

### Entry points

- **Commit details:** right-click a file → **Blame** (`rev = details.hash`).
  Disabled with a tooltip for deleted files (`D`) and submodules.
- **Changes view:** right-click a file (through `FileList`'s `onMenu`) →
  **Blame** (`rev = ''`). Disabled for deleted, untracked and submodule
  entries.

The disabled rules live in `lib/blame.ts` (`blameBlocker(file, where)`,
returns the tooltip or `null`) so both views share them and they are
unit-tested.

### `BlameView.svelte`

- **Header:** `← Back`, the path, the revision (`a1b2c3d` or
  "Working tree"), an **Ignore whitespace** checkbox. When `Truncated`, a
  notice: "Showing the first 20 000 lines".
- **Body:** a monospace two-column table.
  - **Gutter:** on a block's first line only, `short · author · relative
    date`; other lines of the block are blank. Blocks alternate a subtle
    background band so boundaries are visible. Hovering the gutter shows the
    commit summary as a tooltip. Uncommitted blocks read "Not committed yet"
    in a faint colour; boundary blocks prefix the hash with `^`.
  - Then the line number and the line text.
- **Hash click** → `selectedHash.set(hash)`, `jumpTo.set(hash)` and
  `mainView.set('log')` explicitly: when the blame was opened from that same
  commit, `selectedHash` does not change, so App.svelte's "a selection means
  the log" rule would not fire on its own. No link on uncommitted blocks.
- **Line selection:** click a line number to select it, shift-click to
  extend to a range. Right-clicking inside the selection acts on it;
  right-clicking elsewhere acts on that line's block.
- **Context menu** (on a block or the selection):
  - **✨ Explain these lines in chat** — opens the chat and calls
    `explainLinesInChat`; never disabled, like the log's "Explain in chat":
    AI off, busy chat or a provider error comes back as a toast.
  - **Blame previous revision** — pushes the current target and opens the
    blame at `Previous` / `PrevPath`. Disabled when the range spans more
    than one commit, the block has no `Previous` (the lines were born
    there), is uncommitted, or is a boundary.
  - **Show commit** — same as clicking the hash (disabled when uncommitted).
  - **Copy hash** (disabled when uncommitted).
- **Back** pops `blameStack`; when it is empty, returns to `from`
  (`mainView = 'log'` or `'changes'`).
- **Loading:** a request counter drops stale responses (as in
  CommitDetails); "Loading blame…" while waiting. A working-tree blame
  reloads on `worktree:changed` for the selected repo.

## Errors and edge cases

| Case | Behaviour |
|------|-----------|
| Binary file | View shows "Binary file — blame not available". |
| Path missing at that revision / bad rev | Git's message shown in the view. |
| Blame over 30 s | "Blame took too long" with a **Retry** button. |
| Over 20 000 lines | First 20 000 shown, truncation notice. |
| Repo deselected, removed or missing | Target cleared, back to the log. |
| Shallow clone boundary | `^` marker; Blame previous revision disabled. |
| Explain: AI off, chat busy, provider error | Toast with the error, as in the log. |

## Testing

- **Go, `internal/gitlog/blame_test.go`** (with `internal/testrepo`): block
  grouping; metadata for a hash repeated in non-adjacent blocks; `previous`
  and `PrevPath` after a rename; uncommitted lines in a working-tree blame;
  `-w` ignoring an indentation-only change; `-L` range; truncation with a
  lowered cap; binary error; boundary flag.
- **Go, parser:** table tests of `parsePorcelain` over fixed porcelain
  output.
- **Go, `internal/ai/tools`:** `blame_file` output format, the 200-block cap,
  error text.
- **Go, `internal/ai/tasks`:** `ExplainLines` context: at most 5 commits,
  newest first, budget trimming, uncommitted range uses the working-tree
  diff.
- **Go, `internal/app`:** `ExplainLinesInChat` question text, `ErrChatBusy`,
  `ErrAIDisabled`; `ExplainInChat` still passes after the helper extraction.
- **Vitest, `lib/blame.test.ts`:** open/back stack, range selection, block →
  range for the menu, `blameBlocker` rules, Blame-previous-revision disabled
  rules, relative date formatting.

## Documentation

Updated in the same commits as the behaviour:

- `docs/spec/02-log-and-history.md` — new "Blame" section (entry from Commit
  details, the view, navigation, limits).
- `docs/spec/03-working-tree.md` — Blame from the Changes view.
- `docs/spec/06-ai.md` — `blame_file` tool, "Explain these lines in chat",
  the `explain-lines` prompt in Settings.
