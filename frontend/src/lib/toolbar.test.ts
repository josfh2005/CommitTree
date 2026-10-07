import { describe, expect, it } from 'vitest'
import { mergeCandidates, toolbarItems, type ToolbarInput } from './toolbar'
import type { MergeState, Refs, WorktreeState } from './types'

const refs = (over: Partial<Refs> = {}): Refs => ({ head: 'main', headHash: 'abc123', detached: false, local: [], remotes: [], tags: [], ...over })
const clean: WorktreeState = { staged: [], unstaged: [], untracked: [], merging: false }
const dirty: WorktreeState = { staged: [], unstaged: [{ path: 'a.txt', status: 'M' }], untracked: [], merging: false }
const conflict = (kind: MergeState['kind']): MergeState => ({ kind, merging: true, from: 'x', into: 'main', conflicts: ['a.txt'], manual: [], staged: [], unstaged: [] })
const input = (over: Partial<ToolbarInput> = {}): ToolbarInput => ({
  refs: refs(), worktree: clean, merge: null, busy: '', remote: null, terminalOpen: false, commandsOpen: false, chatOpen: true, platform: 'darwin', paused: [], ...over,
})
const item = (over: Partial<ToolbarInput>, id: string) => toolbarItems(input(over)).find((i) => i.id === id)!

describe('toolbarItems', () => {
  it('disables Flow while busy or during a conflict', () => {
    expect(item({}, 'flow')).toMatchObject({ enabled: true, label: 'Flow', title: 'git-flow: start or finish a feature, release, hotfix or warmfix' })
    expect(item({ busy: 'Pushing…' }, 'flow')).toMatchObject({ enabled: false, title: 'Pushing…' })
    expect(item({ merge: conflict('merge') }, 'flow')).toMatchObject({ enabled: false, title: 'Resolve the conflict first' })
  })

  it('marks Fetch with a dot and says why while a remote is paused', () => {
    expect(item({}, 'fetch')).toMatchObject({ dot: false, title: 'Fetch from all remotes' })
    expect(item({ paused: ['origin'] }, 'fetch')).toMatchObject({
      enabled: true, dot: true, title: 'Background fetch paused for origin: authentication failed. Fetch to retry.',
    })
    expect(item({ paused: ['origin'], busy: 'Pushing…' }, 'fetch')).toMatchObject({ enabled: false, dot: true, title: 'Pushing…' })
    expect(item({ paused: ['origin'] }, 'pull').dot).toBe(false)
  })

  it('lists the twelve buttons in their groups and order', () => {
    expect(toolbarItems(input()).map((i) => `${i.group}:${i.id}`)).toEqual([
      'work:commit', 'work:stash', 'sync:fetch', 'sync:pull', 'sync:push', 'refs:branch', 'refs:merge', 'refs:flow', 'tools:terminal', 'tools:commands', 'tools:folder', 'tools:chat',
    ])
  })

  it('disables Commit and Stash with nothing changed, enables them with changes', () => {
    expect(item({}, 'commit')).toMatchObject({ enabled: false, title: 'Nothing to commit' })
    expect(item({}, 'stash')).toMatchObject({ enabled: false, title: 'Nothing to stash' })
    expect(item({ worktree: dirty }, 'commit').enabled).toBe(true)
    expect(item({ worktree: dirty }, 'stash').enabled).toBe(true)
  })

  it('blocks writes during any conflict, including a dismissed stash conflict', () => {
    for (const kind of ['merge', 'rebase', 'stash'] as const) {
      const over = { worktree: dirty, merge: conflict(kind) }
      for (const id of ['commit', 'stash', 'pull', 'push', 'merge']) {
        expect(item(over, id), `${kind}/${id}`).toMatchObject({ enabled: false, title: 'Resolve the conflict first' })
      }
      expect(item(over, 'fetch').enabled).toBe(true)
      expect(item(over, 'branch').enabled).toBe(true)
    }
  })

  it('shows the busy label as the reason while an operation runs', () => {
    for (const id of ['commit', 'stash', 'fetch', 'pull', 'push', 'branch', 'merge']) {
      expect(item({ worktree: dirty, busy: 'Pushing…' }, id), id).toMatchObject({ enabled: false, title: 'Pushing…' })
    }
    for (const id of ['terminal', 'commands', 'folder', 'chat']) expect(item({ busy: 'Pushing…' }, id).enabled).toBe(true)
  })

  it('disables Merge on a detached HEAD but keeps Branch', () => {
    const over = { refs: refs({ detached: true, head: 'abc1234' }) }
    expect(item(over, 'merge')).toMatchObject({ enabled: false, title: 'Check out a branch first' })
    expect(item(over, 'branch').enabled).toBe(true)
  })

  it('badges Pull and Push with behind and ahead', () => {
    const over = { remote: { ahead: 1, behind: 2 } }
    expect(item(over, 'pull').badge).toBe(2)
    expect(item(over, 'push').badge).toBe(1)
    expect(item({}, 'pull').badge).toBe(0)
  })

  it('marks Terminal and Chat active while open and says what a click does', () => {
    expect(item({ terminalOpen: true }, 'terminal')).toMatchObject({ active: true, title: 'Hide terminal (⌘J or Ctrl+`)' })
    expect(item({ terminalOpen: false, platform: 'linux' }, 'terminal')).toMatchObject({ active: false, title: 'Show terminal (Ctrl+J or Ctrl+`)' })
    expect(item({ chatOpen: true }, 'chat')).toMatchObject({ active: true, title: 'Hide chat' })
    expect(item({ chatOpen: false }, 'chat')).toMatchObject({ active: false, title: 'Show chat' })
  })

  it('has a Commands toggle after Terminal', () => {
    const ids = toolbarItems(input()).map((i) => i.id)
    expect(ids.indexOf('commands')).toBe(ids.indexOf('terminal') + 1)
    expect(item({ commandsOpen: true }, 'commands')).toMatchObject({ active: true, label: 'Commands', title: 'Hide git commands (⌘⇧J)' })
    expect(item({ commandsOpen: false, platform: 'linux' }, 'commands')).toMatchObject({ active: false, title: 'Show git commands (Ctrl+Shift+J)' })
  })

  it('names the folder button after the platform', () => {
    expect(item({ platform: 'darwin' }, 'folder')).toMatchObject({ label: 'Finder', title: 'Show in Finder' })
    expect(item({ platform: 'linux' }, 'folder').label).toBe('Folder')
  })
})

describe('mergeCandidates', () => {
  const b = (name: string, remote = '', current = false) => ({ name, remote, hash: 'h', current, upstream: '' })
  it('offers local branches then remote ones, never the current branch or a remote HEAD', () => {
    const r = refs({
      local: [b('main', '', true), b('feature/login'), b('fix/typo')],
      remotes: [{ name: 'origin', branches: [b('HEAD', 'origin'), b('main', 'origin'), b('develop', 'origin')] }],
    })
    expect(mergeCandidates(r).map((c) => `${c.group}:${c.label}`)).toEqual([
      'Local:feature/login', 'Local:fix/typo', 'Remote:origin/main', 'Remote:origin/develop',
    ])
    expect(mergeCandidates(r)[2].branch).toMatchObject({ name: 'main', remote: 'origin' })
  })
  it('is empty without refs', () => {
    expect(mergeCandidates(null)).toEqual([])
  })
  it('lists the branches on the selected commit first, once', () => {
    const at = (name: string, remote: string, hash: string, current = false) => ({ name, remote, hash, current, upstream: '' })
    const r = refs({
      local: [at('main', '', 'm', true), at('master', '', 'old'), at('dev', '', 'new')],
      remotes: [{ name: 'origin', branches: [at('HEAD', 'origin', 'new'), at('master', 'origin', 'new'), at('dev', 'origin', 'x')] }],
    })
    expect(mergeCandidates(r, 'new').map((c) => `${c.group}:${c.label}`)).toEqual([
      'On selected commit:dev', 'On selected commit:origin/master', 'Local:master', 'Remote:origin/dev',
    ])
    expect(mergeCandidates(r, 'm').map((c) => c.group)).not.toContain('On selected commit')
  })
})

describe('toolbarItems, follow-ups', () => {
  const b = (name: string, current = false) => ({ name, remote: '', hash: 'h', current, upstream: '' })
  it('disables Merge when there is no other branch to merge', () => {
    expect(item({ refs: refs({ local: [b('main', true)] }) }, 'merge')).toMatchObject({ enabled: false, title: 'No other branches' })
    expect(item({ refs: refs({ local: [b('main', true), b('dev')] }) }, 'merge').enabled).toBe(true)
  })

  it('asks to finish the operation once its conflicts are all resolved', () => {
    const done = (kind: MergeState['kind']): MergeState => ({ ...conflict(kind), conflicts: [] })
    expect(item({ worktree: dirty, merge: done('merge') }, 'commit').title).toBe('Finish the merge first')
    expect(item({ merge: done('rebase') }, 'push').title).toBe('Finish the rebase first')
    expect(item({ merge: done('am') }, 'pull').title).toBe('Finish the patch first')
    expect(item({ worktree: dirty, merge: done('stash') }, 'stash').title).toBe('Resolve the conflict first')
  })
})

it('the Push tooltip says what a click does', () => {
  expect(item({ pushScope: 'current' }, 'push').title).toBe('Push main')
  expect(item({ pushScope: 'all' }, 'push').title).toBe('Push all branches')
  expect(item({}, 'push').title).toBe('Push — asks current or all branches')
  expect(item({ busy: 'Pushing branches…', pushScope: 'all' }, 'push').title).toBe('Pushing branches…')
})
