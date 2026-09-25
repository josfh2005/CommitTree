package tasks_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"git-ui/internal/ai"
	"git-ui/internal/ai/tasks"
)

func TestParseReplies(t *testing.T) {
	long := strings.Repeat("a", 61)
	cases := map[string][]string{
		`["ok dale", "sí, crea la rama fix/login"]`: {"ok dale", "sí, crea la rama fix/login"},
		"```json\n[\"ok\", \"no\"]\n```":            {"ok", "no"},
		`Here you go: ["ok"] hope it helps`:         {"ok"},
		`["a", "b", "c", "d"]`:                      {"a", "b", "c"},
		`["` + long + `", "short"]`:                 {"short"},
		`["ok", 3, null, {"x": 1}, " fine "]`:       {"ok", "fine"},
		`["Ok", "ok", "OK dale"]`:                   {"Ok", "OK dale"},
	}
	for in, want := range cases {
		got, err := tasks.ParseReplies(in)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("ParseReplies(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "no array here", `["", "  "]`, `[1, 2]`, `[not json]`} {
		if got, err := tasks.ParseReplies(in); !errors.Is(err, tasks.ErrNoReplies) {
			t.Errorf("ParseReplies(%q) = %q, %v; want ErrNoReplies", in, got, err)
		}
	}
}

func TestSuggestContextKeepsTheLastTextTurns(t *testing.T) {
	history := []ai.Message{
		{Role: ai.RoleUser, Content: "one"},
		{Role: ai.RoleAssistant, Content: "two"},
		{Role: ai.RoleUser, Content: "three"},
		{Role: ai.RoleAssistant, Content: "", ToolCalls: []ai.ToolCall{{ID: "c", Name: "list_refs"}}},
		{Role: ai.RoleTool, ToolName: "list_refs", Content: "Current branch: main"},
		{Role: ai.RoleAssistant, Content: "four"},
		{Role: ai.RoleUser, Content: "five"},
		{Role: ai.RoleAssistant, Content: "six"},
		{Role: ai.RoleUser, Content: "seven"},
		{Role: ai.RoleAssistant, Content: strings.Repeat("z", 2000)},
	}
	got := tasks.SuggestContext(history)
	if strings.Contains(got, "one") || strings.Contains(got, "two") || strings.Contains(got, "Current branch") {
		t.Fatalf("context kept old turns or tool output:\n%s", got)
	}
	if !strings.HasPrefix(got, "User: three\n\nAssistant: four\n\nUser: five") {
		t.Fatalf("context = %q", got)
	}
	if strings.Count(got, "z") != 1500 {
		t.Fatalf("last turn not cut to 1500 characters: %d", strings.Count(got, "z"))
	}
}

type scriptedResponder struct {
	instructions, prompt string
	chunks               []ai.Chunk
}

func (s *scriptedResponder) Respond(ctx context.Context, instructions, prompt string) (<-chan ai.Chunk, error) {
	s.instructions, s.prompt = instructions, prompt
	ch := make(chan ai.Chunk, len(s.chunks))
	for _, c := range s.chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func TestSuggestReplies(t *testing.T) {
	r := &scriptedResponder{chunks: []ai.Chunk{{Delta: `["ok `}, {Delta: `dale"]`}, {Done: true}}}
	got, err := tasks.SuggestReplies(context.Background(), r, "sys", []ai.Message{{Role: ai.RoleUser, Content: "¿creo la rama?"}})
	if err != nil || !reflect.DeepEqual(got, []string{"ok dale"}) {
		t.Fatalf("got %q, %v", got, err)
	}
	if r.instructions != "sys" || r.prompt != "User: ¿creo la rama?" {
		t.Fatalf("sent %q / %q", r.instructions, r.prompt)
	}

	boom := errors.New("boom")
	r = &scriptedResponder{chunks: []ai.Chunk{{Delta: `["ok"]`}, {Err: boom}}}
	if _, err := tasks.SuggestReplies(context.Background(), r, "sys", nil); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the stream error", err)
	}
}
