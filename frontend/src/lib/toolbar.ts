import { commandsShortcutLabel } from './cmdlog'
import { revealLabel } from './platform'
import { terminalShortcutLabel } from './terminal'
import type { PickItem } from './pick'
import { pushTitle } from './push'
import type { AheadBehind, Branch, MergeState, PushScope, Refs, WorktreeState } from './types'
import { uncommittedCount } from './uncommitted'

export type ToolbarId = 'commit' | 'stash' | 'fetch' | 'pull' | 'push' | 'branch' | 'merge' | 'flow' | 'terminal' | 'commands' | 'folder' | 'chat'
export type ToolbarGroup = 'work' | 'sync' | 'refs' | 'tools'

export interface ToolbarInput {
  refs: Refs | null
  worktree: WorktreeState | null
  merge: MergeState | null
  busy: string
  remote: AheadBehind | null
  terminalOpen: boolean
  commandsOpen: boolean
  chatOpen: boolean
  platform: string
  /** Remotes background fetches skip for want of credentials. */
  paused: string[]
  /** Settings → General → Push; the Push tooltip says what a click does. */
  pushScope?: PushScope
}

// title is the tooltip: why the button is disabled, or what it does.
export interface ToolbarItem { id: ToolbarId; label: string; icon: string; group: ToolbarGroup; enabled: boolean; title: string; active: boolean; badge: number; dot: boolean }

const CONFLICT = 'Resolve the conflict first'

/** The toolbar Fetch's tooltip while background fetches skip remotes that
 *  failed for want of credentials; '' when none is paused. */
export function pausedTooltip(remotes: string[]): string {
  if (remotes.length === 0) return ''
  return `Background fetch paused for ${remotes.join(', ')}: authentication failed. Fetch to retry.`
}

// What is left to finish once every conflict is resolved; a stash conflict
// has nothing to finish, only files to resolve.
const FINISH: Partial<Record<MergeState['kind'], string>> = {
  merge: 'Finish the merge first',
  rebase: 'Finish the rebase first',
  'cherry-pick': 'Finish the cherry-pick first',
  revert: 'Finish the revert first',
  am: 'Finish the patch first',
}

/** toolbarItems is the repository toolbar: every button with whether it can
 *  run now and, when not, why. A conflict of any kind blocks what writes to
 *  the working tree or moves the branch; Fetch and Branch stay available. */
export function toolbarItems(i: ToolbarInput): ToolbarItem[] {
  const conflict = !!i.merge?.merging
  const blocked = (i.merge && !i.merge.conflicts.length && FINISH[i.merge.kind]) || CONFLICT
  const changes = uncommittedCount(i.worktree) > 0
  // first returns the first reason that applies, or '' when none does.
  const first = (...rules: [boolean, string][]) => rules.find(([when]) => when)?.[1] ?? ''
  const item = (id: ToolbarId, label: string, icon: string, group: ToolbarGroup, reason: string, title: string, extra: Partial<ToolbarItem> = {}): ToolbarItem =>
    ({ id, label, icon, group, enabled: reason === '', title: reason || title, active: false, badge: 0, dot: false, ...extra })
  const shortcut = `${terminalShortcutLabel(i.platform)} or Ctrl+\``
  return [
    item('commit', 'Commit', 'commit', 'work', first([!!i.busy, i.busy], [conflict, blocked], [!changes, 'Nothing to commit']), 'Commit the changes'),
    item('stash', 'Stash', 'stash', 'work', first([!!i.busy, i.busy], [conflict, blocked], [!changes, 'Nothing to stash']), 'Stash the changes'),
    item('fetch', 'Fetch', 'refresh', 'sync', first([!!i.busy, i.busy]), pausedTooltip(i.paused) || 'Fetch from all remotes', { dot: i.paused.length > 0 }),
    item('pull', 'Pull', 'download', 'sync', first([!!i.busy, i.busy], [conflict, blocked]), 'Pull', { badge: i.remote?.behind ?? 0 }),
    item('push', 'Push', 'upload', 'sync', first([!!i.busy, i.busy], [conflict, blocked]), pushTitle(i.pushScope ?? 'ask', i.refs), { badge: i.remote?.ahead ?? 0 }),
    item('branch', 'Branch', 'branch', 'refs', first([!!i.busy, i.busy]), 'New branch from HEAD'),
    item('merge', 'Merge', 'merge', 'refs', first([!!i.busy, i.busy], [conflict, blocked], [!!i.refs?.detached, 'Check out a branch first'], [mergeCandidates(i.refs).length === 0, 'No other branches']), 'Merge a branch into the current one'),
    item('flow', 'Flow', 'flow', 'refs', first([!!i.busy, i.busy], [conflict, blocked]), 'git-flow: start or finish a feature, release, hotfix or warmfix'),
    item('terminal', 'Terminal', 'terminal', 'tools', '', `${i.terminalOpen ? 'Hide' : 'Show'} terminal (${shortcut})`, { active: i.terminalOpen }),
    item('commands', 'Commands', 'list', 'tools', '', `${i.commandsOpen ? 'Hide' : 'Show'} git commands (${commandsShortcutLabel(i.platform)})`, { active: i.commandsOpen }),
    item('folder', i.platform === 'darwin' ? 'Finder' : 'Folder', 'folder', 'tools', '', revealLabel(i.platform)),
    item('chat', 'Chat', 'chat', 'tools', '', i.chatOpen ? 'Hide chat' : 'Show chat', { active: i.chatOpen }),
  ]
}

export interface MergeCandidate extends PickItem { group: 'On selected commit' | 'Local' | 'Remote'; branch: Branch }

/** mergeCandidates is what the Merge picker offers: the branches on the
 *  selected commit first (so the one picked in the log is at hand), then the
 *  other local branches, then remote-tracking ones as remote/name, leaving
 *  out the current branch and a remote's HEAD symref. */
export function mergeCandidates(refs: Refs | null, selectedHash = ''): MergeCandidate[] {
  if (!refs) return []
  const local = refs.local.filter((b) => !b.current).map((b): MergeCandidate => ({ key: b.name, label: b.name, group: 'Local', branch: b }))
  const remote = refs.remotes.flatMap((r) =>
    r.branches.filter((b) => b.name !== 'HEAD').map((b): MergeCandidate => ({ key: `${r.name}/${b.name}`, label: `${r.name}/${b.name}`, group: 'Remote', branch: b })),
  )
  const all = [...local, ...remote]
  const onSelected = (c: MergeCandidate) => selectedHash !== '' && c.branch.hash === selectedHash
  return [...all.filter(onSelected).map((c): MergeCandidate => ({ ...c, group: 'On selected commit' })), ...all.filter((c) => !onSelected(c))]
}
