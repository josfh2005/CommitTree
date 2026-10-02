import { get } from 'svelte/store'
import { describe, expect, it } from 'vitest'
import { dialog, formDialog, formMessage, initialFormValues, type FormField, SEPARATOR, tidySeparators, type MenuEntry } from './ui'

const fields: FormField[] = [
  { kind: 'text', key: 'name', label: 'Name', value: 'x', prefix: 'feature/' },
  { kind: 'select', key: 'release', label: 'Release', value: 'release/a', options: [{ value: 'release/a', label: 'release/a' }] },
  { kind: 'checkbox', key: 'also', label: 'Also', value: false },
]

describe('form dialog', () => {
  it('starts from each field value', () => {
    expect(initialFormValues(fields)).toEqual({ name: 'x', release: 'release/a', also: false })
  })

  it('computes a message from the current values', () => {
    const opts = { title: 't', fields, submitLabel: 'Go', message: (v: Record<string, string | boolean>) => `name=${v.name}` }
    expect(formMessage(opts, { name: 'y' })).toBe('name=y')
    expect(formMessage({ ...opts, message: 'fixed' }, {})).toBe('fixed')
    expect(formMessage({ ...opts, message: undefined }, {})).toBe('')
  })

  it('resolves to the values, or null on cancel', async () => {
    const p = formDialog({ title: 't', fields, submitLabel: 'Go' })
    const d = get(dialog)
    expect(d?.kind).toBe('form')
    if (d?.kind === 'form') d.resolve({ name: 'z' })
    await expect(p).resolves.toEqual({ name: 'z' })
  })
})

describe('tidySeparators', () => {
  const a = { label: 'A', action: () => {} }
  const b = { label: 'B', action: () => {} }
  it('drops leading, trailing and doubled separators', () => {
    const entries: MenuEntry[] = [SEPARATOR, a, SEPARATOR, SEPARATOR, b, SEPARATOR]
    expect(tidySeparators(entries)).toEqual([a, SEPARATOR, b])
  })
  it('leaves an empty list empty', () => {
    expect(tidySeparators([SEPARATOR])).toEqual([])
  })
})
