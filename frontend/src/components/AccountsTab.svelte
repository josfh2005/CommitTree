<script lang="ts">
  import { onMount } from 'svelte'
  import { BrowserOpenURL } from '../../wailsjs/runtime/runtime'
  import { canSaveBitbucket, canSaveGitHub, githubSourceFor, type GitHubSource } from '../lib/accounts'
  import { api } from '../lib/api'
  import type { HostingAccount, HostName } from '../lib/types'
  import { loadJira } from '../lib/tickets'
  import { errorMessage } from '../lib/ui'

  const BITBUCKET_TOKENS_URL = 'https://id.atlassian.com/manage-profile/security/api-tokens'
  const GITHUB_TOKENS_URL = 'https://github.com/settings/personal-access-tokens'
  // Scopes an Atlassian API token needs for the read-only integration.
  const BITBUCKET_SCOPES = ['read:user:bitbucket', 'read:repository:bitbucket', 'read:pullrequest:bitbucket', 'read:pipeline:bitbucket']

  let bitbucket: HostingAccount | null = null
  let github: HostingAccount | null = null

  let bbEmail = ''
  let bbToken = ''
  let ghSource: GitHubSource = 'token'
  let ghToken = ''

  // Why the last Save or Remove of a block failed. The fields keep what was typed.
  let bbError = ''
  let ghError = ''
  let busy: HostName | '' = ''

  // Jira: where ticket keys in commit and pull request text link to.
  let jiraSite = ''
  let jiraKeys = ''
  let jiraError = ''
  let jiraBusy = false

  // The radio follows the saved account, else offers the CLI when it exists.
  function applyGitHub(acc: HostingAccount) {
    github = acc
    ghSource = githubSourceFor(acc)
  }

  function applyBitbucket(acc: HostingAccount) {
    bitbucket = acc
    if (acc.email) bbEmail = acc.email
  }

  async function load() {
    try {
      for (const acc of await api.getHostingAccounts()) {
        if (acc.host === 'bitbucket') applyBitbucket(acc)
        else applyGitHub(acc)
      }
    } catch (e) {
      bbError = errorMessage(e)
    }
  }
  async function loadJiraFields() {
    try {
      const j = await api.getJiraSettings()
      jiraSite = j.site
      jiraKeys = (j.keys ?? []).join(', ')
    } catch (e) {
      jiraError = errorMessage(e)
    }
  }
  onMount(() => {
    load()
    loadJiraFields()
  })

  async function saveJira() {
    jiraBusy = true
    jiraError = ''
    try {
      const j = await api.saveJiraSettings(jiraSite, jiraKeys)
      // The fields show what was kept: trimmed site, upper-case keys.
      jiraSite = j.site
      jiraKeys = (j.keys ?? []).join(', ')
      await loadJira()
    } catch (e) {
      jiraError = errorMessage(e)
    } finally {
      jiraBusy = false
    }
  }

  async function saveBitbucket() {
    busy = 'bitbucket'
    bbError = ''
    try {
      applyBitbucket(await api.saveBitbucketAccount(bbEmail, bbToken))
      bbToken = '' // never echo a token back
    } catch (e) {
      bbError = errorMessage(e)
    } finally {
      busy = ''
    }
  }

  async function saveGitHub() {
    busy = 'github'
    ghError = ''
    try {
      applyGitHub(await api.saveGitHubAccount(ghSource, ghSource === 'token' ? ghToken : ''))
      ghToken = ''
    } catch (e) {
      ghError = errorMessage(e)
    } finally {
      busy = ''
    }
  }

  async function remove(host: HostName) {
    busy = host
    if (host === 'bitbucket') bbError = ''
    else ghError = ''
    try {
      await api.removeHostingAccount(host)
      if (host === 'bitbucket') {
        bbEmail = ''
        bbToken = ''
      } else {
        ghToken = ''
      }
      await load()
    } catch (e) {
      if (host === 'bitbucket') bbError = errorMessage(e)
      else ghError = errorMessage(e)
    } finally {
      busy = ''
    }
  }

  // The backend refuses a blank email or token too; the button just says so early.
  $: bbCanSave = canSaveBitbucket(bbEmail, bbToken, busy)
  $: ghCanSave = canSaveGitHub(ghSource, ghToken, busy)
</script>

<section class="bitbucket">
  <h4>Bitbucket</h4>
  <label>
    <span>Atlassian email</span>
    <input type="email" autocomplete="off" bind:value={bbEmail} />
  </label>
  <label>
    <span>API token</span>
    <input type="password" autocomplete="off" placeholder={bitbucket?.connected ? 'Paste a new token to replace it' : ''} bind:value={bbToken} />
  </label>
  <p class="hint">
    Create an API token with scopes at
    <a href={BITBUCKET_TOKENS_URL} on:click|preventDefault={() => BrowserOpenURL(BITBUCKET_TOKENS_URL)}>id.atlassian.com</a>
    and give it read access to: {#each BITBUCKET_SCOPES as s, i}<code>{s}</code>{i < BITBUCKET_SCOPES.length - 1 ? ', ' : '.'}{/each}
  </p>
  <div class="actions">
    <button class="btn primary" disabled={!bbCanSave} on:click={saveBitbucket}>Save</button>
    {#if bitbucket?.connected}
      <button class="btn" disabled={busy !== ''} on:click={() => remove('bitbucket')}>Remove</button>
    {/if}
  </div>
  {#if bitbucket?.rejected}
    <p class="warn status">Bitbucket rejected the token</p>
  {:else if bitbucket?.connected}
    <p class="ok status">Connected as {bitbucket.name}</p>
  {/if}
  {#if bbError}<p class="warn error">{bbError}</p>{/if}
</section>

<section class="github">
  <h4>GitHub</h4>
  <div class="choices" role="radiogroup" aria-label="GitHub sign-in">
    <label class="choice" class:off={!github?.ghAvailable}>
      <input type="radio" name="github-source" value="gh" bind:group={ghSource} disabled={!github?.ghAvailable} />
      <span>Use GitHub CLI login</span>
      {#if github && !github.ghAvailable}<span class="hint">gh not found</span>{/if}
    </label>
    <label class="choice">
      <input type="radio" name="github-source" value="token" bind:group={ghSource} />
      <span>Personal access token</span>
    </label>
  </div>
  {#if ghSource === 'token'}
    <label>
      <span>Token</span>
      <input type="password" autocomplete="off" placeholder={github?.connected && github.source === 'token' ? 'Paste a new token to replace it' : ''} bind:value={ghToken} />
    </label>
    <p class="hint">
      Create a token at
      <a href={GITHUB_TOKENS_URL} on:click|preventDefault={() => BrowserOpenURL(GITHUB_TOKENS_URL)}>github.com/settings/personal-access-tokens</a>.
    </p>
  {/if}
  <div class="actions">
    <button class="btn primary" disabled={!ghCanSave} on:click={saveGitHub}>Save</button>
    {#if github?.connected}
      <button class="btn" disabled={busy !== ''} on:click={() => remove('github')}>Remove</button>
    {/if}
  </div>
  {#if github?.rejected}
    <p class="warn status">GitHub rejected the token</p>
  {:else if github?.connected}
    <p class="ok status">Connected as {github.name}</p>
  {/if}
  {#if ghError}<p class="warn error">{ghError}</p>{/if}
</section>

<section class="jira">
  <h4>Jira</h4>
  <label>
    <span>Site</span>
    <input type="url" autocomplete="off" placeholder="https://your-team.atlassian.net" bind:value={jiraSite} />
  </label>
  <label>
    <span>Project keys</span>
    <input type="text" autocomplete="off" placeholder="NEXO, SCL" bind:value={jiraKeys} />
  </label>
  <p class="hint">Ticket keys of these projects, like <code>NEXO-123</code>, become links to the ticket in commit details and pull requests. Both fields are needed.</p>
  <div class="actions">
    <button class="btn primary" disabled={jiraBusy} on:click={saveJira}>Save</button>
  </div>
  {#if jiraError}<p class="warn error">{jiraError}</p>{/if}
</section>

<style>
  section { display: flex; flex-direction: column; gap: 8px; padding: 14px 0; border-bottom: 1px solid var(--border); }
  section:last-of-type { border-bottom: 0; }
  h4 { margin: 0 0 8px; font-size: 12px; font-weight: 500; color: var(--muted); }
  label { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: var(--muted); }
  .choices { display: flex; flex-direction: column; gap: 6px; }
  label.choice { flex-direction: row; align-items: center; gap: 6px; color: var(--text); font-size: 13px; }
  label.choice.off { color: var(--muted); }
  .actions { display: flex; gap: 6px; }
  p { margin: 0; }
  .hint { font-size: 12px; color: var(--muted); }
  .hint code { font-size: 11px; }
  .warn { font-size: 12px; color: var(--danger); }
  .ok { font-size: 12px; color: var(--ok); }
  a { color: var(--accent); cursor: pointer; }
</style>
