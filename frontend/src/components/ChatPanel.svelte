<script lang="ts">
  import Icon from './Icon.svelte'
  import { chatOpen, selectedRepo } from '../lib/stores'
</script>

<div class="chat">
  <header class="drag">
    <span class="title">Chat</span>
    <button class="icon-btn" title="Hide chat" on:click={() => chatOpen.set(false)}><Icon name="panel-right" /></button>
  </header>
  <div class="empty">
    <Icon name="sparkle" size={22} />
    <p>Set up an AI provider to chat with {$selectedRepo ? $selectedRepo.name : 'your repositories'}.</p>
    <p class="hint">Anthropic, OpenAI and Ollama support arrives in a later version.</p>
  </div>
  <div class="composer">
    <textarea rows="2" placeholder="Ask about this repo…" disabled></textarea>
    <button class="icon-btn" disabled title="Send"><Icon name="send" /></button>
  </div>
</div>

<style>
  .chat { display: flex; flex-direction: column; height: 100%; }
  header { display: flex; align-items: center; justify-content: space-between; height: 44px; padding: 0 10px 0 16px; flex: none; }
  .title { font-weight: 500; }
  .empty { flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 4px; padding: 24px; text-align: center; color: var(--muted); }
  .empty p { margin: 0; }
  .hint { font-size: 12px; color: var(--faint); }
  .composer { display: flex; align-items: flex-end; gap: 6px; margin: 12px; padding: 8px; background: var(--surface); border: 1px solid var(--border); border-radius: 12px; }
  textarea { flex: 1; resize: none; border: 0; padding: 2px 4px; background: transparent; }
</style>
