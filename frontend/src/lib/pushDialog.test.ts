import { describe, expect, it } from 'vitest'
import type { Branch, Refs } from './types'
import {
  canPush,
  changeRemote,
  counterpart,
  defaultRemote,
  editRemote,
  initialRows,
  mainBranchesSelection,
  pushRequest,
  selectAll,
  setTicked,
  splitUpstream,
  type PushRow,
} from './pushDialog'

const local = (name: string, extra: Partial<Branch> = {}): Branch => ({
  name, remote: '', hash: 'h', current: false, upstream: '', official: false, ...extra,
})
const remoteBranch = (name: string, remote: string): Branch => ({
  name, remote, hash: 'h', current: false, upstream: '', official: false,
})

const refs = (over: Partial<Refs> = {}): Refs => ({
  head: 'main',
  headHash: 'h',
  detached: false,
  local: [
    local('develop', { upstream: 'origin/develop', ahead: 1, official: true }),
    local('feature/x'),
    local('main', { current: true, upstream: 'origin/main', official: true }),
    local('release/1.0', { upstream: 'backup/release/1.0', ahead: 2, official: true }),
  ],
  remotes: [
    { name: 'origin', branches: [remoteBranch('main', 'origin'), remoteBranch('develop', 'origin')] },
    { name: 'backup', branches: [remoteBranch('release/1.0', 'backup')] },
  ],
  tags: [],
  ...over,
})

const row = (over: Partial<PushRow> = {}): PushRow => ({
  local: 'feature/x', remote: '', push: false, track: false, typed: false, ...over,
})

describe('splitUpstream', () => {
  it('uses the longest remote name that prefixes the upstream', () => {
    expect(splitUpstream('origin/feature/x', ['origin', 'or'])).toEqual({ remote: 'origin', branch: 'feature/x' })
    expect(splitUpstream('team/fork/x', ['team', 'team/fork'])).toEqual({ remote: 'team/fork', branch: 'x' })
  })
  it('is null when no remote prefixes it', () => {
    expect(splitUpstream('main', ['origin'])).toBeNull()
    expect(splitUpstream('', ['origin'])).toBeNull()
    expect(splitUpstream('origin', ['origin'])).toBeNull()
  })
})

describe('counterpart', () => {
  const r = refs()
  it('is the upstream branch when the upstream is on that remote', () => {
    expect(counterpart(local('a', { upstream: 'origin/other' }), 'origin', r)).toBe('other')
    expect(counterpart(r.local[3], 'backup', r)).toBe('release/1.0')
  })
  it('is the same-name branch on the remote otherwise', () => {
    expect(counterpart(r.local[0], 'backup', r)).toBe('')
    expect(counterpart(local('main'), 'origin', r)).toBe('main')
  })
  it('is blank without either', () => {
    expect(counterpart(r.local[1], 'origin', r)).toBe('')
    expect(counterpart(r.local[1], 'nope', r)).toBe('')
  })
  it('ignores an upstream that is another local branch', () => {
    expect(counterpart(local('a', { upstream: 'origin/a', upstreamLocal: true }), 'origin', r)).toBe('')
  })
})

describe('defaultRemote', () => {
  const r = refs()
  it('is the first ticked branch upstream remote', () => {
    expect(defaultRemote(r, ['release/1.0'])).toBe('backup')
    expect(defaultRemote(r, ['release/1.0', 'main'])).toBe('backup')
  })
  it('is origin with nothing ticked (detached HEAD) or no upstream', () => {
    expect(defaultRemote(r, [])).toBe('origin')
    expect(defaultRemote({ ...r, detached: true }, [])).toBe('origin')
    expect(defaultRemote(r, ['feature/x'])).toBe('origin')
  })
  it('is the first remote without origin, and blank without remotes', () => {
    expect(defaultRemote({ ...r, remotes: [r.remotes[1]] }, [])).toBe('backup')
    expect(defaultRemote({ ...r, remotes: [] }, [])).toBe('')
  })
})

describe('initialRows', () => {
  const rows = initialRows(refs(), 'origin', ['main'])
  const by = (n: string) => rows.find((x) => x.local === n)!
  it('has a row for every local branch', () => {
    expect(rows.map((x) => x.local)).toEqual(['develop', 'feature/x', 'main', 'release/1.0'])
  })
  it('ticks the asked branches, tracking, with their counterpart', () => {
    expect(by('main')).toEqual({ local: 'main', remote: 'main', push: true, track: true, typed: false })
  })
  it('fills a ticked branch without counterpart with its own name', () => {
    const r = initialRows(refs(), 'origin', ['feature/x'])
    expect(r.find((x) => x.local === 'feature/x')).toMatchObject({ remote: 'feature/x', push: true, track: true })
  })
  it('leaves the others unticked with their counterpart or blank', () => {
    expect(by('feature/x')).toEqual({ local: 'feature/x', remote: '', push: false, track: false, typed: false })
    expect(by('develop')).toMatchObject({ push: false, remote: 'develop', track: false })
    expect(by('release/1.0')).toMatchObject({ push: false, remote: '' })
  })
})

describe('setTicked', () => {
  it('fills a blank remote with the local name and turns track on', () => {
    expect(setTicked(row(), true)).toEqual({ local: 'feature/x', remote: 'feature/x', push: true, track: true, typed: false })
  })
  it('keeps a remote that is already there', () => {
    expect(setTicked(row({ remote: 'x' }), true).remote).toBe('x')
  })
  it('unticks: track off, remote kept', () => {
    const on = setTicked(row(), true)
    expect(setTicked(on, false)).toEqual({ ...on, push: false, track: false })
  })
})

describe('selectAll', () => {
  it('ticks and unticks every row', () => {
    const rows = initialRows(refs(), 'origin', [])
    const all = selectAll(rows, true)
    expect(all.every((x) => x.push && x.track && x.remote !== '')).toBe(true)
    const none = selectAll(all, false)
    expect(none.every((x) => !x.push && !x.track)).toBe(true)
    expect(none.map((x) => x.remote)).toEqual(all.map((x) => x.remote))
  })
})

describe('editRemote', () => {
  it('sets the name and marks the row typed', () => {
    expect(editRemote(row({ push: true }), 'mine')).toMatchObject({ remote: 'mine', typed: true, push: true })
  })
})

describe('changeRemote', () => {
  const r = refs()
  it('recomputes untyped rows from the new remote', () => {
    const rows = changeRemote(initialRows(r, 'origin', []), r, 'backup')
    expect(rows.find((x) => x.local === 'develop')!.remote).toBe('')
    expect(rows.find((x) => x.local === 'release/1.0')!.remote).toBe('release/1.0')
  })
  it('keeps a typed name', () => {
    const rows = changeRemote([editRemote(row(), 'mine')], r, 'backup')
    expect(rows[0]).toMatchObject({ remote: 'mine', typed: true })
  })
  it('keeps a ticked row filled: counterpart, else its own name', () => {
    const rows = changeRemote(initialRows(r, 'origin', ['develop']), r, 'backup')
    expect(rows.find((x) => x.local === 'develop')).toMatchObject({ remote: 'develop', push: true })
  })
})

describe('pushRequest', () => {
  it('sends the ticked rows only, in row order', () => {
    const rows = [
      row({ local: 'a', remote: 'a', push: true, track: true }),
      row({ local: 'b', remote: 'b' }),
      row({ local: 'c', remote: 'mine', push: true, typed: true }),
    ]
    expect(pushRequest('origin', rows, true, false)).toEqual({
      remote: 'origin',
      branches: [{ local: 'a', remote: 'a', track: true }, { local: 'c', remote: 'mine', track: false }],
      tags: true,
      force: false,
    })
  })
})

describe('canPush', () => {
  it('is true with tags even with nothing ticked', () => {
    expect(canPush([], true)).toBe(true)
  })
  it('is false with nothing ticked and no tags', () => {
    expect(canPush([row()], false)).toBe(false)
  })
  it('is true with a ticked row', () => {
    expect(canPush([row({ push: true })], false)).toBe(true)
  })
})

describe('mainBranchesSelection', () => {
  it('picks the remote most main branches track and ticks those on it', () => {
    expect(mainBranchesSelection(refs())).toEqual({ remote: 'origin', ticked: ['main', 'develop'] })
  })
  it('ticks a current branch with no upstream too', () => {
    const r = refs()
    r.local[2] = local('main', { current: true, official: true })
    expect(mainBranchesSelection(r)).toEqual({ remote: 'origin', ticked: ['main', 'develop'] })
  })
  it('breaks a tie in favour of origin, then name order', () => {
    const r = refs()
    r.local[2] = local('main', { current: true, upstream: 'backup/main', official: true })
    r.local[3] = local('release/1.0', { upstream: 'backup/release/1.0', ahead: 0, official: true })
    expect(mainBranchesSelection(r)).toEqual({ remote: 'origin', ticked: ['develop'] })
    r.local[0] = local('develop', { upstream: 'zeta/develop', ahead: 1, official: true })
    r.remotes = [{ name: 'zeta', branches: [] }, { name: 'backup', branches: [] }]
    expect(mainBranchesSelection(r)).toEqual({ remote: 'backup', ticked: ['main'] })
  })
  it('skips a detached HEAD and a current branch tracking a local one', () => {
    const r = refs({ detached: true })
    expect(mainBranchesSelection(r).ticked).toEqual(['develop'])
    const r2 = refs()
    r2.local[2] = local('main', { current: true, upstream: 'dev', upstreamLocal: true, official: true })
    expect(mainBranchesSelection(r2).ticked).toEqual(['develop'])
  })
})
