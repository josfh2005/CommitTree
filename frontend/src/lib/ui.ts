import { writable } from 'svelte/store'
import { ClipboardSetText } from '../../wailsjs/runtime/runtime'
import type { PickItem } from './pick'

export interface Toast {
  id: number
  message: string
  kind: 'error' | 'info'
  action?: { label: string; run: () => void }
}

export const toasts = writable<Toast[]>([])
let nextToast = 1

// A toast with an action is not auto-dismissed: it stays until the user acts
// on it or dismisses it themselves, since the action is easy to miss if it
// disappears on its own timer.
export function toast(message: string, kind: Toast['kind'] = 'info', action?: Toast['action']): number {
  const id = nextToast++
  toasts.update((list) => [...list, { id, message, kind, action }])
  if (kind === 'info' && !action) setTimeout(() => dismissToast(id), 3000)
  return id
}

export function dismissToast(id: number) {
  toasts.update((list) => list.filter((t) => t.id !== id))
}

export function errorMessage(e: unknown): string {
  if (typeof e === 'string') return e
  if (e instanceof Error) return e.message
  return String(e)
}

export interface ConfirmOptions {
  title: string
  message: string
  confirmLabel: string
  danger?: boolean
  checkboxLabel?: string
  checked?: boolean
}

/** What confirmDialogWithCheckbox resolves to: whether the user confirmed,
 *  and — only meaningful when ok is true — the checkbox's final state. */
export interface ConfirmResult {
  ok: boolean
  checked: boolean
}

export interface PromptOptions {
  title: string
  label: string
  value?: string
  secondLabel?: string
  checkboxLabel?: string
  checked?: boolean
  submitLabel?: string
}

export interface PromptResult {
  value: string
  second: string
  checked: boolean
}

/** A confirmation whose message and button follow a choice made in it. */
export interface ChoiceOptions<T extends string = string> {
  title: string
  label: string
  options: { value: T; label: string }[]
  value: T
  message: (value: T) => string
  confirmLabel: (value: T) => string
  danger?: (value: T) => boolean
}

/** A searchable list; resolves to the chosen item's key, or null. */
export interface PickOptions { title: string; placeholder: string; empty: string; submitLabel: string; items: PickItem[] }

export type Dialog =
  | (ConfirmOptions & { kind: 'confirm'; resolve: (result: ConfirmResult) => void })
  | (PromptOptions & { kind: 'prompt'; resolve: (result: PromptResult | null) => void })
  | (ChoiceOptions & { kind: 'choice'; resolve: (value: string | null) => void })
  | (PickOptions & { kind: 'pick'; resolve: (key: string | null) => void })

export const dialog = writable<Dialog | null>(null)

export const confirmDialog = (options: ConfirmOptions) =>
  new Promise<boolean>((resolve) => dialog.set({ ...options, kind: 'confirm', resolve: (result) => resolve(result.ok) }))

/** Like confirmDialog, but for a confirmation whose checkbox changes which
 *  action confirming performs (e.g. apply vs pop a stash) — the caller needs
 *  the checkbox's state, not just whether the dialog was confirmed. */
export const confirmDialogWithCheckbox = (options: ConfirmOptions & { checkboxLabel: string }) =>
  new Promise<ConfirmResult>((resolve) => dialog.set({ ...options, kind: 'confirm', resolve }))

export const promptDialog = (options: PromptOptions) =>
  new Promise<PromptResult | null>((resolve) => dialog.set({ ...options, kind: 'prompt', resolve }))

export const choiceDialog = <T extends string>(options: ChoiceOptions<T>) =>
  new Promise<T | null>((resolve) =>
    dialog.set({ ...(options as unknown as ChoiceOptions), kind: 'choice', resolve: resolve as (value: string | null) => void }),
  )

export const pickDialog = (options: PickOptions) =>
  new Promise<string | null>((resolve) => dialog.set({ ...options, kind: 'pick', resolve }))

export interface MenuItem {
  label: string
  action: () => void
  danger?: boolean
  disabled?: boolean
  title?: string
}

export const menu = writable<{ x: number; y: number; items: MenuItem[] } | null>(null)

export function openMenu(event: MouseEvent, items: MenuItem[]) {
  event.preventDefault()
  event.stopPropagation()
  menu.set({ x: event.clientX, y: event.clientY, items })
}

/** openMenuAsync opens a menu whose items need a backend answer first (e.g. whether a commit is already in HEAD). */
export async function openMenuAsync(event: MouseEvent, build: () => Promise<MenuItem[]>) {
  event.preventDefault()
  event.stopPropagation()
  const { clientX: x, clientY: y } = event
  menu.set({ x, y, items: await build() })
}

export async function copyText(text: string) {
  await ClipboardSetText(text)
  toast('Copied')
}
