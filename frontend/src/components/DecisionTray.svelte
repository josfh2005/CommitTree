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
    <!-- New inputs per card: reused radios renamed from one card's group to
         the next would uncheck the new card's default. -->
    {#key id}
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
    {/key}
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
