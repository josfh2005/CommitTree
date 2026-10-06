// Package reposettings stores the AI settings a repository overrides,
// reads the instructions a repository shares in .committree/, and builds
// the final prompt from both.
package reposettings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"git-ui/internal/ai/prompts"
	"git-ui/internal/ai/settings"
)

var (
	ErrInvalid    = errors.New("invalid repository AI settings")
	ErrUnreadable = errors.New("Per-repository AI settings can't be read")
)

// AllActions is the instructions key, private or shared, that applies to
// every action.
const AllActions = "all"

// Override is what one repository changes; an empty field means "use the
// global value". Approval is the hash of the .committree files the user
// approved, or "ignored:<hash>"; only SetApproval changes it.
type Override struct {
	AIOff          bool              `json:"aiOff,omitempty"`
	ChatProvider   string            `json:"chatProvider,omitempty"`
	ChatModel      string            `json:"chatModel,omitempty"`
	TaskProvider   string            `json:"taskProvider,omitempty"`
	TaskModel      string            `json:"taskModel,omitempty"`
	CommitMessage  string            `json:"commitMessage,omitempty"`
	SuggestReplies string            `json:"suggestReplies,omitempty"`
	Instructions   map[string]string `json:"instructions,omitempty"`
	Approval       string            `json:"approvedRepoInstructions,omitempty"`
}

func (o Override) IsEmpty() bool {
	for _, v := range o.Instructions {
		if strings.TrimSpace(v) != "" {
			return false
		}
	}
	return !o.AIOff && o.ChatProvider == "" && o.ChatModel == "" && o.TaskProvider == "" && o.TaskModel == "" &&
		o.CommitMessage == "" && o.SuggestReplies == "" && o.Approval == ""
}

// PathNextTo is repo-ai.json in the directory of the global ai.json.
func PathNextTo(aiSettingsPath string) string {
	return filepath.Join(filepath.Dir(aiSettingsPath), "repo-ai.json")
}

type Store struct {
	path string
	mu   sync.Mutex
}

func New(path string) *Store { return &Store{path: path} }

// load reads every entry. A file that is not JSON, or holds an entry that
// would not validate, is unreadable as a whole: callers must refuse AI
// actions rather than fall back to the global settings.
func (s *Store) load() (map[string]Override, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]Override{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	all := map[string]Override{}
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	for id, o := range all {
		if err := Validate(o); err != nil {
			return nil, fmt.Errorf("%w: entry %s: %v", ErrUnreadable, id, err)
		}
	}
	return all, nil
}

func (s *Store) save(all map[string]Override) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	if len(all) == 0 {
		data = []byte("{}")
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) Get(id string) (Override, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.load()
	if err != nil {
		return Override{}, err
	}
	return all[id], nil
}

// Set replaces id's overrides, keeping the stored approval. An entry left
// empty is removed.
func (s *Store) Set(id string, o Override) error {
	if err := Validate(o); err != nil {
		return err
	}
	return s.update(id, func(cur Override) Override {
		o.Approval = cur.Approval
		return o
	})
}

func (s *Store) SetApproval(id, value string) error {
	return s.update(id, func(cur Override) Override {
		cur.Approval = value
		return cur
	})
}

func (s *Store) Remove(id string) error {
	return s.update(id, func(Override) Override { return Override{} })
}

func (s *Store) update(id string, fn func(Override) Override) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.load()
	if err != nil {
		return err
	}
	cur, had := all[id]
	next := fn(cur)
	if next.IsEmpty() {
		if !had {
			return nil // nothing to remove: don't create or rewrite the file
		}
		delete(all, id)
	} else {
		all[id] = next
	}
	return s.save(all)
}

func Validate(o Override) error {
	pairs := [][2]string{{o.ChatProvider, o.ChatModel}, {o.TaskProvider, o.TaskModel}}
	for _, p := range pairs {
		if (p[0] == "") != (p[1] == "") {
			return fmt.Errorf("%w: a provider and its model are set together", ErrInvalid)
		}
		switch p[0] {
		case "", settings.ProviderOllama, settings.ProviderOpenAI, settings.ProviderAnthropic:
		default:
			return fmt.Errorf("%w: unknown provider %q", ErrInvalid, p[0])
		}
	}
	switch o.CommitMessage {
	case "", settings.CommitAutoLocal, settings.CommitAuto, settings.CommitManual:
	default:
		return fmt.Errorf("%w: unknown commit message mode %q", ErrInvalid, o.CommitMessage)
	}
	switch o.SuggestReplies {
	case "", settings.SuggestAutoLocal, settings.SuggestAuto, settings.SuggestOff:
	default:
		return fmt.Errorf("%w: unknown suggested replies mode %q", ErrInvalid, o.SuggestReplies)
	}
	for k := range o.Instructions {
		if k != AllActions && !isAction(k) {
			return fmt.Errorf("%w: unknown action %q", ErrInvalid, k)
		}
	}
	return nil
}

func isAction(name string) bool {
	for _, n := range prompts.Names() {
		if n == name {
			return true
		}
	}
	return false
}

// Merge returns the global settings with o's non-empty fields on top.
func Merge(g settings.Settings, o Override) settings.Settings {
	if o.ChatProvider != "" {
		g.ChatProvider, g.ChatModel = o.ChatProvider, o.ChatModel
	}
	if o.TaskProvider != "" {
		g.TaskProvider, g.TaskModel = o.TaskProvider, o.TaskModel
	}
	if o.CommitMessage != "" {
		g.CommitMessage = o.CommitMessage
	}
	if o.SuggestReplies != "" {
		g.SuggestReplies = o.SuggestReplies
	}
	return g
}
