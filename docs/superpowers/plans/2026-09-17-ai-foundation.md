# git-ui AI Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Local-model AI in git-ui: a repo chat with read-only git tools (Ollama), "Explain commit" (Apple Intelligence or Ollama), an AI settings dialog with model download, editable prompt "skills", and a new app icon.

**Architecture:** Provider-neutral types in `internal/ai`; an Ollama HTTP client and a Swift helper wrapped by `internal/ai/apple`; read-only git tools, an agent loop and one-shot tasks on top; prompts and settings/chat persistence as small packages. `internal/app` exposes it all to the Svelte frontend and streams output as Wails events. The frontend keeps pure logic (event reducer, Markdown) in tested `lib/` modules.

**Tech Stack:** Go 1.26, Wails v2, Svelte 5 (legacy syntax) + TypeScript, Vitest, Swift 6.2 + FoundationModels (macOS 26+), Ollama HTTP API.

**Spec:** `docs/superpowers/specs/2026-09-17-ai-foundation-design.md`

## Global Constraints

- Go module `git-ui`; all git access through existing packages (`gitcmd`, `gitlog`, `refs`).
- Node 22 (`source ~/.nvm/nvm.sh && nvm use 22`) for npm and wails; Wails CLI at `~/go/bin/wails`.
- Svelte 5.57 is installed; components use legacy syntax (`export let`, `$:`, `on:click`). Never `new Component(...)`.
- AI config dir: `os.UserConfigDir()/git-ui/` → `ai.json`, `chats/<repoID>.json`, `prompts/<name>.md`.
- Defaults: Ollama URL `http://localhost:11434`, model `qwen2.5:7b`.
- Chat: Ollama only, max 8 agent steps, last 40 messages sent, tool results older than the last 10 messages replaced by `[earlier tool result omitted]`, step-limit note `Step limit reached.`.
- Tool output cap 8,000 characters with `[truncated]`; diff tool cap 300 lines; `search_log` limit default 20 max 50; `file_history` default 15 max 30.
- Explain diff budget: 6,000 characters (Ollama), 3,000 (Apple).
- Apple helper: `git-ui-apple status|respond`, JSON lines, 60 s timeout.
- Event names: `chat:delta`, `chat:tool`, `chat:tool_result`, `chat:done`, `chat:error`, `explain:delta`, `explain:done`, `explain:error`, `model:progress`, `model:done`. Every chat event carries `repoID` and `runID`; the frontend generates run IDs.
- AI never writes to a repository in this sub-project.
- Visual style: Claude desktop app; colors only via CSS custom properties in `theme.css`.
- Commits: Conventional Commits. **Never add a `Co-Authored-By` line.**
- TDD for Go packages and frontend `lib/*.ts`.

## File Structure

```
internal/ai/ai.go                     Shared types + Provider/Responder interfaces
internal/ai/settings/settings.go      ai.json load/save/defaults
internal/ai/chatstore/chatstore.go    chats/<repoID>.json
internal/ai/prompts/prompts.go        Prompt skills: embedded defaults + user overrides
internal/ai/prompts/defaults/*.md     chat.md, explain-commit.md
internal/ai/ollama/ollama.go          HTTP client: Chat, ListModels, Pull, Responder
internal/ai/tools/tools.go            Read-only git tools
internal/ai/agent/agent.go            Chat loop, history trim, events
internal/ai/apple/apple.go            Swift helper client
internal/ai/tasks/explain.go          Explain-commit context + run
helpers/apple/Package.swift           Swift package
helpers/apple/Sources/git-ui-apple/main.swift
internal/app/ai.go                    Wails-bound AI API
main.go                               Wire AI deps
Makefile                              helper/build/dev targets
frontend/src/lib/chat.ts              Chat event reducer
frontend/src/lib/markdown.ts          Safe minimal Markdown
frontend/src/lib/format.ts            + formatBytes, percent
frontend/src/lib/types.ts, api.ts, stores.ts   AI additions
frontend/src/components/ChatPanel.svelte       Functional chat
frontend/src/components/SettingsDialog.svelte  AI settings
frontend/src/components/CommitDetails.svelte   Explain button
assets/icon-variants/*.svg, assets/icon.svg, build/appicon.png
```

---

### Task 1: AI core types, settings, chat store and prompts

**Files:**
- Create: `internal/ai/ai.go`
- Create: `internal/ai/settings/settings.go`, test `internal/ai/settings/settings_test.go`
- Create: `internal/ai/chatstore/chatstore.go`, test `internal/ai/chatstore/chatstore_test.go`
- Create: `internal/ai/prompts/prompts.go`, `internal/ai/prompts/defaults/chat.md`, `internal/ai/prompts/defaults/explain-commit.md`, test `internal/ai/prompts/prompts_test.go`

**Interfaces:**
- Produces:
  - `ai.Role` (`RoleSystem`, `RoleUser`, `RoleAssistant`, `RoleTool`), `ai.ToolCall{ID, Name string; Args map[string]any}`, `ai.Message{Role; Content string; ToolCalls []ToolCall; ToolName string; Stopped bool}` (JSON `role`, `content`, `toolCalls,omitempty`, `toolName,omitempty`, `stopped,omitempty`), `ai.ToolSpec{Name, Description string; Parameters map[string]any}` (JSON `name`, `description`, `parameters`), `ai.Request{Model, System string; Messages []Message; Tools []ToolSpec}`, `ai.Chunk{Delta string; ToolCalls []ToolCall; Done bool; Err error}`
  - `ai.Provider` interface `Chat(ctx, Request) (<-chan Chunk, error)`; `ai.Responder` interface `Respond(ctx, instructions, prompt string) (<-chan Chunk, error)`
  - `settings.Settings{OllamaURL, ChatModel, TaskProvider, TaskModel string}` (JSON `ollamaURL`, `chatModel`, `taskProvider`, `taskModel`), constants `ProviderApple="apple"`, `ProviderOllama="ollama"`, `DefaultOllamaURL`, `DefaultModel`; `settings.ErrInvalid`; `settings.DefaultPath() (string, error)`; `settings.Defaults(appleAvailable bool) Settings`; `settings.Load(path string, appleAvailable func() bool) (Settings, error)`; `settings.Save(path string, s Settings) error`
  - `chatstore.New(dir string) *Store`, `chatstore.DefaultDir() (string, error)`, `chatstore.ErrInvalidID`, `(*Store).Load(repoID) ([]ai.Message, error)`, `Save(repoID, []ai.Message) error`, `Clear(repoID) error`
  - `prompts.Chat="chat"`, `prompts.ExplainCommit="explain-commit"`, `prompts.Vars{Repo, Path, Branch, Date string}`, `prompts.Info{Name string; Customized bool}` (JSON `name`, `customized`), `prompts.ErrUnknownPrompt`, `prompts.New(dir) *Store`, `prompts.DefaultDir()`, `prompts.Names() []string`, `(*Store).Dir() string`, `Get(name, Vars) (string, error)`, `List() []Info`, `EnsureFiles() error`, `Reset(name) error`

- [ ] **Step 1: Write the shared types**

`internal/ai/ai.go`:
```go
// Package ai defines provider-neutral types for the app's AI features.
package ai

import "context"

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type ToolCall struct {
	ID   string         `json:"id"`
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type Message struct {
	Role      Role       `json:"role"`
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"toolCalls,omitempty"`
	ToolName  string     `json:"toolName,omitempty"`
	Stopped   bool       `json:"stopped,omitempty"`
}

// ToolSpec describes a callable tool; Parameters is a JSON Schema object.
type ToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type Request struct {
	Model    string
	System   string
	Messages []Message
	Tools    []ToolSpec
}

// Chunk is one piece of a streamed response. A stream ends by closing the
// channel, normally right after a chunk with Done or Err set.
type Chunk struct {
	Delta     string
	ToolCalls []ToolCall
	Done      bool
	Err       error
}

// Provider holds multi-turn conversations with tool calling.
type Provider interface {
	Chat(ctx context.Context, req Request) (<-chan Chunk, error)
}

// Responder answers a single prompt under fixed instructions.
type Responder interface {
	Respond(ctx context.Context, instructions, prompt string) (<-chan Chunk, error)
}
```

- [ ] **Step 2: Write the failing tests for settings, chatstore and prompts**

`internal/ai/settings/settings_test.go`:
```go
package settings_test

import (
	"errors"
	"path/filepath"
	"testing"

	"git-ui/internal/ai/settings"
)

func TestLoadMissingFileUsesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai.json")

	withApple, err := settings.Load(path, func() bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	want := settings.Settings{OllamaURL: "http://localhost:11434", ChatModel: "qwen2.5:7b", TaskProvider: "apple", TaskModel: "qwen2.5:7b"}
	if withApple != want {
		t.Fatalf("got %+v", withApple)
	}
	without, _ := settings.Load(path, func() bool { return false })
	if without.TaskProvider != "ollama" {
		t.Fatalf("task provider = %q", without.TaskProvider)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "ai.json")
	s := settings.Settings{OllamaURL: "http://10.0.0.5:11434", ChatModel: "llama3.1:8b", TaskProvider: "ollama", TaskModel: "qwen2.5:3b"}

	if err := settings.Save(path, s); err != nil {
		t.Fatal(err)
	}
	got, err := settings.Load(path, func() bool { t.Fatal("apple probed although file exists"); return false })
	if err != nil || got != s {
		t.Fatalf("got %+v, err %v", got, err)
	}
}

func TestSaveRejectsInvalidValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai.json")
	bad := []settings.Settings{
		{OllamaURL: "localhost:11434", ChatModel: "m", TaskProvider: "apple", TaskModel: "m"},
		{OllamaURL: "ftp://x", ChatModel: "m", TaskProvider: "apple", TaskModel: "m"},
		{OllamaURL: "http://localhost:11434", ChatModel: "m", TaskProvider: "openai", TaskModel: "m"},
		{OllamaURL: "http://localhost:11434", ChatModel: "", TaskProvider: "apple", TaskModel: "m"},
	}
	for _, s := range bad {
		if err := settings.Save(path, s); !errors.Is(err, settings.ErrInvalid) {
			t.Errorf("%+v: want ErrInvalid, got %v", s, err)
		}
	}
}
```

`internal/ai/chatstore/chatstore_test.go`:
```go
package chatstore_test

import (
	"errors"
	"reflect"
	"testing"

	"git-ui/internal/ai"
	"git-ui/internal/ai/chatstore"
)

func TestSaveLoadClear(t *testing.T) {
	s := chatstore.New(t.TempDir())

	empty, err := s.Load("abc123")
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty = %#v, err %v", empty, err)
	}
	msgs := []ai.Message{
		{Role: ai.RoleUser, Content: "hola"},
		{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{{ID: "c1", Name: "list_refs", Args: map[string]any{"limit": float64(5)}}}},
		{Role: ai.RoleTool, ToolName: "list_refs", Content: "main"},
		{Role: ai.RoleAssistant, Content: "Hay una rama", Stopped: true},
	}
	if err := s.Save("abc123", msgs); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load("abc123")
	if err != nil || !reflect.DeepEqual(got, msgs) {
		t.Fatalf("got %#v, err %v", got, err)
	}
	if err := s.Clear("abc123"); err != nil {
		t.Fatal(err)
	}
	if err := s.Clear("abc123"); err != nil {
		t.Fatalf("clearing twice: %v", err)
	}
	after, _ := s.Load("abc123")
	if len(after) != 0 {
		t.Fatalf("after clear = %#v", after)
	}
}

func TestRejectsUnsafeIDs(t *testing.T) {
	s := chatstore.New(t.TempDir())
	for _, id := range []string{"", "../x", "a/b", "a b"} {
		if _, err := s.Load(id); !errors.Is(err, chatstore.ErrInvalidID) {
			t.Errorf("%q: want ErrInvalidID, got %v", id, err)
		}
	}
}
```

`internal/ai/prompts/prompts_test.go`:
```go
package prompts_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"git-ui/internal/ai/prompts"
)

var vars = prompts.Vars{Repo: "git-ui", Path: "/src/git-ui", Branch: "main", Date: "2026-09-17"}

func TestDefaultsRenderVariables(t *testing.T) {
	s := prompts.New(t.TempDir())

	chat, err := s.Get(prompts.Chat, vars)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"git-ui"`, "/src/git-ui", "main", "2026-09-17", "read-only"} {
		if !strings.Contains(chat, want) {
			t.Errorf("chat prompt missing %q:\n%s", want, chat)
		}
	}
	if strings.Contains(chat, "{{") {
		t.Errorf("unrendered variable in:\n%s", chat)
	}
	explain, err := s.Get(prompts.ExplainCommit, vars)
	if err != nil || !strings.Contains(explain, "3 to 6") {
		t.Fatalf("explain = %q, err %v", explain, err)
	}
	if !reflect.DeepEqual(prompts.Names(), []string{"chat", "explain-commit"}) {
		t.Fatalf("names = %v", prompts.Names())
	}
}

func TestUserOverrideListAndReset(t *testing.T) {
	dir := t.TempDir()
	s := prompts.New(dir)

	for _, info := range s.List() {
		if info.Customized {
			t.Fatalf("customized before any file: %+v", info)
		}
	}
	if err := s.EnsureFiles(); err != nil {
		t.Fatal(err)
	}
	for _, info := range s.List() {
		if info.Customized {
			t.Fatalf("copies of defaults must not count as customized: %+v", info)
		}
	}
	custom := "Explain {{repo}} commits like a pirate."
	if err := os.WriteFile(filepath.Join(dir, "explain-commit.md"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(prompts.ExplainCommit, vars)
	if got != "Explain git-ui commits like a pirate." {
		t.Fatalf("override not used: %q", got)
	}
	if err := s.EnsureFiles(); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.Get(prompts.ExplainCommit, vars); again != got {
		t.Fatal("EnsureFiles overwrote a user file")
	}
	want := []prompts.Info{{Name: "chat"}, {Name: "explain-commit", Customized: true}}
	if !reflect.DeepEqual(s.List(), want) {
		t.Fatalf("list = %+v", s.List())
	}
	if err := s.Reset(prompts.ExplainCommit); err != nil {
		t.Fatal(err)
	}
	if s.List()[1].Customized {
		t.Fatal("still customized after reset")
	}
	if _, err := s.Get("nope", vars); !errors.Is(err, prompts.ErrUnknownPrompt) {
		t.Fatalf("unknown prompt: %v", err)
	}
	if err := s.Reset("nope"); !errors.Is(err, prompts.ErrUnknownPrompt) {
		t.Fatalf("reset unknown: %v", err)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/ai/...`
Expected: FAIL — `undefined: settings.Load`, `undefined: chatstore.New`, `undefined: prompts.New`.

- [ ] **Step 4: Implement settings**

`internal/ai/settings/settings.go`:
```go
// Package settings stores the AI configuration.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
)

const (
	ProviderApple    = "apple"
	ProviderOllama   = "ollama"
	DefaultOllamaURL = "http://localhost:11434"
	DefaultModel     = "qwen2.5:7b"
)

var ErrInvalid = errors.New("invalid AI settings")

type Settings struct {
	OllamaURL    string `json:"ollamaURL"`
	ChatModel    string `json:"chatModel"`
	TaskProvider string `json:"taskProvider"`
	TaskModel    string `json:"taskModel"`
}

func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "git-ui", "ai.json"), nil
}

func Defaults(appleAvailable bool) Settings {
	s := Settings{OllamaURL: DefaultOllamaURL, ChatModel: DefaultModel, TaskProvider: ProviderOllama, TaskModel: DefaultModel}
	if appleAvailable {
		s.TaskProvider = ProviderApple
	}
	return s
}

// Load reads the settings file. When it doesn't exist, defaults are returned
// and appleAvailable is called to pick the task provider.
func Load(path string, appleAvailable func() bool) (Settings, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Defaults(appleAvailable()), nil
	}
	if err != nil {
		return Settings{}, err
	}
	s := Defaults(false)
	if err := json.Unmarshal(data, &s); err != nil {
		return Settings{}, fmt.Errorf("settings: parse %s: %w", path, err)
	}
	return s, nil
}

func Save(path string, s Settings) error {
	if err := validate(s); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func validate(s Settings) error {
	u, err := url.Parse(s.OllamaURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%w: Ollama URL must start with http:// or https://", ErrInvalid)
	}
	if s.ChatModel == "" || s.TaskModel == "" {
		return fmt.Errorf("%w: model names must not be empty", ErrInvalid)
	}
	if s.TaskProvider != ProviderApple && s.TaskProvider != ProviderOllama {
		return fmt.Errorf("%w: unknown task provider %q", ErrInvalid, s.TaskProvider)
	}
	return nil
}
```

- [ ] **Step 5: Implement the chat store**

`internal/ai/chatstore/chatstore.go`:
```go
// Package chatstore keeps one saved conversation per repository.
package chatstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	"git-ui/internal/ai"
)

var (
	ErrInvalidID = errors.New("invalid repository id")
	validID      = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
)

type Store struct{ dir string }

func New(dir string) *Store { return &Store{dir: dir} }

func DefaultDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "git-ui", "chats"), nil
}

func (s *Store) path(repoID string) (string, error) {
	if !validID.MatchString(repoID) {
		return "", fmt.Errorf("%w: %q", ErrInvalidID, repoID)
	}
	return filepath.Join(s.dir, repoID+".json"), nil
}

func (s *Store) Load(repoID string) ([]ai.Message, error) {
	path, err := s.path(repoID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return []ai.Message{}, nil
	}
	if err != nil {
		return nil, err
	}
	msgs := []ai.Message{}
	if err := json.Unmarshal(data, &msgs); err != nil {
		return nil, fmt.Errorf("chatstore: parse %s: %w", path, err)
	}
	return msgs, nil
}

func (s *Store) Save(repoID string, msgs []ai.Message) error {
	path, err := s.path(repoID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(msgs)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *Store) Clear(repoID string) error {
	path, err := s.path(repoID)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
```

- [ ] **Step 6: Implement prompts**

`internal/ai/prompts/defaults/chat.md`:
```markdown
You are the assistant inside git-ui, a desktop Git client. You help the user understand the repository "{{repo}}" at {{path}}. The current branch is {{branch}} and today is {{date}}.

Rules:
- You have read-only access. You cannot change the repository; when the user asks for a change, explain which git command or app action would do it.
- Use the tools to look up commits, diffs, branches and file history instead of guessing. If a result is truncated, call the tool again with narrower arguments.
- Cite commits by their short hash.
- Convert relative dates such as "last week" or "yesterday" to YYYY-MM-DD using today's date before searching.
- Answer in the same language the user writes in.
- Be concise: short paragraphs or bullet lists.
```

`internal/ai/prompts/defaults/explain-commit.md`:
```markdown
You explain Git commits to a developer working in the repository "{{repo}}".

Explain the commit in 3 to 6 short bullet points: what changed and the most likely reason. Mention the key files. Do not repeat the diff line by line. If the commit message is written in Spanish, answer in Spanish; otherwise answer in English.
```

`internal/ai/prompts/prompts.go`:
```go
// Package prompts manages the prompt "skills" used by AI actions: defaults
// embedded in the binary, optionally overridden by files the user edits.
package prompts

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	Chat          = "chat"
	ExplainCommit = "explain-commit"
)

var ErrUnknownPrompt = errors.New("unknown prompt")

//go:embed defaults/*.md
var defaults embed.FS

type Vars struct {
	Repo, Path, Branch, Date string
}

type Info struct {
	Name       string `json:"name"`
	Customized bool   `json:"customized"`
}

type Store struct{ dir string }

func New(dir string) *Store { return &Store{dir: dir} }

func DefaultDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "git-ui", "prompts"), nil
}

func (s *Store) Dir() string { return s.dir }

// Names lists the prompts that have an embedded default, sorted.
func Names() []string {
	entries, _ := fs.ReadDir(defaults, "defaults")
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, strings.TrimSuffix(e.Name(), ".md"))
	}
	sort.Strings(names)
	return names
}

func defaultText(name string) (string, error) {
	data, err := defaults.ReadFile("defaults/" + name + ".md")
	if err != nil {
		return "", fmt.Errorf("%w: %q", ErrUnknownPrompt, name)
	}
	return string(data), nil
}

func (s *Store) userFile(name string) string { return filepath.Join(s.dir, name+".md") }

// Get returns the rendered prompt, preferring the user's file.
func (s *Store) Get(name string, v Vars) (string, error) {
	text, err := defaultText(name)
	if err != nil {
		return "", err
	}
	if data, err := os.ReadFile(s.userFile(name)); err == nil {
		text = string(data)
	}
	r := strings.NewReplacer("{{repo}}", v.Repo, "{{path}}", v.Path, "{{branch}}", v.Branch, "{{date}}", v.Date)
	return strings.TrimSpace(r.Replace(text)), nil
}

func (s *Store) List() []Info {
	infos := []Info{}
	for _, name := range Names() {
		def, _ := defaultText(name)
		data, err := os.ReadFile(s.userFile(name))
		infos = append(infos, Info{Name: name, Customized: err == nil && strings.TrimSpace(string(data)) != strings.TrimSpace(def)})
	}
	return infos
}

// EnsureFiles creates the prompts folder and writes a copy of each default
// that has no user file yet, so the user has something to edit.
func (s *Store) EnsureFiles() error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	for _, name := range Names() {
		path := s.userFile(name)
		if _, err := os.Stat(path); err == nil {
			continue
		}
		def, _ := defaultText(name)
		if err := os.WriteFile(path, []byte(def), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Reset overwrites the user's file with the default text.
func (s *Store) Reset(name string) error {
	def, err := defaultText(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.userFile(name), []byte(def), 0o644)
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/ai/...`
Expected: `ok` for `settings`, `chatstore`, `prompts`.

- [ ] **Step 8: Commit**

```bash
git add internal/ai
git commit -m "feat(ai): core types, settings, chat store and prompt skills"
```

---

### Task 2: Ollama client

**Files:**
- Create: `internal/ai/ollama/ollama.go`
- Test: `internal/ai/ollama/ollama_test.go`

**Interfaces:**
- Consumes: `ai.Request`, `ai.Chunk`, `ai.ToolCall`, `ai.Message`, `ai.ToolSpec`, `ai.Responder` (Task 1)
- Produces:
  - `ollama.ErrUnreachable`, `ollama.ErrModelNotFound`, `ollama.ErrNoToolSupport`
  - `ollama.Model{Name string; Size int64}` (JSON `name`, `size`)
  - `ollama.New(baseURL string) *Client`
  - `(*Client).ListModels(ctx) ([]Model, error)` — never nil on success
  - `(*Client).Chat(ctx, ai.Request) (<-chan ai.Chunk, error)` — implements `ai.Provider`
  - `(*Client).Pull(ctx, name string, progress func(status string, completed, total int64)) error`
  - `(*Client).Responder(model string) ai.Responder`

Wire format verified against Ollama on this machine: tool calls arrive whole in one line as `{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_x","function":{"index":0,"name":"list_refs","arguments":{}}}]},"done":false}`; errors are `{"error":"..."}` (HTTP 404 `model 'x' not found`, 400 `... does not support tools`); pull streams `{"status":"...","total":N,"completed":M}` and ends with `{"status":"success"}` or `{"error":"..."}`.

- [ ] **Step 1: Write the failing tests**

`internal/ai/ollama/ollama_test.go`:
```go
package ollama_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"git-ui/internal/ai"
	"git-ui/internal/ai/ollama"
)

func stream(w http.ResponseWriter, lines ...string) {
	for _, l := range lines {
		fmt.Fprintln(w, l)
		w.(http.Flusher).Flush()
	}
}

func collect(t *testing.T, ch <-chan ai.Chunk) []ai.Chunk {
	t.Helper()
	var out []ai.Chunk
	for c := range ch {
		out = append(out, c)
	}
	return out
}

func TestChatSendsRequestAndStreamsChunks(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		stream(w,
			`{"message":{"role":"assistant","content":"Hel"},"done":false}`,
			`{"message":{"role":"assistant","content":"lo"},"done":false}`,
			`{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","function":{"index":0,"name":"list_refs","arguments":{"limit":5}}}]},"done":false}`,
			`{"message":{"role":"assistant","content":""},"done":true,"done_reason":"stop"}`,
		)
	}))
	defer srv.Close()

	req := ai.Request{
		Model:  "qwen2.5:7b",
		System: "be brief",
		Messages: []ai.Message{
			{Role: ai.RoleUser, Content: "refs?"},
			{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{{ID: "c0", Name: "list_refs", Args: map[string]any{}}}},
			{Role: ai.RoleTool, ToolName: "list_refs", Content: "main"},
		},
		Tools: []ai.ToolSpec{{Name: "list_refs", Description: "List refs", Parameters: map[string]any{"type": "object", "properties": map[string]any{}}}},
	}
	ch, err := ollama.New(srv.URL).Chat(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	chunks := collect(t, ch)

	if got["model"] != "qwen2.5:7b" || got["stream"] != true {
		t.Fatalf("request = %v", got)
	}
	msgs := got["messages"].([]any)
	if len(msgs) != 4 || msgs[0].(map[string]any)["role"] != "system" || msgs[0].(map[string]any)["content"] != "be brief" {
		t.Fatalf("messages = %v", msgs)
	}
	toolMsg := msgs[3].(map[string]any)
	if toolMsg["role"] != "tool" || toolMsg["tool_name"] != "list_refs" {
		t.Fatalf("tool message = %v", toolMsg)
	}
	call := msgs[2].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)
	if call["name"] != "list_refs" {
		t.Fatalf("assistant tool call = %v", call)
	}
	tool := got["tools"].([]any)[0].(map[string]any)
	if tool["type"] != "function" || tool["function"].(map[string]any)["name"] != "list_refs" {
		t.Fatalf("tools = %v", tool)
	}

	want := []ai.Chunk{
		{Delta: "Hel"},
		{Delta: "lo"},
		{ToolCalls: []ai.ToolCall{{ID: "call_1", Name: "list_refs", Args: map[string]any{"limit": float64(5)}}}},
		{Done: true},
	}
	if !reflect.DeepEqual(chunks, want) {
		t.Fatalf("chunks:\n got  %#v\n want %#v", chunks, want)
	}
}

func TestErrorsAreClassified(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   error
	}{
		{http.StatusBadRequest, `{"error":"registry.ollama.ai/library/gemma:2b does not support tools"}`, ollama.ErrNoToolSupport},
		{http.StatusNotFound, `{"error":"model 'nope:1b' not found"}`, ollama.ErrModelNotFound},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			fmt.Fprint(w, c.body)
		}))
		_, err := ollama.New(srv.URL).Chat(context.Background(), ai.Request{Model: "m"})
		srv.Close()
		if !errors.Is(err, c.want) {
			t.Errorf("%s: want %v, got %v", c.body, c.want, err)
		}
	}
}

func TestUnreachable(t *testing.T) {
	_, err := ollama.New("http://127.0.0.1:1").ListModels(context.Background())
	if !errors.Is(err, ollama.ErrUnreachable) {
		t.Fatalf("want ErrUnreachable, got %v", err)
	}
}

func TestListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Errorf("path = %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"models":[{"name":"qwen2.5:7b","size":4683087332},{"name":"llama3.1:8b","size":4920753328}]}`)
	}))
	defer srv.Close()

	got, err := ollama.New(srv.URL + "/").ListModels(context.Background())
	want := []ollama.Model{{Name: "qwen2.5:7b", Size: 4683087332}, {Name: "llama3.1:8b", Size: 4920753328}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, err %v", got, err)
	}
}

func TestPullReportsProgress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "qwen2.5:7b" {
			t.Errorf("body = %v", body)
		}
		stream(w,
			`{"status":"pulling manifest"}`,
			`{"status":"pulling 845dbda0ea48","total":100,"completed":50}`,
			`{"status":"success"}`,
		)
	}))
	defer srv.Close()

	var events []string
	err := ollama.New(srv.URL).Pull(context.Background(), "qwen2.5:7b", func(status string, completed, total int64) {
		events = append(events, fmt.Sprintf("%s %d/%d", status, completed, total))
	})
	want := []string{"pulling manifest 0/0", "pulling 845dbda0ea48 50/100", "success 0/0"}
	if err != nil || !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, err %v", events, err)
	}
}

func TestPullErrorLine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stream(w, `{"status":"pulling manifest"}`, `{"error":"pull model manifest: file does not exist"}`)
	}))
	defer srv.Close()

	err := ollama.New(srv.URL).Pull(context.Background(), "nope", func(string, int64, int64) {})
	if err == nil || !strings.Contains(err.Error(), "file does not exist") {
		t.Fatalf("err = %v", err)
	}
}

func TestPullCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stream(w, `{"status":"pulling manifest"}`)
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	err := ollama.New(srv.URL).Pull(ctx, "m", func(string, int64, int64) { cancel() })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestResponderUsesInstructionsAsSystem(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		stream(w, `{"message":{"role":"assistant","content":"- ok"},"done":false}`, `{"message":{"content":""},"done":true}`)
	}))
	defer srv.Close()

	ch, err := ollama.New(srv.URL).Responder("qwen2.5:3b").Respond(context.Background(), "explain", "commit text")
	if err != nil {
		t.Fatal(err)
	}
	chunks := collect(t, ch)
	msgs := got["messages"].([]any)
	if got["model"] != "qwen2.5:3b" || msgs[0].(map[string]any)["content"] != "explain" || msgs[1].(map[string]any)["content"] != "commit text" {
		t.Fatalf("request = %v", got)
	}
	if _, hasTools := got["tools"]; hasTools {
		t.Fatal("responder must not send tools")
	}
	if len(chunks) != 2 || chunks[0].Delta != "- ok" || !chunks[1].Done {
		t.Fatalf("chunks = %#v", chunks)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ai/ollama/`
Expected: FAIL — `undefined: ollama.New`.

- [ ] **Step 3: Implement**

`internal/ai/ollama/ollama.go`:
```go
// Package ollama is a client for the native Ollama HTTP API.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"git-ui/internal/ai"
)

var (
	ErrUnreachable   = errors.New("ollama is not reachable")
	ErrModelNotFound = errors.New("model not found")
	ErrNoToolSupport = errors.New("model does not support tools")
)

type Model struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type Client struct {
	baseURL string
	http    *http.Client
}

func New(baseURL string) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: &http.Client{}}
}

type wireFunction struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type wireToolCall struct {
	ID       string       `json:"id,omitempty"`
	Function wireFunction `json:"function"`
}

type wireMessage struct {
	Role      string         `json:"role"`
	Content   string         `json:"content"`
	ToolCalls []wireToolCall `json:"tool_calls,omitempty"`
	ToolName  string         `json:"tool_name,omitempty"`
}

type wireTool struct {
	Type     string      `json:"type"`
	Function ai.ToolSpec `json:"function"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []wireMessage `json:"messages"`
	Tools    []wireTool    `json:"tools,omitempty"`
	Stream   bool          `json:"stream"`
}

type chatLine struct {
	Message wireMessage `json:"message"`
	Done    bool        `json:"done"`
	Error   string      `json:"error"`
}

func (c *Client) ListModels(ctx context.Context) ([]Model, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var body struct {
		Models []Model `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("ollama: decode models: %w", err)
	}
	if body.Models == nil {
		body.Models = []Model{}
	}
	return body.Models, nil
}

func (c *Client) Chat(ctx context.Context, req ai.Request) (<-chan ai.Chunk, error) {
	body := chatRequest{Model: req.Model, Stream: true}
	if req.System != "" {
		body.Messages = append(body.Messages, wireMessage{Role: string(ai.RoleSystem), Content: req.System})
	}
	for _, m := range req.Messages {
		wm := wireMessage{Role: string(m.Role), Content: m.Content, ToolName: m.ToolName}
		for _, tc := range m.ToolCalls {
			args := tc.Args
			if args == nil {
				args = map[string]any{}
			}
			wm.ToolCalls = append(wm.ToolCalls, wireToolCall{ID: tc.ID, Function: wireFunction{Name: tc.Name, Arguments: args}})
		}
		body.Messages = append(body.Messages, wm)
	}
	for _, t := range req.Tools {
		body.Tools = append(body.Tools, wireTool{Type: "function", Function: t})
	}
	resp, err := c.post(ctx, "/api/chat", body)
	if err != nil {
		return nil, err
	}

	ch := make(chan ai.Chunk)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		dec := json.NewDecoder(resp.Body)
		for {
			var line chatLine
			if err := dec.Decode(&line); err != nil {
				if ctx.Err() != nil {
					err = ctx.Err()
				} else if errors.Is(err, io.EOF) {
					err = errors.New("ollama: response ended unexpectedly")
				}
				send(ctx, ch, ai.Chunk{Err: err})
				return
			}
			if line.Error != "" {
				send(ctx, ch, ai.Chunk{Err: classify(line.Error)})
				return
			}
			chunk := ai.Chunk{Delta: line.Message.Content, Done: line.Done}
			for _, tc := range line.Message.ToolCalls {
				chunk.ToolCalls = append(chunk.ToolCalls, ai.ToolCall{ID: tc.ID, Name: tc.Function.Name, Args: tc.Function.Arguments})
			}
			if chunk.Delta != "" || len(chunk.ToolCalls) > 0 || chunk.Done {
				if !send(ctx, ch, chunk) {
					return
				}
			}
			if line.Done {
				return
			}
		}
	}()
	return ch, nil
}

func (c *Client) Pull(ctx context.Context, name string, progress func(status string, completed, total int64)) error {
	resp, err := c.post(ctx, "/api/pull", map[string]any{"model": name, "stream": true})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	dec := json.NewDecoder(resp.Body)
	for {
		var line struct {
			Status    string `json:"status"`
			Total     int64  `json:"total"`
			Completed int64  `json:"completed"`
			Error     string `json:"error"`
		}
		if err := dec.Decode(&line); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, io.EOF) {
				return errors.New("ollama: download ended before it finished")
			}
			return err
		}
		if line.Error != "" {
			return classify(line.Error)
		}
		progress(line.Status, line.Completed, line.Total)
		if line.Status == "success" {
			return nil
		}
	}
}

// Responder adapts the client to single-prompt tasks with the given model.
func (c *Client) Responder(model string) ai.Responder { return responder{c: c, model: model} }

type responder struct {
	c     *Client
	model string
}

func (r responder) Respond(ctx context.Context, instructions, prompt string) (<-chan ai.Chunk, error) {
	return r.c.Chat(ctx, ai.Request{
		Model:    r.model,
		System:   instructions,
		Messages: []ai.Message{{Role: ai.RoleUser, Content: prompt}},
	})
}

func (c *Client) post(ctx context.Context, path string, body any) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(ctx, req)
}

func (c *Client) do(ctx context.Context, req *http.Request) (*http.Response, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w at %s: %v", ErrUnreachable, c.baseURL, err)
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return nil, classify(e.Error)
	}
	return resp, nil
}

func classify(msg string) error {
	switch {
	case strings.Contains(msg, "does not support tools"):
		return fmt.Errorf("%w: %s", ErrNoToolSupport, msg)
	case strings.Contains(msg, "not found"):
		return fmt.Errorf("%w: %s", ErrModelNotFound, msg)
	}
	return fmt.Errorf("ollama: %s", msg)
}

func send(ctx context.Context, ch chan<- ai.Chunk, c ai.Chunk) bool {
	select {
	case ch <- c:
		return true
	case <-ctx.Done():
		return false
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ai/ollama/`
Expected: `ok  git-ui/internal/ai/ollama`

- [ ] **Step 5: Commit**

```bash
git add internal/ai/ollama
git commit -m "feat(ai): Ollama client with streaming chat, tools and model pull"
```

---

### Task 3: Read-only git tools

**Files:**
- Create: `internal/ai/tools/tools.go`
- Test: `internal/ai/tools/tools_test.go`

**Interfaces:**
- Consumes: `ai.ToolSpec`, `ai.ToolCall` (Task 1); `gitlog.Get(ctx, dir, Filters, skip, limit) ([]Commit, error)`, `gitlog.Filters{Text, Branch, Author, Since, Until string; Paths []string}`, `gitlog.GetDetails(ctx, dir, hash) (Details, error)` (Details embeds Commit and has `Body string; Files []FileChange{Status, Path, OldPath}`), `gitlog.Diff(ctx, dir, parent, hash string, paths []string) (string, error)`, `gitlog.ResolveCommit(ctx, dir, rev) (string, error)` (returns `""`, nil when unknown), `refs.List(ctx, dir) (Refs, error)` (`Head`, `HeadHash`, `Detached`, `Local []Branch{Name}`, `Remotes []Remote{Name, Branches []Branch{Name}}`, `Tags []Tag{Name}`); `testrepo` helpers.
- Produces:
  - `tools.MaxOutput = 8000`, `tools.MaxDiffLines = 300`
  - `tools.Specs() []ai.ToolSpec` — exactly `search_log`, `show_commit`, `diff_commit_file`, `list_refs`, `file_history`
  - `tools.Run(ctx context.Context, dir string, call ai.ToolCall) string` — never returns an error; failures come back as text starting with `error: `
  - `tools.Truncate(s string, max int) string` — appends `\n[truncated]` when cut

- [ ] **Step 1: Write the failing tests**

`internal/ai/tools/tools_test.go`:
```go
package tools_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"git-ui/internal/ai"
	"git-ui/internal/ai/tools"
	"git-ui/internal/testrepo"
)

var ctx = context.Background()

func run(dir, name string, args map[string]any) string {
	return tools.Run(ctx, dir, ai.ToolCall{ID: "c1", Name: name, Args: args})
}

func TestSpecsListAllTools(t *testing.T) {
	var names []string
	for _, s := range tools.Specs() {
		names = append(names, s.Name)
		if s.Description == "" || s.Parameters["type"] != "object" {
			t.Errorf("%s: incomplete spec %+v", s.Name, s)
		}
	}
	if strings.Join(names, ",") != "search_log,show_commit,diff_commit_file,list_refs,file_history" {
		t.Fatalf("names = %v", names)
	}
}

func TestSearchLogShowCommitAndDiff(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("chore: base")
	r.WriteFile("notes.md", "hello\n")
	r.Git("add", "notes.md")
	r.Git("commit", "-q", "-m", "feat: add notes NEXO-42")
	hash := r.Git("rev-parse", "HEAD")
	short := hash[:7]
	r.Git("tag", "v1.0")

	log := run(r.Dir, "search_log", map[string]any{"text": "NEXO-42"})
	if !strings.Contains(log, short) || !strings.Contains(log, "feat: add notes NEXO-42") || strings.Contains(log, "chore: base") {
		t.Fatalf("search_log = %q", log)
	}
	if !strings.Contains(log, "Test User") || !strings.Contains(log, "v1.0") {
		t.Fatalf("search_log missing author or refs: %q", log)
	}

	show := run(r.Dir, "show_commit", map[string]any{"rev": short})
	for _, want := range []string{hash[:7], "feat: add notes NEXO-42", "A notes.md", "Test User"} {
		if !strings.Contains(show, want) {
			t.Errorf("show_commit missing %q: %q", want, show)
		}
	}

	diff := run(r.Dir, "diff_commit_file", map[string]any{"rev": "HEAD", "path": "notes.md"})
	if !strings.Contains(diff, "+hello") {
		t.Fatalf("diff = %q", diff)
	}
}

func TestListRefsAndFileHistory(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("one")
	r.Git("branch", "feature/x")
	r.WriteFile("a.txt", "1\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-m", "touch a once")
	r.WriteFile("a.txt", "2\n")
	r.Git("commit", "-q", "-am", "touch a twice")

	refs := run(r.Dir, "list_refs", nil)
	for _, want := range []string{"Current branch: main", "feature/x"} {
		if !strings.Contains(refs, want) {
			t.Errorf("list_refs missing %q: %q", want, refs)
		}
	}

	history := run(r.Dir, "file_history", map[string]any{"path": "a.txt"})
	if strings.Count(history, "\n")+1 != 2 || !strings.Contains(history, "touch a twice") || strings.Contains(history, "one") {
		t.Fatalf("file_history = %q", history)
	}
}

func TestDiffIsCappedInLines(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	var b strings.Builder
	for i := 0; i < 400; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	r.WriteFile("big.txt", b.String())
	r.Git("add", "big.txt")
	r.Git("commit", "-q", "-m", "big")

	diff := run(r.Dir, "diff_commit_file", map[string]any{"rev": "HEAD", "path": "big.txt"})
	if !strings.Contains(diff, "[diff truncated at 300 lines]") || strings.Contains(diff, "line 399") {
		t.Fatalf("diff not capped: %d chars", len(diff))
	}
}

func TestErrorsAreText(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"show_commit", map[string]any{"rev": "-x"}, "error: invalid revision"},
		{"show_commit", map[string]any{"rev": "deadbeef"}, "error: unknown revision"},
		{"show_commit", nil, "error: rev is required"},
		{"diff_commit_file", map[string]any{"rev": "HEAD"}, "error: path is required"},
		{"search_log", map[string]any{"branch": "--all"}, "error: invalid branch"},
		{"nope", nil, "error: unknown tool"},
	}
	for _, c := range cases {
		if got := run(r.Dir, c.name, c.args); !strings.HasPrefix(got, c.want) {
			t.Errorf("%s %v = %q, want prefix %q", c.name, c.args, got, c.want)
		}
	}
}

func TestLimitsAndTruncate(t *testing.T) {
	r := testrepo.New(t)
	for i := 0; i < 60; i++ {
		r.Commit(fmt.Sprintf("commit %d", i))
	}
	def := run(r.Dir, "search_log", nil)
	if n := strings.Count(def, "\n") + 1; n != 20 {
		t.Fatalf("default limit returned %d lines", n)
	}
	max := run(r.Dir, "search_log", map[string]any{"limit": float64(500)})
	if n := strings.Count(max, "\n") + 1; n != 50 {
		t.Fatalf("max limit returned %d lines", n)
	}

	long := strings.Repeat("é", tools.MaxOutput)
	cut := tools.Truncate(long, 10)
	if !strings.HasSuffix(cut, "\n[truncated]") || !strings.HasPrefix(cut, "éééé") {
		t.Fatalf("truncate = %q", cut)
	}
	if tools.Truncate("short", 10) != "short" {
		t.Fatal("short text changed")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ai/tools/`
Expected: FAIL — `undefined: tools.Run`.

- [ ] **Step 3: Implement**

`internal/ai/tools/tools.go`:
```go
// Package tools implements the read-only git tools the chat model can call.
package tools

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"git-ui/internal/ai"
	"git-ui/internal/gitlog"
	"git-ui/internal/refs"
)

const (
	MaxOutput    = 8000
	MaxDiffLines = 300
)

func Specs() []ai.ToolSpec {
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	num := func(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }
	obj := func(props map[string]any, required ...string) map[string]any {
		o := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			o["required"] = required
		}
		return o
	}
	return []ai.ToolSpec{
		{
			Name:        "search_log",
			Description: "Search the commit history. Returns one line per commit: short hash, date, author, subject and refs. All filters are optional.",
			Parameters: obj(map[string]any{
				"text":   str("Case-insensitive text that must appear in the commit message"),
				"author": str("Part of the author name or email"),
				"since":  str("Only commits on or after this date (YYYY-MM-DD)"),
				"until":  str("Only commits on or before this date (YYYY-MM-DD)"),
				"branch": str("Branch, tag or revision to search; all refs when empty"),
				"path":   str("Only commits touching this file or directory"),
				"limit":  num("Maximum commits to return (default 20, max 50)"),
			}),
		},
		{
			Name:        "show_commit",
			Description: "Show a commit's full message, author, date, parents and changed files.",
			Parameters:  obj(map[string]any{"rev": str("Commit hash, branch or tag")}, "rev"),
		},
		{
			Name:        "diff_commit_file",
			Description: "Show the diff of one file in a commit, compared with its first parent.",
			Parameters:  obj(map[string]any{"rev": str("Commit hash, branch or tag"), "path": str("File path as listed by show_commit")}, "rev", "path"),
		},
		{
			Name:        "list_refs",
			Description: "List the current branch, local branches, remote branches and tags.",
			Parameters:  obj(map[string]any{}),
		},
		{
			Name:        "file_history",
			Description: "List the commits that changed a file, newest first.",
			Parameters:  obj(map[string]any{"path": str("File path"), "limit": num("Maximum commits (default 15, max 30)")}, "path"),
		},
	}
}

// Run executes a tool call. Problems are returned as text starting with
// "error: " so the model can correct its arguments.
func Run(ctx context.Context, dir string, call ai.ToolCall) string {
	var out string
	var err error
	switch call.Name {
	case "search_log":
		out, err = searchLog(ctx, dir, call.Args)
	case "show_commit":
		out, err = showCommit(ctx, dir, call.Args)
	case "diff_commit_file":
		out, err = diffCommitFile(ctx, dir, call.Args)
	case "list_refs":
		out, err = listRefs(ctx, dir)
	case "file_history":
		out, err = fileHistory(ctx, dir, call.Args)
	default:
		err = fmt.Errorf("unknown tool %q", call.Name)
	}
	if err != nil {
		return "error: " + err.Error()
	}
	if out == "" {
		out = "(no results)"
	}
	return Truncate(out, MaxOutput)
}

// Truncate cuts s to at most max bytes on a rune boundary.
func Truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "\n[truncated]"
}

func utf8RuneStart(b byte) bool { return b&0xC0 != 0x80 }

func searchLog(ctx context.Context, dir string, args map[string]any) (string, error) {
	f := gitlog.Filters{
		Text:   argString(args, "text"),
		Author: argString(args, "author"),
		Since:  argString(args, "since"),
		Until:  argString(args, "until"),
		Branch: argString(args, "branch"),
	}
	if strings.HasPrefix(f.Branch, "-") {
		return "", fmt.Errorf("invalid branch %q", f.Branch)
	}
	if p := argString(args, "path"); p != "" {
		f.Paths = []string{p}
	}
	return logLines(ctx, dir, f, argInt(args, "limit", 20, 50))
}

func fileHistory(ctx context.Context, dir string, args map[string]any) (string, error) {
	path := argString(args, "path")
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	return logLines(ctx, dir, gitlog.Filters{Paths: []string{path}}, argInt(args, "limit", 15, 30))
}

func logLines(ctx context.Context, dir string, f gitlog.Filters, limit int) (string, error) {
	commits, err := gitlog.Get(ctx, dir, f, 0, limit)
	if err != nil {
		return "", err
	}
	lines := make([]string, 0, len(commits))
	for _, c := range commits {
		line := fmt.Sprintf("%s %s %s: %s", c.Short, c.Date.Format("2006-01-02"), c.Author, c.Subject)
		var names []string
		for _, r := range c.Refs {
			if r.Kind != gitlog.RefHead {
				names = append(names, r.Name)
			}
		}
		if len(names) > 0 {
			line += " (" + strings.Join(names, ", ") + ")"
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n"), nil
}

func resolve(ctx context.Context, dir string, args map[string]any) (string, error) {
	rev := argString(args, "rev")
	if rev == "" {
		return "", fmt.Errorf("rev is required")
	}
	if strings.HasPrefix(rev, "-") {
		return "", fmt.Errorf("invalid revision %q", rev)
	}
	hash, err := gitlog.ResolveCommit(ctx, dir, rev)
	if err != nil {
		return "", err
	}
	if hash == "" {
		return "", fmt.Errorf("unknown revision %q", rev)
	}
	return hash, nil
}

func showCommit(ctx context.Context, dir string, args map[string]any) (string, error) {
	hash, err := resolve(ctx, dir, args)
	if err != nil {
		return "", err
	}
	d, err := gitlog.GetDetails(ctx, dir, hash)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "commit %s\nauthor: %s <%s>\ndate: %s\n", d.Short, d.Author, d.Email, d.Date.Format("2006-01-02 15:04"))
	if len(d.Parents) > 0 {
		short := make([]string, len(d.Parents))
		for i, p := range d.Parents {
			short[i] = p[:min(7, len(p))]
		}
		fmt.Fprintf(&b, "parents: %s\n", strings.Join(short, " "))
	}
	fmt.Fprintf(&b, "\n%s\n", d.Subject)
	if d.Body != "" {
		fmt.Fprintf(&b, "\n%s\n", d.Body)
	}
	b.WriteString("\nfiles:\n")
	for _, f := range d.Files {
		if f.OldPath != "" {
			fmt.Fprintf(&b, "%s %s -> %s\n", f.Status, f.OldPath, f.Path)
		} else {
			fmt.Fprintf(&b, "%s %s\n", f.Status, f.Path)
		}
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func diffCommitFile(ctx context.Context, dir string, args map[string]any) (string, error) {
	path := argString(args, "path")
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	hash, err := resolve(ctx, dir, args)
	if err != nil {
		return "", err
	}
	d, err := gitlog.GetDetails(ctx, dir, hash)
	if err != nil {
		return "", err
	}
	parent := ""
	if len(d.Parents) > 0 {
		parent = d.Parents[0]
	}
	patch, err := gitlog.Diff(ctx, dir, parent, hash, []string{path})
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimRight(patch, "\n"), "\n")
	if len(lines) > MaxDiffLines {
		lines = append(lines[:MaxDiffLines], fmt.Sprintf("[diff truncated at %d lines]", MaxDiffLines))
	}
	return strings.Join(lines, "\n"), nil
}

func listRefs(ctx context.Context, dir string) (string, error) {
	r, err := refs.List(ctx, dir)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if r.Detached {
		fmt.Fprintf(&b, "Current branch: (detached at %s)\n", r.HeadHash[:min(7, len(r.HeadHash))])
	} else {
		fmt.Fprintf(&b, "Current branch: %s\n", r.Head)
	}
	b.WriteString("Local branches:")
	for _, br := range r.Local {
		b.WriteString(" " + br.Name)
	}
	b.WriteString("\nRemote branches:")
	count := 0
	for _, remote := range r.Remotes {
		for _, br := range remote.Branches {
			if count == 100 {
				break
			}
			b.WriteString(" " + remote.Name + "/" + br.Name)
			count++
		}
	}
	b.WriteString("\nTags:")
	for i, tag := range r.Tags {
		if i == 100 {
			break
		}
		b.WriteString(" " + tag.Name)
	}
	return b.String(), nil
}

func argString(args map[string]any, key string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// argInt reads a JSON number (or numeric string), applying a default and a cap.
func argInt(args map[string]any, key string, def, max int) int {
	n := def
	switch v := args[key].(type) {
	case float64:
		n = int(v)
	case string:
		if parsed, err := strconv.Atoi(v); err == nil {
			n = parsed
		}
	}
	if n <= 0 {
		n = def
	}
	return min(n, max)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ai/tools/`
Expected: `ok  git-ui/internal/ai/tools`

- [ ] **Step 5: Commit**

```bash
git add internal/ai/tools
git commit -m "feat(ai): read-only git tools for the chat model"
```

---

### Task 4: Chat agent loop

**Files:**
- Create: `internal/ai/agent/agent.go`
- Test: `internal/ai/agent/agent_test.go`

**Interfaces:**
- Consumes: `ai.Provider`, `ai.Request`, `ai.Chunk`, `ai.Message`, `ai.ToolCall`, `ai.ToolSpec` (Task 1)
- Produces:
  - Constants `agent.MaxSteps = 8`, `agent.HistoryLimit = 40`, `agent.KeepToolResults = 10`, `agent.OmittedToolResult = "[earlier tool result omitted]"`, `agent.StepLimitNote = "Step limit reached."`
  - Event names `agent.EventDelta = "chat:delta"`, `EventTool = "chat:tool"`, `EventToolResult = "chat:tool_result"`, `EventDone = "chat:done"`, `EventError = "chat:error"`
  - Payloads `agent.DeltaEvent{RepoID, RunID, Text}` (JSON `repoID`, `runID`, `text`), `agent.ToolEvent{RepoID, RunID, Name string; Args map[string]any}` (JSON `repoID`, `runID`, `name`, `args`), `agent.ToolResultEvent{RepoID, RunID, Name, Summary}` (JSON … `summary`), `agent.DoneEvent{RepoID, RunID}`, `agent.ErrorEvent{RepoID, RunID, Message, Code}` (JSON … `message`, `code`)
  - `agent.Run{RepoID, RunID string; Provider ai.Provider; Model, System string; Tools []ai.ToolSpec; RunTool func(context.Context, ai.ToolCall) string; Emit func(name string, data any)}`
  - `agent.Execute(ctx, run Run, history []ai.Message) ([]ai.Message, error)` — history ends with the user's new message; returns the full updated history. On cancellation it appends an assistant message with the partial text and `Stopped: true` and returns `ctx.Err()`. It emits only delta/tool/tool_result events; the caller emits done/error.
  - `agent.Trim(msgs []ai.Message) []ai.Message`

- [ ] **Step 1: Write the failing tests**

`internal/ai/agent/agent_test.go`:
```go
package agent_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"git-ui/internal/ai"
	"git-ui/internal/ai/agent"
)

// scripted replays one list of chunks per call and records requests.
type scripted struct {
	turns    [][]ai.Chunk
	requests []ai.Request
}

func (s *scripted) Chat(ctx context.Context, req ai.Request) (<-chan ai.Chunk, error) {
	s.requests = append(s.requests, req)
	turn := s.turns[min(len(s.requests), len(s.turns))-1]
	ch := make(chan ai.Chunk, len(turn))
	for _, c := range turn {
		ch <- c
	}
	close(ch)
	return ch, nil
}

type recorder struct {
	mu     sync.Mutex
	names  []string
	data   []any
	onEmit func(name string)
}

func (r *recorder) emit(name string, data any) {
	r.mu.Lock()
	r.names = append(r.names, name)
	r.data = append(r.data, data)
	r.mu.Unlock()
	if r.onEmit != nil {
		r.onEmit(name)
	}
}

func baseRun(p ai.Provider, rec *recorder) agent.Run {
	return agent.Run{
		RepoID: "repo1", RunID: "run1", Provider: p, Model: "m", System: "sys",
		Tools:   []ai.ToolSpec{{Name: "list_refs"}},
		RunTool: func(ctx context.Context, call ai.ToolCall) string { return "main\nfeature" },
		Emit:    rec.emit,
	}
}

var user = []ai.Message{{Role: ai.RoleUser, Content: "what branches?"}}

func TestToolLoop(t *testing.T) {
	p := &scripted{turns: [][]ai.Chunk{
		{{ToolCalls: []ai.ToolCall{{ID: "c1", Name: "list_refs", Args: map[string]any{}}}}, {Done: true}},
		{{Delta: "On "}, {Delta: "main."}, {Done: true}},
	}}
	rec := &recorder{}

	got, err := agent.Execute(context.Background(), baseRun(p, rec), user)
	if err != nil {
		t.Fatal(err)
	}
	want := []ai.Message{
		user[0],
		{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{{ID: "c1", Name: "list_refs", Args: map[string]any{}}}},
		{Role: ai.RoleTool, ToolName: "list_refs", Content: "main\nfeature"},
		{Role: ai.RoleAssistant, Content: "On main."},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("history:\n got  %#v\n want %#v", got, want)
	}
	if !reflect.DeepEqual(rec.names, []string{agent.EventTool, agent.EventToolResult, agent.EventDelta, agent.EventDelta}) {
		t.Fatalf("events = %v", rec.names)
	}
	if rec.data[1] != (agent.ToolResultEvent{RepoID: "repo1", RunID: "run1", Name: "list_refs", Summary: "main"}) {
		t.Fatalf("tool result event = %#v", rec.data[1])
	}
	if p.requests[0].System != "sys" || p.requests[0].Model != "m" || len(p.requests[0].Tools) != 1 {
		t.Fatalf("request = %+v", p.requests[0])
	}
	if len(p.requests[1].Messages) != 3 {
		t.Fatalf("second request should include the tool result: %+v", p.requests[1].Messages)
	}
}

func TestMissingToolCallIDsAreFilled(t *testing.T) {
	p := &scripted{turns: [][]ai.Chunk{
		{{ToolCalls: []ai.ToolCall{{Name: "list_refs"}}}, {Done: true}},
		{{Delta: "ok"}, {Done: true}},
	}}
	got, err := agent.Execute(context.Background(), baseRun(p, &recorder{}), user)
	if err != nil || got[1].ToolCalls[0].ID == "" {
		t.Fatalf("history = %#v, err %v", got, err)
	}
}

func TestStepLimit(t *testing.T) {
	p := &scripted{turns: [][]ai.Chunk{{{ToolCalls: []ai.ToolCall{{ID: "c", Name: "list_refs"}}}, {Done: true}}}}
	rec := &recorder{}

	got, err := agent.Execute(context.Background(), baseRun(p, rec), user)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.requests) != agent.MaxSteps {
		t.Fatalf("requests = %d", len(p.requests))
	}
	last := got[len(got)-1]
	if last.Role != ai.RoleAssistant || last.Content != agent.StepLimitNote {
		t.Fatalf("last = %#v", last)
	}
	if rec.names[len(rec.names)-1] != agent.EventDelta {
		t.Fatalf("step limit note not streamed: %v", rec.names)
	}
}

// blocking sends one delta, then waits for cancellation.
type blocking struct{}

func (blocking) Chat(ctx context.Context, req ai.Request) (<-chan ai.Chunk, error) {
	ch := make(chan ai.Chunk)
	go func() {
		defer close(ch)
		ch <- ai.Chunk{Delta: "Hel"}
		<-ctx.Done()
	}()
	return ch, nil
}

func TestCancelKeepsPartialAnswer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	rec := &recorder{onEmit: func(string) { cancel() }}

	got, err := agent.Execute(ctx, baseRun(blocking{}, rec), user)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	last := got[len(got)-1]
	if last.Role != ai.RoleAssistant || last.Content != "Hel" || !last.Stopped {
		t.Fatalf("last = %#v", last)
	}
}

type failing struct{ err error }

func (f failing) Chat(ctx context.Context, req ai.Request) (<-chan ai.Chunk, error) { return nil, f.err }

func TestProviderErrorIsReturned(t *testing.T) {
	boom := errors.New("boom")
	got, err := agent.Execute(context.Background(), baseRun(failing{boom}, &recorder{}), user)
	if !errors.Is(err, boom) || !reflect.DeepEqual(got, user) {
		t.Fatalf("got %#v, err %v", got, err)
	}
}

func TestStreamErrorIsReturned(t *testing.T) {
	boom := errors.New("stream broke")
	p := &scripted{turns: [][]ai.Chunk{{{Delta: "part"}, {Err: boom}}}}
	got, err := agent.Execute(context.Background(), baseRun(p, &recorder{}), user)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if last := got[len(got)-1]; last.Content != "part" || last.Stopped {
		t.Fatalf("partial answer not kept: %#v", last)
	}
}

func TestTrim(t *testing.T) {
	var msgs []ai.Message
	for i := 0; i < 25; i++ {
		msgs = append(msgs,
			ai.Message{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{{ID: fmt.Sprint(i), Name: "t"}}},
			ai.Message{Role: ai.RoleTool, ToolName: "t", Content: fmt.Sprintf("result %d", i)},
		)
	}
	// 51 messages: the 40-message window would start on a tool result.
	msgs = append(msgs, ai.Message{Role: ai.RoleAssistant, Content: "done"})
	got := agent.Trim(msgs)

	if len(got) != agent.HistoryLimit-1 {
		t.Fatalf("len = %d (must drop the leading orphan tool message)", len(got))
	}
	if got[0].Role == ai.RoleTool {
		t.Fatal("trimmed history starts with a tool message")
	}
	for i, m := range got {
		recent := i >= len(got)-agent.KeepToolResults
		if m.Role != ai.RoleTool {
			continue
		}
		if recent && m.Content == agent.OmittedToolResult {
			t.Errorf("recent tool result %d omitted", i)
		}
		if !recent && m.Content != agent.OmittedToolResult {
			t.Errorf("old tool result %d kept: %q", i, m.Content)
		}
	}
	if msgs[len(msgs)-2].Content != "result 24" || msgs[1].Content != "result 0" {
		t.Fatal("Trim modified its input")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ai/agent/`
Expected: FAIL — `undefined: agent.Execute`.

- [ ] **Step 3: Implement**

`internal/ai/agent/agent.go`:
```go
// Package agent runs the repo chat: it streams model output, executes the
// tools the model asks for and feeds results back until the model answers.
package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"git-ui/internal/ai"
)

const (
	MaxSteps          = 8
	HistoryLimit      = 40
	KeepToolResults   = 10
	OmittedToolResult = "[earlier tool result omitted]"
	StepLimitNote     = "Step limit reached."

	EventDelta      = "chat:delta"
	EventTool       = "chat:tool"
	EventToolResult = "chat:tool_result"
	EventDone       = "chat:done"
	EventError      = "chat:error"
)

type DeltaEvent struct {
	RepoID string `json:"repoID"`
	RunID  string `json:"runID"`
	Text   string `json:"text"`
}

type ToolEvent struct {
	RepoID string         `json:"repoID"`
	RunID  string         `json:"runID"`
	Name   string         `json:"name"`
	Args   map[string]any `json:"args"`
}

type ToolResultEvent struct {
	RepoID  string `json:"repoID"`
	RunID   string `json:"runID"`
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

type DoneEvent struct {
	RepoID string `json:"repoID"`
	RunID  string `json:"runID"`
}

type ErrorEvent struct {
	RepoID  string `json:"repoID"`
	RunID   string `json:"runID"`
	Message string `json:"message"`
	Code    string `json:"code"`
}

type Run struct {
	RepoID, RunID string
	Provider      ai.Provider
	Model, System string
	Tools         []ai.ToolSpec
	RunTool       func(ctx context.Context, call ai.ToolCall) string
	Emit          func(name string, data any)
}

// Execute continues history (which ends with the user's message) and
// returns the updated history.
func Execute(ctx context.Context, r Run, history []ai.Message) ([]ai.Message, error) {
	msgs := append([]ai.Message(nil), history...)
	for step := 0; step < MaxSteps; step++ {
		stream, err := r.Provider.Chat(ctx, ai.Request{Model: r.Model, System: r.System, Messages: Trim(msgs), Tools: r.Tools})
		if err != nil {
			if ctx.Err() != nil {
				return append(msgs, ai.Message{Role: ai.RoleAssistant, Stopped: true}), ctx.Err()
			}
			return msgs, err
		}

		var text strings.Builder
		var calls []ai.ToolCall
		var streamErr error
		done := false
		for chunk := range stream {
			if chunk.Err != nil {
				streamErr = chunk.Err
				continue
			}
			if chunk.Delta != "" {
				text.WriteString(chunk.Delta)
				r.Emit(EventDelta, DeltaEvent{RepoID: r.RepoID, RunID: r.RunID, Text: chunk.Delta})
			}
			calls = append(calls, chunk.ToolCalls...)
			if chunk.Done {
				done = true
			}
		}

		if ctx.Err() != nil {
			return append(msgs, ai.Message{Role: ai.RoleAssistant, Content: text.String(), Stopped: true}), ctx.Err()
		}
		if streamErr != nil || !done {
			if streamErr == nil {
				streamErr = errors.New("agent: response ended unexpectedly")
			}
			if text.Len() > 0 {
				msgs = append(msgs, ai.Message{Role: ai.RoleAssistant, Content: text.String()})
			}
			return msgs, streamErr
		}

		for i := range calls {
			if calls[i].ID == "" {
				calls[i].ID = fmt.Sprintf("call_%d_%d", step, i)
			}
		}
		msgs = append(msgs, ai.Message{Role: ai.RoleAssistant, Content: text.String(), ToolCalls: calls})
		if len(calls) == 0 {
			return msgs, nil
		}

		for _, call := range calls {
			r.Emit(EventTool, ToolEvent{RepoID: r.RepoID, RunID: r.RunID, Name: call.Name, Args: call.Args})
			result := r.RunTool(ctx, call)
			r.Emit(EventToolResult, ToolResultEvent{RepoID: r.RepoID, RunID: r.RunID, Name: call.Name, Summary: summarize(result)})
			msgs = append(msgs, ai.Message{Role: ai.RoleTool, ToolName: call.Name, Content: result})
		}
	}

	r.Emit(EventDelta, DeltaEvent{RepoID: r.RepoID, RunID: r.RunID, Text: StepLimitNote})
	return append(msgs, ai.Message{Role: ai.RoleAssistant, Content: StepLimitNote}), nil
}

// Trim returns the part of the history sent to the model: at most
// HistoryLimit messages, never starting with a tool message, with tool
// results outside the last KeepToolResults messages replaced by a marker.
func Trim(msgs []ai.Message) []ai.Message {
	start := max(0, len(msgs)-HistoryLimit)
	for start < len(msgs) && msgs[start].Role == ai.RoleTool {
		start++
	}
	out := append([]ai.Message(nil), msgs[start:]...)
	for i := 0; i < len(out)-KeepToolResults; i++ {
		if out[i].Role == ai.RoleTool {
			out[i].Content = OmittedToolResult
		}
	}
	return out
}

func summarize(result string) string {
	line, _, _ := strings.Cut(result, "\n")
	if utf8.RuneCountInString(line) > 120 {
		line = string([]rune(line)[:120]) + "…"
	}
	return line
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ai/agent/`
Expected: `ok  git-ui/internal/ai/agent`

- [ ] **Step 5: Commit**

```bash
git add internal/ai/agent
git commit -m "feat(ai): chat agent loop with tool calls, step limit and history trim"
```

---

### Task 5: Apple Intelligence helper and client

**Files:**
- Create: `helpers/apple/Package.swift`
- Create: `helpers/apple/Sources/git-ui-apple/main.swift`
- Create: `internal/ai/apple/apple.go`
- Test: `internal/ai/apple/apple_test.go`
- Create: `Makefile` (helper target only; build/dev targets come in Task 12)
- Modify: `.gitignore` (add `helpers/apple/.build/`)

**Interfaces:**
- Consumes: `ai.Chunk`, `ai.Responder` (Task 1)
- Produces:
  - Executable `git-ui-apple` with subcommands `status` and `respond` (protocol below)
  - `apple.ErrHelperNotFound`
  - `apple.Availability{Available bool; Reason string}` (JSON `available`, `reason,omitempty`); reasons `deviceNotEligible`, `appleIntelligenceNotEnabled`, `modelNotReady`, `unknown`, plus client-side `helperNotFound`, `helperFailed`
  - `apple.DefaultTimeout = 60 * time.Second`
  - `apple.Locate() string` — helper next to the running executable, else `helpers/apple/.build/release/git-ui-apple` under the working directory, else `""`
  - `apple.New(path string) *Client` (`Client.Timeout` defaults to `DefaultTimeout`)
  - `(*Client).Status(ctx) Availability`
  - `(*Client).Respond(ctx, instructions, prompt string) (<-chan ai.Chunk, error)` — implements `ai.Responder`

Protocol (verified on this Mac: `SystemLanguageModel.default.availability` reports `.available`; `streamResponse` yields **cumulative** snapshots, so the helper sends only the new suffix):
- `git-ui-apple status` → one line `{"available":true}` or `{"available":false,"reason":"..."}`, exit 0.
- `git-ui-apple respond` → stdin `{"instructions":"...","prompt":"..."}`; stdout lines `{"delta":"..."}`… then `{"done":true}` (exit 0) or `{"error":"..."}` (exit 1).

- [ ] **Step 1: Write the Swift helper**

`helpers/apple/Package.swift`:
```swift
// swift-tools-version:6.2
import PackageDescription

let package = Package(
    name: "git-ui-apple",
    platforms: [.macOS(.v26)],
    targets: [
        .executableTarget(name: "git-ui-apple", path: "Sources/git-ui-apple")
    ]
)
```

`helpers/apple/Sources/git-ui-apple/main.swift`:
```swift
import Foundation
import FoundationModels

struct Input: Decodable {
    let instructions: String
    let prompt: String
}

func emit(_ object: [String: Any]) {
    guard let data = try? JSONSerialization.data(withJSONObject: object) else { return }
    FileHandle.standardOutput.write(data)
    FileHandle.standardOutput.write(Data("\n".utf8))
}

func status() {
    switch SystemLanguageModel.default.availability {
    case .available:
        emit(["available": true])
    case .unavailable(let reason):
        let name: String
        switch reason {
        case .deviceNotEligible: name = "deviceNotEligible"
        case .appleIntelligenceNotEnabled: name = "appleIntelligenceNotEnabled"
        case .modelNotReady: name = "modelNotReady"
        @unknown default: name = "unknown"
        }
        emit(["available": false, "reason": name])
    }
}

func respond() async -> Int32 {
    let data = FileHandle.standardInput.readDataToEndOfFile()
    guard let input = try? JSONDecoder().decode(Input.self, from: data) else {
        emit(["error": "invalid input"])
        return 1
    }
    let session = LanguageModelSession(instructions: input.instructions)
    var sent = ""
    do {
        for try await snapshot in session.streamResponse(to: input.prompt) {
            let text = snapshot.content
            if text.hasPrefix(sent) {
                let delta = String(text.dropFirst(sent.count))
                if !delta.isEmpty { emit(["delta": delta]) }
            } else {
                emit(["delta": text])
            }
            sent = text
        }
        emit(["done": true])
        return 0
    } catch let error as LanguageModelSession.GenerationError {
        switch error {
        case .exceededContextWindowSize:
            emit(["error": "The commit is too large for Apple Intelligence's context window."])
        case .guardrailViolation:
            emit(["error": "Apple Intelligence declined this request (safety guardrail)."])
        default:
            emit(["error": "Apple Intelligence failed: \(error)"])
        }
        return 1
    } catch {
        emit(["error": "Apple Intelligence failed: \(error)"])
        return 1
    }
}

let command = CommandLine.arguments.count > 1 ? CommandLine.arguments[1] : ""
switch command {
case "status":
    status()
case "respond":
    exit(await respond())
default:
    FileHandle.standardError.write(Data("usage: git-ui-apple status|respond\n".utf8))
    exit(2)
}
```

`Makefile`:
```makefile
HELPER := helpers/apple/.build/release/git-ui-apple

.PHONY: helper

helper:
	swift build -c release --package-path helpers/apple
```
(The recipe line must start with a tab.)

Append to `.gitignore`:
```
helpers/apple/.build/
```

- [ ] **Step 2: Build and smoke-test the helper**

```bash
cd /Users/josfh/playground/git-ui
make helper
helpers/apple/.build/release/git-ui-apple status
printf '{"instructions":"Answer in one short sentence.","prompt":"Say hello."}' | helpers/apple/.build/release/git-ui-apple respond
```
Expected: build succeeds; `status` prints `{"available":true}` on this Mac (or `{"available":false,"reason":...}` elsewhere); `respond` prints one or more `{"delta":...}` lines then `{"done":true}`. If `status` reports unavailable, note the reason in the report and continue — the Go tests below use a fake helper.

- [ ] **Step 3: Write the failing Go tests**

`internal/ai/apple/apple_test.go`:
```go
package apple_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git-ui/internal/ai"
	"git-ui/internal/ai/apple"
)

// fakeHelper writes an executable shell script and returns its path.
func fakeHelper(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "git-ui-apple")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func collect(ch <-chan ai.Chunk) []ai.Chunk {
	var out []ai.Chunk
	for c := range ch {
		out = append(out, c)
	}
	return out
}

func TestStatus(t *testing.T) {
	ok := apple.New(fakeHelper(t, `echo '{"available":true}'`)).Status(context.Background())
	if !ok.Available {
		t.Fatalf("status = %+v", ok)
	}
	off := apple.New(fakeHelper(t, `echo '{"available":false,"reason":"appleIntelligenceNotEnabled"}'`)).Status(context.Background())
	if off.Available || off.Reason != "appleIntelligenceNotEnabled" {
		t.Fatalf("status = %+v", off)
	}
	broken := apple.New(fakeHelper(t, `exit 3`)).Status(context.Background())
	if broken.Available || broken.Reason != "helperFailed" {
		t.Fatalf("status = %+v", broken)
	}
	missing := apple.New("").Status(context.Background())
	if missing.Available || missing.Reason != "helperNotFound" {
		t.Fatalf("status = %+v", missing)
	}
}

func TestRespondStreamsAndSendsInput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.json")
	helper := fakeHelper(t, `cat > '`+input+`'
echo '{"delta":"Hola"}'
echo '{"delta":" mundo"}'
echo '{"done":true}'
`)

	ch, err := apple.New(helper).Respond(context.Background(), "be brief", "commit text")
	if err != nil {
		t.Fatal(err)
	}
	chunks := collect(ch)
	if len(chunks) != 3 || chunks[0].Delta != "Hola" || chunks[1].Delta != " mundo" || !chunks[2].Done {
		t.Fatalf("chunks = %#v", chunks)
	}
	var sent map[string]string
	data, _ := os.ReadFile(input)
	if err := json.Unmarshal(data, &sent); err != nil || sent["instructions"] != "be brief" || sent["prompt"] != "commit text" {
		t.Fatalf("input = %s, err %v", data, err)
	}
}

func TestRespondErrorLine(t *testing.T) {
	helper := fakeHelper(t, `cat >/dev/null
echo '{"error":"too large"}'
exit 1
`)
	ch, err := apple.New(helper).Respond(context.Background(), "i", "p")
	if err != nil {
		t.Fatal(err)
	}
	chunks := collect(ch)
	if len(chunks) != 1 || chunks[0].Err == nil || chunks[0].Err.Error() != "too large" {
		t.Fatalf("chunks = %#v", chunks)
	}
}

func TestRespondExitWithoutDone(t *testing.T) {
	helper := fakeHelper(t, `cat >/dev/null
echo '{"delta":"partial"}'
exit 1
`)
	ch, _ := apple.New(helper).Respond(context.Background(), "i", "p")
	chunks := collect(ch)
	last := chunks[len(chunks)-1]
	if last.Err == nil {
		t.Fatalf("chunks = %#v", chunks)
	}
}

func TestRespondTimeout(t *testing.T) {
	helper := fakeHelper(t, `cat >/dev/null
exec sleep 5
`)
	c := apple.New(helper)
	c.Timeout = 200 * time.Millisecond
	start := time.Now()
	ch, err := c.Respond(context.Background(), "i", "p")
	if err != nil {
		t.Fatal(err)
	}
	chunks := collect(ch)
	if time.Since(start) > 3*time.Second {
		t.Fatal("timeout not enforced")
	}
	last := chunks[len(chunks)-1]
	if !errors.Is(last.Err, context.DeadlineExceeded) {
		t.Fatalf("last = %#v", last)
	}
}

func TestRespondMissingHelper(t *testing.T) {
	if _, err := apple.New("").Respond(context.Background(), "i", "p"); !errors.Is(err, apple.ErrHelperNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestLocateFindsBuiltHelperFromRepoRoot(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "helpers", "apple", ".build", "release", "git-ui-apple")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	if got := apple.Locate(); !strings.HasSuffix(got, "helpers/apple/.build/release/git-ui-apple") {
		t.Fatalf("Locate = %q", got)
	}
}
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `go test ./internal/ai/apple/`
Expected: FAIL — `undefined: apple.New`.

- [ ] **Step 5: Implement the Go client**

`internal/ai/apple/apple.go`:
```go
// Package apple runs Apple Intelligence through the bundled git-ui-apple
// helper, which wraps the FoundationModels framework.
package apple

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"git-ui/internal/ai"
)

const DefaultTimeout = 60 * time.Second

var ErrHelperNotFound = errors.New("apple intelligence helper not found")

type Availability struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

type Client struct {
	Path    string
	Timeout time.Duration
}

func New(path string) *Client { return &Client{Path: path, Timeout: DefaultTimeout} }

// Locate finds the helper next to the running executable (inside the .app),
// or in the Swift build folder when running from the repository.
func Locate() string {
	if exe, err := os.Executable(); err == nil {
		if p := filepath.Join(filepath.Dir(exe), "git-ui-apple"); isExecutable(p) {
			return p
		}
	}
	if wd, err := os.Getwd(); err == nil {
		if p := filepath.Join(wd, "helpers", "apple", ".build", "release", "git-ui-apple"); isExecutable(p) {
			return p
		}
	}
	return ""
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

func (c *Client) Status(ctx context.Context) Availability {
	if c.Path == "" {
		return Availability{Reason: "helperNotFound"}
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, c.Path, "status").Output()
	if err != nil {
		return Availability{Reason: "helperFailed"}
	}
	var a Availability
	if err := json.Unmarshal(bytes.TrimSpace(out), &a); err != nil {
		return Availability{Reason: "helperFailed"}
	}
	return a
}

func (c *Client) Respond(ctx context.Context, instructions, prompt string) (<-chan ai.Chunk, error) {
	if c.Path == "" {
		return nil, ErrHelperNotFound
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)

	input, err := json.Marshal(map[string]string{"instructions": instructions, "prompt": prompt})
	if err != nil {
		cancel()
		return nil, err
	}
	cmd := exec.CommandContext(ctx, c.Path, "respond")
	cmd.Stdin = bytes.NewReader(input)
	cmd.WaitDelay = 2 * time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}

	ch := make(chan ai.Chunk)
	go func() {
		defer cancel()
		defer close(ch)
		finished := false
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scanner.Scan() && !finished {
			var line struct {
				Delta string `json:"delta"`
				Done  bool   `json:"done"`
				Error string `json:"error"`
			}
			if json.Unmarshal(scanner.Bytes(), &line) != nil {
				continue
			}
			switch {
			case line.Error != "":
				ch <- ai.Chunk{Err: errors.New(line.Error)}
				finished = true
			case line.Done:
				ch <- ai.Chunk{Done: true}
				finished = true
			case line.Delta != "":
				ch <- ai.Chunk{Delta: line.Delta}
			}
		}
		waitErr := cmd.Wait()
		if finished {
			return
		}
		err := waitErr
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		if err == nil {
			err = errors.New("apple intelligence helper exited without an answer")
		}
		ch <- ai.Chunk{Err: err}
	}()
	return ch, nil
}
```

Consumers must drain the channel until it closes (sends are unconditional so the final error is never lost).

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/ai/apple/`
Expected: `ok  git-ui/internal/ai/apple`

- [ ] **Step 7: Commit**

```bash
git add helpers/apple/Package.swift helpers/apple/Sources Makefile .gitignore internal/ai/apple
git commit -m "feat(ai): Apple Intelligence helper and Go client"
```

---

### Task 6: Explain-commit task

**Files:**
- Create: `internal/ai/tasks/explain.go`
- Test: `internal/ai/tasks/explain_test.go`

**Interfaces:**
- Consumes: `ai.Responder`, `ai.Chunk` (Task 1); `tools.Truncate(s, max) string` (Task 3); `gitlog.GetDetails`, `gitlog.Diff` (existing); `testrepo`.
- Produces:
  - `tasks.OllamaDiffBudget = 6000`, `tasks.AppleDiffBudget = 3000`
  - `tasks.ExplainContext(ctx, dir, hash string, budget int) (string, error)` — text with `Commit <short> by <author> on <YYYY-MM-DD>`, `Subject: …`, optional `Body:`, `Files:` list, `Diff:` truncated to `budget` bytes
  - `tasks.Explain(ctx, r ai.Responder, instructions, dir, hash string, budget int) (<-chan ai.Chunk, error)`

- [ ] **Step 1: Write the failing tests**

`internal/ai/tasks/explain_test.go`:
```go
package tasks_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"git-ui/internal/ai"
	"git-ui/internal/ai/tasks"
	"git-ui/internal/testrepo"
)

func bigCommit(t *testing.T) (*testrepo.Repo, string) {
	t.Helper()
	r := testrepo.New(t)
	r.Commit("base")
	var b strings.Builder
	for i := 0; i < 800; i++ {
		fmt.Fprintf(&b, "config line number %d\n", i)
	}
	r.WriteFile("config.txt", b.String())
	r.Git("add", "config.txt")
	r.Git("commit", "-q", "-m", "feat: add config", "-m", "Needed for NEXO-7.")
	return r, r.Git("rev-parse", "HEAD")
}

func TestExplainContextTruncatesPerBudget(t *testing.T) {
	r, hash := bigCommit(t)
	ctx := context.Background()

	apple, err := tasks.ExplainContext(ctx, r.Dir, hash, tasks.AppleDiffBudget)
	if err != nil {
		t.Fatal(err)
	}
	ollama, err := tasks.ExplainContext(ctx, r.Dir, hash, tasks.OllamaDiffBudget)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Commit " + hash[:7], "Subject: feat: add config", "Needed for NEXO-7.", "A config.txt", "Diff:", "[truncated]"} {
		if !strings.Contains(apple, want) {
			t.Errorf("context missing %q", want)
		}
	}
	appleDiff := apple[strings.Index(apple, "Diff:"):]
	ollamaDiff := ollama[strings.Index(ollama, "Diff:"):]
	if len(appleDiff) > tasks.AppleDiffBudget+40 || len(ollamaDiff) > tasks.OllamaDiffBudget+40 {
		t.Fatalf("diff over budget: apple %d, ollama %d", len(appleDiff), len(ollamaDiff))
	}
	if len(ollamaDiff) <= len(appleDiff) {
		t.Fatal("Ollama budget should include more of the diff")
	}
}

type fakeResponder struct{ instructions, prompt string }

func (f *fakeResponder) Respond(ctx context.Context, instructions, prompt string) (<-chan ai.Chunk, error) {
	f.instructions, f.prompt = instructions, prompt
	ch := make(chan ai.Chunk, 2)
	ch <- ai.Chunk{Delta: "- adds config"}
	ch <- ai.Chunk{Done: true}
	close(ch)
	return ch, nil
}

func TestExplainPassesInstructionsAndContext(t *testing.T) {
	r, hash := bigCommit(t)
	f := &fakeResponder{}

	ch, err := tasks.Explain(context.Background(), f, "explain it", r.Dir, hash, tasks.OllamaDiffBudget)
	if err != nil {
		t.Fatal(err)
	}
	var text string
	for c := range ch {
		text += c.Delta
	}
	if text != "- adds config" || f.instructions != "explain it" || !strings.Contains(f.prompt, "Subject: feat: add config") {
		t.Fatalf("text %q, instructions %q, prompt %q", text, f.instructions, f.prompt)
	}
}

func TestExplainUnknownCommit(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	if _, err := tasks.Explain(context.Background(), &fakeResponder{}, "i", r.Dir, "deadbeefdeadbeef", 100); err == nil {
		t.Fatal("expected error for unknown commit")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ai/tasks/`
Expected: FAIL — `undefined: tasks.ExplainContext`.

- [ ] **Step 3: Implement**

`internal/ai/tasks/explain.go`:
```go
// Package tasks implements one-shot AI actions that don't need tools.
package tasks

import (
	"context"
	"fmt"
	"strings"

	"git-ui/internal/ai"
	"git-ui/internal/ai/tools"
	"git-ui/internal/gitlog"
)

const (
	OllamaDiffBudget = 6000
	AppleDiffBudget  = 3000
)

// ExplainContext describes a commit for the model, with the diff against its
// first parent cut to budget bytes.
func ExplainContext(ctx context.Context, dir, hash string, budget int) (string, error) {
	d, err := gitlog.GetDetails(ctx, dir, hash)
	if err != nil {
		return "", err
	}
	parent := ""
	if len(d.Parents) > 0 {
		parent = d.Parents[0]
	}
	diff, err := gitlog.Diff(ctx, dir, parent, d.Hash, nil)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Commit %s by %s on %s\n\nSubject: %s\n", d.Short, d.Author, d.Date.Format("2006-01-02"), d.Subject)
	if d.Body != "" {
		fmt.Fprintf(&b, "\nBody:\n%s\n", d.Body)
	}
	b.WriteString("\nFiles:\n")
	for _, f := range d.Files {
		if f.OldPath != "" {
			fmt.Fprintf(&b, "%s %s -> %s\n", f.Status, f.OldPath, f.Path)
		} else {
			fmt.Fprintf(&b, "%s %s\n", f.Status, f.Path)
		}
	}
	b.WriteString("\nDiff:\n")
	b.WriteString(tools.Truncate(diff, budget))
	return b.String(), nil
}

func Explain(ctx context.Context, r ai.Responder, instructions, dir, hash string, budget int) (<-chan ai.Chunk, error) {
	prompt, err := ExplainContext(ctx, dir, hash, budget)
	if err != nil {
		return nil, err
	}
	return r.Respond(ctx, instructions, prompt)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ai/tasks/`
Expected: `ok  git-ui/internal/ai/tasks`

- [ ] **Step 5: Commit**

```bash
git add internal/ai/tasks
git commit -m "feat(ai): explain-commit context and task"
```

---

### Task 7: App AI API and wiring

**Files:**
- Create: `internal/app/ai.go`
- Test: `internal/app/ai_test.go`
- Modify: `internal/app/app.go` (add one field to `App`)
- Modify: `main.go` (enable AI)
- Generated: `frontend/wailsjs/**` (`wails generate module`)

**Interfaces:**
- Consumes: Tasks 1–6 (`settings`, `chatstore`, `prompts`, `ollama`, `tools`, `agent`, `apple`, `tasks`); existing `App.dir(id)`, `App.store.Get(id)`, `refs.CurrentLabel(ctx, dir) string`.
- Produces (exported methods become frontend bindings):
  - `app.AIDeps{SettingsPath string; Chats *chatstore.Store; Prompts *prompts.Store; Apple *apple.Client; Emit func(name string, data any)}`; `(*App).EnableAI(AIDeps)` — nil `Emit` uses Wails `runtime.EventsEmit`
  - `app.ErrAIDisabled`, `app.ErrChatBusy`, `app.ErrPullBusy`
  - `app.OllamaStatus{Running bool; URL, ChatModel string; Models []ollama.Model; ChatModelInstalled bool; Error string}` (JSON `running`, `url`, `chatModel`, `models`, `chatModelInstalled`, `error,omitempty`), `app.AIStatus{Ollama OllamaStatus; Apple apple.Availability}` (JSON `ollama`, `apple`)
  - `app.ModelProgress{Name, Status string; Completed, Total int64}` (JSON `name`, `status`, `completed`, `total`), `app.ModelDone{Name, Error string}` (JSON `name`, `error,omitempty`)
  - `app.ExplainDelta{RunID, Text}` (JSON `runID`, `text`), `app.ExplainDone{RunID}`, `app.ExplainError{RunID, Message}` (JSON `runID`, `message`)
  - Methods: `AIStatus() AIStatus`; `GetAISettings() (settings.Settings, error)`; `SaveAISettings(settings.Settings) error`; `PullModel(name string) error`; `CancelPull() error`; `GetChat(repoID string) ([]ai.Message, error)`; `SendChat(repoID, text, runID string) error`; `StopChat(repoID string) error`; `ClearChat(repoID string) error`; `ExplainCommit(repoID, hash, provider, runID string) error`; `ListPrompts() ([]prompts.Info, error)`; `OpenPromptsFolder() error`; `ResetPrompt(name string) error`
  - Events emitted: `chat:*` (payload types from `agent`), `explain:delta|done|error`, `model:progress|done`

- [ ] **Step 1: Add the field to `App`**

In `internal/app/app.go`, change the `App` struct to:
```go
type App struct {
	ctx    context.Context
	store  *repos.Store
	mu     sync.Mutex
	logs   map[string]*logState
	writes sync.Map // repo ID → *sync.Mutex
	ai     *aiState
}
```

- [ ] **Step 2: Write the failing tests**

`internal/app/ai_test.go`:
```go
package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"git-ui/internal/ai"
	"git-ui/internal/ai/agent"
	"git-ui/internal/ai/apple"
	"git-ui/internal/ai/chatstore"
	"git-ui/internal/ai/prompts"
	"git-ui/internal/ai/settings"
	"git-ui/internal/gitlog"
)

type event struct {
	name string
	data any
}

type events struct {
	mu   sync.Mutex
	list []event
	ch   chan event
}

func newEvents() *events { return &events{ch: make(chan event, 1000)} }

func (e *events) emit(name string, data any) {
	e.mu.Lock()
	e.list = append(e.list, event{name, data})
	e.mu.Unlock()
	e.ch <- event{name, data}
}

// wait returns the first event with the given name, failing after 5 s.
func (e *events) wait(t *testing.T, name string) event {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case ev := <-e.ch:
			if ev.name == name {
				return ev
			}
		case <-timeout:
			t.Fatalf("timed out waiting for %s; got %v", name, e.names())
		}
	}
}

func (e *events) names() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []string
	for _, ev := range e.list {
		out = append(out, ev.name)
	}
	return out
}

func newAIApp(t *testing.T, ollamaURL string) (*App, string, *events) {
	t.Helper()
	a, id := newTestApp(t)
	dir := t.TempDir()
	ev := newEvents()
	a.EnableAI(AIDeps{
		SettingsPath: filepath.Join(dir, "ai.json"),
		Chats:        chatstore.New(filepath.Join(dir, "chats")),
		Prompts:      prompts.New(filepath.Join(dir, "prompts")),
		Apple:        apple.New(""),
		Emit:         ev.emit,
	})
	s, err := a.GetAISettings()
	if err != nil {
		t.Fatal(err)
	}
	s.OllamaURL = ollamaURL
	if err := a.SaveAISettings(s); err != nil {
		t.Fatal(err)
	}
	return a, id, ev
}

func writeLines(w http.ResponseWriter, lines ...string) {
	for _, l := range lines {
		fmt.Fprintln(w, l)
		w.(http.Flusher).Flush()
	}
}

// fakeOllama answers /api/tags, /api/pull and /api/chat. The chat handler asks
// for list_refs first and answers with text once a tool result is present.
func fakeOllama(t *testing.T, onChat func(req map[string]any)) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			fmt.Fprint(w, `{"models":[{"name":"qwen2.5:7b","size":4683087332}]}`)
		case "/api/pull":
			writeLines(w, `{"status":"pulling manifest"}`, `{"status":"downloading","total":10,"completed":5}`, `{"status":"success"}`)
		case "/api/chat":
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			if onChat != nil {
				onChat(req)
			}
			msgs := req["messages"].([]any)
			last := msgs[len(msgs)-1].(map[string]any)
			if _, hasTools := req["tools"]; hasTools && last["role"] != "tool" {
				writeLines(w, `{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","function":{"name":"list_refs","arguments":{}}}]},"done":false}`, `{"message":{"content":""},"done":true}`)
				return
			}
			writeLines(w, `{"message":{"role":"assistant","content":"Hay "},"done":false}`, `{"message":{"content":"ramas."},"done":false}`, `{"message":{"content":""},"done":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAIDisabledByDefault(t *testing.T) {
	a, id := newTestApp(t)
	if err := a.SendChat(id, "hi", "run"); !errors.Is(err, ErrAIDisabled) {
		t.Fatalf("err = %v", err)
	}
}

func TestSendChatRunsToolsStreamsAndSaves(t *testing.T) {
	var system string
	srv := fakeOllama(t, func(req map[string]any) {
		first := req["messages"].([]any)[0].(map[string]any)
		if first["role"] == "system" {
			system = first["content"].(string)
		}
	})
	a, id, ev := newAIApp(t, srv.URL)

	if err := a.SendChat(id, "¿qué ramas hay?", "run-1"); err != nil {
		t.Fatal(err)
	}
	done := ev.wait(t, agent.EventDone)
	if done.data != (agent.DoneEvent{RepoID: id, RunID: "run-1"}) {
		t.Fatalf("done = %#v", done.data)
	}
	names := strings.Join(ev.names(), ",")
	if names != "chat:tool,chat:tool_result,chat:delta,chat:delta,chat:done" {
		t.Fatalf("events = %s", names)
	}
	if !strings.Contains(system, "read-only") || !strings.Contains(system, "main") {
		t.Fatalf("system prompt = %q", system)
	}

	history, err := a.GetChat(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 4 || history[0].Content != "¿qué ramas hay?" || history[2].Role != ai.RoleTool ||
		!strings.Contains(history[2].Content, "Current branch: main") || history[3].Content != "Hay ramas." {
		t.Fatalf("history = %#v", history)
	}

	if err := a.ClearChat(id); err != nil {
		t.Fatal(err)
	}
	if h, _ := a.GetChat(id); len(h) != 0 {
		t.Fatalf("after clear = %#v", h)
	}
}

func TestSendChatBusyAndStop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeLines(w, `{"message":{"role":"assistant","content":"Pensando"},"done":false}`)
		<-r.Context().Done()
	}))
	defer srv.Close()
	a, id, ev := newAIApp(t, srv.URL)

	if err := a.SendChat(id, "hola", "run-1"); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventDelta)
	if err := a.SendChat(id, "otra", "run-2"); !errors.Is(err, ErrChatBusy) {
		t.Fatalf("second send: %v", err)
	}
	if err := a.ClearChat(id); !errors.Is(err, ErrChatBusy) {
		t.Fatalf("clear while running: %v", err)
	}
	if err := a.StopChat(id); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventDone)

	history, _ := a.GetChat(id)
	last := history[len(history)-1]
	if last.Content != "Pensando" || !last.Stopped {
		t.Fatalf("last = %#v", last)
	}
	if err := a.SendChat(id, "de nuevo", "run-3"); err != nil {
		t.Fatalf("send after stop: %v", err)
	}
	ev.wait(t, agent.EventDelta)
	if err := a.StopChat(id); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventDone)
}

func TestSendChatReportsOllamaDown(t *testing.T) {
	a, id, ev := newAIApp(t, "http://127.0.0.1:1")
	if err := a.SendChat(id, "hola", "run-1"); err != nil {
		t.Fatal(err)
	}
	got := ev.wait(t, agent.EventError).data.(agent.ErrorEvent)
	if got.Code != "ollama_down" || got.RunID != "run-1" {
		t.Fatalf("error event = %#v", got)
	}
}

func TestExplainCommitWithOllama(t *testing.T) {
	var system, prompt string
	srv := fakeOllama(t, func(req map[string]any) {
		msgs := req["messages"].([]any)
		system = msgs[0].(map[string]any)["content"].(string)
		prompt = msgs[1].(map[string]any)["content"].(string)
	})
	a, id, ev := newAIApp(t, srv.URL)
	head, err := a.GetLog(id, gitlog.Filters{}, 0, 1)
	if err != nil {
		t.Fatal(err)
	}

	if err := a.ExplainCommit(id, head.Rows[0].Hash, "ollama", "exp-1"); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, "explain:done")
	if !strings.Contains(strings.Join(ev.names(), ","), "explain:delta") {
		t.Fatalf("events = %v", ev.names())
	}
	if !strings.Contains(system, "3 to 6") || !strings.Contains(prompt, "Subject: Merge feature") {
		t.Fatalf("system %q\nprompt %q", system, prompt)
	}
}

func TestExplainCommitWithMissingAppleHelper(t *testing.T) {
	srv := fakeOllama(t, nil)
	a, id, ev := newAIApp(t, srv.URL)
	head, _ := a.GetLog(id, gitlog.Filters{}, 0, 1)

	if err := a.ExplainCommit(id, head.Rows[0].Hash, "apple", "exp-2"); err != nil {
		t.Fatal(err)
	}
	got := ev.wait(t, "explain:error").data.(ExplainError)
	if got.RunID != "exp-2" || !strings.Contains(got.Message, "helper not found") {
		t.Fatalf("error = %#v", got)
	}
}

func TestAIStatusAndPull(t *testing.T) {
	srv := fakeOllama(t, nil)
	a, _, ev := newAIApp(t, srv.URL)

	st := a.AIStatus()
	if !st.Ollama.Running || !st.Ollama.ChatModelInstalled || len(st.Ollama.Models) != 1 || st.Apple.Reason != "helperNotFound" {
		t.Fatalf("status = %+v", st)
	}

	if err := a.PullModel("qwen2.5:7b"); err != nil {
		t.Fatal(err)
	}
	done := ev.wait(t, "model:done").data.(ModelDone)
	if done.Name != "qwen2.5:7b" || done.Error != "" {
		t.Fatalf("done = %#v", done)
	}
	if !strings.Contains(strings.Join(ev.names(), ","), "model:progress") {
		t.Fatalf("no progress events: %v", ev.names())
	}
}

func TestSettingsValidationAndPrompts(t *testing.T) {
	a, _, _ := newAIApp(t, "http://localhost:11434")
	if err := a.SaveAISettings(settings.Settings{OllamaURL: "nope"}); !errors.Is(err, settings.ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
	list, err := a.ListPrompts()
	if err != nil || len(list) != 2 || list[0].Customized {
		t.Fatalf("prompts = %+v, err %v", list, err)
	}
	if err := a.ResetPrompt("explain-commit"); err != nil {
		t.Fatal(err)
	}
	if err := a.ResetPrompt("nope"); !errors.Is(err, prompts.ErrUnknownPrompt) {
		t.Fatalf("err = %v", err)
	}
}

func TestSendChatValidatesInput(t *testing.T) {
	a, id, _ := newAIApp(t, "http://localhost:11434")
	if err := a.SendChat(id, "   ", "run"); err == nil {
		t.Fatal("empty message accepted")
	}
	if err := a.SendChat(id, "hola", ""); err == nil {
		t.Fatal("empty run id accepted")
	}
	if err := a.SendChat("unknown", "hola", "run"); err == nil {
		t.Fatal("unknown repo accepted")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/app/ -run 'AI|Chat|Explain|Prompts|Pull'`
Expected: FAIL — `undefined: aiState`, `undefined: AIDeps`.

- [ ] **Step 4: Implement**

`internal/app/ai.go`:
```go
package app

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"git-ui/internal/ai"
	"git-ui/internal/ai/agent"
	"git-ui/internal/ai/apple"
	"git-ui/internal/ai/chatstore"
	"git-ui/internal/ai/ollama"
	"git-ui/internal/ai/prompts"
	"git-ui/internal/ai/settings"
	"git-ui/internal/ai/tasks"
	"git-ui/internal/ai/tools"
	"git-ui/internal/refs"
)

var (
	ErrAIDisabled = errors.New("AI features are not enabled")
	ErrChatBusy   = errors.New("a chat answer is already running for this repository")
	ErrPullBusy   = errors.New("a model download is already running")
)

type AIDeps struct {
	SettingsPath string
	Chats        *chatstore.Store
	Prompts      *prompts.Store
	Apple        *apple.Client
	// Emit sends an event to the frontend; nil uses the Wails runtime.
	Emit func(name string, data any)
}

type aiState struct {
	deps       AIDeps
	mu         sync.Mutex
	runs       map[string]context.CancelFunc // repo ID → running chat
	pullCancel context.CancelFunc
}

type OllamaStatus struct {
	Running            bool           `json:"running"`
	URL                string         `json:"url"`
	ChatModel          string         `json:"chatModel"`
	Models             []ollama.Model `json:"models"`
	ChatModelInstalled bool           `json:"chatModelInstalled"`
	Error              string         `json:"error,omitempty"`
}

type AIStatus struct {
	Ollama OllamaStatus       `json:"ollama"`
	Apple  apple.Availability `json:"apple"`
}

type ModelProgress struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Completed int64  `json:"completed"`
	Total     int64  `json:"total"`
}

type ModelDone struct {
	Name  string `json:"name"`
	Error string `json:"error,omitempty"`
}

type ExplainDelta struct {
	RunID string `json:"runID"`
	Text  string `json:"text"`
}

type ExplainDone struct {
	RunID string `json:"runID"`
}

type ExplainError struct {
	RunID   string `json:"runID"`
	Message string `json:"message"`
}

// EnableAI turns on the AI API with the given dependencies.
func (a *App) EnableAI(d AIDeps) {
	a.ai = &aiState{deps: d, runs: map[string]context.CancelFunc{}}
}

func (a *App) emit(name string, data any) {
	if a.ai.deps.Emit != nil {
		a.ai.deps.Emit(name, data)
		return
	}
	runtime.EventsEmit(a.ctx, name, data)
}

func (a *App) aiSettings() (settings.Settings, error) {
	if a.ai == nil {
		return settings.Settings{}, ErrAIDisabled
	}
	return settings.Load(a.ai.deps.SettingsPath, func() bool { return a.ai.deps.Apple.Status(a.ctx).Available })
}

func (a *App) GetAISettings() (settings.Settings, error) { return a.aiSettings() }

func (a *App) SaveAISettings(s settings.Settings) error {
	if a.ai == nil {
		return ErrAIDisabled
	}
	return settings.Save(a.ai.deps.SettingsPath, s)
}

func (a *App) AIStatus() AIStatus {
	st := AIStatus{Ollama: OllamaStatus{Models: []ollama.Model{}}}
	cfg, err := a.aiSettings()
	if err != nil {
		st.Ollama.Error = err.Error()
		st.Apple = apple.Availability{Reason: "unknown"}
		return st
	}
	st.Ollama.URL, st.Ollama.ChatModel = cfg.OllamaURL, cfg.ChatModel
	ctx, cancel := context.WithTimeout(a.ctx, 2*time.Second)
	defer cancel()
	if models, err := ollama.New(cfg.OllamaURL).ListModels(ctx); err != nil {
		st.Ollama.Error = err.Error()
	} else {
		st.Ollama.Running, st.Ollama.Models = true, models
		for _, m := range models {
			if m.Name == cfg.ChatModel || m.Name == cfg.ChatModel+":latest" {
				st.Ollama.ChatModelInstalled = true
			}
		}
	}
	st.Apple = a.ai.deps.Apple.Status(a.ctx)
	return st
}

func (a *App) PullModel(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("model name is required")
	}
	cfg, err := a.aiSettings()
	if err != nil {
		return err
	}
	a.ai.mu.Lock()
	if a.ai.pullCancel != nil {
		a.ai.mu.Unlock()
		return ErrPullBusy
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.ai.pullCancel = cancel
	a.ai.mu.Unlock()

	go func() {
		defer func() {
			a.ai.mu.Lock()
			a.ai.pullCancel = nil
			a.ai.mu.Unlock()
			cancel()
		}()
		var last time.Time
		lastStatus := ""
		err := ollama.New(cfg.OllamaURL).Pull(ctx, name, func(status string, completed, total int64) {
			if status == lastStatus && time.Since(last) < 150*time.Millisecond {
				return
			}
			last, lastStatus = time.Now(), status
			a.emit("model:progress", ModelProgress{Name: name, Status: status, Completed: completed, Total: total})
		})
		done := ModelDone{Name: name}
		if err != nil {
			done.Error = err.Error()
		}
		a.emit("model:done", done)
	}()
	return nil
}

func (a *App) CancelPull() error {
	if a.ai == nil {
		return ErrAIDisabled
	}
	a.ai.mu.Lock()
	defer a.ai.mu.Unlock()
	if a.ai.pullCancel != nil {
		a.ai.pullCancel()
	}
	return nil
}

func (a *App) GetChat(repoID string) ([]ai.Message, error) {
	if a.ai == nil {
		return nil, ErrAIDisabled
	}
	return a.ai.deps.Chats.Load(repoID)
}

func (a *App) SendChat(repoID, text, runID string) error {
	if a.ai == nil {
		return ErrAIDisabled
	}
	text = strings.TrimSpace(text)
	if text == "" || runID == "" {
		return errors.New("message and run id are required")
	}
	repo, ok := a.store.Get(repoID)
	if !ok {
		return fmt.Errorf("unknown repository %q", repoID)
	}
	cfg, err := a.aiSettings()
	if err != nil {
		return err
	}

	a.ai.mu.Lock()
	if _, busy := a.ai.runs[repoID]; busy {
		a.ai.mu.Unlock()
		return ErrChatBusy
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.ai.runs[repoID] = cancel
	a.ai.mu.Unlock()
	finish := func() {
		a.ai.mu.Lock()
		delete(a.ai.runs, repoID)
		a.ai.mu.Unlock()
		cancel()
	}

	history, err := a.ai.deps.Chats.Load(repoID)
	if err == nil {
		history = append(history, ai.Message{Role: ai.RoleUser, Content: text})
		err = a.ai.deps.Chats.Save(repoID, history)
	}
	var system string
	if err == nil {
		system, err = a.ai.deps.Prompts.Get(prompts.Chat, prompts.Vars{
			Repo: repo.Name, Path: repo.Path, Branch: refs.CurrentLabel(ctx, repo.Path), Date: time.Now().Format("2006-01-02"),
		})
	}
	if err != nil {
		finish()
		return err
	}

	go func() {
		run := agent.Run{
			RepoID: repoID, RunID: runID,
			Provider: ollama.New(cfg.OllamaURL), Model: cfg.ChatModel, System: system,
			Tools:   tools.Specs(),
			RunTool: func(ctx context.Context, call ai.ToolCall) string { return tools.Run(ctx, repo.Path, call) },
			Emit:    a.emit,
		}
		updated, runErr := agent.Execute(ctx, run, history)
		saveErr := a.ai.deps.Chats.Save(repoID, updated)
		// Release the repo before announcing the end so a new message can
		// be sent as soon as the frontend sees done/error.
		finish()
		switch {
		case runErr != nil && !errors.Is(runErr, context.Canceled):
			a.emit(agent.EventError, agent.ErrorEvent{RepoID: repoID, RunID: runID, Message: runErr.Error(), Code: chatErrorCode(runErr)})
		case saveErr != nil:
			a.emit(agent.EventError, agent.ErrorEvent{RepoID: repoID, RunID: runID, Message: saveErr.Error(), Code: "other"})
		default:
			a.emit(agent.EventDone, agent.DoneEvent{RepoID: repoID, RunID: runID})
		}
	}()
	return nil
}

func chatErrorCode(err error) string {
	switch {
	case errors.Is(err, ollama.ErrUnreachable):
		return "ollama_down"
	case errors.Is(err, ollama.ErrModelNotFound):
		return "model_missing"
	case errors.Is(err, ollama.ErrNoToolSupport):
		return "no_tool_support"
	}
	return "other"
}

func (a *App) StopChat(repoID string) error {
	if a.ai == nil {
		return ErrAIDisabled
	}
	a.ai.mu.Lock()
	defer a.ai.mu.Unlock()
	if cancel, ok := a.ai.runs[repoID]; ok {
		cancel()
	}
	return nil
}

func (a *App) ClearChat(repoID string) error {
	if a.ai == nil {
		return ErrAIDisabled
	}
	a.ai.mu.Lock()
	_, busy := a.ai.runs[repoID]
	a.ai.mu.Unlock()
	if busy {
		return ErrChatBusy
	}
	return a.ai.deps.Chats.Clear(repoID)
}

// ExplainCommit streams an explanation of a commit. provider is "" (use
// settings), settings.ProviderApple or settings.ProviderOllama.
func (a *App) ExplainCommit(repoID, hash, provider, runID string) error {
	if a.ai == nil {
		return ErrAIDisabled
	}
	if runID == "" {
		return errors.New("run id is required")
	}
	repo, ok := a.store.Get(repoID)
	if !ok {
		return fmt.Errorf("unknown repository %q", repoID)
	}
	cfg, err := a.aiSettings()
	if err != nil {
		return err
	}
	if provider == "" {
		provider = cfg.TaskProvider
	}
	var responder ai.Responder
	budget := tasks.OllamaDiffBudget
	switch provider {
	case settings.ProviderApple:
		responder, budget = a.ai.deps.Apple, tasks.AppleDiffBudget
	case settings.ProviderOllama:
		responder = ollama.New(cfg.OllamaURL).Responder(cfg.TaskModel)
	default:
		return fmt.Errorf("unknown provider %q", provider)
	}
	instructions, err := a.ai.deps.Prompts.Get(prompts.ExplainCommit, prompts.Vars{
		Repo: repo.Name, Path: repo.Path, Branch: refs.CurrentLabel(a.ctx, repo.Path), Date: time.Now().Format("2006-01-02"),
	})
	if err != nil {
		return err
	}

	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 3*time.Minute)
		defer cancel()
		stream, err := tasks.Explain(ctx, responder, instructions, repo.Path, hash, budget)
		if err != nil {
			a.emit("explain:error", ExplainError{RunID: runID, Message: err.Error()})
			return
		}
		var streamErr error
		for chunk := range stream {
			switch {
			case chunk.Err != nil:
				streamErr = chunk.Err
			case chunk.Delta != "":
				a.emit("explain:delta", ExplainDelta{RunID: runID, Text: chunk.Delta})
			}
		}
		if streamErr != nil {
			a.emit("explain:error", ExplainError{RunID: runID, Message: streamErr.Error()})
			return
		}
		a.emit("explain:done", ExplainDone{RunID: runID})
	}()
	return nil
}

func (a *App) ListPrompts() ([]prompts.Info, error) {
	if a.ai == nil {
		return nil, ErrAIDisabled
	}
	return a.ai.deps.Prompts.List(), nil
}

func (a *App) OpenPromptsFolder() error {
	if a.ai == nil {
		return ErrAIDisabled
	}
	if err := a.ai.deps.Prompts.EnsureFiles(); err != nil {
		return err
	}
	return exec.Command("open", a.ai.deps.Prompts.Dir()).Start()
}

func (a *App) ResetPrompt(name string) error {
	if a.ai == nil {
		return ErrAIDisabled
	}
	return a.ai.deps.Prompts.Reset(name)
}
```

`apple.Client.Respond` with an empty `Path` returns `apple.ErrHelperNotFound` ("apple intelligence helper not found"), which `TestExplainCommitWithMissingAppleHelper` expects inside `explain:error`.

- [ ] **Step 5: Wire AI in `main.go`**

In `main.go`, add imports:
```go
	"git-ui/internal/ai/apple"
	"git-ui/internal/ai/chatstore"
	"git-ui/internal/ai/prompts"
	"git-ui/internal/ai/settings"
```
and after `api := app.New(store)` add:
```go
	settingsPath, err := settings.DefaultPath()
	if err != nil {
		log.Fatal(err)
	}
	chatsDir, err := chatstore.DefaultDir()
	if err != nil {
		log.Fatal(err)
	}
	promptsDir, err := prompts.DefaultDir()
	if err != nil {
		log.Fatal(err)
	}
	api.EnableAI(app.AIDeps{
		SettingsPath: settingsPath,
		Chats:        chatstore.New(chatsDir),
		Prompts:      prompts.New(promptsDir),
		Apple:        apple.New(apple.Locate()),
	})
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go vet ./... && go test ./...`
Expected: all packages `ok`.

- [ ] **Step 7: Regenerate bindings**

```bash
source ~/.nvm/nvm.sh && nvm use 22
cd /Users/josfh/playground/git-ui && ~/go/bin/wails generate module
grep -c "SendChat\|ExplainCommit\|PullModel" frontend/wailsjs/go/app/App.d.ts
```
Expected: the count is at least 3. Restore file-mode-only churn with `git checkout -- frontend/wailsjs/runtime` if `git status` shows it.

- [ ] **Step 8: Commit**

```bash
git add internal/app main.go frontend/wailsjs
git commit -m "feat(app): AI chat, explain, settings, model pull and prompts API"
```

---

### Task 8: Frontend AI logic (types, API, chat reducer, Markdown, formatting)

**Files:**
- Modify: `frontend/src/lib/types.ts` (append AI types)
- Modify: `frontend/src/lib/api.ts` (add AI methods)
- Modify: `frontend/src/lib/stores.ts` (add `settingsOpen`)
- Modify: `frontend/src/lib/format.ts` (add `formatBytes`, `percent`)
- Create: `frontend/src/lib/chat.ts`, `frontend/src/lib/markdown.ts`
- Test: `frontend/src/lib/chat.test.ts`, `frontend/src/lib/markdown.test.ts`, append to `frontend/src/lib/format.test.ts`

**Interfaces:**
- Consumes: bindings `ai*`, `SendChat`, `ExplainCommit`, … from `frontend/wailsjs/go/app/App` (Task 7); Go JSON shapes from Task 7.
- Produces:
  - Types: `AIToolCall`, `AIMessage`, `AISettings`, `OllamaModel`, `AIStatus`, `PromptInfo`, `ModelProgress`, `ModelDone`, `ChatDeltaEvent`, `ChatToolEvent`, `ChatToolResultEvent`, `ChatDoneEvent`, `ChatErrorEvent`, `ExplainDeltaEvent`, `ExplainDoneEvent`, `ExplainErrorEvent`
  - `api.aiStatus()`, `api.getAISettings()`, `api.saveAISettings(s)`, `api.pullModel(name)`, `api.cancelPull()`, `api.getChat(repoID)`, `api.sendChat(repoID, text, runID)`, `api.stopChat(repoID)`, `api.clearChat(repoID)`, `api.explainCommit(repoID, hash, provider, runID)`, `api.listPrompts()`, `api.openPromptsFolder()`, `api.resetPrompt(name)`
  - `stores.settingsOpen: Writable<boolean>`
  - `chat.ts`: `ChatToolUse{name, args, summary?}`, `ChatItem{role: 'user'|'assistant', text, tools: ChatToolUse[], stopped?, error?: {message, code}}`, `ChatState{repoID, runID: string|null, items}`, `emptyChat(repoID)`, `fromMessages(repoID, messages)`, `startRun(state, text, runID)`, `applyEvent(state, name, payload)`, `toolLabel(tool)`, `errorText(error)`
  - `markdown.ts`: `escapeHtml(s)`, `renderMarkdown(src)` — hashes become `<a href="#" data-hash="…">`
  - `format.ts`: `formatBytes(n)` (`"0 B"`, `"512 MB"`, `"4.7 GB"`), `percent(completed, total)` (integer 0–100)

- [ ] **Step 1: Add types, API methods and the store**

Append to `frontend/src/lib/types.ts`:
```ts
export interface AIToolCall {
  id: string
  name: string
  args: Record<string, unknown> | null
}

export interface AIMessage {
  role: 'system' | 'user' | 'assistant' | 'tool'
  content: string
  toolCalls?: AIToolCall[]
  toolName?: string
  stopped?: boolean
}

export interface AISettings {
  ollamaURL: string
  chatModel: string
  taskProvider: 'apple' | 'ollama'
  taskModel: string
}

export interface OllamaModel {
  name: string
  size: number
}

export interface AIStatus {
  ollama: {
    running: boolean
    url: string
    chatModel: string
    models: OllamaModel[]
    chatModelInstalled: boolean
    error?: string
  }
  apple: { available: boolean; reason?: string }
}

export interface PromptInfo {
  name: string
  customized: boolean
}

export interface ModelProgress {
  name: string
  status: string
  completed: number
  total: number
}

export interface ModelDone {
  name: string
  error?: string
}

export interface ChatDeltaEvent { repoID: string; runID: string; text: string }
export interface ChatToolEvent { repoID: string; runID: string; name: string; args: Record<string, unknown> | null }
export interface ChatToolResultEvent { repoID: string; runID: string; name: string; summary: string }
export interface ChatDoneEvent { repoID: string; runID: string }
export interface ChatErrorEvent { repoID: string; runID: string; message: string; code: string }
export interface ExplainDeltaEvent { runID: string; text: string }
export interface ExplainDoneEvent { runID: string }
export interface ExplainErrorEvent { runID: string; message: string }
```

In `frontend/src/lib/api.ts`, change the type import to:
```ts
import type { AIMessage, AISettings, AIStatus, Details, Filters, LogPage, PromptInfo, Refs, Repo } from './types'
```
and add these entries at the end of the `api` object (after `deleteTag`):
```ts

  aiStatus: () => call<AIStatus>(Go.AIStatus()),
  getAISettings: () => call<AISettings>(Go.GetAISettings()),
  saveAISettings: (s: AISettings) => call<void>(Go.SaveAISettings(s as any)),
  pullModel: (name: string) => call<void>(Go.PullModel(name)),
  cancelPull: () => call<void>(Go.CancelPull()),
  getChat: (repoID: string) => call<AIMessage[]>(Go.GetChat(repoID)),
  sendChat: (repoID: string, text: string, runID: string) => call<void>(Go.SendChat(repoID, text, runID)),
  stopChat: (repoID: string) => call<void>(Go.StopChat(repoID)),
  clearChat: (repoID: string) => call<void>(Go.ClearChat(repoID)),
  explainCommit: (repoID: string, hash: string, provider: '' | 'apple' | 'ollama', runID: string) =>
    call<void>(Go.ExplainCommit(repoID, hash, provider, runID)),
  listPrompts: () => call<PromptInfo[]>(Go.ListPrompts()),
  openPromptsFolder: () => call<void>(Go.OpenPromptsFolder()),
  resetPrompt: (name: string) => call<void>(Go.ResetPrompt(name)),
```

In `frontend/src/lib/stores.ts`, add after `export const busy = writable('')`:
```ts
export const settingsOpen = writable(false)
```

- [ ] **Step 2: Write the failing tests**

`frontend/src/lib/chat.test.ts`:
```ts
import { describe, expect, it } from 'vitest'
import { applyEvent, emptyChat, errorText, fromMessages, startRun, toolLabel } from './chat'
import type { AIMessage } from './types'

describe('fromMessages', () => {
  it('merges tool rounds into one assistant item', () => {
    const messages: AIMessage[] = [
      { role: 'user', content: 'branches?' },
      { role: 'assistant', content: '', toolCalls: [{ id: 'c1', name: 'list_refs', args: {} }] },
      { role: 'tool', toolName: 'list_refs', content: 'Current branch: main\nLocal branches: main' },
      { role: 'assistant', content: 'On main.' },
      { role: 'user', content: 'thanks' },
      { role: 'assistant', content: 'Wai', stopped: true },
    ]
    expect(fromMessages('r1', messages)).toEqual({
      repoID: 'r1',
      runID: null,
      items: [
        { role: 'user', text: 'branches?', tools: [] },
        { role: 'assistant', text: 'On main.', tools: [{ name: 'list_refs', args: {}, summary: 'Current branch: main' }] },
        { role: 'user', text: 'thanks', tools: [] },
        { role: 'assistant', text: 'Wai', tools: [], stopped: true },
      ],
    })
  })
})

describe('applyEvent', () => {
  const running = () => startRun(emptyChat('r1'), 'hola', 'run1')

  it('starts a run with a user and an empty assistant item', () => {
    expect(running()).toEqual({
      repoID: 'r1',
      runID: 'run1',
      items: [
        { role: 'user', text: 'hola', tools: [] },
        { role: 'assistant', text: '', tools: [] },
      ],
    })
  })

  it('applies tool, result, deltas and done', () => {
    let s = running()
    s = applyEvent(s, 'chat:tool', { repoID: 'r1', runID: 'run1', name: 'search_log', args: { text: 'NEXO-1' } })
    s = applyEvent(s, 'chat:tool_result', { repoID: 'r1', runID: 'run1', name: 'search_log', summary: 'a1b2c3d fix' })
    s = applyEvent(s, 'chat:delta', { repoID: 'r1', runID: 'run1', text: 'Found ' })
    s = applyEvent(s, 'chat:delta', { repoID: 'r1', runID: 'run1', text: 'one.' })
    s = applyEvent(s, 'chat:done', { repoID: 'r1', runID: 'run1' })
    expect(s.runID).toBeNull()
    expect(s.items[1]).toEqual({
      role: 'assistant',
      text: 'Found one.',
      tools: [{ name: 'search_log', args: { text: 'NEXO-1' }, summary: 'a1b2c3d fix' }],
    })
  })

  it('records errors and ends the run', () => {
    const s = applyEvent(running(), 'chat:error', { repoID: 'r1', runID: 'run1', message: 'down', code: 'ollama_down' })
    expect(s.runID).toBeNull()
    expect(s.items[1].error).toEqual({ message: 'down', code: 'ollama_down' })
  })

  it('ignores events from other repos or runs', () => {
    const s = running()
    expect(applyEvent(s, 'chat:delta', { repoID: 'r2', runID: 'run1', text: 'x' })).toBe(s)
    expect(applyEvent(s, 'chat:delta', { repoID: 'r1', runID: 'old', text: 'x' })).toBe(s)
    expect(applyEvent(emptyChat('r1'), 'chat:done', { repoID: 'r1', runID: 'run1' }).items).toEqual([])
  })
})

describe('labels', () => {
  it('shows the first non-empty argument', () => {
    expect(toolLabel({ name: 'search_log', args: { text: '', author: 'Ana' } })).toBe('search_log: Ana')
    expect(toolLabel({ name: 'list_refs', args: null })).toBe('list_refs')
  })

  it('explains known error codes', () => {
    expect(errorText({ code: 'no_tool_support', message: 'x' })).toContain('qwen2.5')
    expect(errorText({ code: 'other', message: 'boom' })).toBe('boom')
  })
})
```

`frontend/src/lib/markdown.test.ts`:
```ts
import { describe, expect, it } from 'vitest'
import { escapeHtml, renderMarkdown } from './markdown'

describe('renderMarkdown', () => {
  it('escapes HTML', () => {
    expect(escapeHtml('<b>&"')).toBe('&lt;b&gt;&amp;&quot;')
    expect(renderMarkdown('<script>alert(1)</script>')).toBe('<p>&lt;script&gt;alert(1)&lt;/script&gt;</p>')
  })

  it('renders paragraphs, bold and inline code', () => {
    expect(renderMarkdown('Hello **world**\nline two\n\nuse `git log`')).toBe(
      '<p>Hello <strong>world</strong><br>line two</p><p>use <code>git log</code></p>',
    )
  })

  it('renders lists and headings', () => {
    expect(renderMarkdown('## Changes\n- one\n* two\n1. three')).toBe(
      '<p><strong>Changes</strong></p><ul><li>one</li><li>two</li><li>three</li></ul>',
    )
  })

  it('renders fenced code blocks, even unclosed while streaming', () => {
    expect(renderMarkdown('before\n```go\nx := 1 < 2\n```\nafter')).toBe(
      '<p>before</p><pre><code>x := 1 &lt; 2</code></pre><p>after</p>',
    )
    expect(renderMarkdown('```\npartial')).toBe('<pre><code>partial</code></pre>')
  })

  it('links commit hashes outside code', () => {
    expect(renderMarkdown('see a1b2c3d and `a1b2c3d`')).toBe(
      '<p>see <a href="#" data-hash="a1b2c3d">a1b2c3d</a> and <code>a1b2c3d</code></p>',
    )
    expect(renderMarkdown('year 2026091 and word decade1')).toContain('<a href="#" data-hash="decade1">')
    expect(renderMarkdown('number 1234567')).not.toContain('data-hash')
  })
})
```

Append to `frontend/src/lib/format.test.ts`:
```ts
import { formatBytes, percent } from './format'

describe('formatBytes', () => {
  it('uses binary units with one decimal for GB', () => {
    expect(formatBytes(0)).toBe('0 B')
    expect(formatBytes(512 * 1024 * 1024)).toBe('512 MB')
    expect(formatBytes(4683087332)).toBe('4.4 GB')
  })
})

describe('percent', () => {
  it('clamps and rounds', () => {
    expect(percent(50, 200)).toBe(25)
    expect(percent(5, 0)).toBe(0)
    expect(percent(300, 200)).toBe(100)
  })
})
```
(Keep the existing imports at the top of `format.test.ts`; move this `import` line up next to them so the file has one import block.)

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd frontend && npm test`
Expected: FAIL — cannot resolve `./chat`, `./markdown`; `formatBytes` is not exported.

- [ ] **Step 4: Implement**

Append to `frontend/src/lib/format.ts`:
```ts
export function formatBytes(n: number): string {
  if (n <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(units.length - 1, Math.floor(Math.log(n) / Math.log(1024)))
  const value = n / 1024 ** i
  return `${i >= 3 ? value.toFixed(1) : Math.round(value)} ${units[i]}`
}

export function percent(completed: number, total: number): number {
  if (total <= 0) return 0
  return Math.max(0, Math.min(100, Math.round((completed / total) * 100)))
}
```

`frontend/src/lib/chat.ts`:
```ts
import type { AIMessage, ChatDeltaEvent, ChatErrorEvent, ChatToolEvent, ChatToolResultEvent } from './types'

export interface ChatToolUse {
  name: string
  args: Record<string, unknown> | null
  summary?: string
}

export interface ChatItem {
  role: 'user' | 'assistant'
  text: string
  tools: ChatToolUse[]
  stopped?: boolean
  error?: { message: string; code: string }
}

export interface ChatState {
  repoID: string
  runID: string | null
  items: ChatItem[]
}

export const emptyChat = (repoID: string): ChatState => ({ repoID, runID: null, items: [] })

const firstLine = (s: string) => s.split('\n')[0].slice(0, 120)

// fromMessages turns stored messages into display items: consecutive
// assistant/tool messages of one answer become a single assistant item.
export function fromMessages(repoID: string, messages: AIMessage[]): ChatState {
  const items: ChatItem[] = []
  for (const m of messages) {
    if (m.role === 'user') {
      items.push({ role: 'user', text: m.content, tools: [] })
      continue
    }
    let last = items[items.length - 1]
    if (m.role === 'tool') {
      const tool = last?.tools.find((t) => t.name === m.toolName && t.summary === undefined)
      if (tool) tool.summary = firstLine(m.content)
      continue
    }
    if (m.role !== 'assistant') continue
    if (!last || last.role !== 'assistant') {
      last = { role: 'assistant', text: '', tools: [] }
      items.push(last)
    }
    last.text += m.content
    for (const call of m.toolCalls ?? []) last.tools.push({ name: call.name, args: call.args })
    if (m.stopped) last.stopped = true
  }
  return { repoID, runID: null, items }
}

export function startRun(state: ChatState, text: string, runID: string): ChatState {
  return {
    ...state,
    runID,
    items: [...state.items, { role: 'user', text, tools: [] }, { role: 'assistant', text: '', tools: [] }],
  }
}

type Payload = ChatDeltaEvent | ChatToolEvent | ChatToolResultEvent | ChatErrorEvent | { repoID: string; runID: string }

export function applyEvent(state: ChatState, name: string, payload: Payload): ChatState {
  if (payload.repoID !== state.repoID || payload.runID !== state.runID || state.runID === null) return state
  const items = state.items.slice()
  const last = { ...items[items.length - 1], tools: items[items.length - 1].tools.slice() }
  items[items.length - 1] = last
  switch (name) {
    case 'chat:delta':
      last.text += (payload as ChatDeltaEvent).text
      return { ...state, items }
    case 'chat:tool': {
      const p = payload as ChatToolEvent
      last.tools.push({ name: p.name, args: p.args })
      return { ...state, items }
    }
    case 'chat:tool_result': {
      const p = payload as ChatToolResultEvent
      const i = last.tools.findIndex((t) => t.name === p.name && t.summary === undefined)
      if (i >= 0) last.tools[i] = { ...last.tools[i], summary: p.summary }
      return { ...state, items }
    }
    case 'chat:done':
      return { ...state, runID: null, items }
    case 'chat:error': {
      const p = payload as ChatErrorEvent
      last.error = { message: p.message, code: p.code }
      return { ...state, runID: null, items }
    }
  }
  return state
}

export function toolLabel(tool: { name: string; args: Record<string, unknown> | null }): string {
  const value = Object.values(tool.args ?? {}).find((v) => v !== '' && v !== null && v !== undefined)
  return value === undefined ? tool.name : `${tool.name}: ${String(value)}`
}

export function errorText(error: { message: string; code: string }): string {
  switch (error.code) {
    case 'ollama_down':
      return 'Ollama is not running. Open Ollama and try again.'
    case 'model_missing':
      return 'The chat model is not installed. Download it in Settings.'
    case 'no_tool_support':
      return "This model doesn't support tools. Use qwen2.5 or llama3.1."
  }
  return error.message
}
```

`frontend/src/lib/markdown.ts`:
```ts
// A deliberately small Markdown renderer for model output. Everything is
// HTML-escaped first; only a few constructs are turned back into markup.

export function escapeHtml(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
}

const HASH = /\b([0-9a-f]{7,12})\b/g

function inline(escaped: string): string {
  return escaped
    .split(/(`[^`]+`)/g)
    .map((part, i) => {
      if (i % 2 === 1) return `<code>${part.slice(1, -1)}</code>`
      return part
        .replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
        .replace(HASH, (m) => (/[a-f]/.test(m) && /\d/.test(m) ? `<a href="#" data-hash="${m}">${m}</a>` : m))
    })
    .join('')
}

export function renderMarkdown(src: string): string {
  const out: string[] = []
  src.split('```').forEach((segment, index) => {
    if (index % 2 === 1) {
      const body = segment.replace(/^[^\n]*\n/, '').replace(/\n$/, '')
      out.push(`<pre><code>${escapeHtml(body)}</code></pre>`)
      return
    }
    let list: string[] = []
    let para: string[] = []
    const flushList = () => {
      if (list.length) out.push(`<ul>${list.map((l) => `<li>${l}</li>`).join('')}</ul>`)
      list = []
    }
    const flushPara = () => {
      if (para.length) out.push(`<p>${para.join('<br>')}</p>`)
      para = []
    }
    for (const raw of segment.split('\n')) {
      const line = raw.trimEnd()
      const item = line.match(/^\s*(?:[-*]|\d+\.)\s+(.*)$/)
      if (item) {
        flushPara()
        list.push(inline(escapeHtml(item[1])))
        continue
      }
      if (line.trim() === '') {
        flushList()
        flushPara()
        continue
      }
      flushList()
      const heading = line.match(/^#{1,6}\s+(.*)$/)
      para.push(heading ? `<strong>${inline(escapeHtml(heading[1]))}</strong>` : inline(escapeHtml(line)))
    }
    flushList()
    flushPara()
  })
  return out.join('')
}
```

- [ ] **Step 5: Run tests and type check**

Run: `cd frontend && npm test && npm run check`
Expected: all Vitest files pass; svelte-check 0 errors.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/lib
git commit -m "feat(ui): AI types, API, chat reducer and safe Markdown"
```

---

### Task 9: Functional chat panel

**Files:**
- Modify: `frontend/src/components/ChatPanel.svelte` (replace)
- Modify: `frontend/src/components/Icon.svelte` (add `stop` icon)

**Interfaces:**
- Consumes: `api.aiStatus/getChat/sendChat/stopChat/clearChat`, `emptyChat/fromMessages/startRun/applyEvent/toolLabel/errorText`, `renderMarkdown`, stores `chatOpen`, `jumpTo`, `selectedRepo`, `settingsOpen` (Task 8); `EventsOn(name, cb): () => void` from `frontend/wailsjs/runtime/runtime`; `toast`, `errorMessage` from `lib/ui`.
- Produces: the working chat UI. Enter sends, Shift+Enter inserts a newline; commit-hash links in answers jump to the commit in the log.

- [ ] **Step 1: Add the stop icon**

In `frontend/src/components/Icon.svelte`, add to the `paths` object after `copy`:
```ts
    stop: 'M4.5 4.5h7v7h-7z',
```

- [ ] **Step 2: Replace the chat panel**

`frontend/src/components/ChatPanel.svelte`:
```svelte
<script lang="ts">
  import { onDestroy, tick } from 'svelte'
  import { EventsOn } from '../../wailsjs/runtime/runtime'
  import Icon from './Icon.svelte'
  import { api } from '../lib/api'
  import { applyEvent, emptyChat, errorText, fromMessages, startRun, toolLabel, type ChatState } from '../lib/chat'
  import { renderMarkdown } from '../lib/markdown'
  import { chatOpen, jumpTo, selectedRepo, settingsOpen } from '../lib/stores'
  import type { AIStatus } from '../lib/types'
  import { errorMessage, toast } from '../lib/ui'

  let state: ChatState = emptyChat('')
  let status: AIStatus | null = null
  let input = ''
  let list: HTMLDivElement
  let loadError = ''

  const offs = ['chat:delta', 'chat:tool', 'chat:tool_result', 'chat:done', 'chat:error'].map((name) =>
    EventsOn(name, (payload) => {
      const next = applyEvent(state, name, payload)
      if (next !== state) {
        state = next
        scrollDown()
      }
    }),
  )
  onDestroy(() => offs.forEach((off) => off()))

  $: load($selectedRepo?.id ?? '')
  $: running = state.runID !== null
  $: ready = !!status?.ollama.running && !!status?.ollama.chatModelInstalled

  async function load(repoID: string) {
    state = emptyChat(repoID)
    loadError = ''
    refreshStatus()
    if (!repoID) return
    try {
      const messages = await api.getChat(repoID)
      if (state.repoID === repoID && state.runID === null) {
        state = fromMessages(repoID, messages)
        scrollDown()
      }
    } catch (e) {
      loadError = errorMessage(e)
    }
  }

  async function refreshStatus() {
    try {
      status = await api.aiStatus()
    } catch {
      status = null
    }
  }

  async function send() {
    const text = input.trim()
    if (!text || running || !state.repoID || !ready) return
    const runID = crypto.randomUUID()
    input = ''
    state = startRun(state, text, runID)
    scrollDown()
    try {
      await api.sendChat(state.repoID, text, runID)
    } catch (e) {
      state = applyEvent(state, 'chat:error', { repoID: state.repoID, runID, message: errorMessage(e), code: 'other' })
    }
  }

  function stop() {
    if (state.repoID) api.stopChat(state.repoID).catch(() => {})
  }

  async function clear() {
    if (!state.repoID || running) return
    try {
      await api.clearChat(state.repoID)
      state = emptyChat(state.repoID)
    } catch (e) {
      toast(errorMessage(e), 'error')
    }
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
      e.preventDefault()
      send()
    }
  }

  function onClick(e: MouseEvent) {
    const link = (e.target as HTMLElement).closest('a[data-hash]') as HTMLElement | null
    if (!link) return
    e.preventDefault()
    jumpTo.set(link.dataset.hash ?? '')
  }

  async function scrollDown() {
    await tick()
    if (list) list.scrollTop = list.scrollHeight
  }
</script>

<svelte:window on:focus={refreshStatus} />

<div class="chat">
  <header class="drag">
    <span class="title">Chat</span>
    <span class="spacer"></span>
    <button class="icon-btn" title="New chat" disabled={running || state.items.length === 0} on:click={clear}><Icon name="plus" /></button>
    <button class="icon-btn" title="Hide chat" on:click={() => chatOpen.set(false)}><Icon name="panel-right" /></button>
  </header>

  <div class="messages" bind:this={list} on:click={onClick} role="presentation">
    {#if !$selectedRepo}
      <div class="empty"><Icon name="sparkle" size={22} /><p>Select a repository to chat about it.</p></div>
    {:else if status && !status.ollama.running}
      <div class="notice">
        <strong>Ollama is not running</strong>
        <p>Open Ollama or install it from ollama.com.</p>
        <div class="actions">
          <button class="btn" on:click={refreshStatus}>Retry</button>
          <button class="btn" on:click={() => settingsOpen.set(true)}>Settings</button>
        </div>
      </div>
    {:else if status && !status.ollama.chatModelInstalled}
      <div class="notice">
        <strong>Model {status.ollama.chatModel} is not installed</strong>
        <p>Download it from Settings to start chatting.</p>
        <div class="actions"><button class="btn primary" on:click={() => settingsOpen.set(true)}>Open Settings</button></div>
      </div>
    {:else if loadError}
      <div class="notice"><strong>Couldn't load the conversation</strong><p>{loadError}</p></div>
    {:else if state.items.length === 0}
      <div class="empty">
        <Icon name="sparkle" size={22} />
        <p>Ask anything about {$selectedRepo.name}.</p>
        <p class="hint">For example: “What changed this week on develop?”</p>
      </div>
    {/if}

    {#each state.items as item, i}
      {#if item.role === 'user'}
        <div class="msg user">{item.text}</div>
      {:else}
        <div class="msg assistant">
          {#each item.tools as tool}
            <div class="tool" title={JSON.stringify(tool.args ?? {})}>
              <Icon name="search" size={12} />
              <span class="ellipsis">{toolLabel(tool)}</span>
              {#if tool.summary}<span class="summary ellipsis">· {tool.summary}</span>{/if}
            </div>
          {/each}
          {#if item.text}
            <div class="md">{@html renderMarkdown(item.text)}</div>
          {:else if running && i === state.items.length - 1 && !item.error}
            <div class="typing">Thinking…</div>
          {/if}
          {#if item.stopped}<div class="note">Stopped</div>{/if}
          {#if item.error}<div class="error">{errorText(item.error)}</div>{/if}
        </div>
      {/if}
    {/each}
  </div>

  <div class="composer">
    <textarea
      rows="2"
      placeholder={ready ? 'Ask about this repo…' : 'Set up Ollama to chat'}
      bind:value={input}
      on:keydown={onKey}
      disabled={!$selectedRepo || !ready}
    ></textarea>
    {#if running}
      <button class="icon-btn" title="Stop" on:click={stop}><Icon name="stop" /></button>
    {:else}
      <button class="icon-btn" title="Send" disabled={!input.trim() || !ready} on:click={send}><Icon name="send" /></button>
    {/if}
  </div>
</div>

<style>
  .chat { display: flex; flex-direction: column; height: 100%; }
  header { display: flex; align-items: center; gap: 2px; height: 44px; padding: 0 10px 0 16px; flex: none; }
  .title { font-weight: 500; }
  .spacer { flex: 1; }
  .messages { flex: 1; min-height: 0; overflow-y: auto; display: flex; flex-direction: column; gap: 14px; padding: 8px 16px; }
  .empty { flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 4px; text-align: center; color: var(--muted); }
  .empty p { margin: 0; }
  .hint { font-size: 12px; color: var(--faint); }
  .notice { padding: 12px; border: 1px solid var(--border); border-radius: 10px; background: var(--surface); }
  .notice p { margin: 4px 0 8px; color: var(--muted); }
  .actions { display: flex; gap: 6px; }
  .msg { max-width: 100%; user-select: text; line-height: 1.5; }
  .user { align-self: flex-end; max-width: 85%; padding: 8px 12px; border-radius: 12px; background: var(--active); white-space: pre-wrap; }
  .tool { display: flex; align-items: center; gap: 6px; max-width: 100%; margin-bottom: 4px; padding: 2px 8px; border-radius: 6px; background: var(--hover); color: var(--muted); font-size: 12px; }
  .summary { color: var(--faint); }
  .md :global(p) { margin: 0 0 6px; }
  .md :global(ul) { margin: 4px 0 6px; padding-left: 18px; }
  .md :global(code) { font-family: var(--mono); font-size: 12px; padding: 0 4px; border-radius: 4px; background: var(--hover); }
  .md :global(pre) { margin: 6px 0; padding: 8px; border-radius: 8px; background: var(--hover); overflow-x: auto; }
  .md :global(pre code) { padding: 0; background: none; }
  .md :global(a) { color: var(--accent); cursor: pointer; }
  .typing, .note { font-size: 12px; color: var(--faint); }
  .error { font-size: 12px; color: var(--danger); }
  .composer { display: flex; align-items: flex-end; gap: 6px; margin: 12px; padding: 8px; background: var(--surface); border: 1px solid var(--border); border-radius: 12px; }
  textarea { flex: 1; resize: none; border: 0; padding: 2px 4px; background: transparent; }
</style>
```

- [ ] **Step 3: Verify**

```bash
source ~/.nvm/nvm.sh && nvm use 22
cd /Users/josfh/playground/git-ui/frontend && npm run check && npm test
```
Expected: 0 errors (a11y warnings acceptable), tests pass. The controller verifies behaviour in the running app.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/ChatPanel.svelte frontend/src/components/Icon.svelte
git commit -m "feat(ui): streaming repo chat with tool chips"
```

---

### Task 10: AI settings dialog

**Files:**
- Create: `frontend/src/components/SettingsDialog.svelte`
- Modify: `frontend/src/components/Sidebar.svelte` (Settings button opens the dialog)
- Modify: `frontend/src/App.svelte` (mount the dialog)

**Interfaces:**
- Consumes: `api.aiStatus/getAISettings/saveAISettings/pullModel/cancelPull/listPrompts/openPromptsFolder/resetPrompt`, `formatBytes`, `percent`, `settingsOpen`, types `AISettings`, `AIStatus`, `PromptInfo`, `ModelProgress`, `ModelDone` (Task 8); `EventsOn`.
- Produces: `SettingsDialog.svelte` (no props), shown while `$settingsOpen` is true.

- [ ] **Step 1: Create the dialog**

`frontend/src/components/SettingsDialog.svelte`:
```svelte
<script lang="ts">
  import { onDestroy } from 'svelte'
  import { EventsOn } from '../../wailsjs/runtime/runtime'
  import Icon from './Icon.svelte'
  import { api } from '../lib/api'
  import { formatBytes, percent } from '../lib/format'
  import { settingsOpen } from '../lib/stores'
  import type { AISettings, AIStatus, ModelDone, ModelProgress, PromptInfo } from '../lib/types'
  import { errorMessage, toast } from '../lib/ui'

  const RECOMMENDED = 'qwen2.5:7b'
  const appleReasons: Record<string, string> = {
    deviceNotEligible: 'This Mac does not support Apple Intelligence.',
    appleIntelligenceNotEnabled: 'Turn on Apple Intelligence in System Settings.',
    modelNotReady: 'The Apple Intelligence model is still downloading.',
    helperNotFound: 'Helper not found. Build the app with "make build".',
    helperFailed: 'The Apple Intelligence helper failed to start.',
    unknown: 'Apple Intelligence is unavailable.',
  }

  let settings: AISettings | null = null
  let status: AIStatus | null = null
  let prompts: PromptInfo[] = []
  let pull: ModelProgress | null = null
  let otherModel = ''
  let saving = false

  const offProgress = EventsOn('model:progress', (p: ModelProgress) => (pull = p))
  const offDone = EventsOn('model:done', async (p: ModelDone) => {
    pull = null
    if (p.error && !p.error.includes('context canceled')) toast(p.error, 'error')
    else if (!p.error) toast(`Downloaded ${p.name}`)
    await refresh()
  })
  onDestroy(() => {
    offProgress()
    offDone()
  })

  $: if ($settingsOpen) load()
  $: remote = !!settings && !/^https?:\/\/(localhost|127\.0\.0\.1)(:\d+)?\/?$/.test(settings.ollamaURL)
  $: models = status?.ollama.models ?? []
  $: appleText = status?.apple.available ? 'Available on this Mac' : appleReasons[status?.apple.reason ?? 'unknown'] ?? appleReasons.unknown

  async function load() {
    try {
      settings = await api.getAISettings()
      prompts = await api.listPrompts()
    } catch (e) {
      toast(errorMessage(e), 'error')
    }
    await refresh()
  }

  async function refresh() {
    try {
      status = await api.aiStatus()
    } catch {
      status = null
    }
  }

  async function save() {
    if (!settings) return
    saving = true
    try {
      await api.saveAISettings(settings)
      await refresh()
    } catch (e) {
      toast(errorMessage(e), 'error')
    } finally {
      saving = false
    }
  }

  async function startPull(name: string) {
    const model = name.trim()
    if (!model) return
    try {
      await api.pullModel(model)
      pull = { name: model, status: 'starting', completed: 0, total: 0 }
    } catch (e) {
      toast(errorMessage(e), 'error')
    }
  }

  async function reset(name: string) {
    try {
      await api.resetPrompt(name)
      prompts = await api.listPrompts()
      toast(`Restored ${name}.md`)
    } catch (e) {
      toast(errorMessage(e), 'error')
    }
  }

  async function openFolder() {
    try {
      await api.openPromptsFolder()
      prompts = await api.listPrompts()
    } catch (e) {
      toast(errorMessage(e), 'error')
    }
  }

  const close = () => settingsOpen.set(false)
</script>

<svelte:window on:keydown={(e) => $settingsOpen && e.key === 'Escape' && close()} on:focus={() => $settingsOpen && load()} />

{#if $settingsOpen && settings}
  <div class="backdrop" on:click|self={close} role="presentation">
    <div class="dialog" role="dialog" aria-label="Settings">
      <header>
        <h3>Settings</h3>
        <button class="icon-btn" title="Close" on:click={close}><Icon name="x" /></button>
      </header>

      <section>
        <h4>Ollama</h4>
        <div class="status">
          {#if status?.ollama.running}
            <span class="dot ok"></span> Running · {models.length} {models.length === 1 ? 'model' : 'models'}
          {:else}
            <span class="dot bad"></span> Not responding — open Ollama or install it from ollama.com
          {/if}
        </div>
        <label>
          <span>URL</span>
          <div class="row">
            <input bind:value={settings.ollamaURL} />
            <button class="btn" disabled={saving} on:click={save}>Test</button>
          </div>
        </label>
        {#if remote}<p class="warn">Diffs will be sent over the network to this host.</p>{/if}
        <label>
          <span>Chat model</span>
          <select bind:value={settings.chatModel} on:change={save}>
            {#each models as m}
              <option value={m.name}>{m.name} · {formatBytes(m.size)}</option>
            {/each}
            {#if !models.some((m) => m.name === settings?.chatModel)}
              <option value={settings.chatModel}>{settings.chatModel} (not installed)</option>
            {/if}
          </select>
        </label>
        {#if pull}
          <div class="pull">
            <div class="bar"><div style="width: {percent(pull.completed, pull.total)}%"></div></div>
            <span class="hint">
              {pull.name}: {pull.status}{#if pull.total} · {formatBytes(pull.completed)} / {formatBytes(pull.total)}{/if}
            </span>
            <button class="btn" on:click={() => api.cancelPull()}>Cancel</button>
          </div>
        {:else if status?.ollama.running}
          {#if !status.ollama.chatModelInstalled}
            <button class="btn primary" on:click={() => startPull(settings?.chatModel ?? RECOMMENDED)}>
              Download {settings.chatModel}{settings.chatModel === RECOMMENDED ? ' (~4.7 GB)' : ''}
            </button>
          {/if}
          <div class="row">
            <input placeholder="Other model, e.g. llama3.1:8b" bind:value={otherModel} />
            <button class="btn" disabled={!otherModel.trim()} on:click={() => startPull(otherModel)}>Download</button>
          </div>
        {/if}
      </section>

      <section>
        <h4>Explain commit</h4>
        <label class="radio">
          <input type="radio" bind:group={settings.taskProvider} value="apple" on:change={save} />
          <span>Apple Intelligence <span class="hint">· {appleText}</span></span>
        </label>
        <label class="radio">
          <input type="radio" bind:group={settings.taskProvider} value="ollama" on:change={save} />
          <span>Ollama</span>
        </label>
        {#if settings.taskProvider === 'ollama'}
          <select bind:value={settings.taskModel} on:change={save}>
            {#each models as m}
              <option value={m.name}>{m.name}</option>
            {/each}
            {#if !models.some((m) => m.name === settings?.taskModel)}
              <option value={settings.taskModel}>{settings.taskModel} (not installed)</option>
            {/if}
          </select>
        {/if}
      </section>

      <section>
        <h4>Prompts</h4>
        {#each prompts as p}
          <div class="prompt">
            <span class="mono">{p.name}.md</span>
            {#if p.customized}
              <span class="badge">customized</span>
              <button class="btn" on:click={() => reset(p.name)}>Restore default</button>
            {/if}
          </div>
        {/each}
        <button class="btn" on:click={openFolder}>Open prompts folder</button>
      </section>

      <footer>Everything is processed on this Mac.</footer>
    </div>
  </div>
{/if}

<style>
  .backdrop { position: fixed; inset: 0; z-index: 40; display: grid; place-items: center; background: rgba(0, 0, 0, 0.25); }
  .dialog { width: 520px; max-height: 86vh; overflow-y: auto; padding: 16px 20px; background: var(--surface); border: 1px solid var(--border); border-radius: 12px; box-shadow: var(--shadow); }
  header { display: flex; align-items: center; justify-content: space-between; }
  h3 { margin: 0; font-size: 15px; font-weight: 600; }
  h4 { margin: 0 0 8px; font-size: 12px; font-weight: 500; color: var(--muted); }
  section { display: flex; flex-direction: column; gap: 8px; padding: 14px 0; border-bottom: 1px solid var(--border); }
  label { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: var(--muted); }
  label.radio { flex-direction: row; align-items: center; gap: 8px; font-size: 13px; color: var(--text); }
  .row { display: flex; gap: 6px; }
  .row input { flex: 1; }
  .status { display: flex; align-items: center; gap: 6px; }
  .dot { width: 8px; height: 8px; border-radius: 50%; }
  .dot.ok { background: #4f9d4f; }
  .dot.bad { background: var(--danger); }
  .warn { margin: 0; font-size: 12px; color: var(--danger); }
  .hint { font-size: 12px; color: var(--muted); }
  .pull { display: flex; align-items: center; gap: 8px; }
  .bar { flex: 1; height: 6px; border-radius: 3px; background: var(--hover); overflow: hidden; }
  .bar div { height: 100%; background: var(--accent); }
  .prompt { display: flex; align-items: center; gap: 8px; }
  .badge { font-size: 11px; padding: 0 6px; border-radius: 4px; background: var(--hover); color: var(--muted); }
  footer { padding-top: 12px; font-size: 12px; color: var(--faint); }
</style>
```

- [ ] **Step 2: Open it from the sidebar**

In `frontend/src/components/Sidebar.svelte`:
- Remove the line `let settingsOpen = false`.
- Add `settingsOpen` to the existing import from `'../lib/stores'`.
- Replace the footer block
```svelte
    <button class="row-item" on:click={() => (settingsOpen = !settingsOpen)}><Icon name="settings" /> Settings</button>
    {#if settingsOpen}
      <div class="note">git-ui 0.1 · AI providers arrive in a later version.</div>
    {/if}
```
with
```svelte
    <button class="row-item" on:click={() => settingsOpen.set(true)}><Icon name="settings" /> Settings</button>
```

- [ ] **Step 3: Mount it in the app**

In `frontend/src/App.svelte`, add `import SettingsDialog from './components/SettingsDialog.svelte'` next to the other component imports, and add `<SettingsDialog />` on the line after `<Toasts />`.

- [ ] **Step 4: Verify**

```bash
source ~/.nvm/nvm.sh && nvm use 22
cd /Users/josfh/playground/git-ui/frontend && npm run check && npm test
```
Expected: 0 errors, tests pass.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/SettingsDialog.svelte frontend/src/components/Sidebar.svelte frontend/src/App.svelte
git commit -m "feat(ui): AI settings dialog with model download and prompts"
```

---

### Task 11: Explain commit in the details pane

**Files:**
- Modify: `frontend/src/components/CommitDetails.svelte`

**Interfaces:**
- Consumes: `api.explainCommit(repoID, hash, provider, runID)`, `renderMarkdown`, types `ExplainDeltaEvent`, `ExplainDoneEvent`, `ExplainErrorEvent` (Task 8); `EventsOn`.
- Produces: an "Explain" button under the commit metadata; the streamed explanation renders below it; on error it shows the message and a "Try with Ollama" button.

- [ ] **Step 1: Add explain state and handlers**

In the `<script>` of `frontend/src/components/CommitDetails.svelte`:
- Add `import { onDestroy } from 'svelte'` as the first import, and add after the existing imports:
```ts
  import { EventsOn } from '../../wailsjs/runtime/runtime'
  import { renderMarkdown } from '../lib/markdown'
  import type { ExplainDeltaEvent, ExplainDoneEvent, ExplainErrorEvent } from '../lib/types'
```
- Add after `let request = 0`:
```ts
  let explainRun: string | null = null
  let explainText = ''
  let explainError = ''
  let explaining = false

  const offs = [
    EventsOn('explain:delta', (p: ExplainDeltaEvent) => {
      if (p.runID === explainRun) explainText += p.text
    }),
    EventsOn('explain:done', (p: ExplainDoneEvent) => {
      if (p.runID === explainRun) explaining = false
    }),
    EventsOn('explain:error', (p: ExplainErrorEvent) => {
      if (p.runID !== explainRun) return
      explaining = false
      explainError = p.message
    }),
  ]
  onDestroy(() => offs.forEach((off) => off()))

  async function explain(provider: '' | 'apple' | 'ollama' = '') {
    if (!details) return
    const runID = crypto.randomUUID()
    explainRun = runID
    explainText = ''
    explainError = ''
    explaining = true
    try {
      await api.explainCommit(repoId, details.hash, provider, runID)
    } catch (e) {
      if (explainRun === runID) {
        explaining = false
        explainError = errorMessage(e)
      }
    }
  }
```
- In `load()`, after `diff = ''` add:
```ts
    explainRun = null
    explainText = ''
    explainError = ''
    explaining = false
```

- [ ] **Step 2: Add the markup**

In the markup, directly after the closing `</div>` of `<div class="meta">`, insert:
```svelte
      <div class="explain">
        <button class="btn" disabled={explaining} on:click={() => explain()}>✨ {explaining ? 'Explaining…' : 'Explain'}</button>
        {#if explainText}
          <div class="explanation">{@html renderMarkdown(explainText)}</div>
        {/if}
        {#if explainError}
          <div class="explain-error">
            {explainError}
            <button class="btn" on:click={() => explain('ollama')}>Try with Ollama</button>
          </div>
        {/if}
      </div>
```
and add to the `<style>` block:
```css
  .explain { display: flex; flex-direction: column; align-items: flex-start; gap: 6px; margin-bottom: 10px; }
  .explanation { user-select: text; line-height: 1.5; }
  .explanation :global(p) { margin: 0 0 6px; }
  .explanation :global(ul) { margin: 0; padding-left: 18px; }
  .explanation :global(code) { font-family: var(--mono); font-size: 12px; background: var(--hover); padding: 0 4px; border-radius: 4px; }
  .explain-error { display: flex; flex-direction: column; align-items: flex-start; gap: 6px; font-size: 12px; color: var(--danger); }
```

- [ ] **Step 3: Verify**

```bash
source ~/.nvm/nvm.sh && nvm use 22
cd /Users/josfh/playground/git-ui/frontend && npm run check && npm test
```
Expected: 0 errors, tests pass.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/CommitDetails.svelte
git commit -m "feat(ui): explain commit with Apple Intelligence or Ollama"
```

---

### Task 12: Build tooling, README and icon variants

**Files:**
- Modify: `Makefile` (add `build`, `dev`, `icon`)
- Modify: `README.md`
- Create: `assets/icon-variants/a-lines-behind.svg`, `assets/icon-variants/b-merge-dot.svg`, `assets/icon-variants/c-lines-as-letters.svg`
- Create (git-ignored previews): `build/icon-previews/*.png`

**Interfaces:**
- Consumes: `make helper` (Task 5); `qlmanage` (built into macOS) renders SVG to PNG.
- Produces: `make build` → `build/bin/git-ui.app` containing `Contents/MacOS/git-ui-apple`, re-signed ad hoc; `make dev`; `make icon` (renders `assets/icon.svg` to `build/appicon.png`); three icon variant SVGs with PNG previews for the user to choose from.

- [ ] **Step 1: Complete the Makefile**

Replace `Makefile` with (recipe lines start with a tab):
```makefile
SHELL := /bin/bash
HELPER := helpers/apple/.build/release/git-ui-apple
APP := build/bin/git-ui.app
NODE := source ~/.nvm/nvm.sh && nvm use 22 >/dev/null &&

.PHONY: helper build dev icon icon-previews

helper:
	swift build -c release --package-path helpers/apple

build: helper
	$(NODE) ~/go/bin/wails build
	cp $(HELPER) $(APP)/Contents/MacOS/git-ui-apple
	codesign --force --deep -s - $(APP)
	git checkout -- frontend/wailsjs/runtime 2>/dev/null || true

dev: helper
	$(NODE) ~/go/bin/wails dev

icon:
	qlmanage -t -s 1024 -o build assets/icon.svg >/dev/null
	mv build/icon.svg.png build/appicon.png

icon-previews:
	mkdir -p build/icon-previews
	for f in assets/icon-variants/*.svg; do qlmanage -t -s 512 -o build/icon-previews "$$f" >/dev/null; done
```

Add to `.gitignore`:
```
build/icon-previews/
```

- [ ] **Step 2: Write the icon variants**

`assets/icon-variants/a-lines-behind.svg`:
```svg
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1024 1024">
  <defs>
    <linearGradient id="bg" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0" stop-color="#fbfaf8"/>
      <stop offset="1" stop-color="#e9e7e2"/>
    </linearGradient>
  </defs>
  <rect x="100" y="100" width="824" height="824" rx="185" fill="url(#bg)"/>
  <g fill="none" stroke-width="36" stroke-linecap="round">
    <path d="M290 180 V844" stroke="#4f9d4f"/>
    <path d="M290 290 C290 380 734 350 734 450 V590 C734 690 290 670 290 770" stroke="#a4478f"/>
    <path d="M734 520 C734 470 560 470 560 410 V180" stroke="#3f7fbf"/>
  </g>
  <circle cx="290" cy="290" r="40" fill="#4f9d4f"/>
  <circle cx="734" cy="520" r="40" fill="#a4478f"/>
  <circle cx="290" cy="770" r="40" fill="#4f9d4f"/>
  <text x="540" y="700" text-anchor="middle" font-family="-apple-system, 'SF Pro Display', Helvetica, sans-serif" font-weight="800" font-size="400" fill="#1f1e1c" stroke="#f4f3f0" stroke-width="28" paint-order="stroke">ai</text>
</svg>
```

`assets/icon-variants/b-merge-dot.svg`:
```svg
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1024 1024">
  <rect x="100" y="100" width="824" height="824" rx="185" fill="#f4f3f0"/>
  <g fill="none" stroke-width="40" stroke-linecap="round">
    <path d="M320 170 V420 C320 520 420 520 440 560" stroke="#4f9d4f"/>
    <path d="M704 170 V360 C704 470 610 500 590 550" stroke="#a4478f"/>
    <path d="M512 850 V760" stroke="#3f7fbf"/>
  </g>
  <circle cx="320" cy="300" r="42" fill="#4f9d4f"/>
  <circle cx="704" cy="260" r="42" fill="#a4478f"/>
  <circle cx="512" cy="640" r="190" fill="#1f1e1c"/>
  <text x="512" y="712" text-anchor="middle" font-family="-apple-system, 'SF Pro Display', Helvetica, sans-serif" font-weight="800" font-size="210" fill="#f4f3f0">ai</text>
</svg>
```

`assets/icon-variants/c-lines-as-letters.svg`:
```svg
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1024 1024">
  <rect x="100" y="100" width="824" height="824" rx="185" fill="#1f1e1c"/>
  <g fill="none" stroke-linecap="round" stroke-width="64">
    <path d="M430 180 V330" stroke="#3f7fbf"/>
    <circle cx="430" cy="600" r="170" stroke="#4f9d4f"/>
    <path d="M600 470 V770" stroke="#4f9d4f"/>
    <path d="M760 520 V770" stroke="#a4478f"/>
    <path d="M430 330 C430 400 430 400 430 430" stroke="#3f7fbf"/>
  </g>
  <circle cx="430" cy="300" r="46" fill="#3f7fbf"/>
  <circle cx="760" cy="390" r="52" fill="#a4478f"/>
</svg>
```

- [ ] **Step 3: Render previews**

```bash
cd /Users/josfh/playground/git-ui && make icon-previews && ls build/icon-previews
```
Expected: `a-lines-behind.svg.png`, `b-merge-dot.svg.png`, `c-lines-as-letters.svg.png`. Open each PNG and check it renders (letters visible, lines inside the rounded square). Fix obvious geometry problems (lines crossing the letters illegibly, shapes clipped) with small coordinate changes and describe them in the report.

- [ ] **Step 4: Update the README**

Replace the "Requirements", "Develop" and "Build" sections of `README.md` with:
````markdown
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

## Build

```bash
make build   # → build/bin/git-ui.app (includes the Apple Intelligence helper)
make icon    # re-render build/appicon.png from assets/icon.svg
```

## AI prompts

Each AI action uses a Markdown prompt. Edit them from Settings → "Open prompts folder"
(`~/Library/Application Support/git-ui/prompts/`); "Restore default" undoes your changes.
````

- [ ] **Step 5: Verify the build**

```bash
cd /Users/josfh/playground/git-ui && make build && ls build/bin/git-ui.app/Contents/MacOS/ && codesign --verify --deep build/bin/git-ui.app && echo signed
```
Expected: `git-ui` and `git-ui-apple` listed; `signed` printed.

- [ ] **Step 6: Commit**

```bash
git add Makefile .gitignore README.md assets/icon-variants
git commit -m "build: helper-aware build targets, README and icon variants"
```

---

### Task 13: Apply the chosen icon and verify end to end

**Human checkpoint:** before this task the controller shows the three previews from `build/icon-previews/` to the user and records the chosen variant (and any requested tweaks) in the dispatch.

**Files:**
- Create: `assets/icon.svg` (copy of the chosen variant, with requested tweaks)
- Modify: `build/appicon.png` (rendered)

**Interfaces:**
- Consumes: `make icon`, `make build` (Task 12); the whole AI feature set.
- Produces: final app bundle with the new icon.

- [ ] **Step 1: Apply the icon**

```bash
cd /Users/josfh/playground/git-ui
cp assets/icon-variants/<chosen>.svg assets/icon.svg
make icon && make build
```
Expected: `build/appicon.png` is 1024×1024 (`sips -g pixelWidth -g pixelHeight build/appicon.png`); build succeeds.

- [ ] **Step 2: Full automated check**

```bash
cd /Users/josfh/playground/git-ui
go vet ./... && go test ./...
source ~/.nvm/nvm.sh && nvm use 22 && (cd frontend && npm test && npm run check)
```
Expected: all Go packages `ok`, Vitest passes, svelte-check 0 errors.

- [ ] **Step 3: Commit**

```bash
git add assets/icon.svg build/appicon.png
git commit -m "feat: git-ui app icon with branch lines and ai"
```

- [ ] **Step 4: Manual checklist (controller + human, on `build/bin/git-ui.app` opened from Finder)**

1. The Dock shows the new icon.
2. Settings: Ollama shows running with `qwen2.5:7b`; Apple Intelligence shows "Available on this Mac"; "Open prompts folder" opens Finder with `chat.md` and `explain-commit.md`.
3. Chat on `e2-funnel-puppeteer`: "¿qué cambió esta semana en release/escala-release-20?" shows tool chips and an answer citing short hashes; clicking a hash selects that commit in the log.
4. Stop during an answer shows "Stopped"; relaunching the app shows the saved conversation; "New chat" clears it.
5. Quit Ollama: the chat shows "Ollama is not running" with Retry.
6. Explain on a commit with Apple Intelligence streams bullet points; "Try with Ollama" works after forcing an error (e.g. a very large merge commit).
7. Editing `explain-commit.md` (e.g. "answer in exactly 2 bullets") changes the next explanation; Settings shows it as customized; "Restore default" reverts it.
