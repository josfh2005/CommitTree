# Updates

CommitTree finds out by itself whether a newer release exists, so the user
does not have to watch the project's release page. This document covers where
the latest version comes from, how it is checked and how the new version is
downloaded. Showing it (the banner, the card, Check now) and installing it are
described in later sections.

## Where the latest version comes from

Every GitHub release carries a small file, `latest.json`, the **manifest**. The
application reads it from

`https://github.com/josfh2005/CommitTree/releases/latest/download/latest.json`

GitHub redirects that address to the manifest of the newest release that is
neither a draft nor a pre-release. It is a plain file download, not a call to
the GitHub API, so the API's request limit does not apply to it.

```json
{
  "version": "0.10.0",
  "url": "https://github.com/josfh2005/CommitTree/releases/tag/v0.10.0",
  "assets": {
    "darwin-arm64": { "name": "CommitTree-macos-arm64.zip",    "sha256": "<hex>" },
    "darwin-amd64": { "name": "CommitTree-macos-amd64.zip",    "sha256": "<hex>" },
    "linux-amd64":  { "name": "CommitTree-linux-amd64.tar.gz", "sha256": "<hex>" }
  }
}
```

- **`version`** is `MAJOR.MINOR.PATCH`: three groups of digits, no leading
  `v`, no suffix such as `-beta`. Versions are compared part by part as
  numbers, so 0.10.0 is newer than 0.9.0. A manifest whose version does not
  parse is ignored.
- **`url`** is the release page shown to the user. It is kept only when it is
  an https address on `github.com`; anything else is replaced by
  `https://github.com/josfh2005/CommitTree/releases/tag/v<version>`.
- **`assets`** is keyed by `GOOS-GOARCH` of the running application. An Intel
  build running under Rosetta is `darwin-amd64` and gets the Intel asset. When
  the key for this platform is missing the manifest is still valid: the
  release is known but has no file to install in place. An asset's `name` must
  not be empty, contain `/`, `\` or `..`; its `sha256` must be 64 hexadecimal
  characters (it is stored in lower case). Only the entry for the running
  platform is validated; if it breaks these rules the manifest is ignored,
  while a faulty entry for another platform does not matter.
- **The download address is never read from the manifest.** The application
  builds it: `https://github.com/josfh2005/CommitTree/releases/download/v<version>/<name>`.
- **Size caps:** the manifest is at most 64 KiB; anything larger is an error.

### Where the manifest is allowed to come from

By default the manifest address must be https on `github.com`, and every
redirect on the way, including the one from `latest/download`, must land on
https with the host `github.com` or one ending in `.githubusercontent.com`
(only the default port). A redirect anywhere else stops the request.

For testing, the environment variable `COMMITTREE_UPDATE_MANIFEST` replaces
the manifest address with an `http://`, `https://` or `file://` URL. While it
is set the host allow-list is off, and the download address of an asset is its
`name` resolved against the folder of the manifest URL (so a manifest at
`file:///tmp/x/latest.json` points at `file:///tmp/x/<name>`). Without the
variable a `file://` manifest is refused.

## Checking

A check is one GET of the manifest. It has a 15 second limit; a request
cancelled earlier by the caller ends with the cancellation. The response must
be status 200; any other status, a network error, a body over the cap or an
invalid manifest is a failed check, which says nothing to the user by itself.

**No token or identifying data is sent.** The request has no credentials, no
cookies set by the application and no identifier of the user, the machine or
the installed version beyond what any HTTP request to GitHub carries.

## Downloading

Downloading fetches the asset of the release for this platform (see
*Where the latest version comes from*) into a folder the caller chooses, as
`<folder>/<name>`.

- **No asset, no download.** A release without an asset for this platform
  fails with "release has no download for this platform"; the caller offers
  the release page instead.
- **Same address rules as the manifest.** Without the test variable the asset
  address must be https on an allowed host and every redirect (GitHub sends
  release downloads on to `*.githubusercontent.com`) is checked the same way;
  anything else stops the request. With `COMMITTREE_UPDATE_MANIFEST` set,
  `http(s)://` and `file://` assets are accepted.
- **Checksum.** The SHA-256 of the bytes received is compared with the one in
  the manifest (hex, upper or lower case). A different checksum fails the
  download with "downloaded file does not match its checksum".
- **Size cap: 200 MiB.** A declared size over the cap is refused before any
  byte is read; a body that turns out larger, with or without a declared
  size, is cut off and fails.
- **No total time limit, only a stall rule.** A large file on a slow line may
  take as long as it needs, but the download fails with "download stalled"
  when 30 seconds pass without receiving a single byte (including while
  waiting for the server to answer). Cancelling ends it with the
  cancellation.
- **Progress** is reported as bytes received and the total, or -1 when the
  server does not say.
- **Clean-up.** Bytes are written to a temporary `.part` file in the folder.
  Only after the size cap and the checksum pass is it renamed to its final
  name, so the final file always has a verified content. On any failure or
  cancellation the temporary file is removed and nothing is left behind.

### Unpacking

- **macOS:** the zip is extracted with the system `ditto`. The result must
  contain `CommitTree.app` at its top level (a directory, not a link);
  otherwise the archive is rejected. The bundle's `Info.plist` is then read
  (`CFBundleShortVersionString`) and must equal the version announced by the
  manifest; otherwise the update is refused with "unpacked app has a
  different version than announced", so a release cannot be swapped for
  another build.
- **Linux:** the tar.gz is read entry by entry and only one entry is
  extracted: a regular file whose cleaned name is exactly `CommitTree`
  (a leading `./` is fine). Every other entry, links included, is ignored, so
  no path in the archive can write outside the folder; a `CommitTree` entry
  that is not a regular file rejects the archive, as does an archive without
  the file. The file is written with mode 0755 and is capped at 200 MiB.
- An archive that cannot be read, or lacks the expected content, fails with
  "update archive is not valid".

## Installing in place

Once an update is downloaded and unpacked (see *Unpacking*) it replaces the
running application where it stands, then CommitTree starts again.

- **What is replaced.** On macOS, the app bundle that holds the running
  executable: the nearest folder named `*.app` on the path to the executable
  (symlinks resolved) that contains it under `Contents/MacOS/`. On Linux, the
  running binary itself.
- **Swap on macOS.** The old bundle is renamed aside in the same folder to
  `.CommitTree.app.old-<pid>` (a leftover of that name from an earlier attempt
  is deleted first), then the new bundle is moved in. When the new
  bundle sits on another volume, so a rename is impossible, it is first copied
  next to the old one with the system `ditto` and the copy is renamed. If
  anything fails, the old bundle is renamed back and the update fails with the
  application as it was. Only after success is the old bundle deleted (a
  failure to delete it is ignored).
- **Swap on Linux.** The new binary is copied next to the running one as
  `.CommitTree.new-<pid>`, given mode 0755, flushed to disk and renamed over the
  old binary (atomic on one filesystem). A failure removes the copy and leaves the old
  binary untouched.
- **Restart.** CommitTree starts a detached shell (`/bin/sh`, in its own
  session) that waits until this process has exited (`kill -0 <pid>`), then
  starts the application again (it waits at most 30 seconds for this process to
  exit, then starts it anyway): on macOS with `/usr/bin/open -n '<bundle>'`,
  on Linux with `exec '<binary>'`. Single quotes in the path are escaped. The
  caller then quits, so the new instance opens once the old one is gone.
- **Quarantine and signature.** A file the application downloads itself does
  not carry the `com.apple.quarantine` attribute, so Gatekeeper does not block
  the ad-hoc signed bundle. The new version has a new ad-hoc signature, so
  macOS may ask once more for keychain access to the stored API keys and
  tokens; this already happens with a manual reinstall and nothing is done
  about it.

## When it cannot install

Before offering "Update and restart", CommitTree checks whether it can
install in place. If not, the primary button is "Download" and opens the
release page instead. The reason is one of these, checked in this order:

1. `development build`: the Wails dev build (compiled with `-tags dev`).
2. `not running from an app bundle`: on macOS, the executable is not inside a
   `*.app/Contents/MacOS/`.
3. `no download for this platform`: the release has no asset for this
   platform.
4. `running from a temporary location — move CommitTree to Applications
   first`: macOS runs an app opened straight from a downloaded disk image or
   archive from a read-only randomised copy (a path containing
   `/AppTranslocation/`), which cannot be replaced.
5. `CommitTree can't write to <folder>`: the folder holding the bundle (macOS)
   or the binary (Linux) rejects the creation of a file. This is probed by
   creating and removing a temporary `.committree-write-*` file there.

With none of these, installing is allowed and there is no reason.

## Installing

The frontend drives an install in two steps, each a call into the application,
so that the long part (download) can be shown and cancelled and the short part
(swap) happens only when the user's click asks for it.

1. **Prepare** (`PrepareUpdate`) fetches the manifest again, downloads and
   verifies the asset, unpacks it and keeps the result for the next step. It
   blocks until done and reports `update:progress` events: phase `download`
   with the bytes received and the total (-1 when unknown), at most every
   100 ms plus a last one with the final numbers; then phase `install` (while
   unpacking). It refuses to start when the release is not newer than the
   running version ("CommitTree is already up to date") or when the update
   cannot be installed in place (see *When it cannot install*). Each attempt
   works in its own new, empty temporary folder, deleted when the attempt
   fails or is cancelled, when a later Prepare replaces the staged update, and
   when CommitTree quits without applying it. Only one Prepare or Apply runs
   at a time; another call fails with "an update is already in progress".
2. **Apply** (`ApplyUpdate`) swaps the staged update in for the running
   application, starts the new one (see *Installing in place*) and quits
   CommitTree. Before swapping it checks again that the staged version is newer
   than the running one and that installing is still possible. Without a staged
   update it fails with "no update has been downloaded". If the swap fails the
   old version is untouched, the staged update is kept so the user can try
   again, and CommitTree does not quit. If the swap worked but the restart
   could not be started, CommitTree stays open and says so.

**Cancelling** (`CancelUpdate`) stops a running Prepare, which ends with a
cancellation, not an error message; it does nothing when none is running.

### Errors as the user reads them

| Cause | Text |
| --- | --- |
| Checksum differs from the manifest | The download didn't match the expected checksum |
| Stalled, network failure, too large | Couldn't download the update |
| Archive unreadable or without the expected content, or the unpacked app has another version than announced | The downloaded update isn't valid |
| Unpacking failed for another reason | Couldn't unpack the update: *reason* |
| The swap failed | Couldn't install the update: *reason* |
| The swap worked, the restart did not start | The update was installed, but CommitTree couldn't restart itself. Quit and open it again. |
| Cannot install in place | CommitTree can't install this update itself: *reason* (one of *When it cannot install*) |

The version the application compares against comes from `wails.json`
(`info.productVersion`), embedded in the program; a build whose version is not
`MAJOR.MINOR.PATCH` fails its checks.

## When the card and the pill show

When a check finds a version newer than the running one, two things can show
it: the **card** (a floating panel at the bottom right of the window, above the
status bar, that stays until closed) and the **pill** (a button at the right end
of the status bar that opens the card again).

### When a check runs

- **At app start**, if "Check for updates automatically" is on (the default).
- **When the window gets focus**, if that setting is on and at least 6 hours
  (`6 * 60 * 60 * 1000` ms) have passed since the last check. A check that
  failed also counts as a check. The time of the last check and the versions
  already shown are kept in memory only, so every run of the application starts
  fresh.
- **Check now** in Settings always checks, whatever the setting says, and
  answers in Settings: "You're up to date", "*version* is available", or the
  error's text.

A failed check at start or on focus says nothing and keeps what was already
known.

### When the card opens

- **At start, always** when a newer version is known.
- **On focus, each version once per run.** The first time a focus check finds a
  version not yet shown in this run, the card opens; later focus checks that
  find the same version leave it closed. A still newer version (0.11.0 after
  0.10.0) is new and opens it once.
- **Check now** opens the card when it finds a newer version.
- **The card shown at start, or by Check now, counts as that version's one
  showing** in this run.
- **✕ only closes the card.** No snooze, no "skip this version".

### The pill

The pill shows for as long as a newer version is known, whatever happened to
the card. Clicking it opens the card.

### Before restarting

"Update and restart" downloads and unpacks the update first. If restarting
would interrupt something, CommitTree then asks for confirmation, listing what
is in the way, and swaps only if the user agrees. Declining puts the card back as it was
before the click, so a new click on "Update and restart" downloads and unpacks
again (there is no way to reuse the first download). The reasons, in this
order:

1. One per task running in the status bar: "*task* is running".
2. "The chat is answering".
3. "The commit message has unsaved text" (a commit message that is empty or
   only whitespace does not count).

With none of these there is no question. Cancelling the download is not an
error: the card goes back to how it was, with no message. Any other failure
shows its message in the card (see *Errors as the user reads them*).

## What the user sees

### The card

A floating panel fixed at the bottom right of the window, 16 px from the edge
and just above the status bar, at most 340 px wide. It looks like a toast
(same surface, border and shadow) with a 4 px accent-coloured left edge. When
toasts are on screen the card stacks above them, so neither covers the other.
It stays until closed with ✕ or Esc; closing it never stops a download in
progress, and the pill opens it again.

Its title is "CommitTree *latest* is available", with a muted "You have
*current*" under it. Then, by state:

- **Ready.** Two buttons: **Update and restart** (primary) and **What's new**,
  which opens the release page in the browser.
- **Cannot install in place** (see *When it cannot install*). The primary
  button is **Download** and opens the release page; a muted line gives the
  reason. There is no "What's new" button, since both would open the same page.
- **Downloading.** "Downloading 42 %", or the bytes received ("Downloading
  3 MB") when the server did not say the total, or "Downloading…" before the
  first byte; and a **Cancel** button. Update and restart is disabled.
- **Installing.** "Installing…" while the update is unpacked and swapped in;
  nothing can be clicked. While the restart question is open the card says
  "Waiting for your answer…". A click on Update and restart during any of
  these does nothing.
- **Failed.** The message in one line (see *Errors as the user reads them*)
  in the danger colour, **Update and restart** again (a new attempt downloads
  again) and **Open release page**. A cancelled download is not a failure: the
  card goes back to its ready state with no message.

### The pill

"↑ Update" at the right end of the status bar (`13-status-bar.md`), shown while
a newer version is known. Clicking it opens the card.

### The sidebar

The version label next to Settings reads "v*current* → *latest*" in an accent
colour while a newer version is known (`07-conventions-and-constraints.md`).

### Settings → General → Updates

- A checkbox **Check for updates automatically** (on by default, kept between
  runs). It turns the checks at start and on focus on or off.
- A line "CommitTree *version*" and a **Check now** button, disabled and
  reading "Checking…" while it runs. Its answer shows under it: "You're up to
  date", "*version* is available", or the error's text.
