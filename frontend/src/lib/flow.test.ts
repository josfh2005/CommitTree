import { describe, expect, it } from 'vitest'
import {
  conflictMessage, finishFields, finishMessage, finishPickItems, finishReleases, finishedMessage, finishingLine,
  flowMenu, initConfig, initFields, planReleases, startBase, startFields, startedMessage,
} from './flow'
import type { Flow, FlowBranch, FlowPlan } from './types'

const prefixes = { feature: 'feature/', release: 'release/', hotfix: 'hotfix/', warmfix: 'warmfix/' }
const fb = (name: string, over: Partial<FlowBranch> = {}): FlowBranch => {
  const [type, short] = name.split('/') as [FlowBranch['type'], string]
  return { name, type, short, base: '', inProgress: false, ...over }
}
const flow = (over: Partial<Flow> = {}): Flow => ({
  initialized: true, problem: '', master: 'master', develop: 'develop', prefixes, current: null, branches: [], releases: [], ...over,
})

describe('flowMenu', () => {
  it('offers init when not initialised or broken', () => {
    expect(flowMenu(flow({ initialized: false }))).toEqual({ kind: 'init' })
    expect(flowMenu(flow({ problem: 'develop does not exist' }))).toEqual({ kind: 'init' })
  })

  it('puts finishing the current branch first, then the starts', () => {
    const cur = fb('hotfix/h1')
    const m = flowMenu(flow({ current: cur, branches: [cur] }))
    expect(m.kind === 'menu' && m.entries.map((e) => e.label)).toEqual([
      'Finish hotfix h1…', 'Start feature…', 'Start release…', 'Start hotfix…', 'Start warmfix…',
    ])
  })

  it('offers a picker when not on a flow branch, and disables warmfix without a release', () => {
    const m = flowMenu(flow({ branches: [fb('feature/a')] }))
    if (m.kind !== 'menu') throw new Error('menu')
    expect(m.entries[0]).toEqual({ action: 'finish-pick', label: 'Finish…' })
    expect(m.entries.at(-1)).toEqual({ action: 'start', type: 'warmfix', label: 'Start warmfix…', disabled: true, title: 'No local release branch' })
  })

  it('leaves out Finish when there are no flow branches', () => {
    const m = flowMenu(flow())
    expect(m.kind === 'menu' && m.entries[0].label).toBe('Start feature…')
  })
})

describe('finishPickItems', () => {
  it('lists in-progress branches first, then by type', () => {
    const items = finishPickItems(flow({ branches: [fb('feature/a'), fb('hotfix/h', { inProgress: true }), fb('release/r')] }))
    expect(items.map((i) => `${i.group}:${i.key}`)).toEqual(['In progress:hotfix/h', 'Feature:feature/a', 'Release:release/r'])
  })
})

describe('init', () => {
  it('prefills master from the config, else master, main, or HEAD', () => {
    expect(initFields(flow({ initialized: false, master: '' }), ['main', 'x'], 'x')[0]).toMatchObject({ key: 'master', value: 'main' })
    expect(initFields(flow({ initialized: false, master: '' }), ['master', 'main'], 'main')[0]).toMatchObject({ value: 'master' })
    expect(initFields(flow({ initialized: false, master: '' }), ['trunk'], 'trunk')[0]).toMatchObject({ value: 'trunk' })
    expect(initFields(flow({ problem: 'develop does not exist', master: 'prod' }), ['prod', 'master'], 'prod')[0]).toMatchObject({ value: 'prod' })
  })

  it('turns values into a config', () => {
    const values = { master: 'master', develop: 'dev', feature: 'f/', release: 'r/', hotfix: 'h/', warmfix: 'w/' }
    expect(initConfig(values)).toEqual({ master: 'master', develop: 'dev', prefixes: { feature: 'f/', release: 'r/', hotfix: 'h/', warmfix: 'w/' } })
  })
})

describe('start', () => {
  it('shows the prefix and the base', () => {
    expect(startFields(flow(), 'hotfix')).toEqual([{ kind: 'text', key: 'name', label: 'Name (from master)', value: '', prefix: 'hotfix/' }])
    expect(startBase(flow(), 'feature', {})).toBe('develop')
  })

  it('asks which release for a warmfix only when there are several', () => {
    const one = flow({ releases: ['release/a'] })
    expect(startFields(one, 'warmfix')).toHaveLength(1)
    expect(startFields(one, 'warmfix')[0].label).toBe('Name (from release/a)')
    expect(startBase(one, 'warmfix', {})).toBe('release/a')
    const two = flow({ releases: ['release/a', 'release/b'] })
    expect(startFields(two, 'warmfix')[1]).toMatchObject({ kind: 'select', key: 'release', value: 'release/a' })
    expect(startBase(two, 'warmfix', { release: 'release/b' })).toBe('release/b')
  })

  it('says what it did', () => {
    expect(startedMessage({ branch: 'feature/x', notes: [] }, 'develop')).toBe('Started feature/x from develop.')
    expect(startedMessage({ branch: 'feature/x', notes: ['develop fast-forwarded to origin/develop'] }, 'develop'))
      .toBe('Started feature/x from develop.\ndevelop fast-forwarded to origin/develop')
  })
})

describe('finish', () => {
  const plan = (steps: [string, boolean][], ending = 'develop'): FlowPlan =>
    ({ branch: 'hotfix/h', type: 'hotfix', steps: steps.map(([target, done]) => ({ target, done })), ending })

  it('offers each local release, unticked, for a hotfix', () => {
    const f = flow({ releases: ['release/a'] })
    expect(finishFields(f, fb('hotfix/h'))).toEqual([{ kind: 'checkbox', key: 'release:release/a', label: 'Also merge into release/a', value: false }])
    expect(planReleases(f, fb('hotfix/h'))).toEqual(['release/a'])
    expect(finishReleases(f, fb('hotfix/h'), { 'release:release/a': true })).toEqual(['release/a'])
    expect(finishReleases(f, fb('hotfix/h'), { 'release:release/a': false })).toEqual([])
  })

  it('asks for the release of a warmfix with no recorded base', () => {
    const f = flow({ releases: ['release/a', 'release/b'] })
    expect(finishFields(f, fb('warmfix/w', { base: 'release/a' }))).toEqual([])
    expect(finishFields(f, fb('warmfix/w'))[0]).toMatchObject({ kind: 'select', key: 'release' })
    expect(finishReleases(f, fb('warmfix/w'), { release: 'release/b' })).toEqual(['release/b'])
    expect(finishReleases(f, fb('feature/x'), {})).toEqual([])
  })

  it('spells out the plan, following the ticked releases', () => {
    const p = plan([['master', true], ['release/a', false], ['develop', false]])
    expect(finishMessage(p, fb('hotfix/h'), { 'release:release/a': false }))
      .toBe('Merge hotfix/h into master (already there), develop.\nThen delete hotfix/h and stay on develop.')
    expect(finishMessage(p, fb('hotfix/h'), { 'release:release/a': true }))
      .toBe('Merge hotfix/h into master (already there), release/a, develop.\nThen delete hotfix/h and stay on develop.')
  })

  it('follows the chosen release of a warmfix with no base', () => {
    const p: FlowPlan = { branch: 'warmfix/w', type: 'warmfix', steps: [{ target: 'release/a', done: false }], ending: 'release/a' }
    expect(finishMessage(p, fb('warmfix/w'), { release: 'release/b' }))
      .toBe('Merge warmfix/w into release/b.\nThen delete warmfix/w and stay on release/b.')
  })

  it('reports the outcome', () => {
    expect(finishedMessage('hotfix/h', { outcome: 'finished', target: '', conflicts: [], merged: ['master', 'develop'], notes: [] }))
      .toBe('Finished hotfix/h: merged into master, develop; branch deleted.')
    expect(finishedMessage('feature/f', { outcome: 'finished', target: '', conflicts: [], merged: [], notes: ['n'] }))
      .toBe('Finished feature/f: already merged; branch deleted.\nn')
    expect(conflictMessage('hotfix/h', { outcome: 'conflicted', target: 'develop', conflicts: ['a'], merged: ['master'], notes: [] }))
      .toBe('hotfix/h conflicts with develop. Resolve the conflict and commit; finishing continues from there.')
  })

  it('labels the Merge view only for the repository being finished', () => {
    const p = { repoId: 'r1', branch: 'hotfix/h', releases: [], target: 'develop' }
    expect(finishingLine(p, 'r1')).toBe('Part of finishing hotfix/h (into develop)')
    expect(finishingLine(p, 'r2')).toBe('')
    expect(finishingLine(null, 'r1')).toBe('')
  })
})
