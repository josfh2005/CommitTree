import { describe, expect, it } from 'vitest'
import { CHAT_WATCH_EVENTS, emptyWatch, watchChat } from './notifyChat'

const start = { repoID: 'r1', runID: 'run1', text: 'hi', provider: 'anthropic', model: 'm' }

describe('watchChat', () => {
  it('a finished turn reports its duration', () => {
    let { watch } = watchChat(emptyWatch(), 'chat:start', start, 1_000)
    const r = watchChat(watch, 'chat:done', { repoID: 'r1', runID: 'run1' }, 13_000)
    expect(r.event).toEqual({ category: 'done', repoID: 'r1', target: 'chat', body: 'Chat answer finished · 12 s', durationMs: 12_000 })
    expect(r.watch.runs).toEqual({})
  })

  it('a write card asks for the user', () => {
    const { watch } = watchChat(emptyWatch(), 'chat:start', start, 0)
    const r = watchChat(watch, 'chat:confirm', { repoID: 'r1', runID: 'run1', confirmID: 'c', tool: 'commit', title: 'Commit 2 files', details: [] }, 5)
    expect(r.event).toEqual({ category: 'ai', repoID: 'r1', target: 'chat', body: 'The AI is waiting for you to confirm: Commit 2 files' })
  })

  const shown = 'Shown to the user as a card with 2 options; the user will choose.'

  it('a shown decision card asks for the user and silences the finished turn', () => {
    let { watch } = watchChat(emptyWatch(), 'chat:start', start, 0)
    const card = watchChat(watch, 'chat:tool_result', { repoID: 'r1', runID: 'run1', name: 'propose_options', summary: shown }, 5)
    expect(card.event).toEqual({ category: 'ai', repoID: 'r1', target: 'chat', body: 'The AI has a decision for you' })
    const done = watchChat(card.watch, 'chat:done', { repoID: 'r1', runID: 'run1' }, 60_000)
    expect(done.event).toBeNull()
  })

  it('a rejected card says nothing and the finished turn still notifies', () => {
    let { watch } = watchChat(emptyWatch(), 'chat:start', start, 0)
    const card = watchChat(watch, 'chat:tool_result', { repoID: 'r1', runID: 'run1', name: 'propose_options', summary: 'options must be a list of {label, text}' }, 5)
    expect(card.event).toBeNull()
    const done = watchChat(card.watch, 'chat:done', { repoID: 'r1', runID: 'run1' }, 60_000)
    expect(done.event?.category).toBe('done')
  })

  it('tool calls and other tools say nothing', () => {
    const { watch } = watchChat(emptyWatch(), 'chat:start', start, 0)
    expect(watchChat(watch, 'chat:tool', { repoID: 'r1', runID: 'run1', name: 'propose_options', args: {} }, 5).event).toBeNull()
    expect(watchChat(watch, 'chat:tool_result', { repoID: 'r1', runID: 'run1', name: 'git_log', summary: shown }, 5).event).toBeNull()
  })

  it('watches tool results, not tool calls', () => {
    expect(CHAT_WATCH_EVENTS).toContain('chat:tool_result')
    expect(CHAT_WATCH_EVENTS).not.toContain('chat:tool')
  })

  it('an error is a problem', () => {
    const { watch } = watchChat(emptyWatch(), 'chat:start', start, 0)
    const r = watchChat(watch, 'chat:error', { repoID: 'r1', runID: 'run1', message: 'model not found\ndetails', code: 'model_missing' }, 5)
    expect(r.event).toEqual({ category: 'problem', repoID: 'r1', target: 'chat', body: 'Chat answer failed: model not found' })
    expect(r.watch.runs).toEqual({})
  })

  it('done for an unknown run says nothing', () => {
    expect(watchChat(emptyWatch(), 'chat:done', { repoID: 'r1', runID: 'x' }, 5).event).toBeNull()
  })
})
