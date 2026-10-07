<script lang="ts">
  import Icon from './Icon.svelte'
  import { fetchRemote, newBranch, openRepoFolder, pickAndMerge, pull, push, startCommit, stashChanges } from '../lib/actions'
  import { openFlowMenu } from '../lib/flowActions'
  import { toolbarItems, type ToolbarGroup, type ToolbarId } from '../lib/toolbar'
  import { busy, chatOpen, commandsOpen, gitSettings, mergeState, pausedRemotes, platform, refs, remoteInfo, stashConflictDismissed, terminalOpen, worktreeState } from '../lib/stores'

  export let repoId: string

  $: items = toolbarItems({ refs: $refs, worktree: $worktreeState, merge: $mergeState, busy: $busy, remote: $remoteInfo, terminalOpen: $terminalOpen, commandsOpen: $commandsOpen, chatOpen: $chatOpen, platform: $platform, paused: $pausedRemotes, pushScope: $gitSettings?.pushScope ?? 'ask' })
  const GROUPS: ToolbarGroup[] = ['work', 'sync', 'refs', 'tools']

  function act(id: ToolbarId, event: MouseEvent) {
    switch (id) {
      case 'commit': return startCommit()
      case 'stash': return stashChanges(repoId)
      case 'fetch': return fetchRemote(repoId)
      case 'pull': return pull(repoId)
      case 'push': return push(repoId)
      case 'branch': return newBranch(repoId, $refs?.headHash ?? 'HEAD', $refs?.head ?? 'HEAD')
      case 'merge': return pickAndMerge(repoId)
      case 'flow': return openFlowMenu(repoId, event)
      case 'terminal': return terminalOpen.update((open) => !open)
      case 'commands': return commandsOpen.update((open) => !open)
      case 'folder': return openRepoFolder(repoId)
      case 'chat': return chatOpen.update((open) => !open)
    }
  }
</script>

<div class="toolbar">
  <!-- The only way back into a stash conflict the user dismissed with
       "Done": without it the conflict view would be unreachable until the
       files happen to resolve. -->
  {#if $mergeState?.kind === 'stash' && $stashConflictDismissed}
    <button class="btn" on:click={() => stashConflictDismissed.set(false)}>Resolve conflicts</button>
  {/if}
  {#each GROUPS as group, g}
    {#if g > 0}<span class="sep"></span>{/if}
    {#each items.filter((i) => i.group === group) as item (item.id)}
      <button class="tool" class:toggle={item.group === 'tools' && item.id !== 'folder'} class:active={item.active} aria-pressed={item.group === 'tools' && item.id !== 'folder' ? item.active : undefined} title={item.title} aria-label={item.label} disabled={!item.enabled} on:click={(e) => act(item.id, e)}>
        <span class="icon"><Icon name={item.icon} size={20} />{#if item.badge}<span class="badge">{item.badge}</span>{/if}{#if item.dot}<span class="dot" aria-hidden="true"></span>{/if}</span>
        <span class="label">{item.label}</span>
      </button>
    {/each}
  {/each}
</div>

<style>
  /* Shrinks after the title has ellipsized away; at the window's minimum
     width with the side column open even icon-only buttons may not fit, so
     the toolbar scrolls rather than spilling under the side column. */
  .toolbar { display: flex; align-items: center; gap: 2px; flex: 0 1 auto; min-width: 0; overflow-x: auto; scrollbar-width: none; }
  .tool, .toolbar > :global(.btn) { flex: none; }
  .tool { display: flex; flex-direction: column; align-items: center; gap: 2px; min-width: 50px; padding: 4px 6px; border-radius: 8px; color: var(--muted); font-size: 11px; }
  .tool:hover:not(:disabled) { background: var(--hover); color: var(--text); }
  /* Same grey as every other disabled button; no extra dimming on top. */
  .tool:disabled { color: var(--faint); cursor: default; }
  /* Terminal and Chat are toggles: pressed while their panel is open. */
  .tool.toggle { border: 1px solid transparent; }
  .tool.toggle.active { background: var(--active); border-color: var(--border); color: var(--text); }
  .icon { position: relative; display: inline-flex; }
  .badge { position: absolute; top: -4px; right: -8px; font-size: 9px; line-height: 1; padding: 1px 3px; border-radius: 6px; background: var(--accent); color: white; }
  .dot { position: absolute; top: -2px; right: -3px; width: 7px; height: 7px; border-radius: 50%; background: var(--warning); }
  .sep { width: 1px; height: 28px; margin: 0 6px; background: var(--border); }
  /* The header is the container (LogView). Too narrow for labelled buttons:
     icons only, the name stays in the tooltip. */
  @container repo-header (max-width: 860px) {
    .label { display: none; }
    .tool { min-width: 32px; }
  }
  @container repo-header (max-width: 480px) {
    .tool { min-width: 26px; padding: 3px; }
    .sep { margin: 0 2px; }
  }
</style>
