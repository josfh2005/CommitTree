<script lang="ts">
  import { isSeparator, menu, type MenuEntry, type MenuItem } from '../lib/ui'

  const close = () => menu.set(null)
  // A separator is a 9 px line, an item a 30 px row: the clamp keeps the
  // whole menu on screen.
  const menuHeight = (items: MenuEntry[]) => items.reduce((h, e) => h + (isSeparator(e) ? 9 : 30), 0)

  function choose(item: MenuItem) {
    close()
    item.action()
  }
</script>

<svelte:window on:click={close} on:blur={close} on:resize={close} on:keydown={(e) => e.key === 'Escape' && close()} />

{#if $menu}
  <div
    class="menu"
    style="left: {Math.min($menu.x, window.innerWidth - 230)}px; top: {Math.min($menu.y, window.innerHeight - menuHeight($menu.items) - 16)}px"
    on:contextmenu|preventDefault
  >
    {#each $menu.items as item}
      {#if isSeparator(item)}
        <div class="sep" role="separator"></div>
      {:else}
      <button
        class="item"
        class:danger={item.danger}
        disabled={item.disabled}
        title={item.disabled ? item.title : undefined}
        on:click|stopPropagation={() => choose(item)}
      >
        {item.label}
      </button>
      {/if}
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
  .sep { height: 1px; margin: 4px 6px; background: var(--border); }
</style>
