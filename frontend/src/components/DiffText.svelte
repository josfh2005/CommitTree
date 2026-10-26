<script lang="ts">
  import { lineClass } from '../lib/diff'

  /** A diff as plain text, a line per row, coloured by kind. */
  export let text: string

  const MAX_LINES = 5000

  $: lines = text.split('\n')
</script>

{#each lines.slice(0, MAX_LINES) as line}
  <div class="line {lineClass(line)}">{line || ' '}</div>
{/each}
{#if lines.length > MAX_LINES}
  <div class="line meta">… diff truncated after {MAX_LINES} lines</div>
{/if}

<style>
  .line { padding: 0 12px; white-space: pre; line-height: 18px; }
  .add { background: var(--add-bg); }
  .del { background: var(--del-bg); }
  .hunk { color: #3f7fbf; }
  .meta { color: var(--faint); }
</style>
