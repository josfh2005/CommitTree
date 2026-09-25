import { describe, expect, it } from 'vitest'
import { toolbarItems, type ToolbarInput } from './toolbar'
import type { MergeState, Refs, WorktreeState } from './types'

const refs = (over: Partial<Refs> = {}): Refs => ({ head: 'main', headHash: 'abc123', detached: false, local: [], remotes: [], tags: [], ...over })
const clean: WorktreeState = { staged: [], unstaged: [], untracked: [], merging: false }
const dirty: WorktreeState = { staged: [], unstaged: [{ path: 'a.txt', status: 'M' }], untracked: [], merging: false }
const conflict = (kind: MergeState['kind']): MergeState => ({ kind, merging: true, from: 'x', into: 'main', conflicts: ['a.txt'], manual: [], staged: [], unstaged: [] })
const input = (over: Partial<ToolbarInput> = {}): ToolbarInput => ({
  refs: refs(), worktree: clean, merge: null, busy: '', remote: null, terminalOpen: false, chatOpen: true, platform: 'darwin', ...over,
})
const item = (over: Partial<ToolbarInput>, id: string) => toolbarItems(input(over)).find((i) => i.id === id)!

describe('toolbarItems', () => {
  it('lists the ten buttons in their groups and order', () => {
    expect(toolbarItems(input()).map((i) => `${i.group}:${i.id}`)).toEqual([
      'work:commit', 'work:stash', 'sync:fetch', 'sync:pull', 'sync:push', 'refs:branch', 'refs:merge', 'tools:terminal', 'tools:folder', 'tools:chat',
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
    for (const id of ['terminal', 'folder', 'chat']) expect(item({ busy: 'Pushing…' }, id).enabled).toBe(true)
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

  it('names the folder button after the platform', () => {
    expect(item({ platform: 'darwin' }, 'folder')).toMatchObject({ label: 'Finder', title: 'Show in Finder' })
    expect(item({ platform: 'linux' }, 'folder').label).toBe('Folder')
  })
})
