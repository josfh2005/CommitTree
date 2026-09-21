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
	ProviderOllama    = "ollama"
	ProviderOpenAI    = "openai"
	ProviderAnthropic = "anthropic"
	// ProviderApple is only recognised when reading a settings file written
	// by an older version; a later task removes this constant.
	ProviderApple = "apple"

	DefaultOllamaURL      = "http://localhost:11434"
	DefaultModel          = "qwen2.5:7b"
	DefaultAnthropicModel = "claude-opus-5"

	// CommitAutoLocal generates the message automatically when the task
	// provider is local and free, and offers a button otherwise.
	CommitAutoLocal = "auto-local"
	CommitAuto      = "auto"
	CommitManual    = "manual"
)

var ErrInvalid = errors.New("invalid AI settings")

type Settings struct {
	OllamaURL     string `json:"ollamaURL"`
	ChatProvider  string `json:"chatProvider"`
	ChatModel     string `json:"chatModel"`
	TaskProvider  string `json:"taskProvider"`
	TaskModel     string `json:"taskModel"`
	CommitMessage string `json:"commitMessage"`
}

func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "git-ui", "ai.json"), nil
}

func Defaults() Settings {
	return Settings{
		OllamaURL:     DefaultOllamaURL,
		ChatProvider:  ProviderOllama,
		ChatModel:     DefaultModel,
		TaskProvider:  ProviderOllama,
		TaskModel:     DefaultModel,
		CommitMessage: CommitAutoLocal,
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
	if s.ChatProvider == ProviderApple || s.ChatProvider == "" {
		s.ChatProvider = ProviderOllama
	}
	if s.TaskProvider == ProviderApple || s.TaskProvider == "" {
		s.TaskProvider = ProviderOllama
	}
	if s.CommitMessage == "" {
		s.CommitMessage = CommitAutoLocal
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
	for _, p := range []string{s.ChatProvider, s.TaskProvider} {
		switch p {
		case ProviderOllama, ProviderOpenAI, ProviderAnthropic:
		default:
			return fmt.Errorf("%w: unknown provider %q", ErrInvalid, p)
		}
	}
	switch s.CommitMessage {
	case CommitAutoLocal, CommitAuto, CommitManual:
	default:
		return fmt.Errorf("%w: unknown commit message mode %q", ErrInvalid, s.CommitMessage)
	}
	return nil
}
