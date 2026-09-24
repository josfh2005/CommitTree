// Package prompts manages the prompt "skills" used by AI actions: defaults
// embedded in the binary, optionally overridden by files the user edits.
package prompts

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	Chat             = "chat"
	ExplainCommit    = "explain-commit"
	ExplainLines     = "explain-lines"
	ResolveConflicts = "resolve-conflicts"
	CommitMessage    = "commit-message"
)

var ErrUnknownPrompt = errors.New("unknown prompt")

//go:embed defaults/*.md
var defaults embed.FS

type Vars struct {
	Repo, Path, Branch, Date string
}

type Info struct {
	Name       string `json:"name"`
	Customized bool   `json:"customized"`
}

type Store struct{ dir string }

func New(dir string) *Store { return &Store{dir: dir} }

func DefaultDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "git-ui", "prompts"), nil
}

func (s *Store) Dir() string { return s.dir }

// Names lists the prompts that have an embedded default, sorted.
func Names() []string {
	entries, _ := fs.ReadDir(defaults, "defaults")
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, strings.TrimSuffix(e.Name(), ".md"))
	}
	sort.Strings(names)
	return names
}

func defaultText(name string) (string, error) {
	data, err := defaults.ReadFile("defaults/" + name + ".md")
	if err != nil {
		return "", fmt.Errorf("%w: %q", ErrUnknownPrompt, name)
	}
	return string(data), nil
}

func (s *Store) userFile(name string) string { return filepath.Join(s.dir, name+".md") }

// Get returns the rendered prompt, preferring the user's file.
func (s *Store) Get(name string, v Vars) (string, error) {
	text, err := defaultText(name)
	if err != nil {
		return "", err
	}
	if data, err := os.ReadFile(s.userFile(name)); err == nil && strings.TrimSpace(string(data)) != "" {
		text = string(data)
	}
	r := strings.NewReplacer("{{repo}}", v.Repo, "{{path}}", v.Path, "{{branch}}", v.Branch, "{{date}}", v.Date)
	return strings.TrimSpace(r.Replace(text)), nil
}

func (s *Store) List() []Info {
	infos := []Info{}
	for _, name := range Names() {
		def, _ := defaultText(name)
		data, err := os.ReadFile(s.userFile(name))
		infos = append(infos, Info{Name: name, Customized: err == nil && strings.TrimSpace(string(data)) != strings.TrimSpace(def)})
	}
	return infos
}

// EnsureFiles creates the prompts folder and writes a copy of each default
// that has no user file yet, so the user has something to edit.
func (s *Store) EnsureFiles() error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	for _, name := range Names() {
		path := s.userFile(name)
		if _, err := os.Stat(path); err == nil {
			continue
		}
		def, _ := defaultText(name)
		if err := os.WriteFile(path, []byte(def), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Reset overwrites the user's file with the default text.
func (s *Store) Reset(name string) error {
	def, err := defaultText(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.userFile(name), []byte(def), 0o644)
}
