# git-ui

AI-first desktop Git client (Wails + Go + Svelte). Sub-project 1: core viewer.

## Requirements

- macOS 26+ on Apple silicon, Go 1.26, git ≥ 2.28
- Node 22 (`nvm use 22`); Svelte 5 is installed through npm
- Wails CLI: `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- Swift 6.2 (Xcode command line tools) for the Apple Intelligence helper
- [Ollama](https://ollama.com) with a tool-capable model (default `qwen2.5:7b`) for the chat

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
make build   # → build/bin/git-ui.app (includes the Apple Intelligence helper)
make icon    # re-render build/appicon.png from assets/icon.svg
```

## AI prompts

Each AI action uses a Markdown prompt. Edit them from Settings → "Open prompts folder"
(`~/Library/Application Support/git-ui/prompts/`); "Restore default" undoes your changes.
