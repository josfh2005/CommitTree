// Package repos stores the user's list of repositories.
package repos

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"git-ui/internal/gitcmd"
)

var (
	ErrNotRepo     = errors.New("not a git repository")
	ErrUnknownRepo = errors.New("unknown repository")
)

type Repo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Missing bool   `json:"missing"`
}

type Store struct {
	path  string
	mu    sync.Mutex
	repos []Repo
}

// DefaultPath is ~/Library/Application Support/git-ui/repos.json on macOS.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "git-ui", "repos.json"), nil
}

func Open(path string) (*Store, error) {
	s := &Store{path: path, repos: []Repo{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &s.repos); err != nil {
		return nil, fmt.Errorf("repos: parse %s: %w", path, err)
	}
	return s, nil
}

func (s *Store) List() []Repo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Repo, len(s.repos))
	for i, r := range s.repos {
		r.Missing = !isRepo(r.Path)
		out[i] = r
	}
	return out
}

func (s *Store) Get(id string) (Repo, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.repos {
		if r.ID == id {
			return r, true
		}
	}
	return Repo{}, false
}

// Add registers the work tree containing path. Adding a repo twice returns
// the existing entry.
func (s *Store) Add(ctx context.Context, path string) (Repo, error) {
	top, err := toplevel(ctx, path)
	if err != nil {
		return Repo{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.repos {
		if r.Path == top {
			return r, nil
		}
	}
	r := Repo{ID: s.uniqueID(top), Name: filepath.Base(top), Path: top}
	s.repos = append(s.repos, r)
	if err := s.save(); err != nil {
		s.repos = s.repos[:len(s.repos)-1]
		return Repo{}, err
	}
	return r, nil
}

// Relocate points an existing entry at a new path, keeping its ID.
func (s *Store) Relocate(ctx context.Context, id, path string) (Repo, error) {
	top, err := toplevel(ctx, path)
	if err != nil {
		return Repo{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, r := range s.repos {
		if r.ID != id {
			continue
		}
		old := r
		r.Path, r.Name = top, filepath.Base(top)
		s.repos[i] = r
		if err := s.save(); err != nil {
			s.repos[i] = old
			return Repo{}, err
		}
		return r, nil
	}
	return Repo{}, ErrUnknownRepo
}

func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, r := range s.repos {
		if r.ID == id {
			old := s.repos
			s.repos = append(append([]Repo{}, old[:i]...), old[i+1:]...)
			if err := s.save(); err != nil {
				s.repos = old
				return err
			}
			return nil
		}
	}
	return ErrUnknownRepo
}

func (s *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.repos, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func toplevel(ctx context.Context, path string) (string, error) {
	out, err := gitcmd.Run(ctx, path, gitcmd.ReadTimeout, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrNotRepo, path)
	}
	return strings.TrimSpace(out), nil
}

func isRepo(path string) bool {
	_, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil
}

func idFor(path string) string {
	sum := sha1.Sum([]byte(path))
	return hex.EncodeToString(sum[:])[:12]
}

// uniqueID derives an ID for path, disambiguating it from any existing
// entry's ID (e.g. a Relocate that left the old ID pointing elsewhere).
// Callers must hold s.mu.
func (s *Store) uniqueID(path string) string {
	id := idFor(path)
	for n := 1; s.idInUse(id); n++ {
		id = idFor(fmt.Sprintf("%s#%d", path, n))
	}
	return id
}

func (s *Store) idInUse(id string) bool {
	for _, r := range s.repos {
		if r.ID == id {
			return true
		}
	}
	return false
}
