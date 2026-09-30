// Package agent runs the repo chat: it streams model output, executes the
// tools the model asks for and feeds results back until the model answers.
package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"git-ui/internal/ai"
)

const (
	MaxSteps          = 8
	HistoryLimit      = 40
	KeepToolResults   = 10
	OmittedToolResult = "[earlier tool result omitted]"
	StepLimitNote     = "Step limit reached."

	EventStart       = "chat:start"
	EventDelta       = "chat:delta"
	EventTool        = "chat:tool"
	EventToolResult  = "chat:tool_result"
	EventDone        = "chat:done"
	EventError       = "chat:error"
	EventNotice      = "chat:notice"
	EventSuggestions = "chat:suggestions"

	noticeContinue    = "The model stopped with work left; CommitTree asked it to carry on."
	noticeRecovered   = "The model wrote a tool call as text; CommitTree ran it."
	noticeUnrecovered = "The model wrote a tool call as text that CommitTree could not run. Try a model with reliable tool calling."
)

// StartEvent announces a new answer, so the chat can show the question and an
// empty answer even when the run was started from somewhere else (the log's
// "Explain" action).
type StartEvent struct {
	RepoID string `json:"repoID"`
	RunID  string `json:"runID"`
	Text   string `json:"text"`
	// Provider and Model are what will produce the answer.
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

type DeltaEvent struct {
	RepoID string `json:"repoID"`
	RunID  string `json:"runID"`
	Text   string `json:"text"`
}

type ToolEvent struct {
	RepoID string `json:"repoID"`
	RunID  string `json:"runID"`
	// ID is the call's id, unique within the chat's history: a card built
	// from the call (propose_options) is answered by it.
	ID   string         `json:"id"`
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type ToolResultEvent struct {
	RepoID  string `json:"repoID"`
	RunID   string `json:"runID"`
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

type DoneEvent struct {
	RepoID string `json:"repoID"`
	RunID  string `json:"runID"`
	// At is when the answer finished, as stored on it (RFC 3339).
	At string `json:"at,omitempty"`
}

type ErrorEvent struct {
	RepoID  string `json:"repoID"`
	RunID   string `json:"runID"`
	Message string `json:"message"`
	Code    string `json:"code"`
}

// SuggestionsEvent offers replies the user may send next after the answer
// of RunID. It arrives some seconds after chat:done, or never.
type SuggestionsEvent struct {
	RepoID  string   `json:"repoID"`
	RunID   string   `json:"runID"`
	Replies []string `json:"replies"`
}

// NoticeEvent is a transient, informational aside about the run - e.g. that
// the model wrote a tool call as text. It is never added to the message
// history, so it is never fed back to the model.
type NoticeEvent struct {
	RepoID string `json:"repoID"`
	RunID  string `json:"runID"`
	Text   string `json:"text"`
}

type Run struct {
	RepoID, RunID string
	Provider      ai.Provider
	Model, System string
	Tools         []ai.ToolSpec
	// RunTool executes one tool call. step is the index of the model
	// round-trip the call belongs to (all the calls in one model response
	// share the same step), so a caller can tell which calls came from the
	// same assistant response without tracking it itself.
	RunTool func(ctx context.Context, call ai.ToolCall, step int) string
	Emit    func(name string, data any)
	// MaxSteps caps the model/tool rounds for this run; 0 uses MaxSteps.
	// Resolving a merge takes many more rounds than answering a question.
	MaxSteps int
	// Continue, when set, is asked each time the model answers without a
	// tool call. A non-empty reply is sent back as a user message and the
	// run goes on: small models announce "next I will…" and stop there.
	// It decides itself when to give up; "" ends the run.
	Continue func(ctx context.Context) string
}

// Execute continues history (which ends with the user's message) and
// returns the updated history.
func Execute(ctx context.Context, r Run, history []ai.Message) ([]ai.Message, error) {
	steps := r.MaxSteps
	if steps <= 0 {
		steps = MaxSteps
	}
	msgs := append([]ai.Message(nil), history...)
	used := map[string]bool{}
	for _, m := range msgs {
		for _, c := range m.ToolCalls {
			used[c.ID] = true
		}
	}
	for step := 0; step < steps; step++ {
		stream, err := r.Provider.Chat(ctx, ai.Request{Model: r.Model, System: r.System, Messages: Trim(msgs), Tools: r.Tools})
		if err != nil {
			if ctx.Err() != nil {
				return append(msgs, ai.Message{Role: ai.RoleAssistant, Stopped: true}), ctx.Err()
			}
			return msgs, err
		}

		var text strings.Builder
		var calls []ai.ToolCall
		var streamErr error
		done := false
		for chunk := range stream {
			if chunk.Err != nil {
				streamErr = chunk.Err
				continue
			}
			if chunk.Delta != "" {
				text.WriteString(chunk.Delta)
				r.Emit(EventDelta, DeltaEvent{RepoID: r.RepoID, RunID: r.RunID, Text: chunk.Delta})
			}
			calls = append(calls, chunk.ToolCalls...)
			if chunk.Done {
				done = true
			}
		}

		if ctx.Err() != nil {
			return append(msgs, ai.Message{Role: ai.RoleAssistant, Content: text.String(), Stopped: true}), ctx.Err()
		}
		if streamErr != nil || !done {
			if streamErr == nil {
				streamErr = errors.New("agent: response ended unexpectedly")
			}
			if text.Len() > 0 {
				msgs = append(msgs, ai.Message{Role: ai.RoleAssistant, Content: text.String()})
			}
			return msgs, streamErr
		}

		// Some models write a tool call into the text instead of returning
		// it structured. Only look when the model returned no structured
		// calls at all - a structured call always wins over scanning the
		// text.
		if len(calls) == 0 {
			if recovered, attempted := recoverCalls(text.String(), r.Tools); len(recovered) > 0 {
				for i := range recovered {
					recovered[i].ID = fmt.Sprintf("recovered_%s_%d_%d", r.RunID, step, i)
				}
				calls = recovered
				r.Emit(EventNotice, NoticeEvent{RepoID: r.RepoID, RunID: r.RunID, Text: noticeRecovered})
			} else if attempted {
				r.Emit(EventNotice, NoticeEvent{RepoID: r.RepoID, RunID: r.RunID, Text: noticeUnrecovered})
			}
		}

		// Ids must be unique across the whole history (a decision card is
		// answered by its call's id); a provider may send none, or reuse
		// one from an earlier run (Ollama's "call_1").
		for i := range calls {
			if calls[i].ID == "" || used[calls[i].ID] {
				calls[i].ID = fmt.Sprintf("call_%s_%d_%d", r.RunID, step, i)
			}
			used[calls[i].ID] = true
		}
		msgs = append(msgs, ai.Message{Role: ai.RoleAssistant, Content: text.String(), ToolCalls: calls})
		if len(calls) == 0 {
			if r.Continue != nil {
				if more := r.Continue(ctx); more != "" {
					r.Emit(EventNotice, NoticeEvent{RepoID: r.RepoID, RunID: r.RunID, Text: noticeContinue})
					msgs = append(msgs, ai.Message{Role: ai.RoleUser, Content: more})
					continue
				}
			}
			return msgs, nil
		}

		for _, call := range calls {
			r.Emit(EventTool, ToolEvent{RepoID: r.RepoID, RunID: r.RunID, ID: call.ID, Name: call.Name, Args: call.Args})
			result := r.RunTool(ctx, call, step)
			r.Emit(EventToolResult, ToolResultEvent{RepoID: r.RepoID, RunID: r.RunID, Name: call.Name, Summary: summarize(result)})
			msgs = append(msgs, ai.Message{Role: ai.RoleTool, ToolName: call.Name, Content: result})
		}
	}

	r.Emit(EventDelta, DeltaEvent{RepoID: r.RepoID, RunID: r.RunID, Text: StepLimitNote})
	return append(msgs, ai.Message{Role: ai.RoleAssistant, Content: StepLimitNote}), nil
}

// Trim returns the part of the history sent to the model: at most
// HistoryLimit messages, never starting with a tool message, with tool
// results outside the last KeepToolResults messages replaced by a marker.
func Trim(msgs []ai.Message) []ai.Message {
	start := max(0, len(msgs)-HistoryLimit)
	for start < len(msgs) && msgs[start].Role == ai.RoleTool {
		start++
	}
	out := append([]ai.Message(nil), msgs[start:]...)
	for i := 0; i < len(out)-KeepToolResults; i++ {
		if out[i].Role == ai.RoleTool {
			out[i].Content = OmittedToolResult
		}
	}
	return out
}

func summarize(result string) string {
	line, _, _ := strings.Cut(result, "\n")
	if utf8.RuneCountInString(line) > 120 {
		line = string([]rune(line)[:120]) + "…"
	}
	return line
}
