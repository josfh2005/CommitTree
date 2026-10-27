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
  (a leading `./` is fine). Every other entry is ignored, so no path in the
  archive can write outside the folder; a link, or a `CommitTree` entry that
  is not a regular file, rejects the archive, as does an archive without the
  file. The file is written with mode 0755 and is capped at 200 MiB.
- An archive that cannot be read, or lacks the expected content, fails with
  "update archive is not valid".
