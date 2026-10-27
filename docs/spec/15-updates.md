# Updates

CommitTree finds out by itself whether a newer release exists, so the user
does not have to watch the project's release page. This document covers where
the latest version comes from and how it is checked. Showing it (the banner,
the card, Check now) and installing it are described in later sections.

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
  characters (it is stored in lower case). Otherwise the whole manifest is
  ignored.
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
