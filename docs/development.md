# Developing CommitTree

CommitTree is a desktop app built with Wails, Go and Svelte. What the app does is
described in [the specification](spec/README.md); this page covers building it.

## Requirements

- macOS 26+ on Apple silicon, Go 1.26, git ≥ 2.28
- Node 22 (`nvm use 22`); Svelte 5 is installed through npm
- Wails CLI: `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- For AI features: [Ollama](https://ollama.com) with a tool-capable model (default `qwen2.5:7b`) runs locally; OpenAI and Anthropic require an API key entered in Settings

## Develop

```bash
make dev
```

## Test

```bash
go test ./...
cd frontend && npm test && npm run check
```

## Build

```bash
make build   # → build/bin/CommitTree.app
make icon    # re-render build/appicon.png from assets/icon.svg
             # (macOS 26+ uses assets/CommitTree.icon, compiled by scripts/mac-icon.sh on every build; needs Xcode)
make build-linux            # Docker → build/bin/CommitTree-linux-amd64.tar.gz
make build-linux ARCH=arm64 # same for arm64
```

The Linux build runs in Docker (`build/linux/Dockerfile`, Debian 12), so it needs Docker Desktop with buildx but no Linux machine. On Apple silicon the amd64 build is emulated and slower.

## Releases

Releases are made by hand. The workflow in `.github/workflows/release.yml` (a `v*` tag builds a universal macOS app and a Linux amd64 binary and attaches them to a GitHub Release) has never run, because Actions billing is locked, and it does not produce the files the in-place update needs, so do not rely on it. Windows is not built yet: the embedded terminal uses Unix-only syscalls.

The update check reads `latest.json` from the latest release (`docs/spec/15-updates.md`), so every release carries it next to three assets with fixed names. To publish version X.Y.Z:

1. Set `info.productVersion` in `wails.json` to `X.Y.Z` and commit it.
2. Intel build and zip. `make build` is the host-architecture build, so for Intel run the same steps with the platform flag:
   ```bash
   source ~/.nvm/nvm.sh && nvm use 22 && ~/go/bin/wails build -platform darwin/amd64
   codesign --force --deep -s - build/bin/CommitTree.app
   ditto -c -k --keepParent build/bin/CommitTree.app build/bin/CommitTree-macos-amd64.zip
   ```
3. Apple Silicon build and zip (on an arm64 Mac): `make build`, then
   `ditto -c -k --keepParent build/bin/CommitTree.app build/bin/CommitTree-macos-arm64.zip`.
4. Linux: `make build-linux` (Docker; writes `build/bin/CommitTree-linux-amd64.tar.gz`).
5. `make release-manifest` hashes the three files in `build/bin` and writes `build/bin/latest.json`. It fails, naming the file, if one is missing, is older than `wails.json` (a build left over from the previous release), is a macOS zip whose `Info.plist` version differs from `wails.json`, or if the version in `wails.json` is not `MAJOR.MINOR.PATCH`. So make the version commit first, then build; if it complains, rebuild that asset.
6. Tag and publish, attaching the three assets and `latest.json`:
   ```bash
   git tag vX.Y.Z && git push origin main vX.Y.Z
   gh release create vX.Y.Z --title "vX.Y.Z" --notes "..." \
     build/bin/CommitTree-macos-arm64.zip build/bin/CommitTree-macos-amd64.zip \
     build/bin/CommitTree-linux-amd64.tar.gz build/bin/latest.json
   ```

## AI providers

API keys are stored securely in the operating system's secret store (Keychain on macOS, Secret Service on Linux, Credential Manager on Windows) and never in the settings file. Configure providers and API keys in Settings.

## AI prompts

Each AI action uses a Markdown prompt. Edit them from Settings → "Open prompts folder"
(`~/Library/Application Support/git-ui/prompts/`); "Restore default" undoes your changes.

## README media

The screenshots in `docs/media/` show fictional demo repositories (Acme, with made-up
authors) and a local Ollama model. They were taken against the Wails dev server
(`http://localhost:34115`) with Playwright, with the app running under an isolated
`HOME` so no real repository, user or hostname appears.
