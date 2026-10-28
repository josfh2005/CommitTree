<script lang="ts">
  import { pushDialogState, runPush } from '../lib/actions'
  import { canPush, changeRemote, editRemote, pushRequest, selectAll, setTicked, type PushRow } from '../lib/pushDialog'
  import type { PushDialogState } from '../lib/actions'
  import { dialog } from '../lib/ui'

  // The dialog keeps its own working copy: every opening starts from the
  // rows the entry point prepared (docs/spec/05-remote-and-stash.md).
  let opened: PushDialogState | null = null
  let remote = ''
  let rows: PushRow[] = []
  let tags = false
  let force = false
  $: if ($pushDialogState !== opened) {
    opened = $pushDialogState
    if (opened) {
      remote = opened.remote
      rows = opened.rows
      tags = false
      force = false
    }
  }

  $: url = opened ? (opened.remotes.find((r) => r.name === remote)?.pushURL || opened.remotes.find((r) => r.name === remote)?.fetchURL || '') : ''
  // What the last fetch knows of the selected remote (HEAD is not a branch to push to).
  $: remoteBranches = (opened?.refs.remotes.find((r) => r.name === remote)?.branches ?? []).map((b) => b.name).filter((n) => n !== 'HEAD')
  $: ticked = rows.filter((r) => r.push).length
  $: allTicked = rows.length > 0 && ticked === rows.length

  const replace = (i: number, next: PushRow) => (rows = rows.map((r, j) => (j === i ? next : r)))
  const onRemote = (name: string) => {
    if (!opened) return
    remote = name
    rows = changeRemote(rows, opened.refs, name)
  }
  // Select All is indeterminate while only some rows are ticked.
  const indeterminate = (node: HTMLInputElement, on: boolean) => {
    node.indeterminate = on
    return { update: (v: boolean) => (node.indeterminate = v) }
  }

  const close = () => pushDialogState.set(null)
  function submit() {
    if (!opened || !canPush(rows, tags)) return
    const { id } = opened
    const req = pushRequest(remote, rows, tags, force)
    close()
    void runPush(id, req)
  }
  // Escape closes a confirm opened on top of this dialog first.
  const onKey = (e: KeyboardEvent) => { if (opened && e.key === 'Escape' && !$dialog && !e.defaultPrevented) close() }
</script>

<svelte:window on:keydown={onKey} />

{#if opened}
  <div class="backdrop" on:click|self={close} role="presentation">
    <div class="dialog" role="dialog" aria-label="Push to repository">
      <div class="head">
        <label class="remote">
          <span>Push to repository:</span>
          <select value={remote} on:change={(e) => onRemote(e.currentTarget.value)}>
            {#each opened.remotes as r (r.name)}
              <option value={r.name}>{r.name}</option>
            {/each}
          </select>
        </label>
        <span class="url ellipsis" title={url}>{url}</span>
      </div>

      <fieldset>
        <legend>Branches to push</legend>
        <div class="table">
          <div class="tr th">
            <span class="c-push">Push?</span>
            <span>Local branch</span>
            <span>Remote branch</span>
            <span class="c-track">Track?</span>
          </div>
          <div class="body">
            {#each rows as row, i (row.local)}
              <div class="tr">
                <span class="c-push">
                  <input type="checkbox" checked={row.push} aria-label="Push {row.local}"
                    on:change={(e) => replace(i, setTicked(row, e.currentTarget.checked))} />
                </span>
                <span class="ellipsis local" title={row.local}>{row.local}</span>
                <input class="rb" list="push-remote-branches" spellcheck="false" aria-label="Remote branch for {row.local}"
                  value={row.remote} on:input={(e) => replace(i, editRemote(row, e.currentTarget.value))} />
                <span class="c-track">
                  <input type="checkbox" checked={row.track && row.push} disabled={!row.push}
                    use:indeterminate={!row.push} aria-label="Track {row.local}"
                    on:change={(e) => replace(i, { ...row, track: e.currentTarget.checked })} />
                </span>
              </div>
            {/each}
          </div>
        </div>
        <datalist id="push-remote-branches">
          {#each remoteBranches as name (name)}<option value={name}></option>{/each}
        </datalist>
        <label class="check">
          <input type="checkbox" checked={allTicked} use:indeterminate={ticked > 0 && !allTicked}
            on:change={(e) => (rows = selectAll(rows, e.currentTarget.checked))} />
          <span>Select All</span>
        </label>
      </fieldset>

      <div class="foot">
        <div class="opts">
          <label class="check"><input type="checkbox" bind:checked={tags} /><span>Push all tags</span></label>
          <label class="check"><input type="checkbox" bind:checked={force} /><span>Force push</span></label>
        </div>
        <div class="buttons">
          <button type="button" class="btn" on:click={close}>Cancel</button>
          <button type="button" class="btn primary" disabled={!canPush(rows, tags)} on:click={submit}>OK</button>
        </div>
      </div>
    </div>
  </div>
{/if}

<style>
  .backdrop { position: fixed; inset: 0; z-index: 40; display: grid; place-items: center; background: rgba(0, 0, 0, 0.25); }
  .dialog {
    width: min(640px, 94vw);
    padding: 18px 20px 16px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 12px;
    box-shadow: var(--shadow);
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .head { display: flex; align-items: center; gap: 12px; min-width: 0; }
  .remote { display: flex; align-items: center; gap: 8px; font-size: 13px; flex: none; }
  .remote select { font-size: 13px; }
  .url { color: var(--muted); font-family: var(--mono); font-size: 12px; min-width: 0; }
  fieldset { margin: 0; padding: 8px 10px 10px; border: 1px solid var(--border); border-radius: 8px; min-width: 0; }
  legend { padding: 0 6px; color: var(--muted); font-size: 12px; }
  .table { font-size: 13px; }
  .tr { display: grid; grid-template-columns: 52px minmax(0, 1fr) minmax(0, 1fr) 56px; align-items: center; gap: 8px; padding: 2px 0; }
  .th { color: var(--muted); font-size: 12px; border-bottom: 1px solid var(--border); padding-bottom: 4px; }
  .body { max-height: 280px; overflow-y: auto; }
  .c-push, .c-track { display: flex; justify-content: center; }
  .local { font-family: var(--mono); font-size: 12px; }
  .rb { font-family: var(--mono); font-size: 12px; padding: 2px 6px; min-width: 0; }
  input[type='checkbox'] { padding: 0; margin: 0; }
  .check { display: flex; align-items: center; gap: 6px; font-size: 13px; }
  fieldset > .check { margin-top: 8px; }
  .foot { display: flex; align-items: flex-end; justify-content: space-between; gap: 12px; }
  .opts { display: flex; flex-direction: column; gap: 6px; }
  .buttons { display: flex; gap: 8px; }
</style>
