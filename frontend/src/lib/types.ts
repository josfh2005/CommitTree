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
  /** Commits the branch has that its upstream lacks, as of the last fetch. */
  ahead?: number
  /** Commits the upstream has that the branch lacks, as of the last fetch. */
  behind?: number
  /** The upstream is configured but its remote branch no longer exists. */
  upstreamGone?: boolean
  /** The upstream is another local branch, not a remote one. */
  upstreamLocal?: boolean
  /** A local branch "Main branches" pushes: main, master, develop, release/*
   *  or the repository's git-flow names (decided in Go, refs.OfficialRule). */
  official: boolean
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

/** Go's repoinfo.Info: git count-objects -v in bytes, plus git's GC rule. */
export interface RepoInfo {
  loose: number
  looseSize: number
  packed: number
  packs: number
  packSize: number
  prunePackable: number
  garbage: number
  garbageSize: number
  /** loose + packs + garbage, in bytes. */
  size: number
  autoLimit: number
  packLimit: number
  /** Why a garbage collection is recommended; '' when it isn't. */
  gcReason: string
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

/** The repository's git user.name and user.email ('' when not set). */
export interface Identity {
  name: string
  email: string
}

export interface FileChange {
  status: string
  path: string
  oldPath?: string
  submodule?: boolean
}

export interface BlameBlock {
  hash: string
  short: string
  author: string
  email: string
  date: string
  summary: string
  filename: string
  start: number
  count: number
  previous?: string
  prevPath?: string
  boundary?: boolean
  uncommitted?: boolean
}

export interface Blame {
  path: string
  rev: string
  startLine: number
  lines: string[]
  blocks: BlameBlock[]
  truncated: boolean
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
  provider?: string
  model?: string
  at?: string
  usage?: AIUsage
  /** The tool-call limit this answer reached; absent when it ended on its own. */
  limited?: number
  /** A limited answer from "Resolve with AI" (Continue resolves again). */
  resolve?: boolean
}

export interface AIUsage { input: number; output: number; cacheRead?: number; cacheWrite?: number }

export type ProviderName = 'ollama' | 'openai' | 'anthropic'

export interface ProviderStatus {
  provider: string
  hasKey: boolean
  keyHint: string
  error?: string
}

export type CommitMessageMode = 'auto-local' | 'auto' | 'manual'
export type SuggestRepliesMode = 'auto-local' | 'auto' | 'off'

export interface AISettings {
  ollamaURL: string
  chatProvider: ProviderName
  chatModel: string
  taskProvider: ProviderName
  taskModel: string
  commitMessage: CommitMessageMode
  suggestReplies: SuggestRepliesMode
  /** Tool calls a chat answer, and a "Resolve with AI" run, may make (1–500). */
  chatToolLimit: number
  resolveToolLimit: number
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

export interface ChatStartEvent { repoID: string; runID: string; text: string; provider: string; model: string }
export interface ChatDeltaEvent { repoID: string; runID: string; text: string }
export interface ChatToolEvent { repoID: string; runID: string; id?: string; name: string; args: Record<string, unknown> | null }
export interface ChatChoiceEvent { repoID: string; callID: string; summary: string }
export interface ChatToolResultEvent { repoID: string; runID: string; name: string; summary: string }
export interface ChatNoticeEvent { repoID: string; runID: string; text: string }
export interface ChatSuggestionsEvent { repoID: string; runID: string; replies: string[] }
export interface ChatDoneEvent { repoID: string; runID: string; at?: string; limited?: number; resolve?: boolean }
export interface ChatUsageEvent { repoID: string; runID: string; usage: AIUsage }
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

/** A configured remote (Go ops.Remote); Remote is the refs' remote with its branches. */
export interface RemoteConfig { name: string; fetchURL: string; pushURL: string }
/** How a connection test ended (Go ops.RemoteTest). */
export interface RemoteTest { ok: boolean; message: string }

/** One background fetch (Go ops.AutoFetchResult). */
export interface AutoFetchResult {
  skipped: boolean
  branch: string
  upstream: string
  newCommits: number
  refsChanged: boolean
  authFailed: string[] | null
}

export const PULL_UP_TO_DATE = 0
export const PULL_MERGED = 1
export const PULL_REBASED = 2
export const PULL_CONFLICTED = 3

export interface PullResult {
  outcome: number
  conflicts: string[]
  /** The remote branch the pull first made the current branch track ("origin/x"); '' when it had an upstream. */
  tracked?: string
}

export interface GitSettings {
  pullStrategy: 'auto' | 'merge' | 'rebase'
}

/** One branch's outcome of a push of the main branches (Go's ops.BranchPushResult). */
export interface BranchPushResult {
  branch: string
  /** <remote>/<branch> */
  target: string
  status: 'pushed' | 'upToDate' | 'rejected' | 'failed'
  reason?: string
  /** The single "Tags" row of a push that also sent the tags. */
  tags?: boolean
}

/** One branch of the Push dialog: local branch, remote branch name, set upstream. */
export interface PushSpec {
  local: string
  remote: string
  track: boolean
}

/** What the Push dialog sends (Go's ops.PushRequest). */
export interface PushRequest {
  remote: string
  branches: PushSpec[]
  tags: boolean
  force: boolean
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
  /** where each conflict region sits in text, by line (backend-parsed) */
  regions: Region[]
  /** Restart file can put it back as git first wrote it */
  restartable: boolean
}

/** One conflict region of a ConflictFile: lines start..end (exclusive). */
export interface Region {
  id: string
  start: number
  end: number
  /** the ||||||| line, -1 when there is no ancestor section */
  baseAt: number
  /** the ======= line */
  sep: number
}

export interface RegionResult {
  left: number
  staged: boolean
  // A decision card whose region was already resolved another way.
  settled?: boolean
}

export type RegionChoice = 'ours' | 'theirs' | 'both' | 'text'

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

export type CommandOrigin = 'you' | 'ai' | 'auto'
/** One git command the app ran, as the Commands panel lists it. */
export interface CommandEntry {
  id: number
  /** The backend's key for this repository (cmdlog.RepoKey — a cleaned
   *  path), not necessarily identical to Repo.path; match it against the
   *  `repo` a CommandLog call returned, not against Repo.path directly. */
  repo: string
  /** The sidebar repository id the command ran in; '' outside every listed
   *  repository (e.g. a clone's destination). */
  repoId: string
  args: string[]
  origin: CommandOrigin
  kind: 'read' | 'write'
  /** ISO timestamp. */
  start: string
  durationMs: number
  exitCode: number
  /** 'running' until a write ends; reads are only sent once they end. */
  outcome: 'running' | 'ok' | 'failed' | 'timeout' | 'cancelled'
  outputTruncated: boolean
  outputDropped: boolean
}
export interface CommandOutput { stdout: string; stderr: string }
/** CommandLog's result: the key the backend logs this repository's
 *  commands under, and the commands themselves (never null). */
export interface CommandLogView { repo: string; entries: CommandEntry[] }

/** One changed file's diff for the pane (app.WorktreeDiff). `hash` identifies
 *  the full diff and goes back with a hunk or line action. */
export interface WorktreeDiff {
  text: string
  hash: string
  truncated: boolean
  patchable: boolean
}

/** Lines of one hunk, by index among its body lines; empty = the whole hunk. */
export interface HunkPick {
  hunk: number
  lines: number[]
}

export type HunkAction = 'stage' | 'unstage' | 'discard'

export type FlowType = 'feature' | 'release' | 'hotfix' | 'warmfix'

export interface FlowPrefixes { feature: string; release: string; hotfix: string; warmfix: string }

/** A local branch whose name starts with a git-flow prefix. */
export interface FlowBranch {
  name: string
  type: FlowType
  short: string
  /** gitflow.branch.<name>.base, '' when not recorded */
  base: string
  /** A finish already merged it into at least one target. */
  inProgress: boolean
}

export interface Flow {
  initialized: boolean
  /** Set when the config names a master/develop that does not exist. */
  problem: string
  master: string
  develop: string
  prefixes: FlowPrefixes
  current: FlowBranch | null
  branches: FlowBranch[]
  releases: string[]
}

/** The Git-flow tab of Repository settings: the repository's branch roles,
 *  or the app's defaults when git-flow is not set up. */
export interface FlowSettings {
  initialized: boolean
  /** Set when the configured production or development branch does not exist. */
  problem: string
  master: string
  develop: string
  prefixes: FlowPrefixes
}

export interface FlowConfig { master: string; develop: string; prefixes: FlowPrefixes }
export interface FlowStartResult { branch: string; notes: string[] }
export interface FlowStep { target: string; done: boolean }
export interface FlowPlan { branch: string; type: FlowType; steps: FlowStep[]; ending: string }
export interface FlowFinishResult {
  outcome: 'finished' | 'conflicted'
  target: string
  conflicts: string[]
  merged: string[]
  notes: string[]
}

export interface RepoAIOverride {
  aiOff?: boolean
  chatProvider?: ProviderName | ''
  chatModel?: string
  taskProvider?: ProviderName | ''
  taskModel?: string
  commitMessage?: string
  suggestReplies?: string
  instructions?: Record<string, string>
}
export interface RepoInstructionFile { name: string; text: string; ignored?: string }
export interface RepoInstructions { files: RepoInstructionFile[]; hash: string; error?: string }
export type RepoAIState = 'none' | 'approved' | 'pending' | 'ignored' | 'changed'
export interface RepoAIInfo {
  effective: AISettings
  global: AISettings
  overrides: RepoAIOverride
  aiOff: boolean
  repoInstructions: RepoInstructions
  state: RepoAIState
  error?: string
}

export interface CloneProgress { phase: string; percent: number; detail: string }
export interface CloneStatus { running: boolean; url: string; dest: string; progress: CloneProgress | null; lastError: string }
export interface CloneDone { repo: Repo | null; error: string; cancelled: boolean }

/** A repository directly inside the scanned folder (Add from folder…). */
export interface ScanCandidate {
  name: string
  path: string
  /** Already in the sidebar. */
  added: boolean
}

export interface ScanResult {
  /** The scanned folder, ~ expanded. */
  parent: string
  repos: ScanCandidate[]
  /** Non-hidden children that are not repositories. */
  skipped: number
}

export interface AddFailure {
  path: string
  name: string
  reason: string
}

export interface AddReposResult {
  added: Repo[]
  failed: AddFailure[]
}

// Bitbucket / GitHub integration: mirrors internal/hosting and internal/app/hosting.go.

export type HostName = 'bitbucket' | 'github'
/** A commit's pipeline or checks state; 'none' draws nothing. */
export type HostState = 'success' | 'failed' | 'running' | 'stopped' | 'none'
export type PRFilter = 'open' | 'mine' | 'review'

/** What the app knows about a repository's code host. Empty (supported false) off Bitbucket and GitHub. */
export interface HostingInfo {
  supported: boolean
  host: HostName
  owner: string
  name: string
  /** The git remote it was found through. */
  remote: string
  webURL: string
  pullsURL: string
  /** "Pipelines" on Bitbucket, "Checks" on GitHub. */
  noun: 'Pipelines' | 'Checks'
  account: 'none' | 'connected' | 'rejected'
}

/** One host's account, for Settings → Accounts. */
export interface HostingAccount {
  host: HostName
  connected: boolean
  rejected: boolean
  /** Bitbucket display name or GitHub login. */
  name: string
  /** Bitbucket only. */
  email: string
  /** GitHub: 'gh' | 'token'; Bitbucket: 'token'; '' when not connected. */
  source: 'gh' | 'token' | ''
  ghAvailable: boolean
}

/** One pipeline or check run of a commit. Times are RFC 3339. */
export interface Check {
  key: string
  name: string
  state: HostState
  url: string
  /** Left out when the host did not give a time. */
  started?: string
  durationSec: number
  created?: string
}

export interface CommitStatus {
  hash: string
  state: HostState
  checks: Check[] | null
}

/** One row of the pull request list. */
export interface PullRequest {
  number: number
  title: string
  draft: boolean
  author: string
  authorID: string
  source: string
  target: string
  headHash: string
  url: string
  fork: boolean
  approvals: number
  commentCount: number
  updated: string
  /** The head commit's aggregate; '' when unknown. */
  state: HostState | ''
}

export interface PullRequestList {
  items: PullRequest[]
  /** The host has more than the 100 shown. */
  more: boolean
}

export interface Reviewer {
  name: string
  verdict: 'approved' | 'changes' | 'pending'
}

export interface Comment {
  id: string
  parent: string
  author: string
  created: string
  body: string
  /** Empty for a general comment. */
  path: string
  line: number
  verdict: string
}

export interface PRCommit {
  hash: string
  short: string
  subject: string
  author: string
}

export interface PullRequestDetail extends PullRequest {
  description: string
  created: string
  reviewers: Reviewer[] | null
  commits: PRCommit[] | null
  comments: Comment[] | null
  commentsMore: boolean
  checks: CommitStatus
}

export interface PRFile {
  path: string
  oldPath: string
  status: string
  patch: string
  /** The host gave no patch for this file. */
  noDiff: boolean
}

/** Go's hosting.Count: a number that is a lower bound when more is set. */
export interface HostCount {
  n: number
  more: boolean
}

/** Payload of the hosting:status event: a watched push changed state. */
export interface WatchEvent {
  repoID: string
  branch: string
  hash: string
  state: HostState
  final: boolean
  noun: 'Pipelines' | 'Checks'
}

/** Payload of the hosting:limit event: a host's request limit started, or ended (until null). */
export interface HostingLimit {
  host: HostName
  until: string | null
  /** The host gave the time (GitHub's reset); false means a guess. */
  known: boolean
}

/** Result of CheckForUpdate: this version, the newest release and whether it can be installed in place. */
export interface UpdateInfo {
  current: string
  latest: string
  newer: boolean
  pageURL: string
  canInstall: boolean
  /** Why it cannot be installed in place; '' when it can. */
  reason: string
}

/** Payload of the update:progress event (total is -1 when unknown). */
export interface UpdateProgress {
  phase: 'download' | 'install'
  done: number
  total: number
}
