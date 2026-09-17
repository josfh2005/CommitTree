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
