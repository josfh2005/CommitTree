// Package prompts manages the prompt "skills" used by AI actions: defaults
// embedded in the binary, optionally overridden by files the user edits.
package prompts

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
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
	SuggestReplies   = "suggest-replies"
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

// registryFile records, per prompt, the hash of the default text the app
// last wrote to the user's folder, so an untouched copy is told apart from
// an edit once the embedded default changes.
const registryFile = ".defaults.json"

func textHash(text string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(text)))
	return hex.EncodeToString(sum[:])
}

// written reads the registry; a missing or unreadable one is empty, which
// only loses the detection of copies written since it existed.
func (s *Store) written() map[string]string {
	m := map[string]string{}
	if data, err := os.ReadFile(filepath.Join(s.dir, registryFile)); err == nil {
		_ = json.Unmarshal(data, &m)
	}
	return m
}

// writeDefault writes name's default to the user's folder and records it.
func (s *Store) writeDefault(name, def string, written map[string]string) error {
	if err := os.WriteFile(s.userFile(name), []byte(def), 0o644); err != nil {
		return err
	}
	written[name] = textHash(def)
	data, err := json.MarshalIndent(written, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, registryFile), data, 0o644)
}

// userText is the user's own version of name, or "" when there is none: no
// file, a blank one, or an untouched copy of a default the app wrote (now or
// in an earlier version) — which must not shadow the current default.
func (s *Store) userText(name string, written map[string]string) string {
	data, err := os.ReadFile(s.userFile(name))
	if err != nil || strings.TrimSpace(string(data)) == "" {
		return ""
	}
	if h := textHash(string(data)); h == written[name] || pastDefaults[h] {
		return ""
	}
	return string(data)
}

// Get returns the rendered prompt, preferring the user's own version.
func (s *Store) Get(name string, v Vars) (string, error) {
	text, err := defaultText(name)
	if err != nil {
		return "", err
	}
	if mine := s.userText(name, s.written()); mine != "" {
		text = mine
	}
	r := strings.NewReplacer("{{repo}}", v.Repo, "{{path}}", v.Path, "{{branch}}", v.Branch, "{{date}}", v.Date)
	return strings.TrimSpace(r.Replace(text)), nil
}

func (s *Store) List() []Info {
	infos := []Info{}
	written := s.written()
	for _, name := range Names() {
		def, _ := defaultText(name)
		mine := s.userText(name, written)
		infos = append(infos, Info{Name: name, Customized: mine != "" && strings.TrimSpace(mine) != strings.TrimSpace(def)})
	}
	return infos
}

// EnsureFiles creates the prompts folder and writes the current default for
// each prompt that has no file yet or only an untouched copy of an older
// default, so the folder shows what the app uses. Edited files are left
// alone.
func (s *Store) EnsureFiles() error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	written := s.written()
	for _, name := range Names() {
		data, err := os.ReadFile(s.userFile(name))
		if err == nil && s.userText(name, written) != "" {
			continue // the user's own version
		}
		def, _ := defaultText(name)
		if err == nil && string(data) == def {
			continue
		}
		if err == nil && strings.TrimSpace(string(data)) == "" {
			continue // blank already falls back to the default; leave it
		}
		if err := s.writeDefault(name, def, written); err != nil {
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
	return s.writeDefault(name, def, s.written())
}
