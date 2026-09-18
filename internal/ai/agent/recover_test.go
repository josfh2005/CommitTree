package agent

import (
	"reflect"
	"testing"

	"git-ui/internal/ai"
)

var recoverTools = []ai.ToolSpec{{Name: "resolve_hunk"}, {Name: "read_conflict"}}

func TestRecoverCalls(t *testing.T) {
	cases := []struct {
		name      string
		text      string
		wantCalls []ai.ToolCall
		attempted bool
	}{
		{
			name:      "bare object as the whole text",
			text:      `{"name": "resolve_hunk", "arguments": {"path": "a.go"}}`,
			wantCalls: []ai.ToolCall{{Name: "resolve_hunk", Args: map[string]any{"path": "a.go"}}},
			attempted: true,
		},
		{
			name:      "object inside a fenced block with prose around it",
			text:      "Sure, here's the call:\n```json\n{\"name\": \"resolve_hunk\", \"arguments\": {\"path\": \"a.go\"}}\n```\nDone.",
			wantCalls: []ai.ToolCall{{Name: "resolve_hunk", Args: map[string]any{"path": "a.go"}}},
			attempted: true,
		},
		{
			name:      "parameters instead of arguments",
			text:      `{"name": "resolve_hunk", "parameters": {"path": "a.go"}}`,
			wantCalls: []ai.ToolCall{{Name: "resolve_hunk", Args: map[string]any{"path": "a.go"}}},
			attempted: true,
		},
		{
			name: "two objects in one text, in order",
			text: `{"name": "read_conflict", "arguments": {"path": "a.go"}} then {"name": "resolve_hunk", "arguments": {"path": "b.go"}}`,
			wantCalls: []ai.ToolCall{
				{Name: "read_conflict", Args: map[string]any{"path": "a.go"}},
				{Name: "resolve_hunk", Args: map[string]any{"path": "b.go"}},
			},
			attempted: true,
		},
		{
			name:      "unknown tool name",
			text:      `{"name": "delete_repo", "arguments": {"path": "a.go"}}`,
			wantCalls: nil,
			attempted: true,
		},
		{
			name:      "malformed JSON containing name and arguments",
			text:      `{"name": "resolve_hunk", "arguments": {"path": "a.go"`,
			wantCalls: nil,
			attempted: true,
		},
		{
			name:      "ordinary prose",
			text:      "I read the conflict and it looks fine to me.",
			wantCalls: nil,
			attempted: false,
		},
		{
			name:      "prose containing an unrelated JSON object",
			text:      `Here is some data: {"a": 1} nothing more.`,
			wantCalls: nil,
			attempted: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotCalls, gotAttempted := recoverCalls(tc.text, recoverTools)
			for i := range gotCalls {
				gotCalls[i].ID = ""
			}
			if !reflect.DeepEqual(gotCalls, tc.wantCalls) {
				t.Errorf("calls = %#v, want %#v", gotCalls, tc.wantCalls)
			}
			if gotAttempted != tc.attempted {
				t.Errorf("attempted = %v, want %v", gotAttempted, tc.attempted)
			}
		})
	}
}
