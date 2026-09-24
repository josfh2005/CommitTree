export type RefKind = 'head' | 'local' | 'remote' | 'tag'

export interface Ref {
  name: string
  kind: RefKind
}

export interface Commit {
  hash: string
  short: string
  parents: string[]
  author: string
  email: string
  date: string
  subject: string
  refs: Ref[]
}

export const EDGE_LINE = 0
export const EDGE_ARROW_DOWN = 1
export const EDGE_ARROW_UP = 2

export interface Edge {
  from: number
  to: number
  color: number
  kind: number
  target?: string
}

export interface LogRow extends Commit {
  lane: number
  color: number
  edges: Edge[]
  isMerge: boolean
  isHead: boolean
}

export interface LogPage {
  rows: LogRow[]
  hasMore: boolean
  graphVisible: boolean
}

export interface Filters {
  text: string
  branch: string
  author: string
  since: string
  until: string
  paths: string[]
}

export const emptyFilters = (): Filters => ({ text: '', branch: '', author: '', since: '', until: '', paths: [] })

/** How the log is walked. 'topo' (the default) keeps a merged branch's own
 *  history together; 'date' is a strict walk by commit date. This is a
 *  global preference (see stores.ts's logOrder), not per-repository. */
export type LogOrder = 'topo' | 'date'

export interface Repo {
  id: string
  name: string
  path: string
  missing: boolean
  branch: string
  group?: string
  /** The main repository this is a linked worktree of, when listed. */
  parentId?: string
  /** A detected worktree: not a list entry (cannot be removed or grouped). */
  worktree?: boolean
  /** An initialised submodule, listed under its parent repository. */
  submodule?: boolean
  /** Path of this submodule relative to the top repository, when
   *  `submodule` (not its immediate parent — see submoduleRepoId). */
  subPath?: string
  /** Number of submodules this repository has (initialised or not). */
  submoduleCount?: number
}

export interface Branch {
  name: string
  remote: string
  hash: string
  current: boolean
  upstream: string
  /** Path of another worktree that has this branch checked out. */
  worktree?: string
  /** That worktree's directory is gone but git has not pruned it yet. */
  worktreeGone?: boolean
}

/** What the "Remove worktree…" confirmation shows, from WorktreeRemovalInfo. */
export interface WorktreeRemovalInfo {
  branch: string
  detached: boolean
  changes: number
  locked: boolean
  /** Whether branch is merged into the main working tree's HEAD — what
   *  `git branch -d` would accept. Meaningless when detached. */
  merged: boolean
}

export interface Remote {
  name: string
  branches: Branch[]
}

export interface Tag {
  name: string
  hash: string
}

export interface Refs {
  head: string
  headHash: string
  detached: boolean
  local: Branch[]
  remotes: Remote[]
  tags: Tag[]
}

export interface FileChange {
  status: string
  path: string
  oldPath?: string
  submodule?: boolean
}

export interface Details extends Commit {
  body: string
  committer: string
  committerEmail: string
  commitDate: string
  files: FileChange[]
}

export interface AIToolCall {
  id: string
  name: string
  args: Record<string, unknown> | null
}

export interface AIMessage {
  role: 'system' | 'user' | 'assistant' | 'tool'
  content: string
  toolCalls?: AIToolCall[]
  toolName?: string
  stopped?: boolean
}

export type ProviderName = 'ollama' | 'openai' | 'anthropic'

export interface ProviderStatus {
  provider: string
  hasKey: boolean
  keyHint: string
  error?: string
}

export type CommitMessageMode = 'auto-local' | 'auto' | 'manual'

export interface AISettings {
  ollamaURL: string
  chatProvider: ProviderName
  chatModel: string
  taskProvider: ProviderName
  taskModel: string
  commitMessage: CommitMessageMode
}

export interface OllamaModel {
  name: string
  size: number
}

export interface AIStatus {
  ollama: {
    running: boolean
    url: string
    chatModel: string
    models: OllamaModel[]
    chatModelInstalled: boolean
    error?: string
  }
  providers: ProviderStatus[]
  // keyStore is why keys cannot be saved on this system, when they cannot.
  keyStore?: string
}

export interface PromptInfo {
  name: string
  customized: boolean
}

export interface ModelProgress {
  name: string
  status: string
  completed: number
  total: number
}

export interface ModelDone {
  name: string
  error?: string
  canceled?: boolean
}

export interface ChatStartEvent { repoID: string; runID: string; text: string }
export interface ChatDeltaEvent { repoID: string; runID: string; text: string }
export interface ChatToolEvent { repoID: string; runID: string; name: string; args: Record<string, unknown> | null }
export interface ChatToolResultEvent { repoID: string; runID: string; name: string; summary: string }
export interface ChatNoticeEvent { repoID: string; runID: string; text: string }
export interface ChatDoneEvent { repoID: string; runID: string }
export interface ChatErrorEvent { repoID: string; runID: string; message: string; code: string }
export interface ChatConfirmEvent { repoID: string; runID: string; confirmID: string; tool: string; title: string; details: string[] }
export interface RepoChangedEvent { repoID: string }
export interface ExplainDeltaEvent { runID: string; text: string }
export interface ExplainDoneEvent { runID: string }
export interface ExplainErrorEvent { runID: string; message: string }

export type ConflictKind = 'merge' | 'rebase' | 'cherry-pick' | 'revert' | 'am' | 'stash' | ''

export interface MergeState {
  kind: ConflictKind
  merging: boolean
  from: string
  into: string
  conflicts: string[]
  manual: string[]
  staged: string[]
  unstaged: string[]
  step?: number
  total?: number
  subject?: string
  oursLabel?: string
  theirsLabel?: string
}

export interface AheadBehind {
  ahead: number
  behind: number
}

export const PULL_UP_TO_DATE = 0
export const PULL_MERGED = 1
export const PULL_REBASED = 2
export const PULL_CONFLICTED = 3

export interface PullResult {
  outcome: number
  conflicts: string[]
}

export interface GitSettings {
  pullStrategy: 'auto' | 'merge' | 'rebase'
}

export interface StashEntry {
  index: number
  message: string
  branch: string
  hash: string
}

export interface StashFile {
  path: string
  oldPath?: string
  status: string
  untracked: boolean
}

export const MERGED = 0
export const CONFLICTED = 1
export const UP_TO_DATE = 2
export const REBASED = 3
export const PICKED = 4
export const NOTHING_TO_APPLY = 5

export interface MergeResult {
  outcome: number
  conflicts: string[]
}

export interface RebasePreview {
  commits: number
  merges: number
  published: number
  upstream: string
}

export interface ConflictFile {
  path: string
  resolved: boolean
  text: string
}

export interface MergeChangedEvent { repoID: string }

export type ResetMode = 'soft' | 'mixed' | 'hard'
export interface ResetInfo { undone: number; gained: number; pushed: number; upstream: string }

export interface FileStatus { path: string; oldPath?: string; status: string; submodule?: boolean; subCommit?: boolean; subModified?: boolean; subUntracked?: boolean }

export interface Submodule {
  name: string
  path: string
  url: string
  recorded: string
  checkedOut: string
  branch: string
  initialised: boolean
  configured: boolean
  moved: boolean
  modified: boolean
  untracked: boolean
  conflict: boolean
}
export interface WorktreeState { staged: FileStatus[]; unstaged: FileStatus[]; untracked: FileStatus[]; merging: boolean }
export interface CommitInfo { stagedCount: number; canAmend: boolean; lastMessage: string; pushed: boolean; upstream: string }
export interface WorktreeChangedEvent { repoID: string }
export interface CommitDeltaEvent { repoID: string; runID: string; text: string }
export interface CommitDoneEvent { repoID: string; runID: string; error?: string }
