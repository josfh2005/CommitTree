# AI settings per repository

Date: 2026-10-05. Requested by the owner on 2026-09-18; designed with them on
2026-10-05. Behaviour goes to `docs/spec/06-ai.md` (resolution, instructions,
AI off) and `docs/spec/12-repository-settings.md` (the AI tab).

## Goal

Let each repository use its own AI: a different provider and model, its own
automations, extra prompt instructions, or no AI at all. Examples the owner
gave: work repositories limited to the local model, a large codebase on a
stronger model, a confidential repository with the AI turned off, and team
conventions (commit message style, project context) shared through the
repository.

The global settings stay the base. A repository overrides only what it names.

Out of scope: settings per sidebar group (can be added later as a layer
between global and repository), replacing a whole prompt per repository,
settings for submodules, MCP servers.

## 1. What a repository can set

| Setting | Where it lives | Notes |
|---|---|---|
| AI off | private | Hides and refuses every AI action in the repository |
| Chat provider + model | private | Same rules as the global pair |
| Task provider + model | private | Same rules as the global pair |
| Commit message mode | private | `auto-local` / `auto` / `manual` |
| Suggested replies mode | private | `auto-local` / `auto` / `off` |
| Instructions, every action | private and/or repository file | Appended to the prompt |
| Instructions, one action | private and/or repository file | Appended to that action's prompt |

**Private** means the app's own settings on this machine. **Repository file**
means a file committed in the repository, shared with whoever clones it.
A repository file can only add instructions: it can never choose the
provider or the model, and never turn the AI on or off. That keeps a cloned
repository from sending its own code to a hosted provider the user didn't
pick.

The actions are the six existing prompts: `chat`, `commit-message`,
`resolve-conflicts`, `explain-commit`, `explain-lines`, `suggest-replies`.

## 2. Private settings: `repo-ai.json`

A new file next to `ai.json` (`<UserConfigDir>/git-ui/repo-ai.json`), owned
by a new package `internal/ai/reposettings`:

```json
{
  "a1b2c3d4e5f6": {
    "aiOff": false,
    "chatProvider": "anthropic",
    "chatModel": "claude-opus-5",
    "taskProvider": "",
    "taskModel": "",
    "commitMessage": "",
    "suggestReplies": "",
    "instructions": { "all": "…", "commit-message": "…" },
    "approvedRepoInstructions": "sha256:…"
  }
}
```

- Keyed by the repository's ID from `repos.json`. Every field is optional; an
  empty string, a missing field or a missing entry means "use the global
  value".
- The file is absent until the first override is saved. Saving an entry
  whose fields are all empty removes the entry; removing a repository from
  the list removes its entry. Re-adding a repository gives it a new ID, so it
  starts again from the global settings (documented in the spec).
- Writes are atomic (temp file + rename), like `ai.json`.
- A provider and its model are saved together: setting a provider without a
  model is refused with the same validation as the global settings. Setting
  the provider back to Global clears its model too.

## 3. Shared instructions: `.committree/`

- `.committree/instructions.md` holds instructions for every action.
- `.committree/<action>.md` (e.g. `commit-message.md`) holds instructions
  for one action.
- Read from the working tree of the repository (or linked worktree) at the
  moment of each AI call, so switching branches switches instructions.
- Only regular `.md` files directly inside `.committree/` count. Ignored,
  each with a reason shown in the AI tab: symbolic links, files that are not
  UTF-8, files over 16 KB, everything past 32 KB in total (files taken in
  name order), and names that are not an action (shown as "not used").
- **Approval.** The used files are hashed together (SHA-256 over each name
  and content, in name order). They are applied only when that hash equals
  `approvedRepoInstructions` for the repository. Approving stores the
  current hash; **Ignore** stores the hash with an `ignored:` prefix so the
  notice stops until the files change. Any change to the files (edit, new
  file, branch switch) yields a new hash and asks again. Until approved the
  AI works without them; nothing is blocked.

## 4. Resolution and prompt assembly

- `App.aiSettings()` becomes `App.aiSettingsFor(repoID)`, returning the
  effective `settings.Settings` (global with the repository's overrides on
  top) plus whether the AI is off. Every AI entry point already has a
  repository: `SendChat`, `explainTask` (explain commit, explain lines),
  `ResolveConflicts`, `GenerateCommitMessage` and suggested replies.
  `AIStatus`, `PullModel` and `ListModels` stay global.
- A linked worktree resolves to its main repository's private settings (the
  same entry as the main repository) but reads `.committree/` from its own
  working tree.
- The prompt for an action is, in order:
  1. the global prompt (`prompts.Store.Get`, unchanged);
  2. if approved repository instructions exist for it: a heading
     `## Project instructions`, then the `instructions.md` text, then the
     action's file;
  3. if private instructions exist for it: a heading
     `## Your instructions for this repository`, then `all`, then the
     action's entry.

  Private instructions come last so they win over the repository's.
  Assembly is a pure function in `internal/ai/reposettings` so it is
  tested without the app.
- **AI off.** Every entry point above returns `ErrAIOff` ("AI is off for
  this repository") before building a provider; automatic actions (the
  commit message in `auto` modes, suggested replies, their notifications)
  are skipped silently. Turning the AI off cancels the repository's running
  chat answer, commit message, conflict resolution and pending
  suggestions.

## 5. Errors

- **`repo-ai.json` can't be read or parsed:** every AI action in every
  repository is refused with "Per-repository AI settings can't be read:
  <reason>", and the AI tab shows the same message. The app never falls
  back to the global settings in this case, because a repository that was
  off or limited to the local model would then silently use a hosted one.
- **Missing key or uninstalled model from a repository override:** the
  existing errors, followed by "(set in this repository's settings)".
- **Saving fails** (validation, disk): shown at the top of the AI tab, and
  the form keeps what was entered, like the Remotes tab.
- **`.committree/` unreadable** (permissions): treated as no repository
  instructions, with the reason shown in the AI tab.

## 6. Interface

### Repository settings → AI tab

Next to Remotes, for main repositories (a linked worktree's row has no
Repository settings, as today).

- **Use AI in this repository** — a switch, on by default. When off, the
  rest of the tab is dimmed.
- **Models** — Chat & agent and Tasks, each a provider select and a model
  select as in Settings → AI models. Each select's first option is
  **Global (<model> · <provider>)**, showing the inherited value; choosing
  anything else creates an override for this repository only.
- **Automation** — Commit message and Suggested replies, also with a
  **Global (…)** first option.
- **Your instructions** — a text box "For every action", and "Add
  instructions for…" (a select of the six actions) that adds one text box
  per chosen action, each removable. Saved when a box loses focus. A note:
  "Private: stays on this computer."
- **Repository instructions** — the files found in `.committree/`, each
  with its content folded, its state and, when relevant, why it is ignored.
  States: *Approved*; *Not approved* with **Approve** and **Ignore**;
  *Ignored* with **Approve**; *Changed since you approved* with **Approve**
  and **Ignore**. Approve and Ignore act on all files at once. With no
  files: "This repository has no shared instructions. Add
  `.committree/instructions.md` to share them with your team."

### Elsewhere

- **Chat panel:** when the repository's instructions are neither approved
  nor ignored, one strip above the message box: "This repository has
  instructions for the AI. **Review**" — Review opens the AI tab. When the
  AI is off, the panel shows "AI is off for this repository · **Repository
  settings**" instead of the conversation and the box.
- **AI off hides** Write with AI, Resolve with AI, Explain commit, Explain
  lines and suggested replies in that repository.
- **Model picker under the chat box:** when the repository overrides the
  chat model it reads "<model> · this repo" and changing it changes the
  override; otherwise it changes the global model, as today.
- **Settings → Prompts:** one line: "Repositories can add their own
  instructions in Repository settings → AI."
- **API.** The frontend's AI settings store becomes per repository. Go
  gains `GetRepoAISettings(repoID)` returning `{ effective, overrides,
  aiOff, repoInstructions: { files, state, hash }, error }`, plus
  `SaveRepoAISettings(repoID, overrides)`,
  `ApproveRepoInstructions(repoID, hash)` and
  `IgnoreRepoInstructions(repoID, hash)`. An event
  `repo-ai-settings-changed` (repoID) refreshes open views. Approve and
  Ignore take the hash the user saw, and are refused if the files changed
  since, so a file edited while the tab was open is never approved unseen.

## 7. Testing

- **Go, `internal/ai/reposettings`:** merge field by field (empty means
  global; provider and model together); store load, save, atomic write,
  corrupt file, entry removal; `.committree` reader (hash stability, name
  order, 16 KB / 32 KB limits, symlink, non-UTF-8, unknown names, missing
  directory); prompt assembly order and headings; approve / ignore /
  changed states.
- **Go, `internal/app`:** each entry point uses the repository's provider
  and model; each refuses with `ErrAIOff` when off; automatic actions skip;
  turning off cancels running work; a corrupt `repo-ai.json` refuses
  everything; removing a repository removes its entry; a linked worktree
  uses its main repository's entry; Approve with a stale hash is refused.
- **Frontend (vitest):** the Global (…) option and the override round
  trip; approve / ignore / changed rendering; the chat strip; the AI-off
  panel and hidden buttons; the model picker's "· this repo".
- **Manual pass** with the demo repositories: acme-web on Anthropic with a
  `.committree/commit-message.md`, acme-api with the AI off.
