package settings_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git-ui/internal/ai/settings"
)

func TestLoadMissingFileUsesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai.json")

	got, err := settings.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != settings.Defaults() {
		t.Fatalf("got %+v, want %+v", got, settings.Defaults())
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "ai.json")
	s := settings.Settings{OllamaURL: "http://10.0.0.5:11434", ChatProvider: "ollama", ChatModel: "llama3.1:8b", TaskProvider: "ollama", TaskModel: "qwen2.5:3b"}

	if err := settings.Save(path, s); err != nil {
		t.Fatal(err)
	}
	got, err := settings.Load(path)
	if err != nil || got != s {
		t.Fatalf("got %+v, err %v", got, err)
	}
}

func TestSaveRejectsInvalidValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai.json")
	bad := []settings.Settings{
		{OllamaURL: "localhost:11434", ChatProvider: "ollama", ChatModel: "m", TaskProvider: "ollama", TaskModel: "m"},
		{OllamaURL: "ftp://x", ChatProvider: "ollama", ChatModel: "m", TaskProvider: "ollama", TaskModel: "m"},
		{OllamaURL: "http://localhost:11434", ChatProvider: "ollama", ChatModel: "m", TaskProvider: "apple", TaskModel: "m"},
		{OllamaURL: "http://localhost:11434", ChatProvider: "ollama", ChatModel: "", TaskProvider: "ollama", TaskModel: "m"},
	}
	for _, s := range bad {
		if err := settings.Save(path, s); !errors.Is(err, settings.ErrInvalid) {
			t.Errorf("%+v: want ErrInvalid, got %v", s, err)
		}
	}
}

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
