# In-app credential prompts — design

Date: 2026-10-13. Status: approved in chat, awaiting written-spec review.

## Goal

When git needs a credential that no helper already has, CommitTree asks for
it in its own dialog instead of failing: a username and token for HTTPS, a
key passphrase, a password or an unknown host key for SSH. Today every git
command runs without a terminal (`Setsid`) and with `GIT_TERMINAL_PROMPT=0`,
so a missing credential is an "authentication failed" error, and the clone
spec told the user to set up a helper first.

## Decisions (from the brainstorm)

- **Mechanism: CommitTree as the last credential helper, plus askpass for
  ssh.** The app's own binary, relaunched in a helper mode, is appended
  after the user's credential helpers (through `GIT_CONFIG_COUNT` in the
  environment, so the command's args do not change) and set as
  `GIT_ASKPASS` and `SSH_ASKPASS` with `SSH_ASKPASS_REQUIRE=force`. It talks
  to the running app over a private Unix socket. Rejected: askpass only
  (two free-text prompts per sign-in and no signal that the credential
  worked) and a pre-flight `git credential fill` (needs the URL in advance,
  breaks with `pushurl`, `insteadOf`, submodules and redirects, useless for
  ssh).
- **Saving: a "Remember" checkbox, through git's own helpers.** Checked
  keeps the credential in the user's configured helper (osxkeychain here),
  or in the platform helper when none is configured; unchecked undoes the
  store git does on its own. CommitTree never keeps a secret itself.
- **SSH: passphrase, password and unknown host key.** The host key dialog
  shows the fingerprint and defaults to Cancel. An SSH passphrase is never
  saved (no `ssh-add`); the dialog points at `UseKeychain` / ssh-agent.
- **Timeout: 1 minute** per dialog, with a countdown in the last 20 s.
- **Which commands may ask: every network command the user starts,**
  including the AI chat's `fetch`, `push` and `pull` tools (each already
  confirmed by a card). The background fetch never asks and keeps pausing
  the remote on an auth failure.

## Behaviour

### Commands that may ask

A command may open a credential dialog only when its call site marks it.
Marked:

- toolbar **Fetch**, **Pull**, **Push**; **Push all**;
- branch menu **Fetch <remote>**, **Pull** (fast-forward of a branch not
  checked out), **Push**; deleting a remote branch;
- **Clone**;
- submodule **Update** / **Init** / **Update all** (and the `submodule
  update` they run);
- **Test** in Repository settings → Remotes (`ls-remote`);
- the AI chat's `fetch`, `push` and `pull` tools, after the user accepted
  the card.

Everything else is unmarked and behaves exactly as today. In particular:

- the **background fetch** keeps `NoPromptEnv` and is never marked, so it
  cannot ask; an auth failure still pauses that remote until a Fetch or Pull
  the user starts succeeds (which may now ask and so unpause it);
- commit signing, hooks run by a local write, git-flow's local `fetch .` and
  every read get no prompt environment.

### Who answers first

The user's setup always wins. Git asks its configured helpers in order
(osxkeychain, `store`, Git Credential Manager, …); CommitTree's helper is
last, so it is asked only when none of them returned a credential. A GCM
that shows its own window keeps doing so. ssh-agent and `IdentityFile`
keys without a passphrase never reach askpass.

A stale entry in a helper (e.g. a revoked token in the keychain) makes the
command fail once: git sends `erase` to every helper, which removes it, and
the next attempt reaches CommitTree's dialog.

### HTTPS: username and token

Git's `get` request carries `protocol`, `host`, `path` (only with
`credential.useHttpPath`) and `username` (when the URL has one). The dialog:

- **Title** "Sign in to <host>"; under it the context line (see below).
- **Username** — prefilled when git sent one; editable.
- **Token** — a password field with a show/hide toggle. For `github.com`,
  `gitlab.com` and `bitbucket.org` a hint: "Use a personal access token, not
  your account password."
- **Remember in <helper>** — shown when a helper applies (see Remember);
  checked by default.
- **Cancel** / **Continue**. Enter submits (Continue is disabled while the
  token is empty), Esc cancels.

### Remember

Which helper the checkbox names:

1. The user's helpers that apply to the URL, as `git config` reports them in
   the repository (for a clone, the system and global config git reads
   outside any repository), excluding CommitTree's own. URL-scoped helpers
   (`credential.<url>.helper`) count when they match.
   An empty `credential.helper` value resets the list, as in git. The label
   names the last one, e.g. "Remember in osxkeychain".
2. Else the platform helper, when it is installed in `git --exec-path`:
   `git-credential-osxkeychain` on macOS, `git-credential-libsecret` on
   Linux.
3. Else no checkbox, and the dialog says "Not saved: no credential helper
   is set up."

What happens when the command succeeded — git then sends `store` to every
helper, CommitTree's last, so the user's helpers have already stored:

| Remember | User helper (case 1) | CommitTree does |
|---|---|---|
| checked | yes | nothing (git stored it) |
| unchecked | yes | `git credential reject` with the same fields, run with the user's helpers only, to remove what they stored |
| checked | no, platform helper (case 2) | `git -c credential.helper= -c credential.helper=<platform> credential approve` |
| unchecked | no | nothing |

`approve` / `reject` get the fields on stdin, never in args; they run
through `gitcmd` so they show in the Commands panel like any command (their
args hold no secret, stdin is never recorded).

When the command failed because the credential was refused, git sends
`erase`; CommitTree saves nothing and the operation's error becomes "The
username or token was not accepted." The next attempt asks again.

### SSH

ssh calls askpass with its prompt as the only argument; output is in
English (`LC_ALL=C`), so the kind is recognised from the text:

- `Enter passphrase for key '<path>':` → **"SSH key passphrase"**: shows
  the key path, one hidden field. No Remember; a hint: "To stop being asked,
  add the key to ssh-agent (on macOS `UseKeychain yes` in ~/.ssh/config)."
- `<user>@<host>'s password:` → **"SSH password for <user>@<host>"**: one
  hidden field.
- `The authenticity of host '<host> (<ip>)' can't be established. … key
  fingerprint is SHA256:… Are you sure you want to continue connecting
  (yes/no/[fingerprint])?` → **"Unknown SSH host"**: host, key type and
  fingerprint (monospace), the sentence "Check the fingerprint with the
  server's owner before trusting it." Buttons **Cancel** (default; answers
  `no`) and **Trust and connect** (answers `yes`; ssh adds the key to
  `known_hosts`). A *changed* host key is refused by ssh without calling
  askpass and fails as today.
- `SSH_ASKPASS_PROMPT=none` (e.g. "Confirm user presence for key …", a FIDO
  touch): an info toast with ssh's text; the helper returns at once.
- Any other text (e.g. a keyboard-interactive "Verification code:"):
  a generic dialog **"<host> asks"** showing ssh's text verbatim with one
  hidden field.

### Context line, queue, timeout, cancel

- **Context line** under the title: the operation and where — `Push ·
  my-repo`, `Fetch origin · my-repo`, `Clone · github.com/owner/repo` (the
  redacted URL, as the clone dialog shows it) — plus "Requested by the AI
  chat" when the command's origin is the chat.
- **One dialog at a time.** Further requests queue (e.g. a clone and a push
  in another repo); the dialog shows "1 more waiting". Each request's own
  minute starts when it is shown.
- **Timeout.** A dialog unanswered for 60 s closes; the last 20 s show
  "Closes in N s". The helper answers "no credential", git fails, and the
  operation's error becomes "No credentials entered (timed out)."
- **Cancel** in the dialog: same, with "No credentials entered."
- **The command ends first** (Cancel in the status bar or Commands panel,
  the command's own time limit, a clone's stall watchdog): its dialog — or
  its queued requests — close without a message (`credential:closed`).
- **Quit** with a dialog open answers "no credential" to every pending
  request, then shuts down as today.

### Elsewhere in the app

- **Status bar.** While a task waits on a dialog its label reads "Waiting
  for credentials…".
- **Clone dialog.** Its progress view shows the same line while waiting.
- **Notification.** A request shown while the window is not focused raises
  a `problem` notification: "CommitTree needs credentials for <host>
  (<context line>)". Clicking it focuses the app. Nothing typed, and no
  username, is ever in it.
- **AI chat.** The tool result says only whether the command worked and git's
  error; the AI has no tool to see or answer a credential dialog.

## Security

A secret travels: dialog input → `App.AnswerCredential` → the in-memory
pending answer of that request → the socket → the helper's stdout → git.
It is never written to:

- the command log or the Commands panel (args, stdout and stderr are git's;
  the helper writes to git's pipe, not to ours);
- any Wails event (`credential:request` carries host, path, username and
  ssh's prompt text — never an answer), toast, notification or status bar
  text;
- the AI chat: tool results, conversation store, prompts;
- Go logs. The plan checks that `wails dev` does not log binding call
  arguments; if it does, the dev log level is lowered for that binding or
  the answer travels in a form the log does not print.

The channel:

- the socket lives in a directory created at start with `os.MkdirTemp`
  (mode `0700`), the socket itself `0600`; both are removed on shutdown;
- every marked command gets a session with a 32-byte `crypto/rand` nonce,
  compared in constant time; a request with an unknown or ended nonce is
  refused (the helper exits non-zero, git treats it as "no credential");
- a session lives only while its git command runs; the pending answer is
  kept in a `[]byte` zeroed when the session ends. Best effort: copies made
  by JSON decoding and Go strings on the way cannot be wiped.

Out of scope, documented: a process running as the same user, or a hook
git runs during a marked command, can read the nonce from the environment
and open a dialog while the command lives. The dialog always shows the host
and the operation, so the user sees what is asking.

## Architecture

### `internal/credprompt` (new)

- **`Server`** — started by the app: socket directory + listener, session
  registry (`nonce → Session`), request queue, the one-minute timer per shown
  request, `Shutdown`. Hands requests to the app through a callback and takes
  answers back by request ID.
- **`Session`** — `Begin(info)` returns a session whose `Env()` is:
  - `GIT_CONFIG_COUNT=<n+1>`, `GIT_CONFIG_KEY_<n>=credential.helper`,
    `GIT_CONFIG_VALUE_<n>=!'<exe>' credential-helper` (`n` is the inherited
    `GIT_CONFIG_COUNT`, 0 when unset; the exe path is shell-quoted, it may
    contain spaces or quotes);
  - `GIT_ASKPASS=<exe>`, `SSH_ASKPASS=<exe>`, `SSH_ASKPASS_REQUIRE=force`;
  - `COMMITREE_PROMPT_SOCK=<socket path>`, `COMMITREE_PROMPT_NONCE=<nonce>`.

  `End()` closes it: queued and shown requests of that session close with
  `credential:closed`; after a successful `store` the Remember action runs
  before the answer is zeroed.
- **Helper mode** — `RunHelper(args, stdin, stdout) int`: with
  `credential-helper get|store|erase` it speaks git's credential protocol
  (key=value lines, blank line ends); otherwise its one argument is an
  askpass prompt. It sends one request to the socket and writes the answer.
  It never writes to stderr anything it received from the app.
- **Pure helpers, unit-tested:** credential protocol parse/format, ssh prompt
  classification (`kind`, host, key path, fingerprint), env building
  (count offset, shell quoting), helper discovery for Remember.
- Unix only (`//go:build !windows`); on Windows `Begin` returns a session
  with no environment, so behaviour stays as today.

### `main.go`

First thing in `main()`, before `FixPath` and Wails: when
`COMMITREE_PROMPT_SOCK` is set, `os.Exit(credprompt.RunHelper(...))`. The
process never initialises the GUI, so no window or Dock icon appears.

### `internal/gitcmd`

`credprompt.Allow(ctx, info)` puts a session request in the context; `run`
asks for it just before starting git, appends `Env()` before the caller's
`env` (so `NoPromptEnv` still wins if a caller passes both), and ends the
session after `Wait`. Unmarked contexts change nothing. Stdin for
`approve`/`reject`: `RunEnv` gains a stdin option (not recorded).

### `internal/app`

- `credentials.go`: owns the `Server`; `AnswerCredential(id, answer)` and
  `CancelCredential(id)` bindings; emits `credential:request` (`{id, kind,
  repoId, context, host, path?, username?, prompt?, keyPath?, fingerprint?,
  keyType?, remember: {name}|null, waiting, deadline}`) and
  `credential:closed` (`{id}`); raises the notification when unfocused.
- The marked call sites listed above wrap their context with
  `credprompt.Allow`. A command whose session recorded an outcome (refused,
  cancelled, timed out) returns that outcome's message in place of git's.
- `Shutdown` stops the server after cancelling running commands.

### Frontend

- `lib/credential.ts` — pure: queue, countdown text, labels per kind, the
  token hint by host, the "Waiting for credentials…" flag per task.
- `components/CredentialDialog.svelte` — the modal, mounted once at the
  app root; listens to the two events; Enter/Esc; focus on the first empty
  field; the answer is cleared from component state as soon as it is sent.
- `StatusBar` and `CloneDialog` read the waiting flag.

## Testing

- **Unit (Go):** credential protocol round-trip; ssh prompt classification
  on the real OpenSSH texts (passphrase, password, host key with ED25519 and
  RSA, `none`, unknown); env building (no inherited count, inherited count
  2, exe path with spaces and a single quote); Remember helper discovery
  (none, osxkeychain, an empty value resetting the list, a URL-scoped
  helper); server: unknown nonce refused, ended session refused, queue
  order, timeout, `End` closing pending requests, `Shutdown`.
- **Integration (hermetic):** a local HTTP git server requiring basic auth,
  `HOME` and `XDG_CONFIG_HOME` isolated, git's system config ignored
  (`GIT_CONFIG_NOSYSTEM=1`), the test binary relaunched as the helper
  (`TestMain` dispatch) and a test answerer in place of the UI:
  - push and fetch succeed after an answer; a clone too;
  - Remember unchecked with a `store --file` helper → the file no longer
    holds the entry; checked → it does;
  - Remember checked with no helper → `approve` reaches a fake platform
    helper;
  - a wrong token → `erase`, message "The username or token was not
    accepted.";
  - timeout (short timeout in tests) and Cancel → their messages;
  - the background fetch against the same server makes **zero** requests and
    still reports the auth failure;
  - **hygiene:** a push answered with `SENTINEL-<random>` — the sentinel
    appears in no command log entry, no emitted event payload and no
    notification.
- **SSH (hermetic):** `GIT_SSH_COMMAND` points at a fake ssh that calls
  `$SSH_ASKPASS` with OpenSSH's exact prompts and checks the answers.
- **Frontend (vitest):** `lib/credential.ts`.
- **Manual (owner, `make build`):** HTTPS push to a GitHub repo with a
  fine-grained PAT, Remember on and off (check Keychain Access); clone of a
  private repo; ssh key with a passphrase; first connection to a new ssh
  host; leave a dialog for a minute; a chat `push` that asks.

## Docs

Same commit as the behaviour (keep the spec in sync):

- `docs/spec/05-remote-and-stash.md` — credentials section: who answers
  first, the dialog, Remember, ssh, timeout; background fetch unchanged.
- `docs/spec/01-repositories-and-sidebar.md` — clone: credentials are now
  asked for; drop "set up a helper first".
- `docs/spec/06-ai.md` — chat network tools may open the dialog; the AI
  never sees it.
- `docs/spec/07-conventions-and-constraints.md` — the "git never prompts"
  rule becomes "only marked network commands prompt, through CommitTree".
- `docs/spec/11-notifications.md`, `13-status-bar.md`,
  `12-repository-settings.md` (Test) — their lines above.

## Out of scope

- Saving SSH passphrases (`ssh-add`, `UseKeychain`).
- OAuth / device-flow sign-in to GitHub and the like.
- Managing saved credentials from the app (list, delete).
- A changed ssh host key (ssh refuses it before any prompt).
- Windows.
- Re-running the operation automatically after a refused credential.
