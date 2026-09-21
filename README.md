# git-ui

AI-first desktop Git client (Wails + Go + Svelte).

Browse the log and the graph, merge a branch and resolve its conflicts with an
AI agent, and work the working tree: see your changes, stage and unstage by
file, discard, and commit with a message the model writes from what you staged.

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
make build   # → build/bin/git-ui.app
make icon    # re-render build/appicon.png from assets/icon.svg
```

## AI providers

API keys are stored securely in the operating system's secret store (Keychain on macOS, Secret Service on Linux, Credential Manager on Windows) and never in the settings file. Configure providers and API keys in Settings.

## AI prompts

Each AI action uses a Markdown prompt. Edit them from Settings → "Open prompts folder"
(`~/Library/Application Support/git-ui/prompts/`); "Restore default" undoes your changes.
