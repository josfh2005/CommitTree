import { describe, expect, it } from 'vitest'
import { addTab, emptyTermState, markExited, removeRepoTabs, removeTab, setActive, settledAction, tabTitle, tabsFor } from './terminal'

describe('terminal tabs', () => {
  it('numbers tabs per repository and makes the new one active', () => {
    let s = addTab(emptyTermState(), 'r1', 't1', 'zsh')
    s = addTab(s, 'r1', 't2', 'zsh')
    s = addTab(s, 'r2', 't3', 'zsh')
    expect(tabsFor(s, 'r1').map((t) => t.label)).toEqual(['zsh 1', 'zsh 2'])
    expect(tabsFor(s, 'r2').map((t) => t.label)).toEqual(['zsh 1'])
    expect(s.active).toEqual({ r1: 't2', r2: 't3' })
  })

  it('never reuses a number after a tab is closed', () => {
    let s = addTab(emptyTermState(), 'r1', 't1', 'zsh')
    s = addTab(s, 'r1', 't2', 'zsh')
    s = removeTab(s, 't2')
    s = addTab(s, 'r1', 't3', 'zsh')
    expect(tabsFor(s, 'r1').map((t) => t.label)).toEqual(['zsh 1', 'zsh 3'])
  })

  it('moves the active tab to a neighbour when the active one closes', () => {
    let s = addTab(emptyTermState(), 'r1', 't1', 'zsh')
    s = addTab(s, 'r1', 't2', 'zsh')
    s = addTab(s, 'r1', 't3', 'zsh')
    s = setActive(s, 'r1', 't2')
    s = removeTab(s, 't2')
    expect(s.active.r1).toBe('t1')
    s = removeTab(s, 't1')
    expect(s.active.r1).toBe('t3')
    s = removeTab(s, 't3')
    expect(s.active.r1).toBeUndefined()
  })

  it('keeps an exited tab and titles it with its code', () => {
    let s = addTab(emptyTermState(), 'r1', 't1', 'zsh')
    s = markExited(s, 't1', 0)
    expect(tabTitle(tabsFor(s, 'r1')[0])).toBe('zsh 1 — exited (0)')
  })

  it('ignores events for unknown tabs', () => {
    const s = addTab(emptyTermState(), 'r1', 't1', 'zsh')
    expect(markExited(s, 'nope', 1)).toBe(s)
    expect(removeTab(s, 'nope')).toBe(s)
  })

  it('drops every tab of a removed repository', () => {
    let s = addTab(emptyTermState(), 'r1', 't1', 'zsh')
    s = addTab(s, 'r2', 't2', 'zsh')
    s = removeRepoTabs(s, 'r1')
    expect(s.tabs.map((t) => t.id)).toEqual(['t2'])
    expect(s.active.r1).toBeUndefined()
  })
})

describe('settledAction', () => {
  it('runs the full check only for the selected repository', () => {
    expect(settledAction('r1', 'r1')).toBe('check')
    expect(settledAction('r1', 'r2')).toBe('list')
    expect(settledAction('', 'r2')).toBe('list')
  })
})
