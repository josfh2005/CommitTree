<script lang="ts">
  import { onDestroy, onMount } from 'svelte'
  import { Terminal } from '@xterm/xterm'
  import { FitAddon } from '@xterm/addon-fit'
  import '@xterm/xterm/css/xterm.css'
  import { api } from '../lib/api'
  import { highContrast, theme } from '../lib/stores'

  export let tab: string
  export let visible: boolean
  /** Receives this tab's writer so the panel can route terminal:data here. */
  export let register: (tab: string, write: ((data: string) => void) | null) => void

  let host: HTMLDivElement
  let term: Terminal
  let fit: FitAddon
  let observer: ResizeObserver

  const css = (name: string) => getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  const colours = () => ({ background: css('--surface'), foreground: css('--text'), cursor: css('--text'), selectionBackground: css('--selection') })

  // xterm paints with fixed colours, so follow theme and contrast changes by
  // re-reading the palette (main.ts has already updated the root by now).
  $: if (term) {
    void $theme, $highContrast
    term.options.theme = colours()
  }

  function refit() {
    if (!visible || !host?.offsetWidth) return
    fit.fit()
    api.terminalResize(tab, term.cols, term.rows).catch(() => {})
  }

  onMount(() => {
    term = new Terminal({
      scrollback: 5000,
      fontSize: 12,
      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
      theme: colours(),
    })
    fit = new FitAddon()
    term.loadAddon(fit)
    term.open(host)
    // Exited or unknown tab: nothing useful to tell the user.
    term.onData((data) => api.terminalWrite(tab, data).catch(() => {}))
    register(tab, (data) => term.write(data))
    observer = new ResizeObserver(refit)
    observer.observe(host)
    refit()
  })

  onDestroy(() => {
    register(tab, null)
    observer?.disconnect()
    term?.dispose()
  })

  $: if (visible && term) {
    // Wait for display:none to lift before measuring.
    requestAnimationFrame(() => {
      refit()
      term.focus()
    })
  }
</script>

<div class="view" class:hidden={!visible} bind:this={host}></div>

<style>
  .view { position: absolute; inset: 4px 0 0 8px; }
  .hidden { display: none; }
</style>
