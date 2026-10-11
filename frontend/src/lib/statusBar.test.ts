import { describe, expect, it } from 'vitest'
import { elapsedText, outcomeIcon, openAction } from './statusBar'
import type { Repo } from './types'

const repos = [{ id: 'a', name: 'alpha', missing: false }, { id: 'm', name: 'gone', missing: true }] as Repo[]

describe('elapsedText', () => {
  it('is empty for the first second, then whole seconds', () => {
    expect(elapsedText(1000, 1999)).toBe('')
    expect(elapsedText(1000, 2000)).toBe('1 s')
    expect(elapsedText(1000, 13_500)).toBe('12 s')
  })
})

describe('outcomeIcon', () => {
  it('maps outcomes to ✓ ✗ ⊘', () => {
    expect(outcomeIcon('ok')).toBe('✓')
    expect(outcomeIcon('failed')).toBe('✗')
    expect(outcomeIcon('cancelled')).toBe('⊘')
  })
})

describe('openAction', () => {
  it('reopens a running clone, but not a finished one', () => {
    expect(openAction({ kind: 'clone', repoId: '' }, true, repos)).toEqual({ kind: 'clone' })
    expect(openAction({ kind: 'clone', repoId: '' }, false, repos)).toBeNull()
  })
  it('opens Settings → Providers for a model download', () => {
    expect(openAction({ kind: 'model', repoId: '' }, false, repos)).toEqual({ kind: 'providers' })
  })
  it('opens the Commands panel of a present repository, nothing otherwise', () => {
    for (const kind of ['op', 'fetch', 'git'] as const) {
      expect(openAction({ kind, repoId: 'a' }, false, repos)).toEqual({ kind: 'commands', repoId: 'a' })
    }
    expect(openAction({ kind: 'op', repoId: 'm' }, false, repos)).toBeNull()
    expect(openAction({ kind: 'op', repoId: 'x' }, false, repos)).toBeNull()
  })
})
