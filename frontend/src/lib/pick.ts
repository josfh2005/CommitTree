export interface PickItem { key: string; label: string; group?: string }

/** filterPick keeps the items whose label contains query, ignoring case;
 *  an empty query keeps them all. */
export function filterPick<T extends PickItem>(items: T[], query: string): T[] {
  const q = query.trim().toLowerCase()
  return q ? items.filter((i) => i.label.toLowerCase().includes(q)) : items
}
