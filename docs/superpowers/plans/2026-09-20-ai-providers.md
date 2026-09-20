# Hosted AI Providers (OpenAI, Anthropic) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace Apple Intelligence with OpenAI and Anthropic as AI providers, chosen per feature, with API keys stored in the operating system's secret store.

**Architecture:** Two new packages implement the existing provider-neutral `ai.Provider` (streaming chat with tool calling) and `ai.Responder` (single prompt) interfaces using each vendor's official Go SDK. A `keys` package wraps `go-keyring`. The app layer gains a factory that turns a provider name plus the stored key into a `Provider` or `Responder`, replacing today's `switch` over Apple/Ollama. The agent loop, the merge tools and the chat store are untouched.

**Tech Stack:** Go 1.22+, Wails v2, Svelte 5, `github.com/anthropics/anthropic-sdk-go`, `github.com/openai/openai-go`, `github.com/zalando/go-keyring`.

**Spec:** `docs/superpowers/specs/2026-09-20-ai-providers-design.md`

## Global Constraints

- Provider names are exactly `"ollama"`, `"openai"`, `"anthropic"`. Any other value is rejected by `settings.validate`.
- API keys are **never** written to `ai.json`, never logged, and never returned to the frontend after being saved. The frontend only ever receives a masked hint (`sk-…abcd`).
- There is **no plain-text fallback** when the OS secret store is unavailable: saving fails with an explicit error.
- The default Anthropic model offered in the UI is `claude-opus-5`. Model ids are plain strings; never append a date suffix.
- Anthropic requests use adaptive thinking (`thinking: {type: "adaptive"}`) and streaming; `budget_tokens` is rejected by current models and must not be sent.
- No new dependency may require cgo.
- Commit messages must not contain `Co-Authored-By` lines.
- Run Go tests with `go test ./...`. Run frontend checks with `PATH=$HOME/.nvm/versions/node/v22.23.1/bin:$PATH` — the default shell `node` is v14 and fails.
- Never commit `frontend/wailsjs/runtime`; `make build` restores it.

---

### Task 1: Keys package

**Files:**
- Create: `internal/ai/keys/keys.go`
- Create: `internal/ai/keys/keys_test.go`
- Modify: `go.mod`, `go.sum` (via `go get`)

**Interfaces:**
- Consumes: nothing.
- Produces:
  ```go
  package keys
  const Service = "git-ui"
  var ErrUnavailable = errors.New("keys: no usable secret store on this system")
  type Store interface {
      Get(service, user string) (string, error)
      Set(service, user, password string) error
      Delete(service, user string) error
  }
  func Get(provider string) (string, error)          // "" and nil error when unset
  func Set(provider, key string) error
  func Delete(provider string) error
  func Available() error                             // nil when the store works
  func UseStore(s Store) (restore func())            // tests only
  func Mask(key string) string                       // "sk-…abcd", "" for ""
  ```

- [ ] **Step 1: Add the dependency**

```bash
go get github.com/zalando/go-keyring@latest
```

- [ ] **Step 2: Write the failing test**

Create `internal/ai/keys/keys_test.go`:

```go
package keys_test

import (
	"errors"
	"testing"

	"git-ui/internal/ai/keys"
)

// fake is an in-memory Store standing in for the OS secret store.
type fake struct {
	items map[string]string
	err   error
}

func (f *fake) Get(service, user string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	v, ok := f.items[service+"/"+user]
	if !ok {
		return "", keys.ErrNotFound
	}
	return v, nil
}

func (f *fake) Set(service, user, password string) error {
	if f.err != nil {
		return f.err
	}
	f.items[service+"/"+user] = password
	return nil
}

func (f *fake) Delete(service, user string) error {
	if f.err != nil {
		return f.err
	}
	delete(f.items, service+"/"+user)
	return nil
}

func withFake(t *testing.T, f *fake) {
	t.Helper()
	restore := keys.UseStore(f)
	t.Cleanup(restore)
}

func TestSetGetDelete(t *testing.T) {
	withFake(t, &fake{items: map[string]string{}})
	if err := keys.Set("openai", "sk-test-1234"); err != nil {
		t.Fatal(err)
	}
	got, err := keys.Get("openai")
	if err != nil || got != "sk-test-1234" {
		t.Fatalf("Get = %q, %v", got, err)
	}
	if err := keys.Delete("openai"); err != nil {
		t.Fatal(err)
	}
	if got, err := keys.Get("openai"); err != nil || got != "" {
		t.Fatalf("after Delete, Get = %q, %v; want empty and no error", got, err)
	}
}

// A key that was never stored is not an error: the UI shows "no key".
func TestGetUnsetIsEmpty(t *testing.T) {
	withFake(t, &fake{items: map[string]string{}})
	got, err := keys.Get("anthropic")
	if err != nil || got != "" {
		t.Fatalf("Get = %q, %v; want empty and no error", got, err)
	}
}

func TestStoreFailuresSurface(t *testing.T) {
	withFake(t, &fake{items: map[string]string{}, err: errors.New("no dbus")})
	if err := keys.Set("openai", "sk-x"); err == nil {
		t.Error("Set: want an error when the store is unusable")
	}
	if err := keys.Available(); err == nil {
		t.Error("Available: want an error when the store is unusable")
	}
}

func TestSetRejectsUnknownProviderAndEmptyKey(t *testing.T) {
	withFake(t, &fake{items: map[string]string{}})
	if err := keys.Set("acme", "sk-x"); err == nil {
		t.Error("want an error for an unknown provider")
	}
	if err := keys.Set("openai", "   "); err == nil {
		t.Error("want an error for a blank key")
	}
}

func TestMask(t *testing.T) {
	cases := map[string]string{
		"":                  "",
		"sk-abcdefghijklmn": "sk-…klmn",
		"tiny":              "…",
	}
	for in, want := range cases {
		if got := keys.Mask(in); got != want {
			t.Errorf("Mask(%q) = %q, want %q", in, got, want)
		}
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/ai/keys/`
Expected: FAIL — `no required module provides package git-ui/internal/ai/keys`.

- [ ] **Step 4: Write the implementation**

Create `internal/ai/keys/keys.go`:

```go
// Package keys stores one API key per AI provider in the operating system's
// own secret store: Keychain on macOS, Secret Service on Linux, Credential
// Manager on Windows. Keys never touch the settings file, and there is no
// plain-text fallback: on a system without a usable store, saving fails.
package keys

import (
	"errors"
	"fmt"
	"strings"

	"github.com/zalando/go-keyring"
)

// Service is the entry name every key is stored under; the provider name is
// the account.
const Service = "git-ui"

var (
	// ErrNotFound reports a provider with no stored key. It wraps the
	// keyring package's own sentinel so a fake store can return it too.
	ErrNotFound = keyring.ErrNotFound
	// ErrUnavailable reports a system with no usable secret store.
	ErrUnavailable = errors.New("keys: no usable secret store on this system")
)

// Store is the subset of the keyring package this package uses, so tests can
// replace it.
type Store interface {
	Get(service, user string) (string, error)
	Set(service, user, password string) error
	Delete(service, user string) error
}

type osStore struct{}

func (osStore) Get(service, user string) (string, error) { return keyring.Get(service, user) }
func (osStore) Set(service, user, password string) error {
	return keyring.Set(service, user, password)
}
func (osStore) Delete(service, user string) error { return keyring.Delete(service, user) }

var store Store = osStore{}

// UseStore swaps the backing store and returns a function restoring the
// previous one. It exists for tests; production code never calls it.
func UseStore(s Store) (restore func()) {
	previous := store
	store = s
	return func() { store = previous }
}

// known guards what can be used as an account name, so a caller can't reach
// another application's entries.
func known(provider string) error {
	switch provider {
	case "openai", "anthropic":
		return nil
	}
	return fmt.Errorf("keys: unknown provider %q", provider)
}

// Get returns the stored key, or "" when the provider has none.
func Get(provider string) (string, error) {
	if err := known(provider); err != nil {
		return "", err
	}
	key, err := store.Get(Service, provider)
	if errors.Is(err, ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return key, nil
}

func Set(provider, key string) error {
	if err := known(provider); err != nil {
		return err
	}
	if strings.TrimSpace(key) == "" {
		return errors.New("keys: the API key is empty")
	}
	if err := store.Set(Service, provider, strings.TrimSpace(key)); err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return nil
}

// Delete removes the provider's key. Removing one that isn't there succeeds.
func Delete(provider string) error {
	if err := known(provider); err != nil {
		return err
	}
	err := store.Delete(Service, provider)
	if err == nil || errors.Is(err, ErrNotFound) {
		return nil
	}
	return fmt.Errorf("%w: %v", ErrUnavailable, err)
}

// Available reports whether the store can be used at all, so Settings can say
// so before the user types a key.
func Available() error {
	if _, err := store.Get(Service, "openai"); err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return nil
}

// Mask is the hint shown in the UI once a key is stored; the key itself never
// goes back to the frontend.
func Mask(key string) string {
	if key == "" {
		return ""
	}
	if len(key) < 8 {
		return "…"
	}
	return key[:3] + "…" + key[len(key)-4:]
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/ai/keys/ -v`
Expected: PASS for all five tests. If a compile error mentions `keyring.ErrNotFound`, check the installed version's sentinel name with `go doc github.com/zalando/go-keyring` and use the real one.

- [ ] **Step 6: Commit**

```bash
git add internal/ai/keys go.mod go.sum
git commit -m "feat(ai): store provider API keys in the OS secret store"
```

---

### Task 2: Settings with three providers

**Files:**
- Modify: `internal/ai/settings/settings.go`
- Modify: `internal/ai/settings/settings_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  ```go
  const (
      ProviderOllama    = "ollama"
      ProviderOpenAI    = "openai"
      ProviderAnthropic = "anthropic"
      DefaultOllamaURL  = "http://localhost:11434"
      DefaultModel      = "qwen2.5:7b"
      DefaultAnthropicModel = "claude-opus-5"
  )
  type Settings struct {
      OllamaURL    string `json:"ollamaURL"`
      ChatProvider string `json:"chatProvider"`
      ChatModel    string `json:"chatModel"`
      TaskProvider string `json:"taskProvider"`
      TaskModel    string `json:"taskModel"`
  }
  func Defaults() Settings
  func Load(path string) (Settings, error)
  func Save(path string, s Settings) error
  ```
  `Defaults` and `Load` lose their `appleAvailable` parameter.

- [ ] **Step 1: Write the failing tests**

Append to `internal/ai/settings/settings_test.go`:

```go
// A file written before hosted providers existed names "apple"; it must load
// as a working configuration rather than an invalid one.
func TestLoadMigratesApple(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ai.json")
	if err := os.WriteFile(path, []byte(`{"ollamaURL":"http://localhost:11434","chatModel":"qwen2.5:7b","taskProvider":"apple","taskModel":"qwen2.5:7b"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := settings.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.TaskProvider != settings.ProviderOllama {
		t.Errorf("taskProvider = %q, want ollama", s.TaskProvider)
	}
	if err := settings.Save(path, s); err != nil {
		t.Errorf("the migrated settings must be valid: %v", err)
	}
}

// An older file has no chatProvider at all.
func TestLoadDefaultsChatProvider(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ai.json")
	if err := os.WriteFile(path, []byte(`{"ollamaURL":"http://localhost:11434","chatModel":"qwen2.5:7b","taskProvider":"ollama","taskModel":"qwen2.5:7b"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := settings.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.ChatProvider != settings.ProviderOllama {
		t.Errorf("chatProvider = %q, want ollama", s.ChatProvider)
	}
}

func TestSaveAcceptsHostedProviders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai.json")
	s := settings.Defaults()
	s.ChatProvider, s.ChatModel = settings.ProviderAnthropic, "claude-opus-5"
	s.TaskProvider, s.TaskModel = settings.ProviderOpenAI, "gpt-4.1-mini"
	if err := settings.Save(path, s); err != nil {
		t.Fatal(err)
	}
	got, err := settings.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ChatProvider != settings.ProviderAnthropic || got.TaskProvider != settings.ProviderOpenAI {
		t.Errorf("round trip lost the providers: %+v", got)
	}
}

func TestSaveRejectsUnknownProvider(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai.json")
	s := settings.Defaults()
	s.ChatProvider = "acme"
	if err := settings.Save(path, s); !errors.Is(err, settings.ErrInvalid) {
		t.Errorf("err = %v, want ErrInvalid", err)
	}
}

// Keys belong in the OS store; nothing key-shaped may reach the file.
func TestSaveWritesNoKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai.json")
	if err := settings.Save(path, settings.Defaults()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "key") || strings.Contains(string(data), "sk-") {
		t.Errorf("the settings file mentions a key: %s", data)
	}
}
```

Make sure the test file imports `errors`, `os`, `path/filepath` and `strings`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/ai/settings/`
Expected: FAIL — `settings.Load` takes two arguments, and `ProviderOpenAI` is undefined.

- [ ] **Step 3: Update the implementation**

In `internal/ai/settings/settings.go`:

Replace the provider constants:

```go
const (
	ProviderOllama    = "ollama"
	ProviderOpenAI    = "openai"
	ProviderAnthropic = "anthropic"
	// providerApple is only recognised when reading an older file.
	providerApple = "apple"

	DefaultOllamaURL      = "http://localhost:11434"
	DefaultModel          = "qwen2.5:7b"
	DefaultAnthropicModel = "claude-opus-5"
)
```

Add `ChatProvider` to the struct, right above `ChatModel`:

```go
type Settings struct {
	OllamaURL    string `json:"ollamaURL"`
	ChatProvider string `json:"chatProvider"`
	ChatModel    string `json:"chatModel"`
	TaskProvider string `json:"taskProvider"`
	TaskModel    string `json:"taskModel"`
}
```

Replace `Defaults` and `Load`:

```go
func Defaults() Settings {
	return Settings{
		OllamaURL:    DefaultOllamaURL,
		ChatProvider: ProviderOllama,
		ChatModel:    DefaultModel,
		TaskProvider: ProviderOllama,
		TaskModel:    DefaultModel,
	}
}

// Load reads the settings file, returning defaults when it doesn't exist.
// A provider this version no longer supports — Apple Intelligence, which had
// no tool calling — becomes Ollama, so an upgrade never lands on a
// configuration Save would reject.
func Load(path string) (Settings, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Defaults(), nil
	}
	if err != nil {
		return Settings{}, err
	}
	s := Defaults()
	if err := json.Unmarshal(data, &s); err != nil {
		return Settings{}, fmt.Errorf("settings: parse %s: %w", path, err)
	}
	if s.ChatProvider == providerApple || s.ChatProvider == "" {
		s.ChatProvider = ProviderOllama
	}
	if s.TaskProvider == providerApple || s.TaskProvider == "" {
		s.TaskProvider = ProviderOllama
	}
	return s, nil
}
```

Replace the provider check in `validate`:

```go
	for _, p := range []string{s.ChatProvider, s.TaskProvider} {
		switch p {
		case ProviderOllama, ProviderOpenAI, ProviderAnthropic:
		default:
			return fmt.Errorf("%w: unknown provider %q", ErrInvalid, p)
		}
	}
```

- [ ] **Step 4: Fix the callers**

`internal/app/ai.go` calls `settings.Load(a.ai.deps.SettingsPath, func() bool {...})`. Change it to `settings.Load(a.ai.deps.SettingsPath)`. Leave the rest of that file for Task 5; the build only has to compile at the end of this task.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/ai/settings/ ./internal/app/ -v 2>&1 | tail -20`
Expected: PASS for the settings package. Compile errors elsewhere in `internal/app` about Apple are expected until Task 5 — if any appear, stop and finish this task by only removing the second `Load` argument.

- [ ] **Step 6: Commit**

```bash
git add internal/ai/settings internal/app/ai.go
git commit -m "feat(ai): settings accept ollama, openai and anthropic per feature"
```

---

### Task 3: Anthropic provider

**Files:**
- Create: `internal/ai/anthropic/anthropic.go`
- Create: `internal/ai/anthropic/anthropic_test.go`
- Modify: `go.mod`, `go.sum`

**Interfaces:**
- Consumes: `ai.Provider`, `ai.Responder`, `ai.Request`, `ai.Chunk` from `internal/ai`.
- Produces:
  ```go
  package anthropic
  func New(apiKey string, opts ...Option) *Client
  func WithBaseURL(u string) Option          // tests point this at httptest
  func (c *Client) Chat(ctx context.Context, req ai.Request) (<-chan ai.Chunk, error)
  func (c *Client) Responder(model string) ai.Responder
  func (c *Client) ListModels(ctx context.Context) ([]string, error)
  const MaxTokens = 64000
  ```

- [ ] **Step 1: Add the dependency**

```bash
go get github.com/anthropics/anthropic-sdk-go@latest
```

- [ ] **Step 2: Write the failing test**

Create `internal/ai/anthropic/anthropic_test.go`:

```go
package anthropic_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"git-ui/internal/ai"
	"git-ui/internal/ai/anthropic"
)

// sse writes Server-Sent Events the way the Messages API streams them.
func sse(w http.ResponseWriter, events ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, e := range events {
		name := e[:strings.Index(e, " ")]
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, e[strings.Index(e, " ")+1:])
	}
}

// textStream is a reply of two text deltas and a clean stop.
func textStream(w http.ResponseWriter) {
	sse(w,
		`message_start {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5","content":[],"stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`,
		`content_block_start {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`content_block_delta {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hola"}}`,
		`content_block_delta {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" mundo"}}`,
		`content_block_stop {"type":"content_block_stop","index":0}`,
		`message_delta {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
		`message_stop {"type":"message_stop"}`,
	)
}

// toolStream is a reply whose only content block is a tool call, streamed as
// partial JSON the way the API sends it.
func toolStream(w http.ResponseWriter) {
	sse(w,
		`message_start {"type":"message_start","message":{"id":"msg_2","type":"message","role":"assistant","model":"claude-opus-5","content":[],"stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`,
		`content_block_start {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"list_conflicts","input":{}}}`,
		`content_block_delta {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\""}}`,
		`content_block_delta {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":":\"a.go\"}"}}`,
		`content_block_stop {"type":"content_block_stop","index":0}`,
		`message_delta {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":2}}`,
		`message_stop {"type":"message_stop"}`,
	)
}

func collect(t *testing.T, ch <-chan ai.Chunk) (text string, calls []ai.ToolCall, done bool, err error) {
	t.Helper()
	for c := range ch {
		text += c.Delta
		calls = append(calls, c.ToolCalls...)
		if c.Done {
			done = true
		}
		if c.Err != nil {
			err = c.Err
		}
	}
	return text, calls, done, err
}

func TestChatStreamsText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "sk-test" {
			t.Errorf("x-api-key = %q", r.Header.Get("x-api-key"))
		}
		textStream(w)
	}))
	defer srv.Close()

	c := anthropic.New("sk-test", anthropic.WithBaseURL(srv.URL))
	ch, err := c.Chat(context.Background(), ai.Request{
		Model:    "claude-opus-5",
		System:   "be brief",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "hola"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	text, calls, done, err := collect(t, ch)
	if err != nil {
		t.Fatal(err)
	}
	if text != "Hola mundo" || len(calls) != 0 || !done {
		t.Errorf("text = %q, calls = %v, done = %v", text, calls, done)
	}
}

func TestChatStreamsAToolCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { toolStream(w) }))
	defer srv.Close()

	c := anthropic.New("sk-test", anthropic.WithBaseURL(srv.URL))
	ch, err := c.Chat(context.Background(), ai.Request{
		Model:    "claude-opus-5",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "resolve"}},
		Tools:    []ai.ToolSpec{{Name: "list_conflicts", Description: "list", Parameters: map[string]any{"type": "object"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, calls, done, err := collect(t, ch)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].Name != "list_conflicts" || calls[0].ID != "toolu_1" {
		t.Fatalf("calls = %#v", calls)
	}
	if calls[0].Args["path"] != "a.go" {
		t.Errorf("args = %#v, want the accumulated partial JSON", calls[0].Args)
	}
	if !done {
		t.Error("done = false")
	}
}

func TestChatReportsRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`)
	}))
	defer srv.Close()

	c := anthropic.New("sk-test", anthropic.WithBaseURL(srv.URL))
	ch, err := c.Chat(context.Background(), ai.Request{Model: "claude-opus-5", Messages: []ai.Message{{Role: ai.RoleUser, Content: "hola"}}})
	if err == nil {
		_, _, _, err = collect(t, ch)
	}
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "rate") {
		t.Fatalf("err = %v, want a rate limit error", err)
	}
}

func TestListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"id":"claude-opus-5","display_name":"Claude Opus 5","type":"model"},{"id":"claude-sonnet-5","display_name":"Claude Sonnet 5","type":"model"}],"has_more":false}`)
	}))
	defer srv.Close()

	got, err := anthropic.New("sk-test", anthropic.WithBaseURL(srv.URL)).ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "claude-opus-5" {
		t.Errorf("models = %v", got)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/ai/anthropic/`
Expected: FAIL — the package doesn't exist.

- [ ] **Step 4: Write the implementation**

Create `internal/ai/anthropic/anthropic.go`:

```go
// Package anthropic adapts the Anthropic Messages API to the app's
// provider-neutral interfaces.
package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"git-ui/internal/ai"
)

// MaxTokens is generous because answers carry resolved file regions; the
// request streams, so a large ceiling costs nothing when unused.
const MaxTokens = 64000

type Client struct {
	sdk sdk.Client
}

type Option func(*[]option.RequestOption)

// WithBaseURL points the client at another host; tests use it.
func WithBaseURL(u string) Option {
	return func(opts *[]option.RequestOption) { *opts = append(*opts, option.WithBaseURL(u)) }
}

func New(apiKey string, opts ...Option) *Client {
	req := []option.RequestOption{option.WithAPIKey(apiKey)}
	for _, o := range opts {
		o(&req)
	}
	return &Client{sdk: sdk.NewClient(req...)}
}

// Chat streams one assistant turn. Text arrives as deltas; a tool call is
// emitted once its block closes, because its arguments stream as partial
// JSON that is only valid when complete.
func (c *Client) Chat(ctx context.Context, req ai.Request) (<-chan ai.Chunk, error) {
	params := sdk.MessageNewParams{
		Model:     sdk.Model(req.Model),
		MaxTokens: MaxTokens,
		Thinking:  sdk.ThinkingConfigParamUnion{OfAdaptive: &sdk.ThinkingConfigAdaptiveParam{}},
		Messages:  messages(req.Messages),
	}
	if req.System != "" {
		params.System = []sdk.TextBlockParam{{Text: req.System}}
	}
	for _, t := range req.Tools {
		params.Tools = append(params.Tools, sdk.ToolUnionParam{OfTool: &sdk.ToolParam{
			Name:        t.Name,
			Description: sdk.String(t.Description),
			InputSchema: sdk.ToolInputSchemaParam{Properties: t.Parameters["properties"]},
		}})
	}

	stream := c.sdk.Messages.NewStreaming(ctx, params)
	ch := make(chan ai.Chunk)
	go func() {
		defer close(ch)
		message := sdk.Message{}
		for stream.Next() {
			event := stream.Current()
			if err := message.Accumulate(event); err != nil {
				send(ctx, ch, ai.Chunk{Err: fmt.Errorf("anthropic: %w", err)})
				return
			}
			switch e := event.AsAny().(type) {
			case sdk.ContentBlockDeltaEvent:
				if d, ok := e.Delta.AsAny().(sdk.TextDelta); ok && d.Text != "" {
					if !send(ctx, ch, ai.Chunk{Delta: d.Text}) {
						return
					}
				}
			case sdk.ContentBlockStopEvent:
				if call, ok := toolCall(message, int(e.Index)); ok {
					if !send(ctx, ch, ai.Chunk{ToolCalls: []ai.ToolCall{call}}) {
						return
					}
				}
			}
		}
		if err := stream.Err(); err != nil {
			send(ctx, ch, ai.Chunk{Err: classify(err)})
			return
		}
		send(ctx, ch, ai.Chunk{Done: true})
	}()
	return ch, nil
}

// toolCall reads the accumulated block at index when it is a completed tool
// use.
func toolCall(m sdk.Message, index int) (ai.ToolCall, bool) {
	if index < 0 || index >= len(m.Content) {
		return ai.ToolCall{}, false
	}
	block, ok := m.Content[index].AsAny().(sdk.ToolUseBlock)
	if !ok {
		return ai.ToolCall{}, false
	}
	args := map[string]any{}
	if len(block.Input) > 0 {
		if err := json.Unmarshal(block.Input, &args); err != nil {
			args = map[string]any{}
		}
	}
	return ai.ToolCall{ID: block.ID, Name: block.Name, Args: args}, true
}

// messages converts the app's history. Every tool result for one assistant
// turn goes into a single user message, which is what the API expects.
func messages(history []ai.Message) []sdk.MessageParam {
	var out []sdk.MessageParam
	var pendingResults []sdk.ContentBlockParamUnion
	flush := func() {
		if len(pendingResults) > 0 {
			out = append(out, sdk.NewUserMessage(pendingResults...))
			pendingResults = nil
		}
	}
	for _, m := range history {
		switch m.Role {
		case ai.RoleTool:
			pendingResults = append(pendingResults, sdk.NewToolResultBlock(m.ToolName, m.Content, false))
		case ai.RoleUser:
			flush()
			out = append(out, sdk.NewUserMessage(sdk.NewTextBlock(m.Content)))
		case ai.RoleAssistant:
			flush()
			blocks := []sdk.ContentBlockParamUnion{}
			if m.Content != "" {
				blocks = append(blocks, sdk.NewTextBlock(m.Content))
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, sdk.ContentBlockParamUnion{OfToolUse: &sdk.ToolUseBlockParam{
					ID: tc.ID, Name: tc.Name, Input: tc.Args,
				}})
			}
			if len(blocks) > 0 {
				out = append(out, sdk.NewAssistantMessage(blocks...))
			}
		}
	}
	flush()
	return out
}

// Responder adapts the client to single-prompt tasks.
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

// ListModels returns the model ids the key can use, newest first as the API
// returns them.
func (c *Client) ListModels(ctx context.Context) ([]string, error) {
	page, err := c.sdk.Models.List(ctx, sdk.ModelListParams{})
	if err != nil {
		return nil, classify(err)
	}
	out := []string{}
	for _, m := range page.Data {
		out = append(out, m.ID)
	}
	return out, nil
}

// classify turns an SDK error into one the chat can show without leaking the
// request or the key.
func classify(err error) error {
	var apierr *sdk.Error
	if !errors.As(err, &apierr) {
		return fmt.Errorf("anthropic: %w", err)
	}
	switch apierr.StatusCode {
	case 401, 403:
		return errors.New("anthropic rejected the API key")
	case 429:
		return errors.New("anthropic is rate limiting; try again in a moment")
	case 400:
		return fmt.Errorf("anthropic rejected the request: %s", apierr.Error())
	default:
		return fmt.Errorf("anthropic returned %d", apierr.StatusCode)
	}
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

- [ ] **Step 5: Run the test and fix compile errors against the installed SDK**

Run: `go test ./internal/ai/anthropic/ -v`

The SDK's exact type names move between releases. Do not research them on the web: let the compiler name them. Check with `go doc github.com/anthropics/anthropic-sdk-go <Name>` and fix. The parts most likely to need adjusting are `ToolInputSchemaParam` (the field carrying the JSON Schema properties), `NewToolResultBlock`'s argument order, and whether `Accumulate` returns an error.
Expected once it compiles: PASS for all four tests.

- [ ] **Step 6: Commit**

```bash
git add internal/ai/anthropic go.mod go.sum
git commit -m "feat(ai): Anthropic provider with streaming tool calls"
```

---

### Task 4: OpenAI provider

**Files:**
- Create: `internal/ai/openai/openai.go`
- Create: `internal/ai/openai/openai_test.go`
- Modify: `go.mod`, `go.sum`

**Interfaces:**
- Consumes: `internal/ai`.
- Produces: the same shape as Task 3, in package `openai`:
  ```go
  func New(apiKey string, opts ...Option) *Client
  func WithBaseURL(u string) Option
  func (c *Client) Chat(ctx context.Context, req ai.Request) (<-chan ai.Chunk, error)
  func (c *Client) Responder(model string) ai.Responder
  func (c *Client) ListModels(ctx context.Context) ([]string, error)
  ```

- [ ] **Step 1: Add the dependency**

```bash
go get github.com/openai/openai-go@latest
```

- [ ] **Step 2: Write the failing test**

Create `internal/ai/openai/openai_test.go`:

```go
package openai_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"git-ui/internal/ai"
	"git-ui/internal/ai/openai"
)

func chunks(w http.ResponseWriter, lines ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, l := range lines {
		fmt.Fprintf(w, "data: %s\n\n", l)
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func collect(t *testing.T, ch <-chan ai.Chunk) (text string, calls []ai.ToolCall, done bool, err error) {
	t.Helper()
	for c := range ch {
		text += c.Delta
		calls = append(calls, c.ToolCalls...)
		if c.Done {
			done = true
		}
		if c.Err != nil {
			err = c.Err
		}
	}
	return text, calls, done, err
}

func TestChatStreamsText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Errorf("Authorization = %q", got)
		}
		chunks(w,
			`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"Hola"}}]}`,
			`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":" mundo"}}]}`,
			`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		)
	}))
	defer srv.Close()

	ch, err := openai.New("sk-test", openai.WithBaseURL(srv.URL)).Chat(context.Background(), ai.Request{
		Model:    "gpt-4.1",
		System:   "be brief",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "hola"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	text, calls, done, err := collect(t, ch)
	if err != nil || text != "Hola mundo" || len(calls) != 0 || !done {
		t.Errorf("text = %q, calls = %v, done = %v, err = %v", text, calls, done, err)
	}
}

// Arguments arrive split across chunks and are only valid once joined.
func TestChatStreamsAToolCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunks(w,
			`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"list_conflicts","arguments":"{\"path\""}}]}}]}`,
			`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"a.go\"}"}}]}}]}`,
			`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		)
	}))
	defer srv.Close()

	ch, err := openai.New("sk-test", openai.WithBaseURL(srv.URL)).Chat(context.Background(), ai.Request{
		Model:    "gpt-4.1",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "resolve"}},
		Tools:    []ai.ToolSpec{{Name: "list_conflicts", Description: "list", Parameters: map[string]any{"type": "object"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, calls, done, err := collect(t, ch)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].Name != "list_conflicts" || calls[0].ID != "call_1" || calls[0].Args["path"] != "a.go" {
		t.Fatalf("calls = %#v", calls)
	}
	if !done {
		t.Error("done = false")
	}
}

func TestChatReportsRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":{"message":"slow down","type":"rate_limit_error"}}`)
	}))
	defer srv.Close()

	ch, err := openai.New("sk-test", openai.WithBaseURL(srv.URL)).Chat(context.Background(), ai.Request{
		Model:    "gpt-4.1",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "hola"}},
	})
	if err == nil {
		_, _, _, err = collect(t, ch)
	}
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "rate") {
		t.Fatalf("err = %v, want a rate limit error", err)
	}
}

// The raw list carries embeddings and image models; only chat models belong
// in the dropdown.
func TestListModelsKeepsOnlyChatModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"object":"list","data":[{"id":"gpt-4.1","object":"model"},{"id":"text-embedding-3-small","object":"model"},{"id":"dall-e-3","object":"model"},{"id":"whisper-1","object":"model"},{"id":"o4-mini","object":"model"}]}`)
	}))
	defer srv.Close()

	got, err := openai.New("sk-test", openai.WithBaseURL(srv.URL)).ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"gpt-4.1": true, "o4-mini": true}
	if len(got) != len(want) {
		t.Fatalf("models = %v, want only the chat ones", got)
	}
	for _, m := range got {
		if !want[m] {
			t.Errorf("models = %v, want only the chat ones", got)
		}
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/ai/openai/`
Expected: FAIL — the package doesn't exist.

- [ ] **Step 4: Write the implementation**

Create `internal/ai/openai/openai.go`. Mirror the Anthropic client's structure. The parts specific to this API:

```go
// Package openai adapts the OpenAI chat completions API to the app's
// provider-neutral interfaces.
package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	sdk "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"

	"git-ui/internal/ai"
)

type Client struct {
	sdk sdk.Client
}

type Option func(*[]option.RequestOption)

func WithBaseURL(u string) Option {
	return func(opts *[]option.RequestOption) { *opts = append(*opts, option.WithBaseURL(u)) }
}

func New(apiKey string, opts ...Option) *Client {
	req := []option.RequestOption{option.WithAPIKey(apiKey)}
	for _, o := range opts {
		o(&req)
	}
	return &Client{sdk: sdk.NewClient(req...)}
}

// pending accumulates one streamed tool call: the arguments arrive as JSON
// fragments split across chunks.
type pending struct {
	id   string
	name string
	args strings.Builder
}

func (c *Client) Chat(ctx context.Context, req ai.Request) (<-chan ai.Chunk, error) {
	params := sdk.ChatCompletionNewParams{
		Model:    sdk.ChatModel(req.Model),
		Messages: messages(req.System, req.Messages),
	}
	for _, t := range req.Tools {
		params.Tools = append(params.Tools, sdk.ChatCompletionToolParam{
			Function: sdk.FunctionDefinitionParam{
				Name:        t.Name,
				Description: sdk.String(t.Description),
				Parameters:  sdk.FunctionParameters(t.Parameters),
			},
		})
	}

	stream := c.sdk.Chat.Completions.NewStreaming(ctx, params)
	ch := make(chan ai.Chunk)
	go func() {
		defer close(ch)
		calls := map[int64]*pending{}
		for stream.Next() {
			chunk := stream.Current()
			if len(chunk.Choices) == 0 {
				continue
			}
			delta := chunk.Choices[0].Delta
			if delta.Content != "" {
				if !send(ctx, ch, ai.Chunk{Delta: delta.Content}) {
					return
				}
			}
			for _, tc := range delta.ToolCalls {
				p := calls[tc.Index]
				if p == nil {
					p = &pending{}
					calls[tc.Index] = p
				}
				if tc.ID != "" {
					p.id = tc.ID
				}
				if tc.Function.Name != "" {
					p.name = tc.Function.Name
				}
				p.args.WriteString(tc.Function.Arguments)
			}
			if chunk.Choices[0].FinishReason != "" {
				for _, call := range finish(calls) {
					if !send(ctx, ch, ai.Chunk{ToolCalls: []ai.ToolCall{call}}) {
						return
					}
				}
				send(ctx, ch, ai.Chunk{Done: true})
				return
			}
		}
		if err := stream.Err(); err != nil {
			send(ctx, ch, ai.Chunk{Err: classify(err)})
			return
		}
		send(ctx, ch, ai.Chunk{Done: true})
	}()
	return ch, nil
}

// finish turns the accumulated fragments into calls, in index order.
func finish(calls map[int64]*pending) []ai.ToolCall {
	out := []ai.ToolCall{}
	for i := int64(0); i < int64(len(calls)); i++ {
		p := calls[i]
		if p == nil || p.name == "" {
			continue
		}
		args := map[string]any{}
		if s := p.args.String(); s != "" {
			if err := json.Unmarshal([]byte(s), &args); err != nil {
				args = map[string]any{}
			}
		}
		out = append(out, ai.ToolCall{ID: p.id, Name: p.name, Args: args})
	}
	return out
}

func messages(system string, history []ai.Message) []sdk.ChatCompletionMessageParamUnion {
	out := []sdk.ChatCompletionMessageParamUnion{}
	if system != "" {
		out = append(out, sdk.SystemMessage(system))
	}
	for _, m := range history {
		switch m.Role {
		case ai.RoleUser:
			out = append(out, sdk.UserMessage(m.Content))
		case ai.RoleTool:
			// The API keys a result to the call's id; the app carries the
			// tool's name, so the matching call is the last one with it.
			out = append(out, sdk.ToolMessage(m.Content, toolCallID(history, m.ToolName)))
		case ai.RoleAssistant:
			msg := sdk.ChatCompletionAssistantMessageParam{}
			if m.Content != "" {
				msg.Content.OfString = sdk.String(m.Content)
			}
			for _, tc := range m.ToolCalls {
				args, _ := json.Marshal(tc.Args)
				msg.ToolCalls = append(msg.ToolCalls, sdk.ChatCompletionMessageToolCallParam{
					ID: tc.ID,
					Function: sdk.ChatCompletionMessageToolCallFunctionParam{
						Name: tc.Name, Arguments: string(args),
					},
				})
			}
			out = append(out, sdk.ChatCompletionMessageParamUnion{OfAssistant: &msg})
		}
	}
	return out
}

// toolCallID finds the id the assistant used for the most recent call to
// name, so the result can be attached to it.
func toolCallID(history []ai.Message, name string) string {
	for i := len(history) - 1; i >= 0; i-- {
		for _, tc := range history[i].ToolCalls {
			if tc.Name == name {
				return tc.ID
			}
		}
	}
	return name
}

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

// ListModels returns the chat models the key can use. The raw list also holds
// embedding, image and audio models, which would only clutter the dropdown.
func (c *Client) ListModels(ctx context.Context) ([]string, error) {
	page, err := c.sdk.Models.List(ctx)
	if err != nil {
		return nil, classify(err)
	}
	out := []string{}
	for _, m := range page.Data {
		if isChatModel(m.ID) {
			out = append(out, m.ID)
		}
	}
	return out, nil
}

// isChatModel keeps the families that can hold a conversation with tools.
func isChatModel(id string) bool {
	for _, prefix := range []string{"gpt-", "o1", "o3", "o4", "chatgpt-"} {
		if strings.HasPrefix(id, prefix) {
			return true
		}
	}
	return false
}

func classify(err error) error {
	var apierr *sdk.Error
	if !errors.As(err, &apierr) {
		return fmt.Errorf("openai: %w", err)
	}
	switch apierr.StatusCode {
	case 401, 403:
		return errors.New("openai rejected the API key")
	case 429:
		return errors.New("openai is rate limiting; try again in a moment")
	case 400:
		return fmt.Errorf("openai rejected the request: %s", apierr.Error())
	default:
		return fmt.Errorf("openai returned %d", apierr.StatusCode)
	}
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

- [ ] **Step 5: Run the test and fix compile errors against the installed SDK**

Run: `go test ./internal/ai/openai/ -v`

As in Task 3, let the compiler name the types; check with `go doc github.com/openai/openai-go <Name>`. The likely adjustments are the exact param union constructors (`SystemMessage`, `ToolMessage`), `FunctionParameters`, and whether `Models.List` takes a params argument.
Expected once it compiles: PASS for all four tests.

- [ ] **Step 6: Commit**

```bash
git add internal/ai/openai go.mod go.sum
git commit -m "feat(ai): OpenAI provider with streaming tool calls"
```

---

### Task 5: App layer — provider factory, keys and model lists

**Files:**
- Modify: `internal/app/ai.go`
- Modify: `internal/app/merge.go:247` (the `ResolveConflicts` run)
- Modify: `internal/app/ai_test.go`, `internal/app/merge_test.go` (helpers that wire `AIDeps`)
- Modify: `main.go`

**Interfaces:**
- Consumes: `keys` (Task 1), `settings` (Task 2), `anthropic` (Task 3), `openai` (Task 4).
- Produces:
  ```go
  func (a *App) chatProvider(cfg settings.Settings) (ai.Provider, error)
  func (a *App) responderFor(provider, model string, cfg settings.Settings) (ai.Responder, error)
  func (a *App) ListModels(provider string) ([]string, error)
  func (a *App) SetProviderKey(provider, key string) error
  func (a *App) DeleteProviderKey(provider string) error
  type ProviderStatus struct {
      Provider string `json:"provider"`
      HasKey   bool   `json:"hasKey"`
      KeyHint  string `json:"keyHint"`
      Error    string `json:"error,omitempty"`
  }
  type AIStatus struct {
      Ollama    OllamaStatus     `json:"ollama"`
      Providers []ProviderStatus `json:"providers"`
      KeyStore  string           `json:"keyStore,omitempty"` // why keys can't be saved
  }
  ```

- [ ] **Step 1: Write the failing tests**

Append to `internal/app/ai_test.go`:

```go
// A hosted provider with no stored key must fail before any request, with a
// message telling the user where to fix it.
func TestChatWithoutAKeyAsksForOne(t *testing.T) {
	a, _, id, _ := newAIApp(t, "http://127.0.0.1:0")
	cfg, err := a.GetAISettings()
	if err != nil {
		t.Fatal(err)
	}
	cfg.ChatProvider, cfg.ChatModel = settings.ProviderAnthropic, "claude-opus-5"
	if err := a.SaveAISettings(cfg); err != nil {
		t.Fatal(err)
	}
	restore := keys.UseStore(&fakeKeys{items: map[string]string{}})
	defer restore()

	err = a.SendChat(id, "hola", "run1")
	if err == nil || !strings.Contains(err.Error(), "API key") {
		t.Fatalf("err = %v, want it to ask for an API key", err)
	}
}

func TestSetAndDeleteProviderKey(t *testing.T) {
	a, _, _, _ := newAIApp(t, "http://127.0.0.1:0")
	restore := keys.UseStore(&fakeKeys{items: map[string]string{}})
	defer restore()

	if err := a.SetProviderKey("openai", "sk-test-abcd1234"); err != nil {
		t.Fatal(err)
	}
	st := a.AIStatus()
	var found bool
	for _, p := range st.Providers {
		if p.Provider == "openai" {
			found = true
			if !p.HasKey || p.KeyHint != "sk-…1234" {
				t.Errorf("status = %+v, want a masked hint", p)
			}
			if strings.Contains(p.KeyHint, "test") {
				t.Error("the status leaks the key")
			}
		}
	}
	if !found {
		t.Fatal("openai missing from the status")
	}
	if err := a.DeleteProviderKey("openai"); err != nil {
		t.Fatal(err)
	}
	for _, p := range st2Providers(a) {
		if p.Provider == "openai" && p.HasKey {
			t.Error("the key is still reported after Delete")
		}
	}
}

func st2Providers(a *App) []ProviderStatus { return a.AIStatus().Providers }

func TestListModelsRefusesAnUnknownProvider(t *testing.T) {
	a, _, _, _ := newAIApp(t, "http://127.0.0.1:0")
	if _, err := a.ListModels("acme"); err == nil {
		t.Error("want an error for an unknown provider")
	}
}
```

Add to the same file a `fakeKeys` type identical to the `fake` store in `internal/ai/keys/keys_test.go` (it can't be imported across packages — repeat the eight lines).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/app/ -run 'ProviderKey|WithoutAKey|ListModels' `
Expected: FAIL — `SetProviderKey` and `ProviderStatus` are undefined.

- [ ] **Step 3: Replace the provider wiring**

In `internal/app/ai.go`:

Remove `Apple *apple.Client` from `AIDeps`, remove `appleAvail` from `aiState`, delete `appleAvailability`, and delete the `apple` import.

Add the factory:

```go
// chatProvider builds the provider for the chat and agent features from the
// settings, reading the API key of a hosted provider from the OS store.
func (a *App) chatProvider(cfg settings.Settings) (ai.Provider, error) {
	switch cfg.ChatProvider {
	case settings.ProviderOllama:
		return ollama.New(cfg.OllamaURL), nil
	case settings.ProviderOpenAI, settings.ProviderAnthropic:
		key, err := keys.Get(cfg.ChatProvider)
		if err != nil {
			return nil, err
		}
		if key == "" {
			return nil, fmt.Errorf("add an API key for %s in Settings", cfg.ChatProvider)
		}
		if cfg.ChatProvider == settings.ProviderOpenAI {
			return openai.New(key), nil
		}
		return anthropic.New(key), nil
	}
	return nil, fmt.Errorf("unknown provider %q", cfg.ChatProvider)
}

// responderFor builds the one-shot responder for tasks: commit messages and
// explanations.
func (a *App) responderFor(provider, model string, cfg settings.Settings) (ai.Responder, error) {
	switch provider {
	case settings.ProviderOllama:
		return ollama.New(cfg.OllamaURL).Responder(model), nil
	case settings.ProviderOpenAI, settings.ProviderAnthropic:
		key, err := keys.Get(provider)
		if err != nil {
			return nil, err
		}
		if key == "" {
			return nil, fmt.Errorf("add an API key for %s in Settings", provider)
		}
		if provider == settings.ProviderOpenAI {
			return openai.New(key).Responder(model), nil
		}
		return anthropic.New(key).Responder(model), nil
	}
	return nil, fmt.Errorf("unknown provider %q", provider)
}
```

Replace the two places that build a provider inline — `internal/app/ai.go` around line 272 (`SendChat`) and `internal/app/merge.go:247` (`ResolveConflicts`) — so that instead of

```go
Provider: ollama.New(cfg.OllamaURL), Model: cfg.ChatModel, System: system,
```

they call the factory before starting the run and return its error to the caller:

```go
provider, err := a.chatProvider(cfg)
if err != nil {
	return err
}
// ... then in the agent.Run literal:
Provider: provider, Model: cfg.ChatModel, System: system,
```

In `ExplainInChat`, replace the `switch` over `ProviderApple` / `ProviderOllama` with:

```go
	responder, err := a.responderFor(provider, cfg.TaskModel, cfg)
	if err != nil {
		return err
	}
	budget := tasks.OllamaDiffBudget
```

Delete the now-unused `tasks.AppleDiffBudget` reference. (The constant itself is removed in Task 6.)

- [ ] **Step 4: Replace AIStatus and add the key endpoints**

Still in `internal/app/ai.go`:

```go
type ProviderStatus struct {
	Provider string `json:"provider"`
	HasKey   bool   `json:"hasKey"`
	KeyHint  string `json:"keyHint"`
	Error    string `json:"error,omitempty"`
}

type AIStatus struct {
	Ollama    OllamaStatus     `json:"ollama"`
	Providers []ProviderStatus `json:"providers"`
	// KeyStore is why keys cannot be saved on this system, when they cannot.
	KeyStore string `json:"keyStore,omitempty"`
}
```

In `AIStatus()`, delete the two `st.Apple = ...` lines and add, before returning:

```go
	if err := keys.Available(); err != nil {
		st.KeyStore = err.Error()
	}
	for _, p := range []string{settings.ProviderOpenAI, settings.ProviderAnthropic} {
		ps := ProviderStatus{Provider: p}
		switch key, err := keys.Get(p); {
		case err != nil:
			ps.Error = err.Error()
		case key != "":
			ps.HasKey, ps.KeyHint = true, keys.Mask(key)
		}
		st.Providers = append(st.Providers, ps)
	}
```

Initialise `Providers` to an empty slice in the early-error return, as `Ollama.Models` already is, so the frontend never sees `null`.

Add the endpoints:

```go
// SetProviderKey stores an API key for a hosted provider. The key is never
// written to the settings file and never returned to the frontend.
func (a *App) SetProviderKey(provider, key string) error { return keys.Set(provider, key) }

func (a *App) DeleteProviderKey(provider string) error { return keys.Delete(provider) }

// ListModels returns the models the given provider offers. Ollama lists what
// is installed; a hosted provider needs its key.
func (a *App) ListModels(provider string) ([]string, error) {
	cfg, err := a.aiSettings()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()
	switch provider {
	case settings.ProviderOllama:
		models, err := ollama.New(cfg.OllamaURL).ListModels(ctx)
		if err != nil {
			return nil, err
		}
		out := []string{}
		for _, m := range models {
			out = append(out, m.Name)
		}
		return out, nil
	case settings.ProviderOpenAI, settings.ProviderAnthropic:
		key, err := keys.Get(provider)
		if err != nil {
			return nil, err
		}
		if key == "" {
			return nil, fmt.Errorf("add an API key for %s in Settings", provider)
		}
		if provider == settings.ProviderOpenAI {
			return openai.New(key).ListModels(ctx)
		}
		return anthropic.New(key).ListModels(ctx)
	}
	return nil, fmt.Errorf("unknown provider %q", provider)
}
```

- [ ] **Step 5: Fix the test helpers and main.go**

In `internal/app/ai_test.go` and `internal/app/merge_test.go`, remove `Apple: apple.New("")` from every `WithAI(...)` call and the `apple` import. In `main.go`, remove `Apple: apple.New(apple.Locate()),` and its import.

- [ ] **Step 6: Run the whole suite**

Run: `go vet ./... && go test ./...`
Expected: everything passes, including the three new tests.

- [ ] **Step 7: Commit**

```bash
git add internal/app main.go
git commit -m "feat(ai): pick the provider per feature and keep keys out of settings"
```

---

### Task 6: Delete Apple Intelligence

**Files:**
- Delete: `internal/ai/apple/` (whole directory)
- Delete: `helpers/apple/` (whole directory)
- Modify: `Makefile` (the `helper` target and the codesign step that copies `git-ui-apple`)
- Modify: `internal/ai/tasks/explain.go` (remove `AppleDiffBudget`)
- Modify: `internal/ai/settings/settings.go` (remove the `providerApple` constant only if nothing reads it — `Load` still does, so keep it)

- [ ] **Step 1: Check what still refers to Apple**

Run: `grep -rni apple --include='*.go' --include='Makefile' --include='*.svelte' --include='*.ts' . | grep -v frontend/wailsjs`
Expected: hits only in the files listed above (plus `settings.go`'s migration constant, which stays).

- [ ] **Step 2: Delete the packages and the helper**

```bash
git rm -r internal/ai/apple helpers/apple
```

- [ ] **Step 3: Update the Makefile**

Remove the `helper:` target, the `build: helper` dependency (leaving `build:`), and the two lines that copy `git-ui-apple` into the bundle and codesign it. Keep the `git checkout -- frontend/wailsjs/runtime` line.

- [ ] **Step 4: Remove AppleDiffBudget**

In `internal/ai/tasks/explain.go`, delete the `AppleDiffBudget` constant. `OllamaDiffBudget` stays as the budget for every provider.

- [ ] **Step 5: Verify**

Run: `go vet ./... && go test ./... && make build`
Expected: the suite passes and the app builds with no Swift helper. Confirm the bundle no longer contains it:

```bash
ls build/bin/git-ui.app/Contents/MacOS/
```
Expected: `git-ui` only.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "feat(ai): remove Apple Intelligence

It is one-shot with no tool calling, so it cannot drive the chat, the
explain feature or the conflict agent, and it pins the app to recent Apple
silicon. The hosted providers replace it."
```

---

### Task 7: Settings UI

**Files:**
- Modify: `frontend/src/lib/types.ts`
- Modify: `frontend/src/lib/api.ts`
- Create: `frontend/src/lib/providers.ts`
- Create: `frontend/src/lib/providers.test.ts`
- Modify: `frontend/src/components/SettingsDialog.svelte`

**Interfaces:**
- Consumes: the Go bindings regenerated by `wails generate module` after Task 5.
- Produces:
  ```ts
  export type ProviderName = 'ollama' | 'openai' | 'anthropic'
  export interface ProviderStatus { provider: string; hasKey: boolean; keyHint: string; error?: string }
  export interface AISettings { ollamaURL: string; chatProvider: ProviderName; chatModel: string; taskProvider: ProviderName; taskModel: string }
  export const PROVIDERS: { value: ProviderName; label: string }[]
  export function needsKey(p: ProviderName): boolean
  export function modelHint(p: ProviderName, status: AIStatus): string   // "" when the dropdown can be shown
  ```

- [ ] **Step 1: Regenerate the bindings**

```bash
export PATH=$HOME/.nvm/versions/node/v22.23.1/bin:$HOME/go/bin:$PATH
wails generate module
git checkout -- frontend/wailsjs/runtime
grep -n "SetProviderKey\|ListModels" frontend/wailsjs/go/app/App.d.ts
```
Expected: both functions are listed.

- [ ] **Step 2: Write the failing test**

Create `frontend/src/lib/providers.test.ts`:

```ts
import { describe, expect, it } from 'vitest'
import { modelHint, needsKey, PROVIDERS } from './providers'
import type { AIStatus } from './types'

const status = (over: Partial<AIStatus> = {}): AIStatus => ({
  ollama: { running: true, url: '', chatModel: '', models: [], chatModelInstalled: true },
  providers: [
    { provider: 'openai', hasKey: false, keyHint: '' },
    { provider: 'anthropic', hasKey: true, keyHint: 'sk-…abcd' },
  ],
  ...over,
})

describe('providers', () => {
  it('lists the three providers, ollama first', () => {
    expect(PROVIDERS.map((p) => p.value)).toEqual(['ollama', 'openai', 'anthropic'])
  })

  it('knows which providers need a key', () => {
    expect(needsKey('ollama')).toBe(false)
    expect(needsKey('openai')).toBe(true)
  })

  it('asks for a key when the provider has none', () => {
    expect(modelHint('openai', status())).toBe('Add a key to see the models')
  })

  it('has no hint once the key is stored', () => {
    expect(modelHint('anthropic', status())).toBe('')
  })

  it('reports that keys cannot be saved at all', () => {
    expect(modelHint('openai', status({ keyStore: 'no usable secret store' }))).toBe('no usable secret store')
  })
})
```

- [ ] **Step 3: Run it to verify it fails**

Run: `cd frontend && PATH=$HOME/.nvm/versions/node/v22.23.1/bin:$PATH npx vitest run src/lib/providers.test.ts`
Expected: FAIL — `./providers` does not exist.

- [ ] **Step 4: Write the module and the types**

Create `frontend/src/lib/providers.ts`:

```ts
import type { AIStatus, ProviderName } from './types'

export const PROVIDERS: { value: ProviderName; label: string }[] = [
  { value: 'ollama', label: 'Ollama (local)' },
  { value: 'openai', label: 'OpenAI' },
  { value: 'anthropic', label: 'Anthropic' },
]

/** Hosted providers need an API key; Ollama runs on the user's machine. */
export function needsKey(p: ProviderName): boolean {
  return p === 'openai' || p === 'anthropic'
}

/**
 * modelHint is what to show instead of the model dropdown: why it is empty,
 * or "" when the models can be listed.
 */
export function modelHint(p: ProviderName, status: AIStatus): string {
  if (!needsKey(p)) return ''
  if (status.keyStore) return status.keyStore
  const found = status.providers.find((s) => s.provider === p)
  if (found?.error) return found.error
  return found?.hasKey ? '' : 'Add a key to see the models'
}
```

In `frontend/src/lib/types.ts`, replace the Apple part of `AIStatus` and extend `AISettings`:

```ts
export type ProviderName = 'ollama' | 'openai' | 'anthropic'
export interface ProviderStatus { provider: string; hasKey: boolean; keyHint: string; error?: string }
export interface AIStatus {
  ollama: OllamaStatus
  providers: ProviderStatus[]
  keyStore?: string
}
export interface AISettings {
  ollamaURL: string
  chatProvider: ProviderName
  chatModel: string
  taskProvider: ProviderName
  taskModel: string
}
```

In `frontend/src/lib/api.ts`, add:

```ts
  listModels: (provider: string) => call<string[]>(Go.ListModels(provider)),
  setProviderKey: (provider: string, key: string) => call<void>(Go.SetProviderKey(provider, key)),
  deleteProviderKey: (provider: string) => call<void>(Go.DeleteProviderKey(provider)),
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `cd frontend && PATH=$HOME/.nvm/versions/node/v22.23.1/bin:$PATH npx vitest run src/lib/providers.test.ts`
Expected: PASS, 5 tests.

- [ ] **Step 6: Update the Settings dialog**

In `frontend/src/components/SettingsDialog.svelte`:

1. Delete the `appleReasons` map, the `appleText` derivation and the Apple row in the markup.
2. Add, for chat/agent and for tasks, a provider `<select>` bound to `settings.chatProvider` / `settings.taskProvider`, built from `PROVIDERS`, and next to each a model `<select>` filled from `api.listModels(provider)`; when `modelHint(provider, status)` is non-empty, show that text instead of the dropdown.
3. Add a key row per hosted provider:

```svelte
{#each PROVIDERS.filter((p) => needsKey(p.value)) as p}
  {@const st = status?.providers.find((s) => s.provider === p.value)}
  <label class="row">
    <span>{p.label} API key</span>
    {#if st?.hasKey}
      <span class="hint">{st.keyHint}</span>
      <button class="btn" on:click={() => removeKey(p.value)}>Remove</button>
    {:else}
      <input type="password" placeholder="sk-…" bind:value={keyInput[p.value]} />
      <button class="btn primary" disabled={!keyInput[p.value]} on:click={() => saveKey(p.value)}>Save</button>
    {/if}
  </label>
{/each}
```

with

```ts
  let keyInput: Record<string, string> = { openai: '', anthropic: '' }

  async function saveKey(provider: string) {
    try {
      await api.setProviderKey(provider, keyInput[provider])
      keyInput[provider] = ''
      await refreshStatus()
      await loadModels()
    } catch (e) {
      toast(errorMessage(e), 'error')
    }
  }

  async function removeKey(provider: string) {
    try {
      await api.deleteProviderKey(provider)
      await refreshStatus()
    } catch (e) {
      toast(errorMessage(e), 'error')
    }
  }
```

`refreshStatus` and `loadModels` are the dialog's existing status/model loaders; rename or add them to match what is already there. Keep the Ollama URL and download sections untouched.

- [ ] **Step 7: Verify the frontend**

Run:
```bash
cd frontend && PATH=$HOME/.nvm/versions/node/v22.23.1/bin:$PATH npm run check && PATH=$HOME/.nvm/versions/node/v22.23.1/bin:$PATH npx vitest run
```
Expected: 0 errors (the 3 pre-existing a11y warnings stay), and every test passes.

- [ ] **Step 8: Commit**

```bash
git add frontend/src frontend/wailsjs/go
git commit -m "feat(ui): choose an AI provider per feature and store its API key"
```

---

### Task 8: Manual verification and documentation

**Files:**
- Modify: `README.md` (the AI section, if it mentions Apple Intelligence)
- Modify: `docs/superpowers/specs/2026-09-20-ai-providers-design.md` (status line)

- [ ] **Step 1: Build and launch**

```bash
export PATH=$HOME/.nvm/versions/node/v22.23.1/bin:$PATH
make build && pkill -x git-ui; open build/bin/git-ui.app
```

- [ ] **Step 2: Walk the manual checklist**

These need a real key and a real OS store, so no automated test covers them. Tick each:

- [ ] Settings shows three providers for chat and for tasks.
- [ ] With no key, the model dropdown says "Add a key to see the models".
- [ ] Saving a key shows it masked; the full key never reappears.
- [ ] The model list loads for the provider with a key.
- [ ] A chat answer streams from the hosted provider.
- [ ] The conflict agent resolves a hunk through the hosted provider (use the merge-demo repository).
- [ ] Removing the key returns the dialog to the "Add a key" state.
- [ ] An invalid key produces "rejected the API key" in the chat, not a raw HTTP error.
- [ ] `~/Library/Application Support/git-ui/ai.json` contains no key:
      `grep -i "sk-\|key" ~/Library/Application\ Support/git-ui/ai.json` prints nothing.

- [ ] **Step 3: Update the docs**

Remove any mention of Apple Intelligence from `README.md` and say which providers exist and where keys are stored. Set the spec's `Status:` line to `Implemented`.

- [ ] **Step 4: Commit**

```bash
git add README.md docs/superpowers/specs/2026-09-20-ai-providers-design.md
git commit -m "docs: hosted AI providers replace Apple Intelligence"
```

---

## Notes for the executor

- **The SDK type names in Tasks 3 and 4 are the most likely thing to be wrong.** Do not research them online first: write the file, run `go build ./...`, and let the compiler name what it wants. `go doc <pkg> <Name>` resolves the rest. The tests are the contract, not the sample code.
- **Never print a key**, not even in a test failure message. Assert on the mask.
- **Keep the agent loop out of this work.** `internal/ai/agent` already handles tool calls, step limits and history trimming for any provider, including the recovery of calls a model writes as text.
- After Task 5 the app no longer compiles against `apple`; Task 6 removes the rest. Do the two in order.
- **Deliberate deviation from the spec:** the spec suggests caching each provider's model list for the session. `ListModels` here fetches on demand, because the dialog only calls it when it opens or when the provider changes — a request per opening, not per keystroke. If that proves slow in the manual check, add the cache then; it is a map in `aiState` plus a refresh button.
