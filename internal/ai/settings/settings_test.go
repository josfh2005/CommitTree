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
