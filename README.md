# git-ui

AI-first desktop Git client (Wails + Go + Svelte). Sub-project 1: core viewer.

## Requirements

- macOS, Go 1.26, git ≥ 2.28
- Node 22 (`nvm use 22`)
- Wails CLI: `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- Svelte 5 is bundled via npm (no extra install)

## Develop

```bash
~/go/bin/wails dev
```

## Test

```bash
go test ./...
cd frontend && npm test && npm run check
```

## Build

```bash
~/go/bin/wails build   # → build/bin/git-ui.app
```
