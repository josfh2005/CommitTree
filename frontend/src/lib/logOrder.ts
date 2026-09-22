import type { LogOrder } from './types'

/** The two orders the log can be walked in. Topological (the default) keeps
 *  a merged branch's own history together instead of interleaving it with
 *  the trunk by date; date order is a strict walk by commit date. */
export const LOG_ORDERS: LogOrder[] = ['topo', 'date']

export function isLogOrder(value: unknown): value is LogOrder {
  return value === 'topo' || value === 'date'
}

/** Normalizes an arbitrary string (a <select>'s value, or whatever a
 *  previous version of this app left in localStorage) to a valid LogOrder,
 *  falling back to the topological default rather than letting a corrupt or
 *  outdated stored value produce a request the backend rejects. */
export function parseLogOrder(value: string): LogOrder {
  return isLogOrder(value) ? value : 'topo'
}
