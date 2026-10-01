# Decision Tray Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Pending AI decision cards move out of the middle of the chat into a carousel above the message box; the conversation keeps a one-line link at each card's place.

**Architecture:** Frontend only. `pendingCards(state)` derives the unanswered cards from the chat state, and `trayIndexAfter` keeps the carousel on a sensible card as that list changes. A new `DecisionTray.svelte` takes over the card body (options, Other…, Apply) from `ChatPanel.svelte`, which renders a pending card as a one-line button.

**Tech Stack:** Svelte 5 (legacy `$:` syntax as in the repo), TypeScript, vitest. Wails app; run with `make dev` (node 22).

**Spec:** `docs/superpowers/specs/2026-10-01-decision-tray-design.md`

## Global Constraints

- Frontend only: no backend change. Apply stays disabled while `running` ("Available when the AI finishes"); `ErrChatBusy` unchanged.
- UI text in English: header "Pending decisions", counter `‹ n/N ›`, line "◆ Decision pending: <path> · region <id> — <question>".
- Chosen/settled cards render as today.
- Tray max height 40% of the panel, scrolls inside.
- Behaviour change updates `docs/spec/04-conflicts.md` ("Decisions left to you") in the same commit (Task 2).
- Commits without `Co-Authored-By`. In this worktree run git as `/usr/bin/git` (the rtk hook rewrite is refused by the worktree guard).
- Merge into main with `git merge --no-ff`; after the change, rebuild and reopen with `make dev`.

## Review Focus

- A card answered from the tray while another card is shown must not move the user's selection on other cards (pick/Other… text are per card id). Pinned by Task 1's `trayIndexAfter` cases and manual check in Task 2.
- A new card arriving during the run must not yank the carousel away from the card being read. Pinned in Task 1 (`appended → stays`).
- Repo switch / New chat: the tray must show the new conversation's cards, never another repo's. Derived from `state`, pinned by Task 1 (`pendingCards` reads only the given state) and manual check.
- Clicking the same pending line twice (after navigating away with ‹ ›) must jump again. Pinned in Task 2 by passing `focus` as a fresh object per click.
- Applying fails (git error toast): the card stays in the tray and stays selected. Covered by the derivation (summary unchanged) — manual check in Task 2.

---

### Task 1: Pending cards and carousel index

**Files:**
- Modify: `frontend/src/lib/chat.ts` (after `choiceState`, ~:440)
- Test: `frontend/src/lib/chat.test.ts`

**Interfaces:**
- Consumes: existing `decisionCard`, `choiceState`, `DecisionCard`, `ChatState`, `withChoice`, `applyEvent`.
- Produces:
  - `export interface PendingCard { id: string; card: DecisionCard }`
  - `export function pendingCards(state: ChatState): PendingCard[]`
  - `export function trayIndexAfter(prevID: string | null, prev: PendingCard[], next: PendingCard[]): number`

- [ ] **Step 1: Write the failing tests** (append to `chat.test.ts`; add `pendingCards, trayIndexAfter` to the `./chat` import on line 2, and `type PendingCard` if TS needs it)

```ts
describe('pending decision cards', () => {
  const shown = 'Shown to the user as a card with 2 options; the user will choose.'
  const args = (region: string) => ({ path: 'a.go', region, question: `q${region}`, options: [{ label: 'x', text: 'x' }, { label: 'y', text: '' }] })
  const tool = (id: string | undefined, region: string, summary?: string) =>
    ({ id, name: 'propose_options', args: args(region), summary, at: 0 })
  const stateWith = (...answers: ReturnType<typeof tool>[][]): ChatState => ({
    repoID: 'r', runID: null,
    items: answers.flatMap((tools) => [{ role: 'user' as const, text: 'q', tools: [] }, { role: 'assistant' as const, text: 't', tools }]),
  })

  it('lists unanswered cards oldest first across answers', () => {
    const s = stateWith([tool('c1', '1', shown), tool('c2', '2', shown)], [tool('c3', '3', shown)])
    expect(pendingCards(s).map((p) => p.id)).toEqual(['c1', 'c2', 'c3'])
    expect(pendingCards(s)[0].card.question).toBe('q1')
  })

  it('leaves out answered, settled, refused, unfinished and id-less cards and other tools', () => {
    const s = stateWith([
      tool('c1', '1', 'The user chose "x" for a.go (region 1); it was written.'),
      tool('c2', '2', 'Settled another way: region 2 of a.go is no longer in conflict.'),
      tool('c3', '3', 'Not shown: a card has 2 to 4 options, not 5.'),
      tool('c4', '4', undefined),
      tool(undefined, '5', shown),
      { id: 'c6', name: 'list_conflicts', args: {}, summary: shown, at: 0 },
      tool('c7', '7', shown),
    ])
    expect(pendingCards(s).map((p) => p.id)).toEqual(['c7'])
  })

  it('drops a card once chat:choice records the answer', () => {
    const s = stateWith([tool('c1', '1', shown), tool('c2', '2', shown)])
    const after = applyEvent(s, 'chat:choice', { repoID: 'r', callID: 'c1', summary: 'The user chose "x" for a.go (region 1); it was written.' })
    expect(pendingCards(after).map((p) => p.id)).toEqual(['c2'])
  })

  const list = (...ids: string[]): PendingCard[] => ids.map((id) => ({ id, card: { path: 'a.go', region: id, question: id, options: [] } }))

  it('keeps the carousel on a sensible card', () => {
    // the shown card is still there (a card before it was answered)
    expect(trayIndexAfter('c', list('a', 'b', 'c'), list('b', 'c'))).toBe(1)
    // the shown card was answered: the next one takes its place
    expect(trayIndexAfter('b', list('a', 'b', 'c'), list('a', 'c'))).toBe(1)
    // it was the last: the previous one
    expect(trayIndexAfter('c', list('a', 'b', 'c'), list('a', 'b'))).toBe(1)
    // a new card arrives during the run: stay on the one being read
    expect(trayIndexAfter('a', list('a', 'b'), list('a', 'b', 'c'))).toBe(0)
    // nothing left
    expect(trayIndexAfter('a', list('a'), list())).toBe(-1)
    // nothing shown yet, or an id from another conversation
    expect(trayIndexAfter(null, list(), list('a', 'b'))).toBe(0)
    expect(trayIndexAfter('zz', list('a'), list('b', 'c'))).toBe(0)
  })
})
```

Check that `ChatState` is imported in the test file (line 2 imports `type ChatState`); `applyEvent` is already imported. If `ChatToolUse` requires more fields than the literal above, add them in `tool()`.

- [ ] **Step 2: Run to verify it fails**

Run: `cd frontend && npx vitest run src/lib/chat.test.ts` (node 22: `source ~/.nvm/nvm.sh && nvm use 22`)
Expected: FAIL, `pendingCards is not a function`.

- [ ] **Step 3: Implement** (after `choiceState` in `chat.ts`)

```ts
/** A decision card still waiting for an answer: its call id and content. */
export interface PendingCard { id: string; card: DecisionCard }

/** pendingCards lists the conversation's cards still waiting for an
 *  answer, oldest first, across every answer. */
export function pendingCards(state: ChatState): PendingCard[] {
  const out: PendingCard[] = []
  for (const item of state.items) {
    for (const tool of item.tools) {
      const card = decisionCard(tool)
      if (card && tool.id && choiceState(tool) === 'pending') out.push({ id: tool.id, card })
    }
  }
  return out
}

/** trayIndexAfter keeps the carousel on a sensible card when the list
 *  changes: the same card if it is still there, otherwise the card that
 *  took its place (the previous one when it was the last); -1 when none
 *  is left. */
export function trayIndexAfter(prevID: string | null, prev: PendingCard[], next: PendingCard[]): number {
  if (next.length === 0) return -1
  if (prevID === null) return 0
  const still = next.findIndex((p) => p.id === prevID)
  if (still >= 0) return still
  const was = prev.findIndex((p) => p.id === prevID)
  if (was < 0) return 0
  return Math.min(was, next.length - 1)
}
```

`item.tools` is in arrival order, which is the order of the calls; `parts()` sorting is not needed here.

- [ ] **Step 4: Run tests**

Run: `cd frontend && npx vitest run && npm run check`
Expected: all PASS; svelte-check 0 errors (2 known warnings).

- [ ] **Step 5: Commit**

```bash
/usr/bin/git add frontend/src/lib/
/usr/bin/git commit -m "feat(chat): list pending decision cards and keep a carousel index"
```

---

### Task 2: Tray component, one-line link in the chat, spec

**Files:**
- Create: `frontend/src/components/DecisionTray.svelte`
- Modify: `frontend/src/components/ChatPanel.svelte` (script :22-41 card state and `applyChoice`; markup :233-269; composer ~:325; styles :380-385)
- Modify: `docs/spec/04-conflicts.md` (:316-355, "Decisions left to you")

**Interfaces:**
- Consumes: `pendingCards`, `trayIndexAfter`, `PendingCard`, `DecisionCard` (Task 1); `chooseRegionOption(repoID, callID, option, text, path)` from `lib/actions`; `isApplyKey` from `lib/shortcuts`.
- Produces: `<DecisionTray repoID cards running focus />` where `focus: { id: string } | null` (a fresh object per click so the same id can be focused twice).

- [ ] **Step 1: Create `DecisionTray.svelte`**

```svelte
<script lang="ts">
  import { chooseRegionOption } from '../lib/actions'
  import { trayIndexAfter, type DecisionCard, type PendingCard } from '../lib/chat'
  import { isApplyKey } from '../lib/shortcuts'
  import Icon from './Icon.svelte'

  export let repoID: string
  export let cards: PendingCard[]
  export let running: boolean
  // Set by a click on a card's line in the conversation; a new object per
  // click, so the same card can be asked for again.
  export let focus: { id: string } | null = null

  let index = 0
  let seen: PendingCard[] = []
  // By call id: the selected option (-1 = Other…), the Other… text, and
  // whether Apply is in flight. Kept while moving between cards.
  let pick: Record<string, number> = {}
  let own: Record<string, string> = {}
  let applying: Record<string, boolean> = {}

  // Only `cards` triggers this; index and seen are read inside follow().
  $: follow(cards)
  function follow(next: PendingCard[]) {
    index = Math.max(0, trayIndexAfter(seen[index]?.id ?? null, seen, next))
    seen = next
  }

  $: if (focus) show(focus.id)
  function show(id: string) {
    const i = cards.findIndex((c) => c.id === id)
    if (i >= 0) index = i
  }

  $: current = cards[index]

  function startOwn(id: string, card: DecisionCard) {
    const from = pick[id] ?? 0
    if (own[id] === undefined) own = { ...own, [id]: card.options[from >= 0 ? from : 0]?.text ?? '' }
    pick = { ...pick, [id]: -1 }
  }

  async function applyChoice(id: string, card: DecisionCard) {
    if (!repoID || running || applying[id]) return
    const k = pick[id] ?? 0
    applying = { ...applying, [id]: true }
    await chooseRegionOption(repoID, id, k, k === -1 ? own[id] ?? '' : '', card.path)
    applying = { ...applying, [id]: false }
  }
</script>

{#if current}
  {@const id = current.id}
  {@const card = current.card}
  <div class="tray">
    <div class="tray-head">
      <span class="tray-title">Pending decisions</span>
      {#if cards.length > 1}
        <span class="spacer"></span>
        <button class="icon-btn" title="Previous decision" disabled={index === 0} on:click={() => (index -= 1)}><Icon name="chevron-left" size={13} /></button>
        <span class="tray-count">{index + 1}/{cards.length}</span>
        <button class="icon-btn" title="Next decision" disabled={index === cards.length - 1} on:click={() => (index += 1)}><Icon name="chevron-right" size={13} /></button>
      {/if}
    </div>
    <div class="tray-body">
      <div class="decision-where">{card.path} · region {card.region}</div>
      <div class="decision-question">{card.question}</div>
      {#each card.options as opt, k}
        <label class="decision-option">
          <input type="radio" name={id} checked={(pick[id] ?? 0) === k} on:change={() => (pick = { ...pick, [id]: k })} />
          <span>{opt.label}</span>
        </label>
        <pre class="decision-text" class:empty={opt.text === ''}>{opt.text === '' ? '(removes the region)' : opt.text}</pre>
      {/each}
      <label class="decision-option">
        <input type="radio" name={id} checked={pick[id] === -1} on:change={() => startOwn(id, card)} />
        <span>Other…</span>
      </label>
      {#if pick[id] === -1}
        <!-- svelte-ignore a11y_autofocus -->
        <textarea class="decision-edit" autofocus bind:value={own[id]}
          on:keydown={(e) => {
            if (isApplyKey(e)) { e.preventDefault(); applyChoice(id, card) }
            if (e.key === 'Escape') pick = { ...pick, [id]: 0 }
          }}></textarea>
      {/if}
    </div>
    <div class="tray-actions">
      {#if running}<span class="decision-wait">Available when the AI finishes</span>{/if}
      <button class="btn primary" disabled={running || applying[id]} on:click={() => applyChoice(id, card)}>Apply</button>
    </div>
  </div>
{/if}

<style>
  .tray { display: flex; flex-direction: column; max-height: 40vh; margin-bottom: 4px; padding: 8px 10px; border: 1px solid var(--border); border-radius: 10px; background: var(--bg); }
  .tray-head { display: flex; align-items: center; gap: 4px; flex: none; }
  .tray-title { font-weight: 500; font-size: 12px; }
  .tray-count { font-size: 11px; color: var(--muted); font-variant-numeric: tabular-nums; }
  .spacer { flex: 1; }
  .tray-body { min-height: 0; overflow-y: auto; }
  .decision-where { margin-top: 4px; font-size: 12px; color: var(--muted); }
  .decision-question { margin: 4px 0 2px; }
  .decision-option { display: flex; align-items: center; gap: 6px; margin-top: 6px; }
  .decision-text { margin: 2px 0 0 22px; padding: 4px 6px; border-radius: 6px; background: var(--hover); font-family: var(--mono); font-size: 11px; white-space: pre-wrap; color: var(--text); }
  .decision-text.empty { background: none; color: var(--faint); font-family: inherit; font-style: italic; }
  .decision-edit { box-sizing: border-box; width: 100%; min-height: 60px; margin-top: 4px; font-family: var(--mono); font-size: 12px; }
  .tray-actions { display: flex; justify-content: flex-end; gap: 6px; margin-top: 8px; flex: none; }
  .decision-wait { margin-right: auto; align-self: center; font-size: 12px; color: var(--faint); }
</style>
```

Check `Icon` has `chevron-left`/`chevron-right` (`grep -n "chevron" frontend/src/components/Icon.svelte`); use the names it has (e.g. `chevron` rotated, or `‹`/`›` text in the buttons) and ledger a ruling if they differ. Check `.icon-btn` and `.btn.primary` are global styles (they are used unscoped in ChatPanel); if `.icon-btn` is scoped there, copy its rule into the tray.

- [ ] **Step 2: Wire it into `ChatPanel.svelte`**

Script:
- Remove `pick`, `own`, `applying`, `startOwn`, `applyChoice` and their comment (:22-41), and the now-unused imports (`chooseRegionOption`, `isApplyKey`, `type DecisionCard` if unused).
- Add `import DecisionTray from './DecisionTray.svelte'`, add `pendingCards` to the `../lib/chat` import, then:

```ts
  $: pending = pendingCards(state)
  // A click on a pending card's line in the conversation; DecisionTray shows it.
  let focus: { id: string } | null = null
```

Markup, inside the `{#if card && tool.id && cardState}` branch, replace the whole `<div class="confirm decision">…</div>` with:

```svelte
                {@const id = tool.id}
                {#if cardState === 'pending'}
                  <button class="decision-line" title="Show in pending decisions" on:click={() => (focus = { id })}>
                    ◆ Decision pending: {card.path} · region {card.region} — {card.question}
                  </button>
                {:else}
                  <div class="confirm decision">
                    <div class="confirm-title">{card.path} · region {card.region}</div>
                    <div class="decision-question">{card.question}</div>
                    <div class="confirm-result">{cardState === 'chosen' ? tool.summary : 'Settled another way'}</div>
                  </div>
                {/if}
```

As the first child of `<div class="composer">`:

```svelte
    <DecisionTray repoID={state.repoID} cards={pending} {running} {focus} />
```

Styles: delete `.decision-option`, `.decision-text`, `.decision-text.empty`, `.decision-edit`, `.decision-wait` (now in the tray); keep `.decision-question`. Add:

```css
  .decision-line { display: block; width: 100%; margin-bottom: 6px; padding: 6px 10px; border: 1px dashed var(--border); border-radius: 8px; background: var(--surface); color: var(--text); font-size: 12px; text-align: left; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .decision-line:hover { background: var(--hover); }
```

- [ ] **Step 3: Spec** — in `docs/spec/04-conflicts.md`, "Decisions left to you":

Replace "it leaves the region as a card in the chat and carries on with the rest. The card names…" so the first paragraph reads:

> When the two sides of a region genuinely contradict each other — two values for the same setting, one side deleting what the other edited — the resolver does not pick one: it leaves the region as a decision card and carries on with the rest. Pending cards wait in a tray above the chat's message box, one at a time, under "Pending decisions" with ‹ n/N › to move between them; in the conversation, at the point where the resolver asked, a line ("◆ Decision pending: config/settings.json · region 3 — …") shows the card in the tray when clicked. The card names the file and the region, asks a one-line question, … (rest unchanged)

In the second paragraph, change "the card is shown but Apply is disabled" to "the card can be read and an option selected — the selection is kept — but Apply is disabled". After the Apply paragraph add:

> Once a card is answered it leaves the tray, which shows the next pending card (the previous one when it was the last) and disappears when none is left. A card arriving while you read another does not move the tray.

And change "Once answered the card shrinks to one line saying what was chosen" to "Once answered, the card's place in the conversation says what was chosen".

- [ ] **Step 4: Verify**

Run: `cd frontend && npx vitest run && npm run check`
Expected: PASS, 0 errors.
Run: `make dev` (node 22; stop a running app first). With the conflict lab (`scripts/conflict-lab`, repo `~/playground/conflict-lab`): Resolve with AI and check —
- cards appear in the tray during the run; an option can be picked; Apply is disabled with the wait note;
- a line in the conversation shows its card in the tray, also twice in a row after moving with ‹ ›;
- after the run, Apply moves to the next card; the last one hides the tray;
- Other… prefills, ⌘↵ applies, Esc returns;
- reload the panel (switch repo and back): pending cards are still in the tray;
- the tray stays usable at the narrowest panel width.

- [ ] **Step 5: Commit**

```bash
/usr/bin/git add frontend/src/components/DecisionTray.svelte frontend/src/components/ChatPanel.svelte docs/spec/04-conflicts.md
/usr/bin/git commit -m "feat(chat): pending decision cards in a tray above the message box"
```

---

### After the tasks

- Final whole-branch review (most capable model).
- Owner manual check, then merge into main with `git merge --no-ff claude/decision-tray -m "Merge claude/decision-tray: pending decision cards in a tray above the message box"`, rebuild and reopen with `make dev`.
- Memory: add a line to `git-ui-region-actions.md` and update `MEMORY.md`.
