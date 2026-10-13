<script lang="ts">
  import { onDestroy, tick } from 'svelte'
  import { api } from '../lib/api'
  import {
    canSubmit, countdown, credentialRequest, onClosed, secretLabel, title, tokenHint, type CredentialAnswer,
  } from '../lib/credential'
  import { errorMessage } from '../lib/ui'
  import Icon from './Icon.svelte'

  let username = ''
  let secret = ''
  let remember = true
  let reveal = false
  let error = ''
  let shownId = ''
  let now = Date.now()
  let usernameEl: HTMLInputElement | null | undefined
  let secretEl: HTMLInputElement | null | undefined
  let cancelBtn: HTMLButtonElement | null | undefined
  const timer = setInterval(() => (now = Date.now()), 250)
  onDestroy(() => clearInterval(timer))

  $: r = $credentialRequest
  // A new request (not a re-send of the same one with a new "waiting") resets the form.
  $: if (r && r.id !== shownId) {
    shownId = r.id
    username = r.username ?? ''
    secret = ''
    remember = true
    reveal = false
    error = ''
    void focusFirst()
  }
  $: if (!r) {
    shownId = ''
    secret = ''
  }
  $: answer = { username, secret, remember: remember && !!r?.remember, trust: false } as CredentialAnswer
  $: ok = r ? canSubmit(r, answer) : false
  $: left = r ? countdown(r.deadline, now) : null

  // The first empty field; the host-key dialog starts on its safe default.
  async function focusFirst() {
    await tick()
    if (r?.kind === 'ssh-hostkey') cancelBtn?.focus()
    else if (r?.kind === 'https' && username.trim() === '') usernameEl?.focus()
    else secretEl?.focus()
  }

  let sending = false

  async function send(a: CredentialAnswer) {
    if (!r || sending) return
    const id = r.id
    // Never keep the secret in component state once it is sent: the copy in
    // `a` is all that is left, and a second submit while this one is in flight
    // is ignored.
    sending = true
    secret = ''
    error = ''
    try {
      await api.answerCredential(id, a)
      onClosed(id)
    } catch (e) {
      error = errorMessage(e)
    } finally {
      sending = false
    }
  }

  function submit() {
    if (ok && !sending) void send({ ...answer })
  }

  async function cancel() {
    if (!r) return
    const id = r.id
    secret = ''
    onClosed(id)
    try { await api.cancelCredential(id) } catch { /* already gone */ }
  }

  // Capture phase on the window, so Escape never reaches the clone dialog or a
  // confirm behind this one. Enter is handled only for the host-key dialog,
  // where it means Cancel; in the forms the browser's own submit applies.
  function onKey(e: KeyboardEvent) {
    if (!r) return
    if (e.key === 'Escape' || (e.key === 'Enter' && r.kind === 'ssh-hostkey')) {
      e.preventDefault()
      e.stopPropagation()
      if (!e.repeat) void cancel()
    }
  }
</script>

<svelte:window on:keydown|capture={onKey} />

{#if r}
  <div class="backdrop" role="presentation">
    <div class="dialog" role="dialog" aria-modal="true" aria-label={title(r)}>
      <h3>{title(r)}</h3>
      <p class="context ellipsis" title={r.context}>{r.context}{#if r.fromAI} · Requested by the AI chat{/if}</p>

      {#if r.kind === 'ssh-hostkey'}
        <p>CommitTree has never connected to <strong>{r.host}</strong>. Check the fingerprint with the server's owner before trusting it.</p>
        <p class="mono">{r.keyType} {r.fingerprint}</p>
        <div class="buttons">
          {#if r.waiting > 0}<span class="left">{r.waiting} more waiting</span>{/if}
          {#if left}<span class="left" aria-live="polite">{left}</span>{/if}
          <button type="button" class="btn" bind:this={cancelBtn} on:click={cancel}>Cancel</button>
          <button type="button" class="btn primary" on:click={() => send({ username: '', secret: '', remember: false, trust: true })}>Trust and connect</button>
        </div>
      {:else}
        <form on:submit|preventDefault={submit}>
          {#if r.kind === 'ssh-passphrase'}<p class="mono ellipsis" title={r.keyPath}>{r.keyPath}</p>{/if}
          {#if r.kind === 'ssh-other'}<p class="prompt">{r.prompt}</p>{/if}
          {#if r.retry}<p class="problem" role="alert">That was not accepted. Try again.</p>{/if}
          {#if r.kind === 'https'}
            <label>Username
              <input bind:this={usernameEl} spellcheck="false" autocomplete="off" bind:value={username} />
            </label>
          {/if}
          <label>{secretLabel(r.kind)}
            <span class="secret">
              <input bind:this={secretEl} type={reveal ? 'text' : 'password'} spellcheck="false" autocomplete="off" bind:value={secret} />
              <button type="button" class="icon-btn" aria-label={reveal ? 'Hide' : 'Show'} aria-pressed={reveal} on:click={() => (reveal = !reveal)}>
                <Icon name={reveal ? 'eye-off' : 'eye'} size={14} />
              </button>
            </span>
          </label>
          {#if r.kind === 'https' && tokenHint(r.host)}<p class="hint">{tokenHint(r.host)}</p>{/if}
          {#if r.kind === 'ssh-passphrase'}<p class="hint">To stop being asked, add the key to ssh-agent (on macOS <code>UseKeychain yes</code> in ~/.ssh/config).</p>{/if}
          {#if r.kind === 'https'}
            {#if r.remember}
              <label class="check"><input type="checkbox" bind:checked={remember} /> Remember in {r.remember.name}</label>
            {:else}
              <p class="hint">Not saved: no credential helper is set up.</p>
            {/if}
          {/if}
          {#if error}<p class="problem" role="alert">{error}</p>{/if}
          <div class="buttons">
            {#if r.waiting > 0}<span class="left">{r.waiting} more waiting</span>{/if}
            {#if left}<span class="left" aria-live="polite">{left}</span>{/if}
            <button type="button" class="btn" on:click={cancel}>Cancel</button>
            <button type="submit" class="btn primary" disabled={!ok || sending}>Continue</button>
          </div>
        </form>
      {/if}
    </div>
  </div>
{/if}

<style>
  .backdrop { position: fixed; inset: 0; z-index: 50; display: grid; place-items: center; background: rgba(0, 0, 0, 0.25); }
  .dialog {
    width: min(420px, 92vw); padding: 18px 20px 16px; background: var(--surface);
    border: 1px solid var(--border); border-radius: 12px; box-shadow: var(--shadow);
    display: flex; flex-direction: column; gap: 12px;
  }
  form { display: flex; flex-direction: column; gap: 12px; }
  h3 { margin: 0; font-size: 14px; font-weight: 600; }
  p { margin: 0; line-height: 1.5; }
  .context { color: var(--muted); font-size: 12px; }
  label { display: flex; flex-direction: column; gap: 5px; color: var(--muted); font-size: 12px; }
  label input { font-size: 13px; }
  label.check { flex-direction: row; align-items: center; gap: 6px; color: var(--text); }
  .secret { display: flex; align-items: center; gap: 6px; }
  .secret input { flex: 1; min-width: 0; }
  .mono { font-family: var(--mono); font-size: 12px; overflow-wrap: anywhere; }
  .prompt { white-space: pre-line; }
  .hint { color: var(--muted); font-size: 12px; }
  .problem { color: var(--danger); font-size: 12px; }
  .buttons { display: flex; justify-content: flex-end; align-items: center; gap: 8px; margin-top: 4px; }
  .left { margin-right: auto; color: var(--muted); font-size: 12px; }
</style>
