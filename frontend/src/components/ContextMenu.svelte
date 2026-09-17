<script lang="ts">
  import { menu, type MenuItem } from '../lib/ui'

  const close = () => menu.set(null)

  function choose(item: MenuItem) {
    close()
    item.action()
  }
</script>

<svelte:window on:click={close} on:blur={close} on:resize={close} on:keydown={(e) => e.key === 'Escape' && close()} />

{#if $menu}
  <div
    class="menu"
    style="left: {Math.min($menu.x, window.innerWidth - 230)}px; top: {Math.min($menu.y, window.innerHeight - $menu.items.length * 30 - 16)}px"
    on:contextmenu|preventDefault
  >
    {#each $menu.items as item}
      <button class="item" class:danger={item.danger} disabled={item.disabled} on:click|stopPropagation={() => choose(item)}>
        {item.label}
      </button>
    {/each}
  </div>
{/if}

<style>
  .menu {
    position: fixed;
    z-index: 50;
    min-width: 210px;
    padding: 4px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 10px;
    box-shadow: var(--shadow);
  }
  .item { display: block; width: 100%; height: 28px; padding: 0 10px; border-radius: 6px; text-align: left; }
  .item:hover:not(:disabled) { background: var(--hover); }
  .danger { color: var(--danger); }
</style>
