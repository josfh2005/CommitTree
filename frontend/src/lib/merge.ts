import type { AheadBehind, ConflictKind, MergeState } from './types'
import type { ChoiceOptions } from './ui'

export type MergeFileStatus = 'conflict' | 'manual' | 'unstaged' | 'staged'

export interface MergeFile {
  path: string
  status: MergeFileStatus
}

export interface MergeSection {
  title: string
  files: MergeFile[]
}

/**
 * mergeSections groups the merge's files for the panel, all read from git so
 * they survive a restart: what is still conflicted or needs a human, what is
 * settled but not staged, and what is staged for the merge commit. Empty
 * sections are left out.
 */
export function mergeSections(state: MergeState): MergeSection[] {
  if (!state.merging) return []
  const rows = (paths: string[], status: MergeFileStatus) => paths.map((path) => ({ path, status }))
  return [
    { title: 'Conflicts', files: [...rows(state.conflicts, 'conflict'), ...rows(state.manual, 'manual')] },
    { title: 'Unstaged', files: rows(state.unstaged, 'unstaged') },
    { title: 'Staged', files: rows(state.staged, 'staged') },
  ].filter((s) => s.files.length > 0)
}

/**
 * commitWarning is the question to ask before committing the merge, or null
 * when there is nothing to warn about. An unstaged file keeps our side in the
 * merge commit, silently dropping theirs.
 */
export function commitWarning(state: MergeState): string | null {
  if (state.kind && state.kind !== 'merge') return null
  const n = state.unstaged.length
  if (n === 0) return null
  const files = n === 1 ? `${state.unstaged[0]} is` : `${n} files are`
  return `${files} not staged and won't be in the merge commit, which keeps this branch's version instead. Commit anyway?`
}

/**
 * takeMessage is the confirmation before taking one side of a Manual file.
 * Taking overwrites the worktree copy, and Unstage does not bring it back.
 */
export function takeMessage(path: string, branch: string): string {
  return `Replace ${path} with ${branch}'s version, or delete it if ${branch} deleted it? Edits you made to it are lost; Unstage won't bring them back.`
}

/** sideLabel is a conflict side's name for a menu: the backend's label, or the fallback, kept short. */
export function sideLabel(label: string | undefined, fallback: string): string {
  const s = label || fallback
  return s.length > 40 ? s.slice(0, 39) + '…' : s
}

/**
 * takeLabels is the "Take …" menu wording for a Manual file's two sides.
 * The backend only names sides for merge, rebase and cherry-pick
 * (nameSides): those always have oursLabel/theirsLabel set, and this
 * fallback never runs for them. Revert, am and stash keep the plain
 * "ours"/"theirs" wording instead, decorated with the branch or commit
 * `into`/`from` names when the backend has one — so a revert's "theirs" is
 * still named by the commit being reverted, never mistaken for its content.
 */
export function takeLabels(state: MergeState | undefined | null): { ours: string; theirs: string } {
  const ours = sideLabel(state?.oursLabel, state?.into ? `ours (${state.into})` : 'ours')
  const theirs = sideLabel(state?.theirsLabel, state?.from ? `theirs (${state.from})` : 'theirs')
  return { ours, theirs }
}

export function skipWarning(state: MergeState): { title: string; message: string; confirmLabel: string } {
  const what = state.theirsLabel || 'This commit'
  return { title: 'Skip this commit', message: `${what} will not be applied. Its changes are dropped from the result.`, confirmLabel: 'Skip this commit' }
}

/** isEmptyStepError recognises git refusing to continue a step whose resolution left nothing to commit. */
export function isEmptyStepError(message: string): boolean {
  return /No changes - did you forget|is now empty|nothing to commit/i.test(message)
}

export interface ConflictHeader {
  lead: string
  from: string
  connector: string
  into: string
  detail: string
}

// One table, so the six kinds' wording lives in one place instead of spread
// through MergeView's template.
const HEADINGS: Record<Exclude<ConflictKind, ''>, { lead: string; connector: string }> = {
  merge: { lead: 'Merging', connector: 'into' },
  rebase: { lead: 'Rebasing', connector: 'onto' },
  'cherry-pick': { lead: 'Cherry-picking', connector: 'onto' },
  revert: { lead: 'Reverting', connector: 'on' },
  am: { lead: 'Applying patch', connector: '' },
  stash: { lead: 'Resolving stashed changes', connector: '' },
}

export function conflictHeader(state: MergeState): ConflictHeader {
  const kind = state.kind || 'merge'
  const { lead, connector } = HEADINGS[kind]
  if (kind === 'stash') return { lead, from: '', connector: '', into: '', detail: '' }
  const detail =
    kind === 'rebase' && state.total
      ? `commit ${state.step} of ${state.total}${state.subject ? `: ${state.subject}` : ''}`
      : kind === 'merge'
        ? ''
        : (state.subject ?? '')
  return { lead, from: state.from, connector: state.into ? connector : '', into: state.into, detail }
}

export interface ConflictActions {
  abort: string | null
  confirm: string | null
  ai: boolean
  done: boolean
  skip: boolean
}

// "Resolve with AI" is offered for a merge, a rebase and a cherry-pick:
// ResolveConflicts ties a run to that operation's fingerprint (for a rebase,
// the step being replayed) and refuses every other kind. A stash conflict
// has no git-level abort or continue — it gets Done (and, when a conflicted
// Pop still owes one, Drop stash, which MergeView adds itself from
// OwedStashDrop rather than from this table).
const ACTIONS: Record<Exclude<ConflictKind, ''>, ConflictActions> = {
  merge: { abort: 'Abort merge', confirm: 'Commit merge', ai: true, done: false, skip: false },
  rebase: { abort: 'Abort rebase', confirm: 'Continue rebase', ai: true, done: false, skip: true },
  'cherry-pick': { abort: 'Abort cherry-pick', confirm: 'Continue cherry-pick', ai: true, done: false, skip: true },
  revert: { abort: 'Abort revert', confirm: 'Continue revert', ai: false, done: false, skip: false },
  am: { abort: 'Abort patch', confirm: 'Continue applying', ai: false, done: false, skip: false },
  stash: { abort: null, confirm: null, ai: false, done: true, skip: false },
}

export function conflictActions(state: MergeState): ConflictActions {
  return ACTIONS[state.kind || 'merge']
}

// abortWarning is the confirmation before throwing a resolution away. The
// wording has to follow the kind: "Abort merge / go back to where the
// branch was" is plainly wrong for a rebase or a cherry-pick.
export function abortWarning(state: MergeState): { title: string; message: string; confirmLabel: string } {
  const label = conflictActions(state).abort ?? 'Abort'
  const what = {
    merge: 'this merge',
    rebase: 'this rebase',
    'cherry-pick': 'this cherry-pick',
    revert: 'this revert',
    am: 'this patch',
    stash: 'this',
  }[state.kind || 'merge']
  return {
    title: label,
    message: `Throw away every resolution from ${what} and go back to where the branch was?`,
    confirmLabel: label,
  }
}

const commits = (n: number) => `${n} commit${n === 1 ? '' : 's'}`

/**
 * staleMergeChoice is the merge confirmation for a local branch that is
 * behind its upstream: merging it as it is leaves out the commits only the
 * remote has, which is rarely what was meant. It offers the upstream first,
 * and the branch as it is second.
 */
export function staleMergeChoice(branch: string, upstream: string, into: string, counts: AheadBehind): ChoiceOptions<string> {
  const { ahead, behind } = counts
  const tail = 'A merge commit is always created.'
  return {
    title: 'Merge branch',
    label: 'What to merge',
    options: [
      { value: upstream, label: `Merge ${upstream}` },
      { value: branch, label: `Merge ${branch} as it is` },
    ],
    value: upstream,
    message: (v) => {
      if (v === branch) {
        const newer = behind === 1 ? 'newer commit' : `${behind} newer commits`
        return `Only what your local ${branch} has will be merged into ${into}; the ${newer} on ${upstream} will not. ${tail}`
      }
      const state =
        ahead > 0
          ? `${branch} is ${commits(behind)} behind ${upstream} and ${ahead} ahead of it; merging ${upstream} leaves ${ahead === 1 ? 'that one' : `those ${ahead}`} out.`
          : `${branch} is ${commits(behind)} behind ${upstream} — those commits are only on the remote.`
      return `${state} Merge ${upstream} into ${into}? ${tail}`
    },
    confirmLabel: () => 'Merge',
  }
}
