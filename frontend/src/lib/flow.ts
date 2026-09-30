import type { PickItem } from './pick'
import type { Flow, FlowBranch, FlowConfig, FlowFinishResult, FlowPlan, FlowStartResult, FlowType } from './types'
import type { FormField, FormValues } from './ui'

export const FLOW_TYPES: FlowType[] = ['feature', 'release', 'hotfix', 'warmfix']
const TITLES: Record<FlowType, string> = { feature: 'Feature', release: 'Release', hotfix: 'Hotfix', warmfix: 'Warmfix' }

export type FlowMenuEntry =
  | { action: 'finish'; branch: string; label: string }
  | { action: 'finish-pick'; label: string }
  | { action: 'start'; type: FlowType; label: string; disabled: boolean; title: string }

/** A finish stopped by a conflict, remembered in memory only. */
export interface PendingFinish { repoId: string; branch: string; releases: string[]; target: string }

/** flowMenu is what the toolbar's Flow button opens. */
export function flowMenu(flow: Flow): { kind: 'init' } | { kind: 'menu'; entries: FlowMenuEntry[] } {
  if (!flow.initialized || flow.problem) return { kind: 'init' }
  const entries: FlowMenuEntry[] = []
  if (flow.current) entries.push({ action: 'finish', branch: flow.current.name, label: `Finish ${flow.current.type} ${flow.current.short}…` })
  else if (flow.branches.length) entries.push({ action: 'finish-pick', label: 'Finish…' })
  for (const type of FLOW_TYPES) {
    const disabled = type === 'warmfix' && flow.releases.length === 0
    entries.push({ action: 'start', type, label: `Start ${type}…`, disabled, title: disabled ? 'No local release branch' : '' })
  }
  return { kind: 'menu', entries }
}

export function finishPickItems(flow: Flow): PickItem[] {
  const item = (b: FlowBranch, group: string): PickItem => ({ key: b.name, label: b.name, group })
  return [
    ...flow.branches.filter((b) => b.inProgress).map((b) => item(b, 'In progress')),
    ...FLOW_TYPES.flatMap((t) => flow.branches.filter((b) => !b.inProgress && b.type === t).map((b) => item(b, TITLES[t]))),
  ]
}

export function initFields(flow: Flow, locals: string[], head: string): FormField[] {
  const master = [flow.master, 'master', 'main', head].find((b) => b && locals.includes(b)) ?? locals[0] ?? ''
  const text = (key: string, label: string, value: string): FormField => ({ kind: 'text', key, label, value })
  return [
    { kind: 'select', key: 'master', label: 'Production branch', value: master, options: locals.map((b) => ({ value: b, label: b })) },
    text('develop', 'Development branch', flow.develop || 'develop'),
    text('feature', 'Feature prefix', flow.prefixes.feature),
    text('release', 'Release prefix', flow.prefixes.release),
    text('hotfix', 'Hotfix prefix', flow.prefixes.hotfix),
    text('warmfix', 'Warmfix prefix', flow.prefixes.warmfix),
  ]
}

export function initConfig(v: FormValues): FlowConfig {
  const s = (k: string) => String(v[k] ?? '').trim()
  return { master: s('master'), develop: s('develop'), prefixes: { feature: s('feature'), release: s('release'), hotfix: s('hotfix'), warmfix: s('warmfix') } }
}

export function startBase(flow: Flow, type: FlowType, values: FormValues): string {
  if (type === 'hotfix') return flow.master
  if (type === 'warmfix') return String(values.release || flow.releases[0] || '')
  return flow.develop
}

export function startFields(flow: Flow, type: FlowType): FormField[] {
  const several = type === 'warmfix' && flow.releases.length > 1
  const from = several ? 'the release below' : startBase(flow, type, {})
  const fields: FormField[] = [{ kind: 'text', key: 'name', label: `Name (from ${from})`, value: '', prefix: flow.prefixes[type] }]
  if (several) fields.push({ kind: 'select', key: 'release', label: 'Release', value: flow.releases[0], options: flow.releases.map((r) => ({ value: r, label: r })) })
  return fields
}

const needsRelease = (b: FlowBranch) => b.type === 'warmfix' && !b.base

/** The releases PlanFinish is asked about when the dialog opens. */
export function planReleases(flow: Flow, b: FlowBranch): string[] {
  if (b.type === 'hotfix') return flow.releases
  if (needsRelease(b)) return flow.releases.slice(0, 1)
  return []
}

/** finishFields: ticked are releases chosen before — by the finish a
 *  conflict interrupted, or already merged — so resuming keeps them. */
export function finishFields(flow: Flow, b: FlowBranch, ticked: string[] = []): FormField[] {
  if (b.type === 'hotfix') return flow.releases.map((r) => ({ kind: 'checkbox', key: `release:${r}`, label: `Also merge into ${r}`, value: ticked.includes(r) }))
  if (needsRelease(b) && flow.releases.length > 1) {
    const value = flow.releases.find((r) => ticked.includes(r)) ?? flow.releases[0]
    return [{ kind: 'select', key: 'release', label: 'Release', value, options: flow.releases.map((r) => ({ value: r, label: r })) }]
  }
  return []
}

export function finishReleases(flow: Flow, b: FlowBranch, values: FormValues): string[] {
  if (b.type === 'hotfix') return flow.releases.filter((r) => values[`release:${r}`] === true)
  if (needsRelease(b)) return [String(values.release || flow.releases[0] || '')].filter(Boolean)
  return []
}

export function finishMessage(plan: FlowPlan, b: FlowBranch, values: FormValues): string {
  let steps = plan.steps
  let ending = plan.ending
  // A release step is shown only when its checkbox is ticked.
  if (b.type === 'hotfix') steps = steps.filter((s) => !(`release:${s.target}` in values) || values[`release:${s.target}`] === true)
  if (needsRelease(b) && values.release) {
    const target = String(values.release)
    steps = [{ target, done: plan.steps.find((s) => s.target === target)?.done ?? false }]
    ending = target
  }
  const into = steps.map((s) => (s.done ? `${s.target} (already there)` : s.target)).join(', ')
  return `Merge ${plan.branch} into ${into}.\nThen delete ${plan.branch} and stay on ${ending}.`
}

const withNotes = (text: string, notes: string[]) => [text, ...notes].join('\n')

export function startedMessage(res: FlowStartResult, base: string): string {
  return withNotes(`Started ${res.branch} from ${base}.`, res.notes)
}

export function finishedMessage(branch: string, res: FlowFinishResult): string {
  const what = res.merged.length ? `merged into ${res.merged.join(', ')}` : 'already merged'
  return withNotes(`Finished ${branch}: ${what}; branch deleted.`, res.notes)
}

export function conflictMessage(branch: string, res: FlowFinishResult): string {
  return `${branch} conflicts with ${res.target}. Resolve the conflict and commit; finishing continues from there.`
}

export function finishingLine(pending: PendingFinish | null, repoId: string): string {
  return pending && pending.repoId === repoId ? `Part of finishing ${pending.branch} (into ${pending.target})` : ''
}
