package chatstore_test

import (
	"errors"
	"reflect"
	"testing"

	"git-ui/internal/ai"
	"git-ui/internal/ai/chatstore"
)

func TestSaveLoadClear(t *testing.T) {
	s := chatstore.New(t.TempDir())

	empty, err := s.Load("abc123")
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty = %#v, err %v", empty, err)
	}
	msgs := []ai.Message{
		{Role: ai.RoleUser, Content: "hola"},
		{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{{ID: "c1", Name: "list_refs", Args: map[string]any{"limit": float64(5)}}}},
		{Role: ai.RoleTool, ToolName: "list_refs", Content: "main"},
		{Role: ai.RoleAssistant, Content: "Hay una rama", Stopped: true},
	}
	if err := s.Save("abc123", msgs); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load("abc123")
	if err != nil || !reflect.DeepEqual(got, msgs) {
		t.Fatalf("got %#v, err %v", got, err)
	}
	if err := s.Clear("abc123"); err != nil {
		t.Fatal(err)
	}
	if err := s.Clear("abc123"); err != nil {
		t.Fatalf("clearing twice: %v", err)
	}
	after, _ := s.Load("abc123")
	if len(after) != 0 {
		t.Fatalf("after clear = %#v", after)
	}
}

func TestRejectsUnsafeIDs(t *testing.T) {
	s := chatstore.New(t.TempDir())
	for _, id := range []string{"", "../x", "a/b", "a b"} {
		if _, err := s.Load(id); !errors.Is(err, chatstore.ErrInvalidID) {
			t.Errorf("%q: want ErrInvalidID, got %v", id, err)
		}
	}
}
