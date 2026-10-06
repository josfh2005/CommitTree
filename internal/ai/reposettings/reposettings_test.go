package reposettings

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"git-ui/internal/ai/settings"
)

func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "repo-ai.json")
	return New(p), p
}

func TestPathNextTo(t *testing.T) {
	if got := PathNextTo("/x/git-ui/ai.json"); got != "/x/git-ui/repo-ai.json" {
		t.Fatalf("got %q", got)
	}
}

func TestGetMissingFileIsEmpty(t *testing.T) {
	s, _ := newStore(t)
	o, err := s.Get("r1")
	if err != nil || !o.IsEmpty() {
		t.Fatalf("got %+v, %v", o, err)
	}
}

func TestSetGetRoundTripAndRemoveWhenEmpty(t *testing.T) {
	s, p := newStore(t)
	o := Override{ChatProvider: settings.ProviderAnthropic, ChatModel: "claude-opus-5", Instructions: map[string]string{"all": "Use English."}}
	if err := s.Set("r1", o); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("r1")
	if err != nil || got.ChatModel != "claude-opus-5" || got.Instructions["all"] != "Use English." {
		t.Fatalf("got %+v, %v", got, err)
	}
	if err := s.Set("r1", Override{}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	if string(data) != "{}" {
		t.Fatalf("entry not removed: %s", data)
	}
}

func TestSetKeepsApproval(t *testing.T) {
	s, _ := newStore(t)
	if err := s.SetApproval("r1", "sha256:abc"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("r1", Override{AIOff: true, Approval: "sha256:forged"}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get("r1")
	if got.Approval != "sha256:abc" || !got.AIOff {
		t.Fatalf("got %+v", got)
	}
}

func TestRemove(t *testing.T) {
	s, _ := newStore(t)
	_ = s.Set("r1", Override{AIOff: true})
	if err := s.Remove("r1"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get("r1")
	if !got.IsEmpty() {
		t.Fatalf("got %+v", got)
	}
}

func TestCorruptFileIsUnreadable(t *testing.T) {
	s, p := newStore(t)
	_ = os.WriteFile(p, []byte("{not json"), 0o644)
	if _, err := s.Get("r1"); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("got %v", err)
	}
	if err := s.Set("r1", Override{AIOff: true}); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("Set over a corrupt file must refuse, got %v", err)
	}
}

func TestLoadRejectsInvalidEntry(t *testing.T) {
	s, p := newStore(t)
	_ = os.WriteFile(p, []byte(`{"r1":{"chatProvider":"bogus","chatModel":"x"}}`), 0o644)
	if _, err := s.Get("r2"); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("an invalid entry anywhere makes the file unreadable, got %v", err)
	}
}

func TestValidate(t *testing.T) {
	bad := []Override{
		{ChatProvider: settings.ProviderOllama},          // provider without model
		{ChatModel: "qwen2.5:7b"},                        // model without provider
		{TaskProvider: "bogus", TaskModel: "x"},          // unknown provider
		{CommitMessage: "sometimes"},                     // unknown mode
		{SuggestReplies: "maybe"},                        // unknown mode
		{Instructions: map[string]string{"deploy": "x"}}, // unknown action
	}
	for i, o := range bad {
		if err := Validate(o); !errors.Is(err, ErrInvalid) {
			t.Errorf("%d: want ErrInvalid, got %v", i, err)
		}
	}
	good := Override{TaskProvider: settings.ProviderOllama, TaskModel: "qwen2.5:7b", CommitMessage: settings.CommitManual,
		Instructions: map[string]string{"all": "a", "commit-message": "b"}}
	if err := Validate(good); err != nil {
		t.Fatal(err)
	}
}

func TestMerge(t *testing.T) {
	g := settings.Defaults()
	got := Merge(g, Override{ChatProvider: settings.ProviderAnthropic, ChatModel: "claude-opus-5", SuggestReplies: settings.SuggestOff})
	if got.ChatProvider != settings.ProviderAnthropic || got.ChatModel != "claude-opus-5" || got.SuggestReplies != settings.SuggestOff {
		t.Fatalf("overrides not applied: %+v", got)
	}
	if got.TaskProvider != g.TaskProvider || got.TaskModel != g.TaskModel || got.CommitMessage != g.CommitMessage || got.OllamaURL != g.OllamaURL {
		t.Fatalf("global values lost: %+v", got)
	}
}

func TestIsEmptyIgnoresBlankInstructions(t *testing.T) {
	if !(Override{Instructions: map[string]string{"all": "  "}}).IsEmpty() {
		t.Fatal("blank instructions count as empty")
	}
	if (Override{Approval: "sha256:x"}).IsEmpty() {
		t.Fatal("an approval is not empty")
	}
}
