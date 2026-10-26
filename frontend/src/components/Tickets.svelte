<script lang="ts">
  import { BrowserOpenURL } from '../../wailsjs/runtime/runtime'
  import { isHttps } from '../lib/hosting'
  import { jiraSettings, linkTickets } from '../lib/tickets'

  /** Plain text in which the listed Jira keys are links to their tickets. The
   *  links are spans, so they can sit inside a clickable row: a click on one
   *  opens the ticket and goes no further. */
  export let text: string

  $: segments = linkTickets(text, $jiraSettings)

  function open(event: Event, url: string) {
    event.preventDefault()
    event.stopPropagation()
    if (isHttps(url)) BrowserOpenURL(url)
  }
  function onKey(event: KeyboardEvent, url: string) {
    if (event.key === 'Enter' || event.key === ' ') open(event, url)
  }
</script>

{#each segments as s}{#if 'key' in s}<span class="ticket" role="link" tabindex="0" title={s.url} on:click={(e) => open(e, s.url)} on:keydown={(e) => onKey(e, s.url)}>{s.label}</span>{:else}{s.text}{/if}{/each}

<style>
  .ticket { color: var(--accent); cursor: pointer; }
  .ticket:hover { text-decoration: underline; }
</style>
