import type { Ref, Refs } from './types'

/** One "Check out <name>" entry in a commit's context menu. */
export interface CheckoutChoice {
  name: string
  /** '' checks out the local branch as it is; otherwise CheckoutRemote(remote, name). */
  remote: string
  /** The branch is already checked out here (only for a local choice). */
  current: boolean
  /** Path of another worktree that has the local branch checked out, or ''. */
  worktree: string
}

/** splitRemote turns "origin/release/21" into ["origin", "release/21"],
 *  matching the longest known remote name so remotes with slashes work. */
function splitRemote(ref: string, remotes: string[]): [string, string] {
  const remote = remotes.filter((r) => ref.startsWith(r + '/')).sort((a, b) => b.length - a.length)[0]
  if (remote) return [remote, ref.slice(remote.length + 1)]
  const slash = ref.indexOf('/')
  return [ref.slice(0, slash), ref.slice(slash + 1)]
}

/**
 * checkoutChoices lists the branches a commit row can check out, in badge
 * order. A local branch and its remote twin on the same commit are one
 * choice (the local branch); a branch only on remotes is checked out
 * through the remote the local branch tracks, else origin, else the first.
 * Tags, HEAD and origin/HEAD are not branches to switch to.
 */
export function checkoutChoices(rowRefs: Ref[], refs: Refs | null): CheckoutChoice[] {
  const locals = new Map((refs?.local ?? []).map((b) => [b.name, b]))
  const remoteNames = (refs?.remotes ?? []).map((r) => r.name)
  const onRow = new Set(rowRefs.filter((r) => r.kind === 'local').map((r) => r.name))
  const choices: CheckoutChoice[] = []
  const remotesByName = new Map<string, string[]>()

  for (const ref of rowRefs) {
    if (ref.kind === 'local') {
      const branch = locals.get(ref.name)
      choices.push({
        name: ref.name,
        remote: '',
        current: !refs?.detached && refs?.head === ref.name,
        worktree: branch?.worktree ?? '',
      })
    } else if (ref.kind === 'remote') {
      const [remote, name] = splitRemote(ref.name, remoteNames)
      if (name === 'HEAD' || onRow.has(name)) continue
      const seen = remotesByName.get(name)
      if (seen) {
        seen.push(remote)
        continue
      }
      remotesByName.set(name, [remote])
      choices.push({ name, remote, current: false, worktree: locals.get(name)?.worktree ?? '' })
    }
  }

  for (const choice of choices) {
    const candidates = remotesByName.get(choice.name)
    if (!candidates || candidates.length < 2) continue
    const tracked = locals.get(choice.name)?.upstream ?? ''
    choice.remote =
      candidates.find((r) => tracked.startsWith(r + '/')) ?? candidates.find((r) => r === 'origin') ?? candidates[0]
  }
  return choices
}

/** checkoutNotice is the toast after checking out remote/name, or '' when
 *  the outcome needs no word (a new tracking branch, or nothing moved). */
export function checkoutNotice(outcome: string, remote: string, name: string): string {
  switch (outcome) {
    case 'fastForwarded':
      return `${name} fast-forwarded to ${remote}/${name}`
    case 'diverged':
      return `${name} has commits not on ${remote}/${name} — checked out your local ${name}, not the remote commit`
    default:
      return ''
  }
}
