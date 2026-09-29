import { get } from 'svelte/store'
import { api } from './api'
import {
  conflictMessage, finishFields, finishMessage, finishPickItems, finishReleases, finishedMessage, flowMenu,
  initConfig, initFields, planReleases, startBase, startFields, startedMessage,
} from './flow'
import { busy, mergeState, pendingFinish, refreshRepo, refs } from './stores'
import type { Flow, FlowType } from './types'
import { errorMessage, formDialog, menu, pickDialog, toast, type MenuItem } from './ui'

async function busyDo(label: string, fn: () => Promise<void>) {
  busy.set(label)
  try {
    await fn()
  } catch (e) {
    toast(errorMessage(e), 'error')
  } finally {
    busy.set('')
    await refreshRepo()
  }
}

/** openFlowMenu is the toolbar's Flow button: Init when git-flow is not set
 *  up, otherwise a menu under the button. */
export async function openFlowMenu(repoId: string, event: MouseEvent) {
  // Stop the click before any await, or the window's click handler closes
  // the menu this opens.
  event.stopPropagation()
  const rect = (event.currentTarget as HTMLElement).getBoundingClientRect()
  let flow: Flow
  try {
    flow = await api.getFlow(repoId)
  } catch (e) {
    toast(errorMessage(e), 'error')
    return
  }
  const m = flowMenu(flow)
  if (m.kind === 'init') return initFlow(repoId, flow)
  const items: MenuItem[] = m.entries.map((e) => {
    if (e.action === 'finish') return { label: e.label, action: () => finishBranch(repoId, e.branch) }
    if (e.action === 'finish-pick') return { label: e.label, action: () => pickAndFinish(repoId, flow) }
    return { label: e.label, disabled: e.disabled, title: e.title, action: () => startBranch(repoId, flow, e.type) }
  })
  menu.set({ x: rect.left, y: rect.bottom + 4, items })
}

async function initFlow(repoId: string, flow: Flow) {
  const r = get(refs)
  const values = await formDialog({
    title: 'Initialise git-flow',
    message: flow.problem ? `${flow.problem}. Choose the branches again.` : 'Stored in this repository’s git config (gitflow.*), the same keys SourceTree uses.',
    fields: initFields(flow, r?.local.map((b) => b.name) ?? [], r?.head ?? ''),
    submitLabel: 'Initialise',
  })
  if (!values) return
  await busyDo('Initialising git-flow…', async () => {
    await api.initFlow(repoId, initConfig(values))
    toast('git-flow initialised')
  })
}

async function startBranch(repoId: string, flow: Flow, type: FlowType) {
  const values = await formDialog({ title: `Start ${type}`, fields: startFields(flow, type), submitLabel: 'Start' })
  if (!values) return
  const base = startBase(flow, type, values)
  await busyDo(`Starting ${type}…`, async () => {
    const res = await api.startFlow(repoId, type, String(values.name ?? ''), base)
    toast(startedMessage(res, base))
  })
}

async function pickAndFinish(repoId: string, flow: Flow) {
  const branch = await pickDialog({ title: 'Finish a git-flow branch', placeholder: 'Branch', empty: 'No git-flow branches', submitLabel: 'Continue', items: finishPickItems(flow) })
  if (branch) await finishBranch(repoId, branch)
}

async function finishBranch(repoId: string, name: string) {
  try {
    const flow = await api.getFlow(repoId)
    const b = flow.branches.find((x) => x.name === name)
    if (!b) throw new Error(`${name} is not a git-flow branch`)
    const plan = await api.planFinish(repoId, name, planReleases(flow, b))
    const values = await formDialog({
      title: `Finish ${b.type} ${b.short}`,
      message: (v) => finishMessage(plan, b, v),
      fields: finishFields(flow, b),
      submitLabel: 'Finish',
    })
    if (values) await runFinish(repoId, name, finishReleases(flow, b, values))
  } catch (e) {
    toast(errorMessage(e), 'error')
  }
}

/** runFinish finishes without asking: from the Finish dialog, or from the
 *  "Continue finishing" toast after a conflict was committed. */
export async function runFinish(repoId: string, branch: string, releases: string[]) {
  await busyDo(`Finishing ${branch}…`, async () => {
    const res = await api.finishFlow(repoId, branch, releases)
    if (res.outcome === 'conflicted') {
      pendingFinish.set({ repoId, branch, releases, target: res.target })
      toast(conflictMessage(branch, res))
    } else {
      pendingFinish.set(null)
      toast(finishedMessage(branch, res))
    }
  })
}

/** Called after a merge commit: the finish it interrupted can go on. */
export function offerContinueFinish(repoId: string) {
  const p = get(pendingFinish)
  if (!p || p.repoId !== repoId || get(mergeState)?.merging) return
  pendingFinish.set(null)
  toast(`Merged ${p.branch} into ${p.target}.`, 'info', { label: `Continue finishing ${p.branch}`, run: () => runFinish(repoId, p.branch, p.releases) })
}

export function forgetPendingFinish(repoId: string) {
  if (get(pendingFinish)?.repoId === repoId) pendingFinish.set(null)
}
