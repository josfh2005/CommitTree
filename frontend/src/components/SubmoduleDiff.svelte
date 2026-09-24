<script lang="ts">
  import type { SubmoduleDiff } from '../lib/submodules'

  export let diff: SubmoduleDiff
  export let onOpen: (() => void) | null = null

  const short = (hash: string) => hash.slice(0, 7)
</script>

<div class="submodule-diff mono">
  <div class="heading">Submodule <code>{diff.path}</code></div>
  {#if diff.from || diff.to}
    <div class="range">{short(diff.from)} → {short(diff.to)}</div>
  {/if}
  {#if diff.note}
    <div class="note">{diff.note}</div>
  {/if}
  {#if diff.commits.length}
    <div class="commits">
      {#each diff.commits as c, i (i)}
        <div class="line {c.dir === '>' ? 'add' : 'del'}">{c.dir} {c.subject}</div>
      {/each}
    </div>
  {/if}
  {#if diff.content.length}
    <div class="content-note">
      Contains {diff.content.map((c) => `${c} content`).join(' and ')}.
      {#if onOpen}<button class="link" on:click={onOpen}>Open submodule</button>{/if}
    </div>
  {/if}
</div>

<style>
  .heading, .range, .note, .content-note { padding: 0 12px; line-height: 18px; }
  .heading code { font-family: var(--mono); }
  .note { color: var(--faint); }
  .commits { margin-top: 4px; }
  .line { padding: 0 12px; white-space: pre; line-height: 18px; }
  .add { background: var(--add-bg); }
  .del { background: var(--del-bg); }
  .content-note { margin-top: 8px; color: var(--muted); }
  .link { color: var(--accent); background: none; border: none; padding: 0; margin-left: 6px; cursor: pointer; font: inherit; }
  .link:hover { text-decoration: underline; }
</style>
