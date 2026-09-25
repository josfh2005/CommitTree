package prompts_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	for _, want := range []string{`"git-ui"`, "/src/git-ui", "main", "2026-09-17", "approves or rejects"} {
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
	if !reflect.DeepEqual(prompts.Names(), []string{"chat", "commit-message", "explain-commit", "explain-lines", "resolve-conflicts", "suggest-replies"}) {
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
	want := []prompts.Info{{Name: "chat"}, {Name: "commit-message"}, {Name: "explain-commit", Customized: true}, {Name: "explain-lines"}, {Name: "resolve-conflicts"}, {Name: "suggest-replies"}}
	if !reflect.DeepEqual(s.List(), want) {
		t.Fatalf("list = %+v", s.List())
	}
	if err := s.Reset(prompts.ExplainCommit); err != nil {
		t.Fatal(err)
	}
	if s.List()[2].Customized {
		t.Fatal("still customized after reset")
	}
	if _, err := s.Get("nope", vars); !errors.Is(err, prompts.ErrUnknownPrompt) {
		t.Fatalf("unknown prompt: %v", err)
	}
	if err := s.Reset("nope"); !errors.Is(err, prompts.ErrUnknownPrompt) {
		t.Fatalf("reset unknown: %v", err)
	}
}

func TestGetFallsBackToDefaultWhenUserFileIsBlank(t *testing.T) {
	dir := t.TempDir()
	s := prompts.New(dir)
	if err := os.WriteFile(filepath.Join(dir, "chat.md"), []byte("   \n\t\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(prompts.Chat, vars)
	if err != nil {
		t.Fatal(err)
	}
	want, err := prompts.New(t.TempDir()).Get(prompts.Chat, vars)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("blank override should fall back to the embedded default: got %q, want %q", got, want)
	}
}

func TestResolveConflictsPromptIsAvailable(t *testing.T) {
	found := false
	for _, name := range prompts.Names() {
		if name == prompts.ResolveConflicts {
			found = true
		}
	}
	if !found {
		t.Fatalf("Names() = %v, want it to include %q", prompts.Names(), prompts.ResolveConflicts)
	}

	s := prompts.New(t.TempDir())
	text, err := s.Get(prompts.ResolveConflicts, prompts.Vars{Repo: "acme", Branch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "acme") {
		t.Errorf("prompt did not expand {{repo}}: %q", text)
	}
}

// current is the rendered embedded default of name.
func current(t *testing.T, name string) string {
	t.Helper()
	text, err := prompts.New(t.TempDir()).Get(name, vars)
	if err != nil {
		t.Fatal(err)
	}
	return text
}

// A copy the app wrote of an older default (here the chat prompt as shipped
// in 71bbd68, before the registry existed) is not a customization: it must
// not shadow the current default, and opening the folder refreshes it.
func TestAnUntouchedOldCopyDoesNotShadowTheDefault(t *testing.T) {
	old, err := os.ReadFile("testdata/chat-71bbd68.md")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "chat.md"), old, 0o644); err != nil {
		t.Fatal(err)
	}
	s := prompts.New(dir)

	if got := mustGet(t, s, prompts.Chat); got != current(t, prompts.Chat) {
		t.Fatalf("old copy shadowed the default:\n%s", got)
	}
	for _, info := range s.List() {
		if info.Name == prompts.Chat && info.Customized {
			t.Fatal("old copy listed as customized")
		}
	}
	if err := s.EnsureFiles(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "chat.md"))
	if strings.Contains(string(data), "read-only access") || !strings.Contains(string(data), "CommitTree") {
		t.Fatalf("EnsureFiles left the old copy in place:\n%s", data)
	}
}

// A copy EnsureFiles wrote is recognised through the registry even after the
// embedded default changes — simulated here by rewriting both the file and
// its registry entry to a text that is no longer the default.
func TestACopyTheAppWroteStaysUntouchedAfterTheDefaultChanges(t *testing.T) {
	dir := t.TempDir()
	s := prompts.New(dir)
	if err := s.EnsureFiles(); err != nil {
		t.Fatal(err)
	}
	stale := "An older default nobody edited, for {{repo}}."
	if err := os.WriteFile(filepath.Join(dir, "chat.md"), []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(stale)))
	registry, _ := json.Marshal(map[string]string{prompts.Chat: hex.EncodeToString(sum[:])})
	if err := os.WriteFile(filepath.Join(dir, ".defaults.json"), registry, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := mustGet(t, s, prompts.Chat); got != current(t, prompts.Chat) {
		t.Fatalf("registered copy shadowed the default: %q", got)
	}
}

func TestAnEditedPromptIsKept(t *testing.T) {
	dir := t.TempDir()
	s := prompts.New(dir)
	if err := s.EnsureFiles(); err != nil {
		t.Fatal(err)
	}
	mine := "My own chat rules for {{repo}}."
	if err := os.WriteFile(filepath.Join(dir, "chat.md"), []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := mustGet(t, s, prompts.Chat); got != "My own chat rules for git-ui." {
		t.Fatalf("edit not used: %q", got)
	}
	if err := s.EnsureFiles(); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "chat.md")); string(data) != mine {
		t.Fatalf("EnsureFiles overwrote an edit: %q", data)
	}
}

func TestACorruptRegistryIsIgnored(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".defaults.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := prompts.New(dir)
	if got := mustGet(t, s, prompts.Chat); got != current(t, prompts.Chat) {
		t.Fatalf("got %q", got)
	}
	if err := s.EnsureFiles(); err != nil {
		t.Fatalf("EnsureFiles with a corrupt registry: %v", err)
	}
	if err := s.Reset(prompts.Chat); err != nil {
		t.Fatalf("Reset with a corrupt registry: %v", err)
	}
}

func mustGet(t *testing.T, s *prompts.Store, name string) string {
	t.Helper()
	text, err := s.Get(name, vars)
	if err != nil {
		t.Fatal(err)
	}
	return text
}
