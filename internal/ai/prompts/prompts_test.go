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
	if !reflect.DeepEqual(prompts.Names(), []string{"chat", "commit-message", "explain-commit", "resolve-conflicts"}) {
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
	want := []prompts.Info{{Name: "chat"}, {Name: "commit-message"}, {Name: "explain-commit", Customized: true}, {Name: "resolve-conflicts"}}
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
