# Update banner and in-app update — design

Date: 2026-10-27. Owner's request: "podemos implementar un banner de update,
para que se sepa que hay actualizaciones", and then "si podemos tener un
autoupdate que le baje la versión y se la instale, mejor".

## Intent

CommitTree is released by hand on GitHub (`josfh2005/CommitTree`). Today
nobody using the app learns that a new release exists unless they go and
look. The owner wants the app to tell its user that a newer version is out
and, where it can, download and install it with one click.

Success: a user of 0.N opens CommitTree after 0.N+1 is published, sees a card
saying so, clicks "Update and restart", and a moment later is running 0.N+1,
without opening a browser or the Finder.

## Goals

- Learn the latest release from a small manifest file published with every
  release (owner: "usemos un archivo que da la última versión en GitHub").
- Tell the user, in the app, when that release is newer than the one running.
- Download, verify and install it in place, then relaunch, on macOS (arm64 and
  Intel) and Linux (amd64).
- Fall back to opening the release page whenever installing in place is not
  possible.
- A Settings switch to turn automatic checks off, plus a manual "Check now".

## Non-goals

- Windows (CommitTree does not build there).
- Silent installs. Nothing is installed until the user clicks.
- Delta updates, update channels, rollback to an older version.
- Notarization or a Developer ID. Builds stay ad-hoc signed.
- Announcing pre-releases or drafts.
- "Skip this version". The owner's rules (below) always show the update in
  the status bar, so skipping has no place.

## Anti-goals (failure even if it works)

- A failed or interrupted update that leaves the user without a working
  CommitTree. The installed app is never touched until the new one has been
  downloaded, verified and unpacked completely.
- Nagging. The card must not pop up on every focus.
- Hitting the GitHub API's 60-requests-per-hour limit, or sending any
  credential, token or identifying data with the check.
- Restarting the app under a running git command, a streaming chat answer or
  unsaved typed work without asking.

## Owner's decisions

These were settled with the owner in the brainstorm; the builder keeps them.

1. **Where it shows: a floating card + a status-bar pill** (mockups B + C).
   - The **card** sits at the bottom right of the window, above the status
     bar. It is styled like a toast, with an accent-coloured left edge, and it
     does not go away on its own. Content: "CommitTree 0.10.0 is available",
     a muted "You have 0.9.0", the buttons **Update and restart** (primary)
     and **What's new** (opens the release page in the browser), and ✕.
   - The **pill** sits at the right end of the status bar: accent-coloured,
     with "⬆ Update" (wording left to the builder). Clicking it opens the card
     again.
2. **When the card shows:**
   - **Always at app start** when a newer version is known.
   - **On window focus, each version at most once.** While the app runs, the
     first time a check run on focus finds a version that has not been shown
     yet in this run, the card pops up. Later focuses for that same version do
     not pop it up again. A newer version (0.11.0 after 0.10.0) counts as new
     and pops up once. The card shown at start counts as that version's one
     showing for the run.
   - ✕ only closes the card. No snooze, no skip.
3. **The pill is always there** while a newer version is known, whatever
   happened to the card.
4. **Clicking "Update and restart" installs it** (download, verify, replace,
   relaunch). It is not "open the release page", except as the fallback.
5. **Settings switch**: "Check for updates automatically", on by default.

## Calls made by Claude (approved in the brainstorm, the owner can still reject them)

- **The manifest is `latest.json`, attached to every GitHub release**, and read
  from `https://github.com/josfh2005/CommitTree/releases/latest/download/latest.json`.
  That URL always redirects to the newest non-draft, non-prerelease release's
  asset, and it is not an API call, so the 60/h limit does not apply.
- **How often:** at start, and on window focus at most once every 6 hours
  since the last successful or failed check. A failed check (no network,
  404, bad JSON) says nothing and is retried at the next opportunity, subject
  to the same 6-hour spacing.
- **Check state lives in memory only:** the last-check time and the versions
  already shown this run reset at every launch; nothing is persisted. The
  start check is always a fresh fetch, so a start check that fails shows no
  card and no pill until a later check succeeds.
- **"Check now" counts as a showing:** when Check now opens the card for a
  version, a later focus does not pop it up again in that run.
- **Two-phase install API:** Go downloads, verifies and unpacks (`Prepare`,
  reporting progress by Wails events, cancellable), the frontend then asks
  the restart confirmation from its own state, and calls Go to swap and
  relaunch (`Apply`). The card and pill decision is a frontend pure function.
- **Highlight in the sidebar too:** the sidebar's version label turns accent
  coloured and reads "v0.9.0 → 0.10.0" while an update is known. The owner may
  drop this.
- **Progress and errors inside the card:** after the click, the card shows the
  download's progress ("Downloading 42 %"), then "Installing…", then the app
  restarts. On failure the card says what went wrong in one line ("Couldn't
  download the update", "The download didn't match the expected checksum",
  "CommitTree can't write to /Applications") and offers **Open release page**.
  A **Cancel** button is there while it downloads.
- **Confirm before restarting when work is in flight:** if a git operation or
  clone is running (the status bar's task list), the chat is answering, or the
  commit message box holds unsaved text, a confirmation dialog lists the reason
  ("A push is running…") and asks "Restart anyway?" before relaunching. The
  download and verification may go ahead first; the question is asked before
  the swap and relaunch.
- **No in-place install** when the running build is a development build
  (`wails dev`, Go build tag `dev`), when the running executable is not inside
  a `CommitTree.app` bundle on macOS, or when its folder is not writable. In
  those cases the primary button reads **Download** and opens the release page.
  The card and the pill still show.
- **Test hook:** an environment variable `COMMITTREE_UPDATE_MANIFEST` replaces
  the manifest URL (any `http(s)://` or `file://`). While it is set, each
  asset's download URL is `<name>` resolved against the manifest URL's own
  folder (so a local `latest.json` next to a local zip works), and the
  download host restriction below is lifted. This is how the feature gets
  tried before a release carries `latest.json`. v0.9.0 has no manifest, so the
  first real announcement will be of the release *after* the one that ships
  this feature.

## Manifest format

```json
{
  "version": "0.10.0",
  "url": "https://github.com/josfh2005/CommitTree/releases/tag/v0.10.0",
  "assets": {
    "darwin-arm64": { "name": "CommitTree-macos-arm64.zip",   "sha256": "<hex>" },
    "darwin-amd64": { "name": "CommitTree-macos-amd64.zip",   "sha256": "<hex>" },
    "linux-amd64":  { "name": "CommitTree-linux-amd64.tar.gz", "sha256": "<hex>" }
  }
}
```

- `version` is plain `MAJOR.MINOR.PATCH`, no leading `v`. Comparison is
  numeric per part. A manifest whose version does not parse is ignored.
- The download URL is
  `https://github.com/josfh2005/CommitTree/releases/download/v<version>/<name>`.
  It is built by the app, not read from the manifest. Redirects are followed
  only to `github.com` and `*.githubusercontent.com` over https.
- The asset key is `GOOS-GOARCH` of the running binary. An Intel build running
  under Rosetta is `darwin-amd64` and gets the Intel asset. A missing key means
  no in-place install for this platform; the card still shows with
  **Download**.
- `url` must be an https URL on `github.com`, or it is replaced by the
  release-tag URL built from `version`.
- Size caps: manifest ≤ 64 KiB; download ≤ 200 MiB.

## The running version

The version's single source stays `wails.json` `info.productVersion`. The
frontend already gets it as `__APP_VERSION__`. The Go side needs it too, to
compare and to refuse a "downgrade"; the builder chooses how. Embedding
`wails.json` in `main.go`, or passing it in from the frontend once at start,
are both fine. It must not become a second hand-edited copy.

## Installing in place

All of this happens in a new Go package (e.g. `internal/update`), with its own
small `http.Client` (timeouts: 15 s for the manifest; a stall timeout rather
than a total one for the download). `internal/hosting`'s `Doer` is the wrong
fit: it pins every request and redirect to one API host and carries auth, and
GitHub release downloads redirect to a different host and need no auth.

1. Download the asset into a fresh temp directory and verify its SHA-256
   against the manifest. A mismatch stops everything.
2. Unpack into that temp directory. On macOS use `/usr/bin/ditto -x -k`, which
   keeps the bundle's attributes and symlinks. On Linux extract the single
   `CommitTree` binary from the tar.gz in Go. Paths that escape the directory
   are refused. On macOS also check that the unpacked
   `CommitTree.app/Contents/Info.plist` has
   `CFBundleShortVersionString == manifest.version`.
3. Ask the restart confirmation if needed (see above).
4. Swap:
   - **macOS:** the target is the bundle containing the running executable
     (`os.Executable()` → symlinks resolved → up to `….app`). Rename the old
     bundle aside in the same folder (`CommitTree.app` →
     `.CommitTree.app.old-<pid>`), move the new bundle in (same volume: a
     rename; otherwise `ditto` then rename), then delete the old one. If moving
     the new one in fails, rename the old one back.
   - **Linux:** write the new binary next to the running one, `chmod 0755`, and
     `rename` it over the old one (atomic on the same filesystem).
5. Relaunch and quit. On macOS start the new bundle with `/usr/bin/open -n
   <path>` from a detached `sh -c 'sleep 1; open -n …'` (or an equivalent that
   waits for this process to exit), then `runtime.Quit`. On Linux exec the new
   binary detached with the same args, then quit.

A file downloaded by the app itself carries no `com.apple.quarantine`
attribute (`LSFileQuarantineEnabled` is not set), so Gatekeeper does not block
the ad-hoc signed bundle. Known effect: a new ad-hoc signature means macOS may
ask once more for keychain access to the stored API keys and tokens. That
already happens with a manual reinstall. The spec mentions it; nothing else
needs to be done.

## Settings

Settings → General gets a section **Updates**:

- A checkbox **Check for updates automatically** (on by default; a `persisted`
  store like the notify switches).
- A line "CommitTree 0.9.0" and a **Check now** button. Check now ignores the
  6-hour spacing and the switch. Its result shows on the same line ("You're up
  to date", "0.10.0 is available", or a one-line failure). When it finds a
  newer version, the card opens and the pill appears as well.

With the switch off: no automatic checks at start or on focus, so no card and
no pill unless Check now found something.

## Release process change

A new target, `make release-manifest`, writes `build/bin/latest.json` from the
three assets in `build/bin/` and the version in `wails.json`. It only reads
and hashes those files; it does not build or zip anything, and it fails if
one is missing. The three files keep coming from today's hand recipe: the
Intel build plus `ditto` zip, `make build` plus `ditto` zip, and `make
build-linux`. `docs/development.md`'s release steps add: run it after the
three builds and attach `latest.json` to the release with the other assets.
`.github/workflows/release.yml` (never run, and it builds one universal macOS
zip with a different name) is left untouched. Aligning it is out of scope.

## Spec documents to keep in sync

- New `docs/spec/15-updates.md`: the check, the manifest, card and pill rules,
  install, fallbacks, Settings, the test variable, and the keychain note.
- `docs/spec/13-status-bar.md`: the pill's place in the bar's layout.
- `docs/spec/01-repositories-and-sidebar.md`: the version label highlight.
- `docs/spec/README.md`: list the new document.
- `docs/development.md`: the release step.

## Testing

- Go unit tests: version compare, manifest parse and validation (bad version,
  bad url, missing asset), host allow-list on redirects, size caps, checksum
  mismatch, tar/zip path escape, Linux swap in a temp dir, macOS bundle-path
  discovery from an executable path, rename-back on a failed swap.
- Frontend (vitest): the card/pill rules as a pure function. Inputs: at start
  or on focus, versions already shown this run, switch on or off, last check
  time. Outputs: check or not, show the card or not, show the pill or not.
- Manual, by the owner: `make build`, copy the app somewhere writable, serve a
  `latest.json` claiming `99.0.0` that points to a zip of a build with a
  changed version, set `COMMITTREE_UPDATE_MANIFEST`, open, click Update and
  restart.

## Left to the builder

Exact wording and icon of the pill; the card's layout details within the
mockup; how Go learns the version; component and file names; how the frontend
learns about download progress (Wails events are the norm here); the restart
helper's exact form; whether the confirmation uses the existing dialog host.
