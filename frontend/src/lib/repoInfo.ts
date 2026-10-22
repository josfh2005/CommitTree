import type { RepoInfo } from './types'

/** Texts for the repository info line and popover in the log header
 *  (docs/spec/01-repositories-and-sidebar.md, "Repository info"). */

const count = (n: number) => n.toLocaleString('en-US')
const plural = (n: number, one: string, many = one + 's') => `${count(n)} ${n === 1 ? one : many}`

/** A size on disk: whole B and KB, one decimal from MB up ("9.8 MB"). */
export function sizeText(n: number): string {
  if (n < 1024) return `${Math.max(0, n)} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let value = n / 1024
  let i = 0
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024
    i++
  }
  return i === 0 ? `${Math.round(value)} KB` : `${value.toFixed(1)} ${units[i]}`
}

/** The header line after the path: "9.8 MB · 6,343 objects". */
export const infoLine = (info: RepoInfo) => `${sizeText(info.size)} · ${plural(info.loose + info.packed, 'object')}`

export const objectsText = (info: RepoInfo) => `${count(info.packed)} packed · ${count(info.loose)} loose · ${plural(info.packs, 'pack')}`

export const garbageText = (info: RepoInfo) => (info.garbage ? `${plural(info.garbage, 'file')} · ${sizeText(info.garbageSize)}` : 'none')

export const statusText = (info: RepoInfo) => info.gcReason || 'No GC needed'

/** The toast after Run garbage collection: the size before and after. */
export function gcDoneMessage(before: RepoInfo, after: RepoInfo): string {
  if (after.size >= before.size) return `Garbage collected: ${sizeText(after.size)}, nothing to reclaim.`
  return `Garbage collected: ${sizeText(before.size)} → ${sizeText(after.size)}.`
}
