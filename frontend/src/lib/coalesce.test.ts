import { describe, expect, it } from 'vitest'
import { coalesce } from './coalesce'

function gated() {
  const releases: (() => void)[] = []
  let runs = 0
  const job = () => new Promise<void>((r) => { runs++; releases.push(r) })
  return { job, releases, runs: () => runs }
}
const tick = () => new Promise((r) => setTimeout(r, 0))

describe('coalesce', () => {
  it('runs once when called alone', async () => {
    const g = gated()
    const run = coalesce(g.job)
    const p = run()
    g.releases[0]()
    await p
    expect(g.runs()).toBe(1)
  })

  it('calls during a run share one more run that starts after it', async () => {
    const g = gated()
    const run = coalesce(g.job)
    const first = run()
    const done: string[] = []
    const a = run().then(() => done.push('a'))
    const b = run().then(() => done.push('b'))
    expect(g.runs()).toBe(1)
    g.releases[0]()
    await first
    await tick()
    expect(g.runs()).toBe(2) // the follow-up started only after the first ended
    expect(done).toEqual([]) // and the waiting callers are not done yet
    g.releases[1]()
    await Promise.all([a, b])
    expect(g.runs()).toBe(2)
  })

  it('a failed run still lets the follow-up run', async () => {
    let n = 0
    const run = coalesce(async () => { n++; if (n === 1) throw new Error('x') })
    const first = run().catch(() => 'failed')
    const second = run()
    expect(await first).toBe('failed')
    await second
    expect(n).toBe(2)
  })
})
