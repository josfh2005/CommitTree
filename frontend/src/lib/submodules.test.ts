import { describe, expect, it } from 'vitest'
import { markers, movedMessage, nextSelection, parseSubmoduleDiff, rowLabel, splitPath, submoduleRepoId, updateMessage } from './submodules'
import type { Repo, Submodule } from './types'

const base: Submodule = { name: 'lib', path: 'vendor/lib', url: 'u', recorded: 'a'.repeat(40), checkedOut: 'a'.repeat(40), branch: '', initialised: true, configured: true, moved: false, modified: false, untracked: false, conflict: false }

describe('markers', () => {
  it('is empty in sync', () => expect(markers(base)).toEqual([]))
  it('moved shows the checked-out short hash', () =>
    expect(markers({ ...base, moved: true, checkedOut: 'b'.repeat(40) })).toEqual([{ kind: 'moved', text: '↕ bbbbbbb' }]))
  it('modified wins over untracked-only', () =>
    expect(markers({ ...base, modified: true, untracked: true }).map((m) => m.kind)).toEqual(['modified']))
  it('untracked only', () => expect(markers({ ...base, untracked: true })).toEqual([{ kind: 'untracked', text: '○' }]))
  it('moved and modified combine', () =>
    expect(markers({ ...base, moved: true, modified: true }).map((m) => m.kind)).toEqual(['moved', 'modified']))
  it('conflict', () => expect(markers({ ...base, conflict: true })).toEqual([{ kind: 'conflict', text: '!' }]))
})

describe('rowLabel', () => {
  it('uninitialised', () => expect(rowLabel({ ...base, initialised: false, checkedOut: '' })).toBe('not initialised'))
  it('not configured wins', () => expect(rowLabel({ ...base, configured: false, initialised: false })).toBe('not configured'))
})

it('splitPath', () => {
  expect(splitPath('vendor/lib/zlib')).toEqual({ dir: 'vendor/lib/', leaf: 'zlib' })
  expect(splitPath('lib')).toEqual({ dir: '', leaf: 'lib' })
})

describe('parseSubmoduleDiff', () => {
  it('forward with commits', () =>
    expect(parseSubmoduleDiff('Submodule third party/lib 1234567..89abcde:\n  > Fix overflow\n  > Bump version\n')).toEqual({
      path: 'third party/lib', from: '1234567', to: '89abcde', note: '', content: [],
      commits: [{ dir: '>', subject: 'Fix overflow' }, { dir: '>', subject: 'Bump version' }],
    }))
  it('rewind', () => expect(parseSubmoduleDiff('Submodule lib 89abcde...1234567 (rewind):\n  < Newer\n')?.commits).toEqual([{ dir: '<', subject: 'Newer' }]))
  it('commits not present', () =>
    expect(parseSubmoduleDiff('Submodule lib 1234567..89abcde (commits not present)\n')).toMatchObject({ from: '1234567', to: '89abcde', note: 'commits not present', commits: [] }))
  it('new submodule', () => expect(parseSubmoduleDiff('Submodule lib 0000000...1234567 (new submodule)\n')?.note).toBe('new submodule'))
  it('content only', () =>
    expect(parseSubmoduleDiff('Submodule lib contains modified content\nSubmodule lib contains untracked content\n')).toMatchObject({ from: '', to: '', content: ['modified', 'untracked'] }))
  it('ordinary diff is null', () => expect(parseSubmoduleDiff('diff --git a/x b/x\n')).toBeNull())
})

it('updateMessage names the commit left and the branch', () => {
  const s = { ...base, moved: true, checkedOut: 'b'.repeat(40), branch: 'main' }
  expect(updateMessage(s)).toBe('vendor/lib will leave bbbbbbb and check out the recorded commit aaaaaaa on a detached HEAD. Branch main itself is not changed.')
})

describe('submoduleRepoId', () => {
  const top: Repo = { id: 't', name: 't', path: '/t', missing: false, branch: 'main' }
  const lib: Repo = { id: 's', name: 'lib', path: '/t/lib', missing: false, branch: 'main', submodule: true, parentId: 't', subPath: 'lib' }
  // A nested submodule's subPath is relative to the TOP repository ('lib/inner'),
  // not to its immediate parent ('s') — the reason submoduleRepoId matches by
  // absolute path instead of parentId/subPath.
  const inner: Repo = { id: 'n', name: 'inner', path: '/t/lib/inner', missing: false, branch: 'main', submodule: true, parentId: 's', subPath: 'lib/inner' }
  const list = [top, lib, inner]

  it('resolves a top-level submodule by its path relative to the top repository', () =>
    expect(submoduleRepoId(list, 't', 'lib')).toBe('s'))
  it('resolves a nested submodule when the current repo is itself an opened submodule', () =>
    expect(submoduleRepoId(list, 's', 'inner')).toBe('n'))
  it('returns undefined when nothing matches', () => {
    expect(submoduleRepoId(list, 't', 'missing')).toBeUndefined()
    expect(submoduleRepoId(list, 'unknown', 'lib')).toBeUndefined()
  })
})

describe('movedMessage', () => {
  it('singular', () => expect(movedMessage(1)).toBe('1 submodule is not at the recorded commit'))
  it('plural', () => expect(movedMessage(3)).toBe('3 submodules are not at the recorded commit'))
})

describe('nextSelection', () => {
  const top: Repo = { id: 't', name: 't', path: '/t', missing: false, branch: 'main' }
  const sub: Repo = { id: 's', name: 'lib', path: '/t/lib', missing: false, branch: 'main', submodule: true, parentId: 't', subPath: 'lib' }
  const worktree: Repo = { id: 'w', name: 'wt', path: '/t/wt', missing: false, branch: 'main', worktree: true, parentId: 't' }
  it('keeps a listed selection', () => expect(nextSelection(sub, [top, sub])).toBeNull())
  it('falls back to the parent of a vanished submodule', () => expect(nextSelection(sub, [top])).toBe('t'))
  it('clears a vanished worktree instead of falling back to its parent', () => expect(nextSelection(worktree, [top])).toBe(''))
  it('clears a vanished repository', () => expect(nextSelection(top, [])).toBe(''))
})
