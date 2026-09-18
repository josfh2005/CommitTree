import { writable } from 'svelte/store'
import { ClipboardSetText } from '../../wailsjs/runtime/runtime'

export interface Toast {
  id: number
  message: string
  kind: 'error' | 'info'
}

export const toasts = writable<Toast[]>([])
let nextToast = 1

export function toast(message: string, kind: Toast['kind'] = 'info') {
  const id = nextToast++
  toasts.update((list) => [...list, { id, message, kind }])
  if (kind === 'info') setTimeout(() => dismissToast(id), 3000)
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

export type Dialog =
  | (ConfirmOptions & { kind: 'confirm'; resolve: (ok: boolean) => void })
  | (PromptOptions & { kind: 'prompt'; resolve: (result: PromptResult | null) => void })
  | (ChoiceOptions & { kind: 'choice'; resolve: (value: string | null) => void })

export const dialog = writable<Dialog | null>(null)

export const confirmDialog = (options: ConfirmOptions) =>
  new Promise<boolean>((resolve) => dialog.set({ ...options, kind: 'confirm', resolve }))

export const promptDialog = (options: PromptOptions) =>
  new Promise<PromptResult | null>((resolve) => dialog.set({ ...options, kind: 'prompt', resolve }))

export const choiceDialog = <T extends string>(options: ChoiceOptions<T>) =>
  new Promise<T | null>((resolve) =>
    dialog.set({ ...(options as unknown as ChoiceOptions), kind: 'choice', resolve: resolve as (value: string | null) => void }),
  )

export interface MenuItem {
  label: string
  action: () => void
  danger?: boolean
  disabled?: boolean
}

export const menu = writable<{ x: number; y: number; items: MenuItem[] } | null>(null)

export function openMenu(event: MouseEvent, items: MenuItem[]) {
  event.preventDefault()
  event.stopPropagation()
  menu.set({ x: event.clientX, y: event.clientY, items })
}

export async function copyText(text: string) {
  await ClipboardSetText(text)
  toast('Copied')
}
