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

Pushing a `v*` tag runs `.github/workflows/release.yml`, which builds a universal macOS app (ad-hoc signed, not notarized) and a Linux amd64 binary and attaches them to a GitHub Release. Running the workflow by hand (Actions → Release → Run workflow) builds the same files as downloadable artifacts without publishing a release. Windows is not built yet: the embedded terminal uses Unix-only syscalls.

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
