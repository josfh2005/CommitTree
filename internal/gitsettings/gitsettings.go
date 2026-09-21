// Package gitsettings stores app-level git-behaviour preferences — today
// just the pull strategy. Kept apart from internal/ai/settings, which is AI
// configuration only, in its own file so the two never collide.
package gitsettings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	PullAuto   = "auto"
	PullMerge  = "merge"
	PullRebase = "rebase"
)

var ErrInvalid = errors.New("gitsettings: invalid settings")

type Settings struct {
	PullStrategy string `json:"pullStrategy"`
}

func Defaults() Settings {
	return Settings{PullStrategy: PullAuto}
}

func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "git-ui", "git.json"), nil
}

// Load reads the settings file, returning defaults when it doesn't exist,
// and filling in a strategy a file from before this setting existed lacks.
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
		return Settings{}, fmt.Errorf("gitsettings: parse %s: %w", path, err)
	}
	if s.PullStrategy == "" {
		s.PullStrategy = PullAuto
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
	switch s.PullStrategy {
	case PullAuto, PullMerge, PullRebase:
		return nil
	default:
		return fmt.Errorf("%w: unknown pull strategy %q", ErrInvalid, s.PullStrategy)
	}
}
