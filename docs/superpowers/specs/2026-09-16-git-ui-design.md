# git-ui — Sub-project 1: Core Viewer — Design

Date: 2026-09-16
Status: Draft for review

## Product context

git-ui is an AI-first desktop Git client. It is built as five sub-projects,
each with its own spec, plan and working build:

1. **Core viewer (this spec):** repos, refs, graph log, checkout, fetch/pull,
   branch/tag create/delete, visual style, chat panel shell.
2. **Working tree:** changes view, stage/unstage (hunk level), commit/amend,
   push, stash.
3. **AI foundation:** provider layer (Anthropic, OpenAI, Ollama), keys in macOS
   Keychain, settings, streaming, context budgeting, secret redaction,
   "local only" mode. Functional repo chat with git tools. First features:
   commit message generation, explain commit/diff.
4. **Merge / rebase / cherry-pick:** conflict resolution UI + AI conflict
   resolver.
5. **AI extras:** split changes into commits, pre-commit review, ticket-aware
   messages, branch name suggestion, PR description, history cleanup,
   release-branch sync, release notes, blame explain, natural-language search,
   reflog recovery, risk explainer.

AI rule for all sub-projects: AI proposes, the user confirms; no git write runs
without explicit confirmation.

## Goal

A desktop Git client for browsing multiple repositories with a JetBrains-style
commit graph, plus common ref operations (checkout, fetch/pull, create/delete
branches and tags), in a minimal Claude-desktop-like visual style, with the
right-hand chat panel in place for sub-project 3.

## Non-goals (this sub-project)

Staging, committing, push (other than remote branch delete), merge, rebase,
stash, pushing tags, repo folder scanning, any AI provider calls (the chat
panel is a placeholder).

## Stack

- **Wails v2** desktop app (Go backend + web view).
- **Backend:** Go 1.26, shells out to the `git` CLI (uses user's SSH agent,
  credential helpers and config).
- **Frontend:** Svelte + TypeScript (Vite), Node 22 via nvm.
- Graph lane layout computed in Go; frontend only renders.

## Visual style

Modeled on the Claude desktop app: light warm-gray background, near-white
content surfaces, thin line icons, small muted section headers with a `+`
action aligned right, list rows without borders, active row as a rounded
light-gray pill, generous spacing, one system font (SF Pro via
`-apple-system`). Dark mode follows the OS via `prefers-color-scheme`.
All colors are CSS custom properties in one `theme.css`. Graph lane colors
are the only saturated colors in the UI.

## Layout

Three columns: sidebar | log (with details pane below) | chat. Sidebar and
chat widths are draggable; the chat panel collapses via a toggle in the top
right. The details pane height is draggable.

```
┌──────────────────┬──────────────────────────────┬─────────────────┐
│ + Add repo       │ [search] Branch▾ User▾ Date▾ │ Chat          ⟩ │
│                  │ ●─┐ Merged in hotfix/...     │                 │
│ Repos            │ │ ● fix: ...                 │                 │
│ ▾ git-ui  main ⟳ │ ●─┘ ...                      │                 │
│   Branches     + │                              │  Set up an AI   │
│   ✓ main         │                              │  provider to    │
│     feature/x    │                              │  chat with this │
│   Remotes        ├──────────────────────────────┤  repo.          │
│   Tags         + │ Commit details               │                 │
│ ▸ nexo-api       │ files │ diff                 │ [disabled input]│
│ ▸ storage        │                              │                 │
│ ──────────────── │                              │                 │
│ ⚙ Settings       │                              │                 │
└──────────────────┴──────────────────────────────┴─────────────────┘
```

- **Sidebar:** `+ Add repo` at the top opens a native folder picker and adds
  one repo (must be a git work tree); the list is persisted. Each repo row
  shows name and current branch; hovering shows Fetch and Pull buttons.
  Clicking a repo selects it and expands it (one expanded at a time).
  Context menu: Remove, Locate (for missing repos). Inside the expanded repo:
  - **Branches** (`+` = new branch from HEAD): local branches, HEAD marked ✓;
    detached HEAD shown first as `HEAD (<short>)`.
  - **Remotes:** collapsible group per remote with its branches.
  - **Tags** (`+` = new tag at HEAD).
  Click a branch → filter log to it (click again to clear). Double-click local
  branch → checkout. Context menu: Checkout, New branch from here, Delete,
  New tag here.
  Bottom: Settings (in this sub-project only shows app version; providers
  arrive in sub-project 3).
- **Chat panel:** header "Chat" with collapse toggle; body shows an empty
  state "Set up an AI provider to chat with this repo" and a disabled input.
  Collapsed state is persisted.
- **Center — Log:** filter bar (text/hash, Branch, User, Date range, Paths),
  virtualized rows with canvas graph column, subject, author, relative date,
  short hash, ref badges. Merge commits rendered gray; HEAD commit drawn as a
  ring. Selecting a commit opens the details pane (full message, metadata,
  changed files with status; clicking a file shows its diff). Commit context
  menu: Checkout (detached), New branch here, New tag here, Copy hash.

## Backend packages

| Package | Responsibility |
|---|---|
| `internal/gitcmd` | Run `git -C <repo> ...` with timeout and `GIT_TERMINAL_PROMPT=0`; return stdout or a typed error carrying stderr. |
| `internal/repos` | Load/save repo list JSON at `~/Library/Application Support/git-ui/repos.json` (via `os.UserConfigDir`); validate paths; flag missing repos. |
| `internal/gitlog` | Build `git log` args from filters, parse NUL-delimited output into `Commit`s, paginate. |
| `internal/graph` | Pure lane layout: `[]Commit` → `[]Row`, with resumable state across pages. No git knowledge. |
| `internal/refs` | List branches/remotes/tags; create/delete branch and tag; delete remote branch. |
| `internal/ops` | Checkout (branch, remote branch, detached), fetch, pull. |
| `app.go` | Wails-bound methods; per-repo write mutex; event emission for progress. |

## Data flow — log

1. Frontend calls `GetLog(repoID, filters, offset, limit=500)`.
2. `gitlog` runs:
   `git log --topo-order --parents --decorate=full -z --format=<fields sep by %x00> [--all | <branch>] [--author=] [--since=] [--until=] [--grep= -i] --skip=<offset> -n <limit> [-- <paths>]`.
   A text filter matching `^[0-9a-f]{4,40}$` is additionally tried as a hash
   (`git rev-parse --verify`); if it resolves, the log jumps to that commit.
3. Parsed commits → `graph.Layout` (state cached per repo+filter key in the
   backend so the next page continues the same lanes; reset when filters or
   refs change).
4. Returns `[]Row`; frontend appends and draws only visible rows. Next page is
   requested when scrolling within ~100 rows of the end.

```go
type Commit struct {
    Hash, Short   string
    Parents       []string
    Author, Email string
    Date          time.Time
    Subject       string
    Refs          []Ref // {Name, Kind: local|remote|tag|head}
}

type EdgeKind int // Straight, MergeIn, ForkOut, ArrowUp, ArrowDown

type Edge struct {
    FromLane, ToLane int
    Color            int
    Kind             EdgeKind
}

type Row struct {
    Commit  Commit
    Lane    int
    Color   int
    Edges   []Edge // segments from this row's vertical center to the next row's
    IsMerge bool
    IsHead  bool
}
```

## Graph lane algorithm

State: `lanes []*lane` where a lane is `{wantHash string, color int}` or nil
(free). Color counter increments when a new line starts.

For each commit C in topo order:

1. Find all lanes whose `wantHash == C.Hash`. If any: C takes the leftmost;
   the others terminate into C (emit `MergeIn` edges toward C's lane) and are
   freed. If none: C takes the first free slot (or appends) with a new color.
2. Parents:
   - First parent inherits C's lane and color (`wantHash = parents[0]`).
   - Each additional parent: if some lane already wants it, emit a `ForkOut`
     edge to that lane; otherwise allocate the first free slot with a new
     color and emit `ForkOut` to it.
   - No parents (root): C's lane is freed.
3. Every other still-active lane emits a `Straight` edge to itself.
4. Trailing free slots are trimmed so width stays minimal.

**Long edges:** a lane that passes through more than `MaxStraight = 30`
consecutive rows without reaching its target is collapsed: emit `ArrowDown`
after the source and `ArrowUp` just before the target; in between, the slot
stays reserved (not reused) but no `Straight` edges are drawn for it. Hovering an arrow shows the
target commit subject; clicking scrolls to it (loading pages as needed).

Colors: a fixed palette of 8 colors, indexed `color % 8`; merge commit rows
use a gray text color, dot keeps the lane color; HEAD dot is a ring.

## Operations

| Action | Command | Guard |
|---|---|---|
| Checkout local branch | `git switch <b>` | git error (e.g. conflicting local changes) surfaced verbatim |
| Checkout remote branch | `git switch --track <remote>/<b>` (or switch to existing local of same name) | same |
| Checkout commit | `git switch --detach <hash>` | confirm dialog |
| Fetch | `git fetch --all --prune` | background, spinner, 5 min timeout |
| Pull | `git pull --ff-only` | non-ff → error explaining it can't fast-forward |
| New branch | `git branch <name> <target>` [+ `git switch <name>`] | `git check-ref-format --branch` |
| Delete branch | `git branch -d <b>` | confirm; on "not fully merged" offer second confirm → `-D`; current branch not deletable |
| Delete remote branch | `git push <remote> --delete <b>` | explicit confirm naming remote |
| New tag | `git tag <name> <hash>` or `git tag -a <name> -m <msg> <hash>` | `git check-ref-format refs/tags/<name>` |
| Delete tag | `git tag -d <name>` | confirm (local only) |

After any write: refresh refs and reset/reload log.

## Error handling & edge cases

- Timeouts: 10 s reads, 5 min fetch/pull.
- `GIT_TERMINAL_PROMPT=0` so auth failures return instead of hanging.
- One write operation per repo at a time (mutex); UI disables actions while
  running.
- Errors displayed as a toast with git's stderr and a Copy button.
- Missing/non-git repo path: listed grayed with "missing" badge; Remove or
  Locate actions.
- Empty repo: "No commits yet".
- Shallow clone: note at bottom of log.
- On window focus: compare `git for-each-ref` output hash to cached; reload
  refs/log only if changed.

## Testing

- **graph:** table-driven unit tests on synthetic commit lists — linear,
  branch+merge, octopus merge, disjoint histories, lane reuse, color stability,
  long-edge arrows, resumable layout across page boundaries (layout of
  page1+page2 equals layout of the concatenation).
- **gitlog:** parser tests on captured output (special chars, many refs,
  no parents); filter → args tests.
- **gitcmd / refs / ops:** integration tests creating temp repos (and a bare
  remote) exercising checkout, branch/tag create/delete, unmerged-delete
  refusal, ff-only pull refusal, remote branch delete, timeout behavior.
- **repos:** persistence round-trip, missing detection.
- **Frontend:** Vitest for canvas geometry (lane/row → pixel coordinates).
- Manual verification against a real repo with heavy merge history.
- TDD throughout.

## Build

- `wails dev` for development; `wails build` → `build/bin/git-ui.app`.
- Wails CLI installed via `go install github.com/wailsapp/wails/v2/cmd/wails@latest`.
- Frontend uses Node 22 (`nvm use 22`).
