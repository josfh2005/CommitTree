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
