<script lang="ts">
  import { createEventDispatcher } from 'svelte'

  export let direction: 'vertical' | 'horizontal' = 'vertical'

  const dispatch = createEventDispatcher<{ drag: number }>()

  function down(event: PointerEvent) {
    const handle = event.currentTarget as HTMLElement
    const pick = (e: PointerEvent) => (direction === 'vertical' ? e.clientX : e.clientY)
    let last = pick(event)
    handle.setPointerCapture(event.pointerId)
    const move = (e: PointerEvent) => {
      const pos = pick(e)
      dispatch('drag', pos - last)
      last = pos
    }
    const up = () => {
      handle.removeEventListener('pointermove', move)
      handle.removeEventListener('pointerup', up)
    }
    handle.addEventListener('pointermove', move)
    handle.addEventListener('pointerup', up)
  }
</script>

<div
  class="splitter {direction}"
  role="separator"
  aria-orientation={direction === 'vertical' ? 'vertical' : 'horizontal'}
  on:pointerdown={down}
></div>

<style>
  .splitter { flex: none; position: relative; z-index: 3; background: var(--border); }
  .vertical { width: 1px; cursor: col-resize; }
  .vertical::after { content: ''; position: absolute; top: 0; bottom: 0; left: -4px; right: -4px; }
  .horizontal { height: 1px; cursor: row-resize; }
  .horizontal::after { content: ''; position: absolute; left: 0; right: 0; top: -4px; bottom: -4px; }
</style>
