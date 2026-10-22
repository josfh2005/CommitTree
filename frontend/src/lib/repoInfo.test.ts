import { describe, expect, it } from 'vitest'
import { garbageText, gcDoneMessage, infoLine, objectsText, statusText } from './repoInfo'
import type { RepoInfo } from './types'

const info = (over: Partial<RepoInfo> = {}): RepoInfo => ({
  loose: 0, looseSize: 0, packed: 6343, packs: 4, packSize: 10_265_600, prunePackable: 0,
  garbage: 0, garbageSize: 0, size: 10_265_600, autoLimit: 6700, packLimit: 50, gcReason: '', ...over,
})

describe('repo info texts', () => {
  it('sums up size and objects for the header line', () => {
    expect(infoLine(info())).toBe('9.8 MB · 6,343 objects')
    expect(infoLine(info({ loose: 12, packed: 0, size: 2048 }))).toBe('2 KB · 12 objects')
    expect(infoLine(info({ loose: 1, packed: 0, size: 512 }))).toBe('512 B · 1 object')
  })

  it('details packed and loose objects and packs', () => {
    expect(objectsText(info())).toBe('6,343 packed · 0 loose · 4 packs')
    expect(objectsText(info({ packs: 1 }))).toBe('6,343 packed · 0 loose · 1 pack')
  })

  it('says none, or how many garbage files and how big', () => {
    expect(garbageText(info())).toBe('none')
    expect(garbageText(info({ garbage: 2, garbageSize: 4096 }))).toBe('2 files · 4 KB')
  })

  it("gives git's reason or says no GC is needed", () => {
    expect(statusText(info())).toBe('No GC needed')
    expect(statusText(info({ gcReason: '53 packs (git suggests GC above 50)' }))).toBe('53 packs (git suggests GC above 50)')
  })

  it('reports what a GC changed', () => {
    expect(gcDoneMessage(info({ size: 10_265_600 }), info({ size: 7_444_889 }))).toBe('Garbage collected: 9.8 MB → 7.1 MB.')
    expect(gcDoneMessage(info(), info())).toBe('Garbage collected: 9.8 MB, nothing to reclaim.')
  })
})
