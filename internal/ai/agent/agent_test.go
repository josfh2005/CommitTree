package agent_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"git-ui/internal/ai"
	"git-ui/internal/ai/agent"
)

// scripted replays one list of chunks per call and records requests.
type scripted struct {
	turns    [][]ai.Chunk
	requests []ai.Request
}

func (s *scripted) Chat(ctx context.Context, req ai.Request) (<-chan ai.Chunk, error) {
	s.requests = append(s.requests, req)
	turn := s.turns[min(len(s.requests), len(s.turns))-1]
	ch := make(chan ai.Chunk, len(turn))
	for _, c := range turn {
		ch <- c
	}
	close(ch)
	return ch, nil
}

type recorder struct {
	mu     sync.Mutex
	names  []string
	data   []any
	onEmit func(name string)
}

func (r *recorder) emit(name string, data any) {
	r.mu.Lock()
	r.names = append(r.names, name)
	r.data = append(r.data, data)
	r.mu.Unlock()
	if r.onEmit != nil {
		r.onEmit(name)
	}
}

func baseRun(p ai.Provider, rec *recorder) agent.Run {
	return agent.Run{
		RepoID: "repo1", RunID: "run1", Provider: p, Model: "m", System: "sys",
		Tools:   []ai.ToolSpec{{Name: "list_refs"}},
		RunTool: func(ctx context.Context, call ai.ToolCall, step int) string { return "main\nfeature" },
		Emit:    rec.emit,
	}
}

var user = []ai.Message{{Role: ai.RoleUser, Content: "what branches?"}}

func TestToolLoop(t *testing.T) {
	p := &scripted{turns: [][]ai.Chunk{
		{{ToolCalls: []ai.ToolCall{{ID: "c1", Name: "list_refs", Args: map[string]any{}}}}, {Done: true}},
		{{Delta: "On "}, {Delta: "main."}, {Done: true}},
	}}
	rec := &recorder{}

	got, err := agent.Execute(context.Background(), baseRun(p, rec), user)
	if err != nil {
		t.Fatal(err)
	}
	want := []ai.Message{
		user[0],
		{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{{ID: "c1", Name: "list_refs", Args: map[string]any{}}}},
		{Role: ai.RoleTool, ToolName: "list_refs", Content: "main\nfeature"},
		{Role: ai.RoleAssistant, Content: "On main."},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("history:\n got  %#v\n want %#v", got, want)
	}
	if !reflect.DeepEqual(rec.names, []string{agent.EventTool, agent.EventToolResult, agent.EventDelta, agent.EventDelta}) {
		t.Fatalf("events = %v", rec.names)
	}
	if rec.data[1] != (agent.ToolResultEvent{RepoID: "repo1", RunID: "run1", Name: "list_refs", Summary: "main"}) {
		t.Fatalf("tool result event = %#v", rec.data[1])
	}
	if p.requests[0].System != "sys" || p.requests[0].Model != "m" || len(p.requests[0].Tools) != 1 {
		t.Fatalf("request = %+v", p.requests[0])
	}
	if len(p.requests[1].Messages) != 3 {
		t.Fatalf("second request should include the tool result: %+v", p.requests[1].Messages)
	}
}

func TestMissingToolCallIDsAreFilled(t *testing.T) {
	p := &scripted{turns: [][]ai.Chunk{
		{{ToolCalls: []ai.ToolCall{{Name: "list_refs"}}}, {Done: true}},
		{{Delta: "ok"}, {Done: true}},
	}}
	got, err := agent.Execute(context.Background(), baseRun(p, &recorder{}), user)
	if err != nil || got[1].ToolCalls[0].ID == "" {
		t.Fatalf("history = %#v, err %v", got, err)
	}
}

func TestStepLimit(t *testing.T) {
	p := &scripted{turns: [][]ai.Chunk{{{ToolCalls: []ai.ToolCall{{ID: "c", Name: "list_refs"}}}, {Done: true}}}}
	rec := &recorder{}

	got, err := agent.Execute(context.Background(), baseRun(p, rec), user)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.requests) != agent.MaxSteps {
		t.Fatalf("requests = %d", len(p.requests))
	}
	last := got[len(got)-1]
	if last.Role != ai.RoleAssistant || last.Content != agent.StepLimitNote {
		t.Fatalf("last = %#v", last)
	}
	if rec.names[len(rec.names)-1] != agent.EventDelta {
		t.Fatalf("step limit note not streamed: %v", rec.names)
	}
}

// blocking sends one delta, then waits for cancellation.
type blocking struct{}

func (blocking) Chat(ctx context.Context, req ai.Request) (<-chan ai.Chunk, error) {
	ch := make(chan ai.Chunk)
	go func() {
		defer close(ch)
		ch <- ai.Chunk{Delta: "Hel"}
		<-ctx.Done()
	}()
	return ch, nil
}

func TestCancelKeepsPartialAnswer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	rec := &recorder{onEmit: func(string) { cancel() }}

	got, err := agent.Execute(ctx, baseRun(blocking{}, rec), user)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	last := got[len(got)-1]
	if last.Role != ai.RoleAssistant || last.Content != "Hel" || !last.Stopped {
		t.Fatalf("last = %#v", last)
	}
}

type failing struct{ err error }

func (f failing) Chat(ctx context.Context, req ai.Request) (<-chan ai.Chunk, error) {
	return nil, f.err
}

func TestProviderErrorIsReturned(t *testing.T) {
	boom := errors.New("boom")
	got, err := agent.Execute(context.Background(), baseRun(failing{boom}, &recorder{}), user)
	if !errors.Is(err, boom) || !reflect.DeepEqual(got, user) {
		t.Fatalf("got %#v, err %v", got, err)
	}
}

func TestStreamErrorIsReturned(t *testing.T) {
	boom := errors.New("stream broke")
	p := &scripted{turns: [][]ai.Chunk{{{Delta: "part"}, {Err: boom}}}}
	got, err := agent.Execute(context.Background(), baseRun(p, &recorder{}), user)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if last := got[len(got)-1]; last.Content != "part" || last.Stopped {
		t.Fatalf("partial answer not kept: %#v", last)
	}
}

func TestExecuteRespectsAPerRunStepLimit(t *testing.T) {
	// A provider that always asks for another tool call, so only the step
	// limit can end the run.
	p := &scripted{turns: [][]ai.Chunk{
		{{ToolCalls: []ai.ToolCall{{ID: "c", Name: "list_refs", Args: map[string]any{}}}}, {Done: true}},
	}}
	run := baseRun(p, &recorder{})
	run.MaxSteps = 2

	if _, err := agent.Execute(context.Background(), run, user); err != nil {
		t.Fatal(err)
	}
	if len(p.requests) != 2 {
		t.Fatalf("provider called %d times, want 2 (MaxSteps)", len(p.requests))
	}
}

func TestExecuteDefaultsToTheGlobalStepLimit(t *testing.T) {
	// A provider that always asks for another tool call, so only the step
	// limit can end the run.
	p := &scripted{turns: [][]ai.Chunk{
		{{ToolCalls: []ai.ToolCall{{ID: "c", Name: "list_refs", Args: map[string]any{}}}}, {Done: true}},
	}}
	run := baseRun(p, &recorder{})
	// MaxSteps left at 0

	if _, err := agent.Execute(context.Background(), run, user); err != nil {
		t.Fatal(err)
	}
	if len(p.requests) != agent.MaxSteps {
		t.Fatalf("provider called %d times, want the default %d", len(p.requests), agent.MaxSteps)
	}
}

func TestRecoveredCallRunsAndNoticesThenContinues(t *testing.T) {
	p := &scripted{turns: [][]ai.Chunk{
		{{Delta: `{"name": "list_refs", "arguments": {}}`}, {Done: true}},
		{{Delta: "On main."}, {Done: true}},
	}}
	rec := &recorder{}

	got, err := agent.Execute(context.Background(), baseRun(p, rec), user)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.requests) != 2 {
		t.Fatalf("provider called %d times, want 2 (the run must continue)", len(p.requests))
	}
	assistant := got[1]
	if len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].Name != "list_refs" || assistant.ToolCalls[0].ID == "" {
		t.Fatalf("recovered call not in history: %#v", assistant)
	}
	toolMsg := got[2]
	if toolMsg.Role != ai.RoleTool || toolMsg.ToolName != "list_refs" || toolMsg.Content != "main\nfeature" {
		t.Fatalf("RunTool result not in history: %#v", toolMsg)
	}
	if last := got[len(got)-1]; last.Content != "On main." {
		t.Fatalf("run did not continue to a final answer: %#v", last)
	}
	var haveNotice, haveTool, haveToolResult bool
	for _, n := range rec.names {
		switch n {
		case agent.EventNotice:
			haveNotice = true
		case agent.EventTool:
			haveTool = true
		case agent.EventToolResult:
			haveToolResult = true
		}
	}
	if !haveNotice || !haveTool || !haveToolResult {
		t.Fatalf("events = %v, want chat:notice, chat:tool and chat:tool_result", rec.names)
	}
}

func TestRecoveredUnknownToolEndsRunWithNotice(t *testing.T) {
	p := &scripted{turns: [][]ai.Chunk{
		{{Delta: `{"name": "delete_repo", "arguments": {}}`}, {Done: true}},
	}}
	rec := &recorder{}

	got, err := agent.Execute(context.Background(), baseRun(p, rec), user)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.requests) != 1 {
		t.Fatalf("provider called %d times, want 1 (the run must end)", len(p.requests))
	}
	for _, n := range rec.names {
		if n == agent.EventTool || n == agent.EventToolResult {
			t.Fatalf("RunTool must not run for an unrecoverable call: events = %v", rec.names)
		}
	}
	var haveNotice bool
	for _, n := range rec.names {
		if n == agent.EventNotice {
			haveNotice = true
		}
	}
	if !haveNotice {
		t.Fatalf("events = %v, want a chat:notice", rec.names)
	}
	if last := got[len(got)-1]; last.Role != ai.RoleAssistant || len(last.ToolCalls) != 0 {
		t.Fatalf("last = %#v", last)
	}
}

func TestStructuredCallWinsOverTextualCall(t *testing.T) {
	p := &scripted{turns: [][]ai.Chunk{
		{
			{ToolCalls: []ai.ToolCall{{ID: "c1", Name: "list_refs", Args: map[string]any{}}}, Delta: `{"name": "list_refs", "arguments": {}}`},
			{Done: true},
		},
		{{Delta: "On main."}, {Done: true}},
	}}
	rec := &recorder{}

	got, err := agent.Execute(context.Background(), baseRun(p, rec), user)
	if err != nil {
		t.Fatal(err)
	}
	assistant := got[1]
	if len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].ID != "c1" {
		t.Fatalf("only the structured call should run: %#v", assistant.ToolCalls)
	}
	for _, n := range rec.names {
		if n == agent.EventNotice {
			t.Fatalf("no notice expected when a structured call is present: events = %v", rec.names)
		}
	}
}

func TestPlainTextNoJSONIsUnchanged(t *testing.T) {
	p := &scripted{turns: [][]ai.Chunk{{{Delta: "Hello there."}, {Done: true}}}}
	rec := &recorder{}

	got, err := agent.Execute(context.Background(), baseRun(p, rec), user)
	if err != nil {
		t.Fatal(err)
	}
	if last := got[len(got)-1]; last.Content != "Hello there." || len(last.ToolCalls) != 0 {
		t.Fatalf("last = %#v", last)
	}
	for _, n := range rec.names {
		if n == agent.EventNotice {
			t.Fatalf("no notice expected for plain text: events = %v", rec.names)
		}
	}
}

func TestTrim(t *testing.T) {
	var msgs []ai.Message
	for i := 0; i < 25; i++ {
		msgs = append(msgs,
			ai.Message{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{{ID: fmt.Sprint(i), Name: "t"}}},
			ai.Message{Role: ai.RoleTool, ToolName: "t", Content: fmt.Sprintf("result %d", i)},
		)
	}
	// 51 messages: the 40-message window would start on a tool result.
	msgs = append(msgs, ai.Message{Role: ai.RoleAssistant, Content: "done"})
	got := agent.Trim(msgs)

	if len(got) != agent.HistoryLimit-1 {
		t.Fatalf("len = %d (must drop the leading orphan tool message)", len(got))
	}
	if got[0].Role == ai.RoleTool {
		t.Fatal("trimmed history starts with a tool message")
	}
	for i, m := range got {
		recent := i >= len(got)-agent.KeepToolResults
		if m.Role != ai.RoleTool {
			continue
		}
		if recent && m.Content == agent.OmittedToolResult {
			t.Errorf("recent tool result %d omitted", i)
		}
		if !recent && m.Content != agent.OmittedToolResult {
			t.Errorf("old tool result %d kept: %q", i, m.Content)
		}
	}
	if msgs[len(msgs)-2].Content != "result 24" || msgs[1].Content != "result 0" {
		t.Fatal("Trim modified its input")
	}
}

// A run with Continue goes on when it says there is work left, sending its
// text as a user message, and ends once it has nothing more to say.
func TestContinueKeepsTheRunGoing(t *testing.T) {
	p := &scripted{turns: [][]ai.Chunk{
		{{Delta: "Next I will resolve b."}, {Done: true}},
		{{ToolCalls: []ai.ToolCall{{ID: "c1", Name: "list_refs"}}}, {Done: true}},
		{{Delta: "Done."}, {Done: true}},
	}}
	rec := &recorder{}
	run := baseRun(p, rec)
	asked := 0
	run.Continue = func(context.Context) string {
		asked++
		if asked == 1 {
			return "b is still conflicted; carry on."
		}
		return ""
	}
	got, err := agent.Execute(context.Background(), run, user)
	if err != nil {
		t.Fatal(err)
	}
	if asked != 2 || len(p.requests) != 3 {
		t.Fatalf("asked %d times, %d requests", asked, len(p.requests))
	}
	if !reflect.DeepEqual(got[2], ai.Message{Role: ai.RoleUser, Content: "b is still conflicted; carry on."}) {
		t.Fatalf("history[2] = %#v", got[2])
	}
	if last := got[len(got)-1]; last.Content != "Done." {
		t.Fatalf("last = %#v", last)
	}
	if rec.names[1] != agent.EventNotice {
		t.Fatalf("events = %v", rec.names)
	}
}
