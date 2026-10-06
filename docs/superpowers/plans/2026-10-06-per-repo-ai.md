# AI settings per repository — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let each repository override the global AI settings (provider, model, automations, AI off) and add prompt instructions, private or shared through `.committree/` files approved by the user.

**Architecture:** A new Go package `internal/ai/reposettings` owns `repo-ai.json` (private overrides by repository ID), the `.committree/` reader with its approval hash, and prompt assembly — all pure or file-level, tested alone. `internal/app` swaps `aiSettings()` for `aiSettingsFor(repoID)` at every AI entry point, builds prompts through one helper, and exposes four new bindings. The frontend keeps its `aiSettings` store but fills it with the selected repository's effective settings, adds a `repoAI` store, a Repository settings → AI tab, and the chat strip / AI-off states.

**Tech Stack:** Go 1.26, Wails v2, Svelte 5 (legacy `$:` syntax as in the codebase), TypeScript, vitest.

**Spec:** `docs/superpowers/specs/2026-10-05-per-repo-ai-design.md`

## Global Constraints

- File: `<UserConfigDir>/git-ui/repo-ai.json`, next to `ai.json`, derived as `filepath.Join(filepath.Dir(AIDeps.SettingsPath), "repo-ai.json")` — no new wiring in `main.go`.
- Shared files: only regular `.md` files directly in `.committree/`; no symlinks; UTF-8 only; ≤ 16 KB each (16384 bytes); ≤ 32 KB total (32768 bytes), files taken in name order.
- Action names are exactly the prompt names: `chat`, `commit-message`, `resolve-conflicts`, `explain-commit`, `explain-lines`, `suggest-replies`. `instructions.md` = every action; private key `all` = every action.
- Prompt headings, verbatim: `## Project instructions` and `## Your instructions for this repository`. Order: global prompt → project → private.
- Error texts, verbatim: `AI is off for this repository`; `Per-repository AI settings can't be read: <reason>`; suffix ` (set in this repository's settings)`.
- A corrupt `repo-ai.json` never falls back to global settings: every AI action refuses.
- A repository file can never set provider, model, or AI on/off.
- Approve/Ignore take the hash the user saw; refused if the current hash differs.
- Commits: no `Co-Authored-By` lines. Use `/usr/bin/git`. Behaviour changes update `docs/spec/` in the same commit.
- Wails bindings in `frontend/wailsjs/go/` are committed: regenerate them with `~/go/bin/wails generate module` after adding bindings, then `git checkout -- frontend/wailsjs/runtime`.
- Run Go tests with `go test ./...`; frontend with `cd frontend && npm test && npm run check` (Node 22: `source ~/.nvm/nvm.sh && nvm use 22`).

## Review Focus

1. **A repository whose `.committree/` changes between Approve being shown and clicked** (e.g. `git pull` while the tab is open) — the click must be refused and the tab must show the new content, never approve unseen text. Pinned in Task 5 (`TestApproveRefusesStaleHash`).
2. **Turning the AI off while a chat answer or commit message is streaming** — the run must stop and no further text reach the UI. Pinned in Task 5 (`TestTurningAIOffCancelsRunning`).
3. **A linked worktree of a repository with the AI off** — the worktree must be off too (same entry), not fall back to global. Pinned in Task 4 (`TestWorktreeUsesMainRepoSettings`).
4. **A `repo-ai.json` that is valid JSON but holds an invalid entry** (unknown provider written by hand) — treat like corrupt: refuse, never fall back. Pinned in Task 1 (`TestLoadRejectsInvalidEntry`).
5. **`.committree` that is a file, or a symlinked directory** — no instructions, a reason shown, no crash, no reading outside the repository. Pinned in Task 2 (`TestReadRepoDirectoryIsSymlinkOrFile`).

---

## File map

| File | Responsibility |
|---|---|
| `internal/ai/reposettings/reposettings.go` (new) | `Override`, `Store` (load/save/remove/approval), `Validate`, `Merge`, errors |
| `internal/ai/reposettings/committree.go` (new) | `.committree/` reader, hash, `StateOf` |
| `internal/ai/reposettings/assemble.go` (new) | `Assemble` — final prompt text |
| `internal/app/repoai.go` (new) | `aiSettingsFor`, `systemPrompt`, `repoNote`, bindings `GetRepoAISettings`, `SaveRepoAISettings`, `ApproveRepoInstructions`, `IgnoreRepoInstructions`, `stopRepoAI` |
| `internal/app/ai.go`, `merge.go`, `worktree.go`, `suggest.go`, `app.go` | switch to `aiSettingsFor` / `systemPrompt`; commit-message cancel; `RemoveRepo` drops the entry |
| `frontend/src/lib/repoAI.ts` (new) | pure helpers for the AI tab and picker |
| `frontend/src/lib/types.ts`, `api.ts`, `stores.ts`, `providers.ts` | types, bindings, per-repo store, blocker takes the model |
| `frontend/src/components/RepoAITab.svelte` (new) | the AI tab |
| `frontend/src/components/RepoSettingsDialog.svelte` | tabs Remotes / AI |
| `ChatPanel.svelte`, `ModelPicker.svelte`, `CommitBox.svelte`, `MergeView.svelte`, `LogList.svelte`, `BlameView.svelte`, `SettingsDialog.svelte`, `App.svelte` | strip, AI-off panel, hidden buttons, "· this repo", Prompts note, reload on repo change |
| `docs/spec/06-ai.md`, `docs/spec/12-repository-settings.md` | behaviour |

---

### Task 1: `reposettings` — overrides store, validation, merge

**Files:**
- Create: `internal/ai/reposettings/reposettings.go`
- Test: `internal/ai/reposettings/reposettings_test.go`

**Interfaces:**
- Consumes: `settings.Settings`, `settings.Provider*`, `settings.Commit*`, `settings.Suggest*` (`internal/ai/settings`), `prompts.Names() []string` (`internal/ai/prompts`).
- Produces:
  - `type Override struct { AIOff bool; ChatProvider, ChatModel, TaskProvider, TaskModel, CommitMessage, SuggestReplies string; Instructions map[string]string; Approval string }` with JSON tags `aiOff, chatProvider, chatModel, taskProvider, taskModel, commitMessage, suggestReplies, instructions, approvedRepoInstructions` (all `omitempty`).
  - `var ErrInvalid`, `var ErrUnreadable`
  - `func PathNextTo(aiSettingsPath string) string`
  - `func New(path string) *Store`
  - `func (s *Store) Get(id string) (Override, error)` — missing file/entry → zero `Override`, nil.
  - `func (s *Store) Set(id string, o Override) error` — validates; keeps the stored `Approval` (callers can't change it here); empty → entry removed.
  - `func (s *Store) SetApproval(id, value string) error`
  - `func (s *Store) Remove(id string) error`
  - `func Validate(o Override) error`
  - `func Merge(g settings.Settings, o Override) settings.Settings`
  - `func (o Override) IsEmpty() bool`

- [ ] **Step 1: Write the failing tests**

```go
package reposettings

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"git-ui/internal/ai/settings"
)

func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "repo-ai.json")
	return New(p), p
}

func TestPathNextTo(t *testing.T) {
	if got := PathNextTo("/x/git-ui/ai.json"); got != "/x/git-ui/repo-ai.json" {
		t.Fatalf("got %q", got)
	}
}

func TestGetMissingFileIsEmpty(t *testing.T) {
	s, _ := newStore(t)
	o, err := s.Get("r1")
	if err != nil || !o.IsEmpty() {
		t.Fatalf("got %+v, %v", o, err)
	}
}

func TestSetGetRoundTripAndRemoveWhenEmpty(t *testing.T) {
	s, p := newStore(t)
	o := Override{ChatProvider: settings.ProviderAnthropic, ChatModel: "claude-opus-5", Instructions: map[string]string{"all": "Use English."}}
	if err := s.Set("r1", o); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("r1")
	if err != nil || got.ChatModel != "claude-opus-5" || got.Instructions["all"] != "Use English." {
		t.Fatalf("got %+v, %v", got, err)
	}
	if err := s.Set("r1", Override{}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	if string(data) != "{}" {
		t.Fatalf("entry not removed: %s", data)
	}
}

func TestSetKeepsApproval(t *testing.T) {
	s, _ := newStore(t)
	if err := s.SetApproval("r1", "sha256:abc"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("r1", Override{AIOff: true, Approval: "sha256:forged"}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get("r1")
	if got.Approval != "sha256:abc" || !got.AIOff {
		t.Fatalf("got %+v", got)
	}
}

func TestRemove(t *testing.T) {
	s, _ := newStore(t)
	_ = s.Set("r1", Override{AIOff: true})
	if err := s.Remove("r1"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get("r1")
	if !got.IsEmpty() {
		t.Fatalf("got %+v", got)
	}
}

func TestCorruptFileIsUnreadable(t *testing.T) {
	s, p := newStore(t)
	_ = os.WriteFile(p, []byte("{not json"), 0o644)
	if _, err := s.Get("r1"); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("got %v", err)
	}
	if err := s.Set("r1", Override{AIOff: true}); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("Set over a corrupt file must refuse, got %v", err)
	}
}

func TestLoadRejectsInvalidEntry(t *testing.T) {
	s, p := newStore(t)
	_ = os.WriteFile(p, []byte(`{"r1":{"chatProvider":"bogus","chatModel":"x"}}`), 0o644)
	if _, err := s.Get("r2"); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("an invalid entry anywhere makes the file unreadable, got %v", err)
	}
}

func TestValidate(t *testing.T) {
	bad := []Override{
		{ChatProvider: settings.ProviderOllama},             // provider without model
		{ChatModel: "qwen2.5:7b"},                          // model without provider
		{TaskProvider: "bogus", TaskModel: "x"},            // unknown provider
		{CommitMessage: "sometimes"},                       // unknown mode
		{SuggestReplies: "maybe"},                          // unknown mode
		{Instructions: map[string]string{"deploy": "x"}},   // unknown action
	}
	for i, o := range bad {
		if err := Validate(o); !errors.Is(err, ErrInvalid) {
			t.Errorf("%d: want ErrInvalid, got %v", i, err)
		}
	}
	good := Override{TaskProvider: settings.ProviderOllama, TaskModel: "qwen2.5:7b", CommitMessage: settings.CommitManual,
		Instructions: map[string]string{"all": "a", "commit-message": "b"}}
	if err := Validate(good); err != nil {
		t.Fatal(err)
	}
}

func TestMerge(t *testing.T) {
	g := settings.Defaults()
	got := Merge(g, Override{ChatProvider: settings.ProviderAnthropic, ChatModel: "claude-opus-5", SuggestReplies: settings.SuggestOff})
	if got.ChatProvider != settings.ProviderAnthropic || got.ChatModel != "claude-opus-5" || got.SuggestReplies != settings.SuggestOff {
		t.Fatalf("overrides not applied: %+v", got)
	}
	if got.TaskProvider != g.TaskProvider || got.TaskModel != g.TaskModel || got.CommitMessage != g.CommitMessage || got.OllamaURL != g.OllamaURL {
		t.Fatalf("global values lost: %+v", got)
	}
}

func TestIsEmptyIgnoresBlankInstructions(t *testing.T) {
	if !(Override{Instructions: map[string]string{"all": "  "}}).IsEmpty() {
		t.Fatal("blank instructions count as empty")
	}
	if (Override{Approval: "sha256:x"}).IsEmpty() {
		t.Fatal("an approval is not empty")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ai/reposettings/`
Expected: FAIL — package has no Go files / undefined `New`.

- [ ] **Step 3: Implement**

```go
// Package reposettings stores the AI settings a repository overrides,
// reads the instructions a repository shares in .committree/, and builds
// the final prompt from both.
package reposettings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"git-ui/internal/ai/prompts"
	"git-ui/internal/ai/settings"
)

var (
	ErrInvalid    = errors.New("invalid repository AI settings")
	ErrUnreadable = errors.New("Per-repository AI settings can't be read")
)

// AllActions is the instructions key, private or shared, that applies to
// every action.
const AllActions = "all"

// Override is what one repository changes; an empty field means "use the
// global value". Approval is the hash of the .committree files the user
// approved, or "ignored:<hash>"; only SetApproval changes it.
type Override struct {
	AIOff          bool              `json:"aiOff,omitempty"`
	ChatProvider   string            `json:"chatProvider,omitempty"`
	ChatModel      string            `json:"chatModel,omitempty"`
	TaskProvider   string            `json:"taskProvider,omitempty"`
	TaskModel      string            `json:"taskModel,omitempty"`
	CommitMessage  string            `json:"commitMessage,omitempty"`
	SuggestReplies string            `json:"suggestReplies,omitempty"`
	Instructions   map[string]string `json:"instructions,omitempty"`
	Approval       string            `json:"approvedRepoInstructions,omitempty"`
}

func (o Override) IsEmpty() bool {
	for _, v := range o.Instructions {
		if strings.TrimSpace(v) != "" {
			return false
		}
	}
	return !o.AIOff && o.ChatProvider == "" && o.ChatModel == "" && o.TaskProvider == "" && o.TaskModel == "" &&
		o.CommitMessage == "" && o.SuggestReplies == "" && o.Approval == ""
}

// PathNextTo is repo-ai.json in the directory of the global ai.json.
func PathNextTo(aiSettingsPath string) string {
	return filepath.Join(filepath.Dir(aiSettingsPath), "repo-ai.json")
}

type Store struct {
	path string
	mu   sync.Mutex
}

func New(path string) *Store { return &Store{path: path} }

// load reads every entry. A file that is not JSON, or holds an entry that
// would not validate, is unreadable as a whole: callers must refuse AI
// actions rather than fall back to the global settings.
func (s *Store) load() (map[string]Override, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]Override{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	all := map[string]Override{}
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	for id, o := range all {
		if err := Validate(o); err != nil {
			return nil, fmt.Errorf("%w: entry %s: %v", ErrUnreadable, id, err)
		}
	}
	return all, nil
}

func (s *Store) save(all map[string]Override) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	if len(all) == 0 {
		data = []byte("{}")
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) Get(id string) (Override, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.load()
	if err != nil {
		return Override{}, err
	}
	return all[id], nil
}

// Set replaces id's overrides, keeping the stored approval. An entry left
// empty is removed.
func (s *Store) Set(id string, o Override) error {
	if err := Validate(o); err != nil {
		return err
	}
	return s.update(id, func(cur Override) Override {
		o.Approval = cur.Approval
		return o
	})
}

func (s *Store) SetApproval(id, value string) error {
	return s.update(id, func(cur Override) Override {
		cur.Approval = value
		return cur
	})
}

func (s *Store) Remove(id string) error {
	return s.update(id, func(Override) Override { return Override{} })
}

func (s *Store) update(id string, fn func(Override) Override) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.load()
	if err != nil {
		return err
	}
	next := fn(all[id])
	if next.IsEmpty() {
		delete(all, id)
	} else {
		all[id] = next
	}
	return s.save(all)
}

func Validate(o Override) error {
	pairs := [][2]string{{o.ChatProvider, o.ChatModel}, {o.TaskProvider, o.TaskModel}}
	for _, p := range pairs {
		if (p[0] == "") != (p[1] == "") {
			return fmt.Errorf("%w: a provider and its model are set together", ErrInvalid)
		}
		switch p[0] {
		case "", settings.ProviderOllama, settings.ProviderOpenAI, settings.ProviderAnthropic:
		default:
			return fmt.Errorf("%w: unknown provider %q", ErrInvalid, p[0])
		}
	}
	switch o.CommitMessage {
	case "", settings.CommitAutoLocal, settings.CommitAuto, settings.CommitManual:
	default:
		return fmt.Errorf("%w: unknown commit message mode %q", ErrInvalid, o.CommitMessage)
	}
	switch o.SuggestReplies {
	case "", settings.SuggestAutoLocal, settings.SuggestAuto, settings.SuggestOff:
	default:
		return fmt.Errorf("%w: unknown suggested replies mode %q", ErrInvalid, o.SuggestReplies)
	}
	for k := range o.Instructions {
		if k != AllActions && !isAction(k) {
			return fmt.Errorf("%w: unknown action %q", ErrInvalid, k)
		}
	}
	return nil
}

func isAction(name string) bool {
	for _, n := range prompts.Names() {
		if n == name {
			return true
		}
	}
	return false
}

// Merge returns the global settings with o's non-empty fields on top.
func Merge(g settings.Settings, o Override) settings.Settings {
	if o.ChatProvider != "" {
		g.ChatProvider, g.ChatModel = o.ChatProvider, o.ChatModel
	}
	if o.TaskProvider != "" {
		g.TaskProvider, g.TaskModel = o.TaskProvider, o.TaskModel
	}
	if o.CommitMessage != "" {
		g.CommitMessage = o.CommitMessage
	}
	if o.SuggestReplies != "" {
		g.SuggestReplies = o.SuggestReplies
	}
	return g
}
```

Before relying on `prompts.Names()`, confirm it returns exactly the six names listed in Global Constraints (`internal/ai/prompts/prompts.go:57`); if it returns more, `isAction` still accepts only real prompt names, which is what we want.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ai/reposettings/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
/usr/bin/git add internal/ai/reposettings
/usr/bin/git commit -m "feat(reposettings): per-repository AI overrides store with validation and merge"
```

---

### Task 2: `.committree/` reader, hash and approval state

**Files:**
- Create: `internal/ai/reposettings/committree.go`
- Test: `internal/ai/reposettings/committree_test.go`

**Interfaces:**
- Consumes: `isAction` (Task 1).
- Produces:
  - `const Dir = ".committree"`, `const MaxFile = 16 << 10`, `const MaxTotal = 32 << 10`, `const SharedFile = "instructions.md"`
  - `type RepoFile struct { Name string \`json:"name"\`; Text string \`json:"text"\`; Ignored string \`json:"ignored,omitempty"\` }`
  - `type RepoInstructions struct { Files []RepoFile \`json:"files"\`; Hash string \`json:"hash"\`; Error string \`json:"error,omitempty"\` }`
  - `func ReadRepo(dir string) RepoInstructions`
  - `func (ri RepoInstructions) Text(action string) []string` — non-ignored texts for `instructions.md` then `<action>.md`.
  - `const StateNone, StateApproved, StatePending, StateIgnored, StateChanged = "none", "approved", "pending", "ignored", "changed"`
  - `const IgnoredPrefix = "ignored:"`
  - `func StateOf(approval, hash string) string`

- [ ] **Step 1: Write the failing tests**

```go
package reposettings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, text string) {
	t.Helper()
	p := filepath.Join(dir, Dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func file(ri RepoInstructions, name string) RepoFile {
	for _, f := range ri.Files {
		if f.Name == name {
			return f
		}
	}
	return RepoFile{Name: "<missing>"}
}

func TestReadRepoNoDirectory(t *testing.T) {
	ri := ReadRepo(t.TempDir())
	if len(ri.Files) != 0 || ri.Hash != "" || ri.Error != "" {
		t.Fatalf("got %+v", ri)
	}
}

func TestReadRepoUsedAndIgnoredFiles(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "instructions.md", "We use Go.")
	write(t, dir, "commit-message.md", "Conventional Commits.")
	write(t, dir, "deploy.md", "x")
	write(t, dir, "notes.txt", "x")
	write(t, dir, "big.md", strings.Repeat("a", MaxFile+1))
	write(t, dir, "bad.md", "\xff\xfe")
	ri := ReadRepo(dir)
	if f := file(ri, "instructions.md"); f.Ignored != "" || f.Text != "We use Go." {
		t.Fatalf("instructions.md: %+v", f)
	}
	if f := file(ri, "deploy.md"); f.Ignored != "not used" {
		t.Fatalf("deploy.md: %+v", f)
	}
	if f := file(ri, "big.md"); !strings.Contains(f.Ignored, "16 KB") {
		t.Fatalf("big.md: %+v", f)
	}
	if f := file(ri, "bad.md"); f.Ignored != "not UTF-8 text" {
		t.Fatalf("bad.md: %+v", f)
	}
	if f := file(ri, "notes.txt"); f.Name != "<missing>" {
		t.Fatalf("non-.md files are not listed: %+v", f)
	}
	if !strings.HasPrefix(ri.Hash, "sha256:") {
		t.Fatalf("hash %q", ri.Hash)
	}
	if got := ri.Text("commit-message"); len(got) != 2 || got[0] != "We use Go." || got[1] != "Conventional Commits." {
		t.Fatalf("Text: %q", got)
	}
	if got := ri.Text("chat"); len(got) != 1 {
		t.Fatalf("Text(chat): %q", got)
	}
}

func TestReadRepoTotalLimitInNameOrder(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "chat.md", strings.Repeat("a", MaxFile))
	write(t, dir, "commit-message.md", strings.Repeat("b", MaxFile))
	write(t, dir, "explain-commit.md", "c")
	ri := ReadRepo(dir)
	if f := file(ri, "explain-commit.md"); !strings.Contains(f.Ignored, "32 KB") {
		t.Fatalf("explain-commit.md should be past the total: %+v", f)
	}
}

func TestReadRepoSkipsSymlinks(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.md")
	_ = os.WriteFile(outside, []byte("secret"), 0o644)
	write(t, dir, "instructions.md", "ok")
	if err := os.Symlink(outside, filepath.Join(dir, Dir, "chat.md")); err != nil {
		t.Fatal(err)
	}
	ri := ReadRepo(dir)
	if f := file(ri, "chat.md"); f.Ignored != "a symbolic link" || f.Text != "" {
		t.Fatalf("chat.md: %+v", f)
	}
}

func TestReadRepoDirectoryIsSymlinkOrFile(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, Dir), []byte("x"), 0o644)
	if ri := ReadRepo(dir); len(ri.Files) != 0 || ri.Hash != "" || ri.Error == "" {
		t.Fatalf("a file named .committree: %+v", ri)
	}
	dir2 := t.TempDir()
	target := t.TempDir()
	_ = os.WriteFile(filepath.Join(target, "instructions.md"), []byte("outside"), 0o644)
	_ = os.Symlink(target, filepath.Join(dir2, Dir))
	if ri := ReadRepo(dir2); len(ri.Files) != 0 || ri.Hash != "" || ri.Error == "" {
		t.Fatalf("a symlinked .committree: %+v", ri)
	}
}

func TestHashChangesWithContentAndName(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "instructions.md", "a")
	h1 := ReadRepo(dir).Hash
	if h1 != ReadRepo(dir).Hash {
		t.Fatal("hash not stable")
	}
	write(t, dir, "instructions.md", "b")
	h2 := ReadRepo(dir).Hash
	write(t, dir, "chat.md", "")
	h3 := ReadRepo(dir).Hash
	if h1 == h2 || h2 == h3 {
		t.Fatalf("hashes %s %s %s", h1, h2, h3)
	}
}

func TestStateOf(t *testing.T) {
	cases := []struct{ approval, hash, want string }{
		{"", "", StateNone},
		{"sha256:a", "", StateNone},
		{"", "sha256:a", StatePending},
		{"sha256:a", "sha256:a", StateApproved},
		{"ignored:sha256:a", "sha256:a", StateIgnored},
		{"sha256:a", "sha256:b", StateChanged},
		{"ignored:sha256:a", "sha256:b", StateChanged},
	}
	for _, c := range cases {
		if got := StateOf(c.approval, c.hash); got != c.want {
			t.Errorf("StateOf(%q,%q) = %s, want %s", c.approval, c.hash, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ai/reposettings/ -run 'ReadRepo|Hash|StateOf'`
Expected: FAIL — undefined `ReadRepo`.

- [ ] **Step 3: Implement**

```go
package reposettings

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	Dir        = ".committree"
	SharedFile = "instructions.md"
	MaxFile    = 16 << 10
	MaxTotal   = 32 << 10

	StateNone     = "none"
	StateApproved = "approved"
	StatePending  = "pending"
	StateIgnored  = "ignored"
	StateChanged  = "changed"

	IgnoredPrefix = "ignored:"
)

type RepoFile struct {
	Name    string `json:"name"`
	Text    string `json:"text"`
	Ignored string `json:"ignored,omitempty"`
}

type RepoInstructions struct {
	Files []RepoFile `json:"files"`
	// Hash covers the used files only; "" when none is used.
	Hash  string `json:"hash"`
	Error string `json:"error,omitempty"`
}

// ReadRepo lists the .md files directly in dir/.committree, in name order,
// and hashes the ones that will be used. It never follows a symbolic link
// and never fails: a problem becomes Error or a file's Ignored reason.
func ReadRepo(dir string) RepoInstructions {
	ri := RepoInstructions{Files: []RepoFile{}}
	root := filepath.Join(dir, Dir)
	info, err := os.Lstat(root)
	switch {
	case os.IsNotExist(err):
		return ri
	case err != nil:
		ri.Error = err.Error()
		return ri
	case info.Mode()&os.ModeSymlink != 0:
		ri.Error = Dir + " is a symbolic link"
		return ri
	case !info.IsDir():
		ri.Error = Dir + " is not a folder"
		return ri
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		ri.Error = err.Error()
		return ri
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	total := 0
	h := sha256.New()
	used := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".md") || e.IsDir() {
			continue
		}
		f := RepoFile{Name: name}
		switch {
		case e.Type()&os.ModeSymlink != 0:
			f.Ignored = "a symbolic link"
		case !e.Type().IsRegular():
			f.Ignored = "not a regular file"
		case name != SharedFile && !isAction(strings.TrimSuffix(name, ".md")):
			f.Ignored = "not used"
		}
		if f.Ignored == "" {
			data, err := os.ReadFile(filepath.Join(root, name))
			switch {
			case err != nil:
				f.Ignored = err.Error()
			case len(data) > MaxFile:
				f.Ignored = "larger than 16 KB"
			case !utf8.Valid(data):
				f.Ignored = "not UTF-8 text"
			case total+len(data) > MaxTotal:
				f.Ignored = "past the 32 KB total"
			default:
				total += len(data)
				f.Text = string(data)
				fmt.Fprintf(h, "%s\x00%s\x00", name, data)
				used++
			}
		}
		ri.Files = append(ri.Files, f)
	}
	if used > 0 {
		ri.Hash = "sha256:" + hex.EncodeToString(h.Sum(nil))
	}
	return ri
}

// Text returns the used texts for action: instructions.md, then
// <action>.md, skipping missing or ignored ones.
func (ri RepoInstructions) Text(action string) []string {
	var out []string
	for _, want := range []string{SharedFile, action + ".md"} {
		for _, f := range ri.Files {
			if f.Name == want && f.Ignored == "" && strings.TrimSpace(f.Text) != "" {
				out = append(out, strings.TrimSpace(f.Text))
			}
		}
	}
	return out
}

// StateOf compares the stored approval with the current hash.
func StateOf(approval, hash string) string {
	switch {
	case hash == "":
		return StateNone
	case approval == "":
		return StatePending
	case approval == hash:
		return StateApproved
	case approval == IgnoredPrefix+hash:
		return StateIgnored
	}
	return StateChanged
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ai/reposettings/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
/usr/bin/git add internal/ai/reposettings
/usr/bin/git commit -m "feat(reposettings): read .committree instructions with limits, hash and approval state"
```

---

### Task 3: Prompt assembly

**Files:**
- Create: `internal/ai/reposettings/assemble.go`
- Test: `internal/ai/reposettings/assemble_test.go`

**Interfaces:**
- Consumes: `RepoInstructions.Text` (Task 2), `AllActions` (Task 1).
- Produces: `func Assemble(base, action string, repo RepoInstructions, approved bool, private map[string]string) string`

- [ ] **Step 1: Write the failing test**

```go
package reposettings

import "testing"

func TestAssembleOrderAndHeadings(t *testing.T) {
	repo := RepoInstructions{Files: []RepoFile{
		{Name: "commit-message.md", Text: "Conventional Commits."},
		{Name: "instructions.md", Text: "We use Go."},
	}, Hash: "sha256:x"}
	private := map[string]string{"all": "Answer in Spanish.", "commit-message": "Mention the ticket.", "chat": "unused here"}
	got := Assemble("BASE", "commit-message", repo, true, private)
	want := "BASE\n\n## Project instructions\n\nWe use Go.\n\nConventional Commits.\n\n## Your instructions for this repository\n\nAnswer in Spanish.\n\nMention the ticket."
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestAssembleSkipsUnapprovedAndEmpty(t *testing.T) {
	repo := RepoInstructions{Files: []RepoFile{{Name: "instructions.md", Text: "We use Go."}}, Hash: "sha256:x"}
	if got := Assemble("BASE", "chat", repo, false, nil); got != "BASE" {
		t.Fatalf("unapproved repo text used: %q", got)
	}
	if got := Assemble("BASE", "chat", RepoInstructions{}, true, map[string]string{"all": "  "}); got != "BASE" {
		t.Fatalf("blank private text used: %q", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/ai/reposettings/ -run Assemble`
Expected: FAIL — undefined `Assemble`.

- [ ] **Step 3: Implement**

```go
package reposettings

import "strings"

const (
	projectHeading = "## Project instructions"
	privateHeading = "## Your instructions for this repository"
)

// Assemble appends to base the repository's approved instructions for
// action and then the user's private ones, each under its heading. Private
// instructions come last so they win over the repository's.
func Assemble(base, action string, repo RepoInstructions, approved bool, private map[string]string) string {
	parts := []string{base}
	if approved {
		if texts := repo.Text(action); len(texts) > 0 {
			parts = append(parts, projectHeading)
			parts = append(parts, texts...)
		}
	}
	var mine []string
	for _, k := range []string{AllActions, action} {
		if v := strings.TrimSpace(private[k]); v != "" {
			mine = append(mine, v)
		}
	}
	if len(mine) > 0 {
		parts = append(parts, privateHeading)
		parts = append(parts, mine...)
	}
	return strings.Join(parts, "\n\n")
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ai/reposettings/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
/usr/bin/git add internal/ai/reposettings
/usr/bin/git commit -m "feat(reposettings): assemble the prompt with project and private instructions"
```

---

### Task 4: App resolution — every AI entry point uses the repository's settings

**Files:**
- Create: `internal/app/repoai.go`
- Modify: `internal/app/ai.go` (`aiState` + `WithAI` ~45-97, `SendChat` ~322-360, `explainTask` ~548-570), `internal/app/merge.go` (~383-420), `internal/app/worktree.go` (~157-215), `internal/app/suggest.go` (`suggestReplies`, `generateReplies`), `internal/app/app.go` (`RemoveRepo` ~318)
- Modify: `docs/spec/06-ai.md` (new section, see Step 6)
- Test: `internal/app/repoai_test.go`

**Interfaces:**
- Consumes: Tasks 1–3.
- Produces (used by Task 5):
  - `var ErrAIOff = errors.New("AI is off for this repository")`
  - `func (a *App) repoAIStore() *reposettings.Store`
  - `func (a *App) settingsKey(id string) string` — a linked worktree's main repository ID, else `id`.
  - `func (a *App) aiSettingsFor(repoID string) (settings.Settings, reposettings.Override, error)` — `ErrAIOff` when off.
  - `func (a *App) systemPrompt(repo repos.Repo, o reposettings.Override, name string, v prompts.Vars, extra string) (string, error)`
  - `func repoNote(err error, overridden bool) error`
  - `aiState.commits map[string]context.CancelFunc` (repo ID → running commit message)

- [ ] **Step 1: Write the failing tests**

```go
package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"git-ui/internal/ai/keys"
	"git-ui/internal/ai/reposettings"
	"git-ui/internal/ai/settings"
)

func setRepoAI(t *testing.T, a *App, id string, o reposettings.Override) {
	t.Helper()
	if err := a.repoAIStore().Set(id, o); err != nil {
		t.Fatal(err)
	}
}

func TestAISettingsForMergesOverride(t *testing.T) {
	a, id, _ := newAIApp(t, "http://127.0.0.1:1")
	setRepoAI(t, a, id, reposettings.Override{TaskProvider: settings.ProviderOllama, TaskModel: "qwen3:14b"})
	cfg, _, err := a.aiSettingsFor(id)
	if err != nil || cfg.TaskModel != "qwen3:14b" || cfg.ChatModel != settings.DefaultModel {
		t.Fatalf("got %+v, %v", cfg, err)
	}
}

func TestAIOffRefusesEveryEntryPoint(t *testing.T) {
	a, id, _ := newAIApp(t, "http://127.0.0.1:1")
	setRepoAI(t, a, id, reposettings.Override{AIOff: true})
	checks := map[string]error{
		"chat":     a.SendChat(id, "hi", "run1"),
		"explain":  a.ExplainInChat(id, "HEAD", "", "run2"),
		"lines":    a.ExplainLinesInChat(id, "", "f.txt", 1, 1, "", "run3"),
		"commit":   a.GenerateCommitMessage(id, "run4"),
		"resolve":  a.ResolveConflicts(id, "run5"),
	}
	for name, err := range checks {
		if !errors.Is(err, ErrAIOff) {
			t.Errorf("%s: want ErrAIOff, got %v", name, err)
		}
	}
}

func TestCorruptRepoAIRefusesInsteadOfGlobal(t *testing.T) {
	a, id, _ := newAIApp(t, "http://127.0.0.1:1")
	_ = os.WriteFile(reposettings.PathNextTo(a.ai.deps.SettingsPath), []byte("{oops"), 0o644)
	_, _, err := a.aiSettingsFor(id)
	if !errors.Is(err, reposettings.ErrUnreadable) {
		t.Fatalf("got %v", err)
	}
	if err := a.SendChat(id, "hi", "run1"); !errors.Is(err, reposettings.ErrUnreadable) {
		t.Fatalf("SendChat: %v", err)
	}
}

func TestChatUsesRepoModelAndInstructions(t *testing.T) {
	var mu sync.Mutex
	var model, system string
	srv := fakeOllama(t, func(req map[string]any) {
		mu.Lock()
		defer mu.Unlock()
		model = req["model"].(string)
		system = req["messages"].([]any)[0].(map[string]any)["content"].(string)
	})
	a, id, ev := newAIApp(t, srv.URL)
	setRepoAI(t, a, id, reposettings.Override{ChatProvider: settings.ProviderOllama, ChatModel: "qwen2.5:7b",
		Instructions: map[string]string{"chat": "Answer in Spanish."}})
	repo, _ := a.repo(id)
	_ = os.MkdirAll(filepath.Join(repo.Path, ".committree"), 0o755)
	_ = os.WriteFile(filepath.Join(repo.Path, ".committree", "instructions.md"), []byte("Project rule."), 0o644)
	if err := a.SendChat(id, "hola", "run1"); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, "chat:done")
	mu.Lock()
	defer mu.Unlock()
	if model != "qwen2.5:7b" {
		t.Fatalf("model %q", model)
	}
	if !strings.Contains(system, "## Your instructions for this repository\n\nAnswer in Spanish.") {
		t.Fatalf("private instructions missing:\n%s", system)
	}
	if strings.Contains(system, "Project rule.") {
		t.Fatal("unapproved repository instructions were used")
	}
}

func TestMissingKeyNamesRepoSettings(t *testing.T) {
	a, id, _ := newAIApp(t, "http://127.0.0.1:1")
	restore := keys.UseStore(&fakeKeys{items: map[string]string{}}) // no key stored (helper in ai_test.go)
	defer restore()
	setRepoAI(t, a, id, reposettings.Override{ChatProvider: settings.ProviderAnthropic, ChatModel: "claude-opus-5"})
	err := a.SendChat(id, "hi", "run1")
	if err == nil || !strings.HasSuffix(err.Error(), "(set in this repository's settings)") {
		t.Fatalf("got %v", err)
	}
}

func TestRemoveRepoDropsItsEntry(t *testing.T) {
	a, id, _ := newAIApp(t, "http://127.0.0.1:1")
	setRepoAI(t, a, id, reposettings.Override{AIOff: true})
	if err := a.RemoveRepo(id); err != nil {
		t.Fatal(err)
	}
	if o, _ := a.repoAIStore().Get(id); !o.IsEmpty() {
		t.Fatalf("entry left: %+v", o)
	}
}

func TestWorktreeUsesMainRepoSettings(t *testing.T) {
	a, id, _ := newAIApp(t, "http://127.0.0.1:1")
	repo, _ := a.repo(id)
	wt := filepath.Join(t.TempDir(), "wt")
	gitC(t, repo.Path, "worktree", "add", "-q", "-b", "wt-branch", wt) // helper used in submodules_test.go
	a.ListRepos() // detects the worktree
	var wtID string
	for _, it := range a.ListRepos() {
		if it.Worktree {
			wtID = it.ID
		}
	}
	if wtID == "" {
		t.Fatal("worktree not detected")
	}
	setRepoAI(t, a, id, reposettings.Override{AIOff: true})
	if _, _, err := a.aiSettingsFor(wtID); !errors.Is(err, ErrAIOff) {
		t.Fatalf("worktree: %v", err)
	}
}
```

`fakeKeys` and `keys.UseStore` already exist (`internal/app/ai_test.go` ~608); `gitC(t, dir, args...)` is the git helper used in `internal/app/submodules_test.go`. Check `ListRepos` items expose `Worktree` and `ID` as used (`RepoItem` in `internal/app/app.go`).

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/app/ -run 'AISettingsFor|AIOff|CorruptRepoAI|ChatUsesRepo|MissingKey|RemoveRepoDrops|WorktreeUsesMain'`
Expected: FAIL — undefined `repoAIStore`, `aiSettingsFor`, `ErrAIOff`.

- [ ] **Step 3: Implement `internal/app/repoai.go`**

```go
package app

import (
	"errors"
	"fmt"

	"git-ui/internal/ai/prompts"
	"git-ui/internal/ai/reposettings"
	"git-ui/internal/ai/settings"
	"git-ui/internal/repos"
)

var ErrAIOff = errors.New("AI is off for this repository")

func (a *App) repoAIStore() *reposettings.Store {
	a.ai.mu.Lock()
	defer a.ai.mu.Unlock()
	if a.ai.repoAI == nil {
		a.ai.repoAI = reposettings.New(reposettings.PathNextTo(a.ai.deps.SettingsPath))
	}
	return a.ai.repoAI
}

// settingsKey is the repository whose private AI settings apply to id: a
// linked worktree uses its main repository's.
func (a *App) settingsKey(id string) string {
	a.wtMu.Lock()
	defer a.wtMu.Unlock()
	if parent, ok := a.wtParent[id]; ok {
		return parent
	}
	return id
}

// aiSettingsFor is the global settings with repoID's overrides on top. An
// unreadable repo-ai.json is an error, never the global settings: a
// repository that was off or limited to the local model must not silently
// use another provider.
func (a *App) aiSettingsFor(repoID string) (settings.Settings, reposettings.Override, error) {
	if a.ai == nil {
		return settings.Settings{}, reposettings.Override{}, ErrAIDisabled
	}
	g, err := settings.Load(a.ai.deps.SettingsPath)
	if err != nil {
		return settings.Settings{}, reposettings.Override{}, err
	}
	o, err := a.repoAIStore().Get(a.settingsKey(repoID))
	if err != nil {
		return settings.Settings{}, reposettings.Override{}, err
	}
	if o.AIOff {
		return settings.Settings{}, o, ErrAIOff
	}
	return reposettings.Merge(g, o), o, nil
}

// systemPrompt is the named prompt with extra (e.g. merge-kind guidance)
// and then the repository's approved and private instructions.
func (a *App) systemPrompt(repo repos.Repo, o reposettings.Override, name string, v prompts.Vars, extra string) (string, error) {
	base, err := a.ai.deps.Prompts.Get(name, v)
	if err != nil {
		return "", err
	}
	if extra != "" {
		base += "\n\n" + extra
	}
	ri := reposettings.ReadRepo(repo.Path)
	approved := reposettings.StateOf(o.Approval, ri.Hash) == reposettings.StateApproved
	return reposettings.Assemble(base, name, ri, approved, o.Instructions), nil
}

// repoNote marks an error caused by a repository override so the user
// knows where to fix it.
func repoNote(err error, overridden bool) error {
	if err == nil || !overridden {
		return err
	}
	return fmt.Errorf("%w (set in this repository's settings)", err)
}
```

Add to `aiState` in `ai.go`:

```go
	// repoAI is the per-repository settings store, created on first use.
	repoAI *reposettings.Store
	// commits holds, per repo ID, the running commit message generation.
	commits map[string]context.CancelFunc
```

and in `WithAI` initialise `commits: map[string]context.CancelFunc{}`. Add the `reposettings` import.

- [ ] **Step 4: Switch the entry points**

`SendChat` (ai.go): replace

```go
	cfg, err := a.aiSettings()
	if err != nil {
		return err
	}
	provider, err := a.chatProvider(cfg)
	if err != nil {
		return err
	}
```

with

```go
	cfg, o, err := a.aiSettingsFor(repoID)
	if err != nil {
		return err
	}
	provider, err := a.chatProvider(cfg)
	if err != nil {
		return repoNote(err, o.ChatProvider != "")
	}
```

and replace the `a.ai.deps.Prompts.Get(prompts.Chat, prompts.Vars{…})` call with
`a.systemPrompt(repo, o, prompts.Chat, prompts.Vars{…same fields…}, "")`.
Change `a.suggestReplies(repoID, runID, cfg)` to `a.suggestReplies(repoID, runID, cfg, o)`.

`explainTask` (ai.go): replace `cfg, err := a.aiSettings()` with `cfg, o, err := a.aiSettingsFor(repoID)`; wrap the `responderFor` error as `repoNote(err, o.TaskProvider != "")`; replace the `Prompts.Get(promptName, …)` call with `a.systemPrompt(repo, o, promptName, prompts.Vars{…}, "")`.

**Order matters:** in `GenerateCommitMessage` and `ResolveConflicts`, call `a.aiSettingsFor` right after the repository is resolved, *before* the "nothing staged" and "not merging" checks, so an off repository answers `ErrAIOff` first (the test calls them on a repository that is neither staged nor merging). Move the existing `cfg, err := a.aiSettings()` line up accordingly.

`ResolveConflicts` (merge.go): `cfg, o, err := a.aiSettingsFor(repoID)`; `repoNote(err, o.ChatProvider != "")` on the provider error; replace the prompt block with

```go
	if err == nil {
		system, err = a.systemPrompt(repo, o, prompts.ResolveConflicts, prompts.Vars{
			Repo: repo.Name, Path: repo.Path, Branch: st.Into, Date: time.Now().Format("2006-01-02"),
		}, kindGuidance(st.Kind))
	}
```

`GenerateCommitMessage` (worktree.go): `cfg, o, err := a.aiSettingsFor(id)`; `repoNote(err, o.TaskProvider != "")`; `a.systemPrompt(repo, o, prompts.CommitMessage, prompts.Vars{Repo: repo.Name, Path: dir, …}, "")`. Make it cancellable:

```go
	ctx, cancel := context.WithCancel(a.ctx)
	a.ai.mu.Lock()
	if old, ok := a.ai.commits[id]; ok {
		old()
	}
	a.ai.commits[id] = cancel
	a.ai.mu.Unlock()
	go func() {
		defer func() {
			a.ai.mu.Lock()
			delete(a.ai.commits, id)
			a.ai.mu.Unlock()
			cancel()
		}()
		stream, err := responder.Respond(ctx, instructions, prompt)
		// … unchanged, but when ctx.Err() != nil set done.Error = ErrAIOff.Error()
```

(the only canceller is turning the AI off, Task 5). Note: the `delete` must only remove its own entry — compare by storing a pointer, as `suggestReplies` does with `pendingSuggestion`, if a second generation can start before the first ends.

`suggest.go`: `suggestReplies(repoID, runID string, cfg settings.Settings, o reposettings.Override)` passes `o` to `generateReplies(ctx, repoID, cfg, o)`, which replaces `Prompts.Get(prompts.SuggestReplies, …)` with `a.systemPrompt(repo, o, prompts.SuggestReplies, prompts.Vars{…}, "")`.

`RemoveRepo` (app.go), before `return a.store.Remove(id)`:

```go
	if a.ai != nil {
		if err := a.repoAIStore().Remove(id); err != nil && !errors.Is(err, reposettings.ErrUnreadable) {
			return err
		}
	}
```

(an unreadable file is left for the user to see in the AI tab; the repository is still removed).

Delete `aiSettings()` only if no caller remains; `GetAISettings`, `AIStatus`, `ListModels` keep reading the global file via `settings.Load(a.ai.deps.SettingsPath)` — keep `aiSettings()` for them.

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/app/ ./internal/ai/...`
Expected: PASS (all existing tests too).

- [ ] **Step 6: Spec**

In `docs/spec/06-ai.md`, add after "### What happens with no key or no model configured" a section:

```markdown
### Settings per repository

A repository can override, for itself only, the chat and task provider and
model, the commit message and suggested replies modes, and turn the AI off.
These overrides are private to this computer, stored by repository; an
empty value uses the global one. A linked worktree uses its main
repository's overrides. Removing a repository from the list removes its
overrides; adding it again starts from the global settings.

When the AI is off for a repository, every AI action there is refused with
"AI is off for this repository" and automatic ones are skipped. If the
file holding the overrides cannot be read, every AI action in every
repository is refused with "Per-repository AI settings can't be read: …" —
never run with the global settings instead. An error caused by an override
(a hosted provider with no key, a local model not installed) ends with
"(set in this repository's settings)".

Instructions are appended to each action's prompt: first the repository's
shared ones under "Project instructions", then the user's private ones
under "Your instructions for this repository". Shared instructions are the
`.md` files directly in the repository's `.committree/` folder:
`instructions.md` for every action and `<action>.md` for one
(`chat`, `commit-message`, `resolve-conflicts`, `explain-commit`,
`explain-lines`, `suggest-replies`). Symbolic links, non-UTF-8 files, files
over 16 KB and anything past 32 KB in total are ignored. They are used only
after the user approves exactly these files; any change asks again.
```

- [ ] **Step 7: Commit**

```bash
/usr/bin/git add internal/app docs/spec/06-ai.md
/usr/bin/git commit -m "feat(ai): AI actions use the repository's settings and instructions; AI off per repository"
```

---

### Task 5: App bindings — read, save, approve, ignore; turning off stops running work

**Files:**
- Modify: `internal/app/repoai.go`
- Test: `internal/app/repoai_test.go`
- Regenerate: `frontend/wailsjs/go/app/App.{js,d.ts}`, `frontend/wailsjs/go/models.ts`

**Interfaces:**
- Consumes: Task 4.
- Produces (the frontend binds these):
  - `type RepoAIInfo struct { Effective settings.Settings \`json:"effective"\`; Global settings.Settings \`json:"global"\`; Overrides reposettings.Override \`json:"overrides"\`; AIOff bool \`json:"aiOff"\`; RepoInstructions reposettings.RepoInstructions \`json:"repoInstructions"\`; State string \`json:"state"\`; Error string \`json:"error,omitempty"\` }`
  - `func (a *App) GetRepoAISettings(repoID string) (RepoAIInfo, error)`
  - `func (a *App) SaveRepoAISettings(repoID string, o reposettings.Override) error`
  - `func (a *App) ApproveRepoInstructions(repoID, hash string) error`
  - `func (a *App) IgnoreRepoInstructions(repoID, hash string) error`
  - `const EventRepoAIChanged = "repo-ai:changed"`; payload `RepoAIChangedEvent{RepoID string \`json:"repoID"\`}`
  - `var ErrInstructionsChanged = errors.New("the repository's instructions changed; review them again")`

- [ ] **Step 1: Write the failing tests**

```go
func TestGetRepoAISettings(t *testing.T) {
	a, id, _ := newAIApp(t, "http://127.0.0.1:1")
	repo, _ := a.repo(id)
	_ = os.MkdirAll(filepath.Join(repo.Path, ".committree"), 0o755)
	_ = os.WriteFile(filepath.Join(repo.Path, ".committree", "instructions.md"), []byte("Rule."), 0o644)
	if err := a.SaveRepoAISettings(id, reposettings.Override{SuggestReplies: settings.SuggestAuto}); err != nil {
		t.Fatal(err)
	}
	info, err := a.GetRepoAISettings(id)
	if err != nil {
		t.Fatal(err)
	}
	if info.Effective.SuggestReplies != settings.SuggestAuto || info.Global.SuggestReplies != settings.SuggestOff ||
		info.State != reposettings.StatePending || len(info.RepoInstructions.Files) != 1 || info.AIOff {
		t.Fatalf("got %+v", info)
	}
}

func TestGetRepoAISettingsCorruptIsOffWithError(t *testing.T) {
	a, id, _ := newAIApp(t, "http://127.0.0.1:1")
	_ = os.WriteFile(reposettings.PathNextTo(a.ai.deps.SettingsPath), []byte("{oops"), 0o644)
	info, err := a.GetRepoAISettings(id)
	if err != nil || !info.AIOff || !strings.HasPrefix(info.Error, "Per-repository AI settings can't be read") {
		t.Fatalf("got %+v, %v", info, err)
	}
}

func TestApproveAndIgnore(t *testing.T) {
	a, id, ev := newAIApp(t, "http://127.0.0.1:1")
	repo, _ := a.repo(id)
	_ = os.MkdirAll(filepath.Join(repo.Path, ".committree"), 0o755)
	_ = os.WriteFile(filepath.Join(repo.Path, ".committree", "instructions.md"), []byte("Rule."), 0o644)
	info, _ := a.GetRepoAISettings(id)
	if err := a.IgnoreRepoInstructions(id, info.RepoInstructions.Hash); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.GetRepoAISettings(id); got.State != reposettings.StateIgnored {
		t.Fatalf("state %s", got.State)
	}
	if err := a.ApproveRepoInstructions(id, info.RepoInstructions.Hash); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.GetRepoAISettings(id); got.State != reposettings.StateApproved {
		t.Fatalf("state %s", got.State)
	}
	ev.wait(t, EventRepoAIChanged)
}

func TestApproveRefusesStaleHash(t *testing.T) {
	a, id, _ := newAIApp(t, "http://127.0.0.1:1")
	repo, _ := a.repo(id)
	_ = os.MkdirAll(filepath.Join(repo.Path, ".committree"), 0o755)
	p := filepath.Join(repo.Path, ".committree", "instructions.md")
	_ = os.WriteFile(p, []byte("Seen."), 0o644)
	seen, _ := a.GetRepoAISettings(id)
	_ = os.WriteFile(p, []byte("Changed after."), 0o644)
	if err := a.ApproveRepoInstructions(id, seen.RepoInstructions.Hash); !errors.Is(err, ErrInstructionsChanged) {
		t.Fatalf("got %v", err)
	}
	if got, _ := a.GetRepoAISettings(id); got.State != reposettings.StatePending {
		t.Fatalf("state %s", got.State)
	}
}

func TestSaveRepoAISettingsRejectsInvalid(t *testing.T) {
	a, id, _ := newAIApp(t, "http://127.0.0.1:1")
	if err := a.SaveRepoAISettings(id, reposettings.Override{ChatProvider: settings.ProviderOllama}); !errors.Is(err, reposettings.ErrInvalid) {
		t.Fatalf("got %v", err)
	}
}

func TestTurningAIOffCancelsRunning(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			fmt.Fprint(w, `{"models":[{"name":"qwen2.5:7b"}]}`)
			return
		}
		writeLines(w, `{"message":{"role":"assistant","content":"partial"},"done":false}`)
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	a, id, ev := newAIApp(t, srv.URL)
	if err := a.SendChat(id, "hi", "run1"); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, "chat:delta")
	if err := a.SaveRepoAISettings(id, reposettings.Override{AIOff: true}); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, "chat:done") // a stopped answer ends with done (context.Canceled)
	if a.aiBusy(id) {
		t.Fatal("chat still running")
	}
}
```

Check the exact event names used by `agent` (`agent.EventStart`, `EventDone`, the delta event) in `internal/ai/agent` and use those constants rather than the literal strings above.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/app/ -run 'GetRepoAISettings|ApproveAndIgnore|ApproveRefuses|SaveRepoAISettings|TurningAIOff'`
Expected: FAIL — undefined `GetRepoAISettings`.

- [ ] **Step 3: Implement (append to `repoai.go`)**

```go
const EventRepoAIChanged = "repo-ai:changed"

type RepoAIChangedEvent struct {
	RepoID string `json:"repoID"`
}

var ErrInstructionsChanged = errors.New("the repository's instructions changed; review them again")

type RepoAIInfo struct {
	Effective        settings.Settings             `json:"effective"`
	Global           settings.Settings             `json:"global"`
	Overrides        reposettings.Override         `json:"overrides"`
	AIOff            bool                          `json:"aiOff"`
	RepoInstructions reposettings.RepoInstructions `json:"repoInstructions"`
	State            string                        `json:"state"`
	Error            string                        `json:"error,omitempty"`
}

// GetRepoAISettings is what the AI tab and the per-repository stores show.
// An unreadable repo-ai.json is reported in Error with AIOff set, so the
// interface hides the AI rather than offer actions that will be refused.
func (a *App) GetRepoAISettings(repoID string) (RepoAIInfo, error) {
	if a.ai == nil {
		return RepoAIInfo{}, ErrAIDisabled
	}
	repo, ok := a.repo(repoID)
	if !ok {
		return RepoAIInfo{}, repos.ErrUnknownRepo
	}
	g, err := settings.Load(a.ai.deps.SettingsPath)
	if err != nil {
		return RepoAIInfo{}, err
	}
	info := RepoAIInfo{Global: g, Effective: g, RepoInstructions: reposettings.ReadRepo(repo.Path)}
	o, err := a.repoAIStore().Get(a.settingsKey(repoID))
	if err != nil {
		info.AIOff, info.Error = true, err.Error()
		info.State = reposettings.StateNone
		return info, nil
	}
	if o.Instructions == nil {
		o.Instructions = map[string]string{}
	}
	info.Overrides, info.AIOff = o, o.AIOff
	info.Effective = reposettings.Merge(g, o)
	info.State = reposettings.StateOf(o.Approval, info.RepoInstructions.Hash)
	return info, nil
}

// SaveRepoAISettings replaces the repository's overrides. Turning the AI
// off stops what is running there.
func (a *App) SaveRepoAISettings(repoID string, o reposettings.Override) error {
	if a.ai == nil {
		return ErrAIDisabled
	}
	if _, ok := a.repo(repoID); !ok {
		return repos.ErrUnknownRepo
	}
	if err := a.repoAIStore().Set(a.settingsKey(repoID), o); err != nil {
		return err
	}
	if o.AIOff {
		a.stopRepoAI(repoID)
	}
	a.emit(EventRepoAIChanged, RepoAIChangedEvent{RepoID: repoID})
	return nil
}

func (a *App) ApproveRepoInstructions(repoID, hash string) error {
	return a.setApproval(repoID, hash, hash)
}

func (a *App) IgnoreRepoInstructions(repoID, hash string) error {
	return a.setApproval(repoID, hash, reposettings.IgnoredPrefix+hash)
}

// setApproval stores value only if the files still hash to seen, the hash
// the user was shown: an edit while the tab was open is never approved.
func (a *App) setApproval(repoID, seen, value string) error {
	if a.ai == nil {
		return ErrAIDisabled
	}
	repo, ok := a.repo(repoID)
	if !ok {
		return repos.ErrUnknownRepo
	}
	if seen == "" || reposettings.ReadRepo(repo.Path).Hash != seen {
		return ErrInstructionsChanged
	}
	if err := a.repoAIStore().SetApproval(a.settingsKey(repoID), value); err != nil {
		return err
	}
	a.emit(EventRepoAIChanged, RepoAIChangedEvent{RepoID: repoID})
	return nil
}

// stopRepoAI cancels the chat answer, explanation or conflict resolution,
// the commit message and the pending suggestions of repoID. A main
// repository's switch also stops its linked worktrees.
func (a *App) stopRepoAI(repoID string) {
	ids := []string{repoID}
	a.wtMu.Lock()
	for wt, parent := range a.wtParent {
		if parent == repoID {
			ids = append(ids, wt)
		}
	}
	a.wtMu.Unlock()
	for _, id := range ids {
		_ = a.StopChat(id)
		a.cancelSuggestions(id)
		a.ai.mu.Lock()
		if cancel, ok := a.ai.commits[id]; ok {
			cancel()
		}
		a.ai.mu.Unlock()
	}
}
```

- [ ] **Step 4: Run tests and regenerate bindings**

Run: `go test ./internal/app/ ./internal/ai/...` — Expected: PASS.
Then: `source ~/.nvm/nvm.sh && nvm use 22 && ~/go/bin/wails generate module && /usr/bin/git checkout -- frontend/wailsjs/runtime`
Check `frontend/wailsjs/go/app/App.d.ts` now has `GetRepoAISettings`, `SaveRepoAISettings`, `ApproveRepoInstructions`, `IgnoreRepoInstructions`.

- [ ] **Step 5: Commit**

```bash
/usr/bin/git add internal/app frontend/wailsjs/go
/usr/bin/git commit -m "feat(ai): bindings to read and save a repository's AI settings and approve its instructions"
```

---

### Task 6: Frontend data — types, API, per-repository store, blocker by model

**Files:**
- Create: `frontend/src/lib/repoAI.ts`, `frontend/src/lib/repoAI.test.ts`
- Modify: `frontend/src/lib/types.ts`, `frontend/src/lib/api.ts`, `frontend/src/lib/stores.ts` (~157, ~487), `frontend/src/lib/providers.ts` (`chatBlocker` ~85), `frontend/src/lib/providers.test.ts`, `frontend/src/App.svelte` (~82), `frontend/src/components/ChatPanel.svelte` (~55)

**Interfaces:**
- Consumes: Task 5 bindings.
- Produces:
  - types `RepoAIOverride`, `RepoInstructionFile`, `RepoInstructions`, `RepoAIState`, `RepoAIInfo`
  - `api.getRepoAISettings(id)`, `api.saveRepoAISettings(id, o)`, `api.approveRepoInstructions(id, hash)`, `api.ignoreRepoInstructions(id, hash)`
  - store `repoAI: Writable<RepoAIInfo | null>`, derived `aiOff: Readable<boolean>`; `loadAISettings()` fills both for the selected repository
  - `repoSettings` store type becomes `{ repoID: string; tab?: 'remotes' | 'ai' } | null`
  - `chatBlocker(provider, status, model?)`
  - `repoAI.ts`: `ACTIONS`, `actionLabel(name)`, `globalLabel(provider, model)`, `withOverride(o, patch)`, `stateText(state)`, `usesRepoChatModel(info)`

- [ ] **Step 1: Write the failing tests** (`frontend/src/lib/repoAI.test.ts`)

```ts
import { describe, expect, it } from 'vitest'
import { ACTIONS, actionLabel, globalLabel, stateText, usesRepoChatModel, withOverride } from './repoAI'

describe('repoAI helpers', () => {
  it('lists the six actions with labels', () => {
    expect(ACTIONS).toEqual(['chat', 'commit-message', 'resolve-conflicts', 'explain-commit', 'explain-lines', 'suggest-replies'])
    expect(actionLabel('commit-message')).toBe('Commit message')
  })
  it('labels the inherited value', () => {
    expect(globalLabel('ollama', 'qwen2.5:7b')).toBe('Global (qwen2.5:7b · Ollama)')
  })
  it('clears a model when its provider goes back to global', () => {
    const o = withOverride({ chatProvider: 'anthropic', chatModel: 'claude-opus-5' }, { chatProvider: '' })
    expect(o.chatProvider).toBe('')
    expect(o.chatModel).toBe('')
  })
  it('drops blank instructions', () => {
    const o = withOverride({}, { instructions: { all: '  ', chat: 'Be brief.' } })
    expect(o.instructions).toEqual({ chat: 'Be brief.' })
  })
  it('describes each approval state', () => {
    expect(stateText('pending')).toBe('Not approved')
    expect(stateText('changed')).toBe('Changed since you approved')
    expect(stateText('approved')).toBe('Approved')
    expect(stateText('ignored')).toBe('Ignored')
  })
  it('knows when the chat model is the repository’s', () => {
    expect(usesRepoChatModel({ overrides: { chatProvider: 'ollama', chatModel: 'x' } } as any)).toBe(true)
    expect(usesRepoChatModel({ overrides: {} } as any)).toBe(false)
    expect(usesRepoChatModel(null)).toBe(false)
  })
})
```

Add to `providers.test.ts` inside `describe('chatBlocker', …)`:

```ts
  it('checks the given model, not the global one', () => {
    const s = status()
    s.ollama.models = [{ name: 'qwen3:14b' } as any]
    s.ollama.chatModelInstalled = true
    expect(chatBlocker('ollama', s, 'qwen2.5:7b')).toEqual({ kind: 'model_missing', model: 'qwen2.5:7b' })
    expect(chatBlocker('ollama', s, 'qwen3')).toEqual({ kind: 'model_missing', model: 'qwen3' })
    expect(chatBlocker('ollama', s, 'qwen3:14b')).toBeNull()
  })
```

(Use the file's existing `status()` helper; if its models entries need more fields, copy the shape it already builds.)

- [ ] **Step 2: Run to verify they fail**

Run: `cd frontend && npx vitest run src/lib/repoAI.test.ts src/lib/providers.test.ts`
Expected: FAIL — cannot find module `./repoAI`; chatBlocker ignores the third argument.

- [ ] **Step 3: Implement**

`types.ts` (append):

```ts
export interface RepoAIOverride {
  aiOff?: boolean
  chatProvider?: ProviderName | ''
  chatModel?: string
  taskProvider?: ProviderName | ''
  taskModel?: string
  commitMessage?: string
  suggestReplies?: string
  instructions?: Record<string, string>
}
export interface RepoInstructionFile { name: string; text: string; ignored?: string }
export interface RepoInstructions { files: RepoInstructionFile[]; hash: string; error?: string }
export type RepoAIState = 'none' | 'approved' | 'pending' | 'ignored' | 'changed'
export interface RepoAIInfo {
  effective: AISettings
  global: AISettings
  overrides: RepoAIOverride
  aiOff: boolean
  repoInstructions: RepoInstructions
  state: RepoAIState
  error?: string
}
```

`api.ts` (next to `getAISettings`):

```ts
  getRepoAISettings: (id: string) => call<RepoAIInfo>(Go.GetRepoAISettings(id)),
  saveRepoAISettings: (id: string, o: RepoAIOverride) => call<void>(Go.SaveRepoAISettings(id, o as any)),
  approveRepoInstructions: (id: string, hash: string) => call<void>(Go.ApproveRepoInstructions(id, hash)),
  ignoreRepoInstructions: (id: string, hash: string) => call<void>(Go.IgnoreRepoInstructions(id, hash)),
```

(add `RepoAIInfo, RepoAIOverride` to the type import).

`lib/repoAI.ts`:

```ts
import { providerShortLabel } from './providers'
import type { ProviderName, RepoAIInfo, RepoAIOverride, RepoAIState } from './types'

export const ACTIONS = ['chat', 'commit-message', 'resolve-conflicts', 'explain-commit', 'explain-lines', 'suggest-replies'] as const
export type Action = (typeof ACTIONS)[number]

const LABELS: Record<Action, string> = {
  chat: 'Chat',
  'commit-message': 'Commit message',
  'resolve-conflicts': 'Resolve conflicts',
  'explain-commit': 'Explain commit',
  'explain-lines': 'Explain lines',
  'suggest-replies': 'Suggested replies',
}

export const actionLabel = (a: string) => LABELS[a as Action] ?? a

export const globalLabel = (provider: ProviderName, model: string) => `Global (${model} · ${providerShortLabel(provider)})`

/** withOverride applies patch to o: a provider set back to Global ('')
 *  clears its model, and blank instruction texts are dropped. */
export function withOverride(o: RepoAIOverride, patch: Partial<RepoAIOverride>): RepoAIOverride {
  const next: RepoAIOverride = { ...o, ...patch }
  if (patch.chatProvider === '') next.chatModel = ''
  if (patch.taskProvider === '') next.taskModel = ''
  if (next.instructions) {
    next.instructions = Object.fromEntries(Object.entries(next.instructions).filter(([, v]) => v.trim() !== ''))
  }
  return next
}

const STATES: Record<RepoAIState, string> = {
  none: '',
  approved: 'Approved',
  pending: 'Not approved',
  ignored: 'Ignored',
  changed: 'Changed since you approved',
}
export const stateText = (s: RepoAIState) => STATES[s]

export const usesRepoChatModel = (info: Pick<RepoAIInfo, 'overrides'> | null) => !!info?.overrides.chatProvider
```

`providers.ts` — `chatBlocker`:

```ts
export function chatBlocker(chatProvider: ProviderName, status: AIStatus, chatModel?: string): ChatBlocker | null {
  if (!needsKey(chatProvider)) {
    if (!status.ollama.running) return { kind: 'ollama_down' }
    if (chatModel === undefined) {
      if (!status.ollama.chatModelInstalled) return { kind: 'model_missing', model: status.ollama.chatModel }
      return null
    }
    const installed = status.ollama.models.some((m) => m.name === chatModel || m.name === chatModel + ':latest')
    return installed ? null : { kind: 'model_missing', model: chatModel }
  }
  const found = status.providers.find((s) => s.provider === chatProvider)
  if (found?.hasKey) return null
  return { kind: 'no_key', message: found?.error || `Add an API key for ${providerShortLabel(chatProvider)} in Settings.` }
}
```

`ChatPanel.svelte` ~55: `chatBlocker($aiSettings?.chatProvider ?? 'ollama', status, $aiSettings?.chatModel)`.

`stores.ts`: change `repoSettings` to `writable<{ repoID: string; tab?: 'remotes' | 'ai' } | null>(null)`; add

```ts
export const repoAI = writable<RepoAIInfo | null>(null)
/** The selected repository has the AI off (or its settings can't be read). */
export const aiOff = derived(repoAI, (r) => !!r?.aiOff)
```

and replace `loadAISettings`:

```ts
/** Loads the AI settings in effect: the selected repository's (global with
 *  its overrides) when one is selected, the global ones otherwise. */
export async function loadAISettings() {
  const id = get(selectedRepoId)
  try {
    if (id) {
      const info = await api.getRepoAISettings(id)
      if (get(selectedRepoId) !== id) return
      repoAI.set(info)
      aiSettings.set(info.effective)
    } else {
      repoAI.set(null)
      aiSettings.set(await api.getAISettings())
    }
  } catch {
    // Left as whatever was last loaded (or null) — the commit box treats a
    // null store as "don't auto-generate" rather than erroring.
  }
}
```

(import `derived`, `get` if not already; check `selectedRepoId` is declared above `loadAISettings` — it is, at ~104).

`App.svelte` `onMount`: replace `loadAISettings()` with

```ts
    const offSelected = selectedRepoId.subscribe(() => loadAISettings())
    const offRepoAI = EventsOn('repo-ai:changed', () => loadAISettings())
```

and call `offSelected()` and `offRepoAI()` in the existing cleanup that `onMount` returns.

- [ ] **Step 4: Run tests**

Run: `cd frontend && npm test && npm run check`
Expected: PASS, 0 errors.

- [ ] **Step 5: Commit**

```bash
/usr/bin/git add frontend/src
/usr/bin/git commit -m "feat(ai): frontend loads the selected repository's AI settings"
```

---

### Task 7: Repository settings → AI tab

**Files:**
- Create: `frontend/src/components/RepoAITab.svelte`
- Modify: `frontend/src/components/RepoSettingsDialog.svelte` (tabs nav ~80-88, pane), `docs/spec/12-repository-settings.md`

**Interfaces:**
- Consumes: Task 6 (`api.*RepoAI*`, `repoAI.ts`, types), `api.getAIStatus()` / `api.listModels(provider)` and `PROVIDERS`, `settingsHaveModels`, `modelForProvider` from `lib/providers.ts` (read their signatures there before use; the global Settings → AI models form in `SettingsDialog.svelte` ~400-460 shows how they are used).
- Produces: `<RepoAITab repoID={…} />`.

- [ ] **Step 1: Tabs in the dialog**

In `RepoSettingsDialog.svelte`:

```ts
  import RepoAITab from './RepoAITab.svelte'
  $: tab = $repoSettings?.tab ?? 'remotes'
  const setTab = (t: 'remotes' | 'ai') => repoSettings.update((s) => (s ? { ...s, tab: t } : s))
```

Replace the single tab button with:

```svelte
        <button class="tab" class:active={tab === 'remotes'} aria-current={tab === 'remotes' ? 'page' : undefined} on:click={() => setTab('remotes')}>Remotes</button>
        <button class="tab" class:active={tab === 'ai'} aria-current={tab === 'ai' ? 'page' : undefined} on:click={() => setTab('ai')}>AI</button>
```

The pane header shows `{tab === 'ai' ? 'AI' : 'Remotes'}`; wrap the current `.content` body in `{#if tab === 'remotes'} … {:else} <RepoAITab {repoID} /> {/if}`. Keep `reset()` for remotes; it still runs on every opening.

- [ ] **Step 2: `RepoAITab.svelte`**

```svelte
<script lang="ts">
  import { api } from '../lib/api'
  import { ACTIONS, actionLabel, globalLabel, stateText, withOverride } from '../lib/repoAI'
  import { PROVIDERS } from '../lib/providers'
  import type { AIStatus, ProviderName, RepoAIInfo, RepoAIOverride } from '../lib/types'
  import { errorMessage } from '../lib/ui'

  export let repoID: string

  let info: RepoAIInfo | null = null
  let status: AIStatus | null = null
  let models: Partial<Record<ProviderName, string[]>> = {}
  let error = ''
  let adding = ''
  let open: Record<string, boolean> = {}

  $: load(repoID)
  $: o = info?.overrides ?? {}
  $: g = info?.global
  $: off = !!info?.aiOff
  $: shownActions = ACTIONS.filter((a) => o.instructions?.[a] !== undefined)

  async function load(id: string) {
    error = ''
    try {
      info = await api.getRepoAISettings(id)
      if (info.error) error = info.error
      status = await api.getAIStatus()
      for (const { value } of PROVIDERS) {
        api.listModels(value).then((l) => (models = { ...models, [value]: l })).catch(() => {})
      }
    } catch (e) {
      error = errorMessage(e)
    }
  }

  async function save(patch: Partial<RepoAIOverride>) {
    if (!info) return
    const next = withOverride(o, patch)
    // A provider chosen without a model takes the first one listed.
    for (const role of ['chat', 'task'] as const) {
      const p = next[`${role}Provider`] as ProviderName | ''
      if (p && !next[`${role}Model`]) next[`${role}Model`] = models[p]?.[0] ?? ''
    }
    try {
      await api.saveRepoAISettings(repoID, next)
      error = ''
      await load(repoID)
    } catch (e) {
      error = errorMessage(e)
      info = { ...info, overrides: next } // keep what was entered
    }
  }

  const setInstruction = (key: string, text: string) => save({ instructions: { ...(o.instructions ?? {}), [key]: text } })
  function addAction() {
    if (!adding) return
    info = info && { ...info, overrides: { ...o, instructions: { ...(o.instructions ?? {}), [adding]: '' } } }
    adding = ''
  }
  function removeAction(a: string) {
    const rest = { ...(o.instructions ?? {}) }
    delete rest[a]
    save({ instructions: rest })
  }
  async function approve() {
    if (!info) return
    try { await api.approveRepoInstructions(repoID, info.repoInstructions.hash); error = '' } catch (e) { error = errorMessage(e) }
    await load(repoID)
  }
  async function ignore() {
    if (!info) return
    try { await api.ignoreRepoInstructions(repoID, info.repoInstructions.hash); error = '' } catch (e) { error = errorMessage(e) }
    await load(repoID)
  }
</script>

{#if error}<p class="warn">{error}</p>{/if}
{#if info}
  <label class="row check">
    <input type="checkbox" checked={!off} disabled={!!info.error} on:change={(e) => save({ aiOff: !e.currentTarget.checked })} />
    <span>Use AI in this repository</span>
  </label>

  <div class="body" class:dim={off}>
    <h4>Models</h4>
    {#each [['chat', 'Chat & agent'], ['task', 'Tasks']] as [role, title]}
      {@const pKey = role === 'chat' ? 'chatProvider' : 'taskProvider'}
      {@const mKey = role === 'chat' ? 'chatModel' : 'taskModel'}
      <div class="pair">
        <span>{title}</span>
        <select disabled={off} value={o[pKey] ?? ''} on:change={(e) => save({ [pKey]: e.currentTarget.value, [mKey]: '' })}>
          <option value="">{g ? globalLabel(g[pKey], g[mKey]) : 'Global'}</option>
          {#each PROVIDERS as p}<option value={p.value}>{p.label}</option>{/each}
        </select>
        {#if o[pKey]}
          <select disabled={off} value={o[mKey]} on:change={(e) => save({ [mKey]: e.currentTarget.value })}>
            {#each models[o[pKey]] ?? [o[mKey]] as m}<option value={m}>{m}</option>{/each}
          </select>
        {/if}
      </div>
    {/each}

    <h4>Automation</h4>
    <label class="pair"><span>Commit message</span>
      <select disabled={off} value={o.commitMessage ?? ''} on:change={(e) => save({ commitMessage: e.currentTarget.value })}>
        <option value="">Global ({g?.commitMessage})</option>
        <option value="auto-local">Automatic with a local model</option>
        <option value="auto">Always automatic</option>
        <option value="manual">Only when I ask</option>
      </select>
    </label>
    <label class="pair"><span>Suggested replies</span>
      <select disabled={off} value={o.suggestReplies ?? ''} on:change={(e) => save({ suggestReplies: e.currentTarget.value })}>
        <option value="">Global ({g?.suggestReplies})</option>
        <option value="auto-local">With a local model</option>
        <option value="auto">Always</option>
        <option value="off">Off</option>
      </select>
    </label>

    <h4>Your instructions</h4>
    <p class="hint">Private: stays on this computer.</p>
    <label class="block"><span>For every action</span>
      <textarea rows="3" disabled={off} value={o.instructions?.all ?? ''} on:blur={(e) => setInstruction('all', e.currentTarget.value)}></textarea>
    </label>
    {#each shownActions as a (a)}
      <label class="block"><span>{actionLabel(a)} <button class="link" on:click|preventDefault={() => removeAction(a)}>Remove</button></span>
        <textarea rows="3" disabled={off} value={o.instructions?.[a] ?? ''} on:blur={(e) => setInstruction(a, e.currentTarget.value)}></textarea>
      </label>
    {/each}
    <div class="pair">
      <select disabled={off} bind:value={adding} on:change={addAction}>
        <option value="">Add instructions for…</option>
        {#each ACTIONS.filter((a) => !shownActions.includes(a)) as a}<option value={a}>{actionLabel(a)}</option>{/each}
      </select>
    </div>

    <h4>Repository instructions</h4>
    {#if info.repoInstructions.error}<p class="warn">{info.repoInstructions.error}</p>{/if}
    {#if info.repoInstructions.files.length === 0}
      <p class="hint">This repository has no shared instructions. Add <code>.committree/instructions.md</code> to share them with your team.</p>
    {:else}
      <div class="state">
        <strong>{stateText(info.state)}</strong>
        {#if info.state === 'pending' || info.state === 'changed'}
          <button class="btn primary" disabled={off} on:click={approve}>Approve</button>
          <button class="btn" disabled={off} on:click={ignore}>Ignore</button>
        {:else if info.state === 'ignored'}
          <button class="btn" disabled={off} on:click={approve}>Approve</button>
        {/if}
      </div>
      <ul class="files">
        {#each info.repoInstructions.files as f (f.name)}
          <li>
            <button class="link" on:click={() => (open = { ...open, [f.name]: !open[f.name] })}>{open[f.name] ? '▾' : '▸'} .committree/{f.name}</button>
            {#if f.ignored}<span class="hint"> — ignored: {f.ignored}</span>{/if}
            {#if open[f.name] && f.text}<pre>{f.text}</pre>{/if}
          </li>
        {/each}
      </ul>
    {/if}
  </div>
{/if}

<style>
  .body.dim { opacity: 0.5; }
  .pair { display: flex; gap: 8px; align-items: center; margin: 6px 0; }
  .pair > span { width: 140px; color: var(--text-muted); }
  .block { display: flex; flex-direction: column; gap: 4px; margin: 6px 0; }
  .block textarea { width: 100%; font: inherit; }
  .state { display: flex; gap: 8px; align-items: center; margin: 6px 0; }
  .files { list-style: none; padding: 0; margin: 0; }
  .files pre { white-space: pre-wrap; background: var(--bg-subtle); padding: 8px; border-radius: 6px; max-height: 200px; overflow: auto; }
  .link { background: none; border: none; padding: 0; color: var(--accent); cursor: pointer; font: inherit; }
  .warn { color: var(--danger); }
  .hint { color: var(--text-muted); }
</style>
```

Before finalising: check `api.getAIStatus` is the real name in `api.ts` and the CSS variable names (`--text-muted`, `--bg-subtle`, `--accent`, `--danger`) against `frontend/src/theme.css`; use what exists. Reuse the `.row.check` / `.warn` / `.hint` classes the dialog already styles if they are global; otherwise keep these local rules. Use the labels the global Settings form uses for the commit message and suggested replies modes (`SettingsDialog.svelte` ~453-470) instead of the ones above if they differ, so both forms read the same.

- [ ] **Step 3: Spec** — in `docs/spec/12-repository-settings.md`, change "today only **Remotes**" to "**Remotes** and **AI**" and append:

```markdown
## AI

**Use AI in this repository** turns every AI action there on or off; off
dims the rest of the tab. **Models** sets the chat and task provider and
model, and **Automation** the commit message and suggested replies modes;
each select's first option, "Global (…)", shows and keeps the global value.
Choosing a provider picks its first model. **Your instructions** — one box
for every action and one per action added with "Add instructions for…" —
are saved when a box loses focus and stay on this computer.

**Repository instructions** lists the `.md` files in the repository's
`.committree/` folder, each foldable to read it and with the reason when it
is ignored. Their state is Approved, Not approved, Ignored, or Changed since
you approved; **Approve** and **Ignore** act on all of them, and are
refused — with the tab showing the new content — if the files changed
since the tab read them. With no files the tab says how to add one.

A failed save is shown at the top of the tab and keeps what was entered.
If the overrides file can't be read, its error is shown and the switch is
disabled.
```

- [ ] **Step 4: Check**

Run: `cd frontend && npm test && npm run check`
Expected: PASS, 0 errors.

- [ ] **Step 5: Commit**

```bash
/usr/bin/git add frontend/src docs/spec/12-repository-settings.md
/usr/bin/git commit -m "feat(ai): Repository settings → AI tab"
```

---

### Task 8: Chat strip, AI-off state, hidden buttons, picker and Prompts note

**Files:**
- Modify: `frontend/src/components/ChatPanel.svelte`, `ModelPicker.svelte`, `CommitBox.svelte` (~98, ~218), `MergeView.svelte` (~216), `LogList.svelte` (~302), `BlameView.svelte` (~104), `SettingsDialog.svelte` (Prompts tab), `docs/spec/06-ai.md`

**Interfaces:**
- Consumes: `repoAI`, `aiOff`, `repoSettings` stores; `usesRepoChatModel`, `withOverride` (Task 6).

- [ ] **Step 1: ChatPanel**

Import `aiOff, repoAI, repoSettings`. First branch inside `.messages` after `!$selectedRepo`:

```svelte
    {:else if $aiOff}
      <div class="notice">
        <strong>AI is off for this repository</strong>
        {#if $repoAI?.error}<p>{$repoAI.error}</p>{/if}
        <div class="actions"><button class="btn" on:click={() => repoSettings.set({ repoID: $selectedRepo.id, tab: 'ai' })}>Repository settings</button></div>
      </div>
```

Hide the composer when off: wrap `<div class="composer">…</div>` in `{#if !$aiOff} … {/if}`. Inside the composer, before `<textarea>`:

```svelte
    {#if $repoAI?.state === 'pending' || $repoAI?.state === 'changed'}
      <div class="strip">
        This repository has instructions for the AI.
        <button class="link" on:click={() => repoSettings.set({ repoID: $selectedRepo.id, tab: 'ai' })}>Review</button>
      </div>
    {/if}
```

Style `.strip` like the existing notices (small, muted background, 6px padding, rounded).

- [ ] **Step 2: ModelPicker**

```ts
  import { aiSettings, loadAISettings, repoAI, selectedRepoId, settingsOpen } from '../lib/stores'
  import { usesRepoChatModel, withOverride } from '../lib/repoAI'
  $: repoModel = usesRepoChatModel($repoAI)
```

In `choose`:

```ts
      if (repoModel && $repoAI && $selectedRepoId) {
        await api.saveRepoAISettings($selectedRepoId, withOverride($repoAI.overrides, { chatProvider: p, chatModel: m }))
      } else {
        const settings = await api.getAISettings()
        await api.saveAISettings({ ...settings, chatProvider: p, chatModel: m })
      }
      await loadAISettings()
```

Button text: `{model || 'Choose a model'}{repoModel ? ' · this repo' : ''}`; title adds "— set for this repository" when `repoModel`.

- [ ] **Step 3: Hide AI buttons when off**

- `CommitBox.svelte`: import `aiOff`; auto-generation guard becomes `if (!info || count <= 0 || !$aiSettings || $aiOff) return`; wrap the Write with AI button in `{#if !$aiOff}…{/if}`.
- `MergeView.svelte`: wrap the Resolve with AI button in `{#if !$aiOff}…{/if}`.
- `LogList.svelte`: the menu entry `{ label: '✨ Explain in chat', … }` is added only when `!get(aiOff)` (build the array conditionally the way neighbouring entries are).
- `BlameView.svelte`: same for '✨ Explain these lines in chat'.

- [ ] **Step 4: Settings → Prompts note**

In `SettingsDialog.svelte`, under the "Open prompts folder" button: `<p class="hint">Repositories can add their own instructions in Repository settings → AI.</p>`

- [ ] **Step 5: Spec** — in `docs/spec/06-ai.md`, "## The chat panel": add

```markdown
When the selected repository has the AI off, the panel shows "AI is off
for this repository" with a Repository settings button instead of the
conversation and the message box, and Write with AI, Resolve with AI and
the Explain menu entries are hidden in that repository. When it has shared
instructions that are not approved (or changed since), a strip above the
message box says "This repository has instructions for the AI" with a
Review button that opens Repository settings → AI. The model picker under
the box reads "<model> · this repo" when the repository sets its own chat
model, and changing it then changes the repository's choice.
```

- [ ] **Step 6: Check, build, look**

Run: `cd frontend && npm test && npm run check` — Expected: PASS.
Run: `go test ./...` — Expected: PASS.
Then `make dev` and check by hand with a repository: AI tab round trip, `.committree/instructions.md` → strip → Approve → strip gone; edit the file → strip back; AI off → panel and buttons hidden; picker "· this repo".

- [ ] **Step 7: Commit**

```bash
/usr/bin/git add frontend/src docs/spec/06-ai.md
/usr/bin/git commit -m "feat(ai): chat strip for unapproved instructions, AI-off state, per-repository model picker"
```

---

## Self-review notes

- Spec §1–§3 → Tasks 1–2; §4 → Tasks 3–4; §5 → Tasks 1, 4, 5, 7; §6 → Tasks 6–8; §7 → tests in every task plus Task 8 Step 6.
- Submodules are out of scope: a submodule ID has no parent mapping and uses its own (empty) entry, i.e. the global settings.
- `explainTask` keeps its explicit `provider` argument: when the frontend passes one, it is used with the effective task model, as today.
