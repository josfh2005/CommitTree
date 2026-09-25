// Package ai defines provider-neutral types for the app's AI features.
package ai

import "context"

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type ToolCall struct {
	ID   string         `json:"id"`
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type Message struct {
	Role      Role       `json:"role"`
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"toolCalls,omitempty"`
	ToolName  string     `json:"toolName,omitempty"`
	Stopped   bool       `json:"stopped,omitempty"`
	// Provider and Model name what produced an assistant message, so the
	// chat can show it; empty on other roles and on messages stored before
	// they were recorded.
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}

// ToolSpec describes a callable tool; Parameters is a JSON Schema object.
type ToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type Request struct {
	Model    string
	System   string
	Messages []Message
	Tools    []ToolSpec
}

// Chunk is one piece of a streamed response. A stream ends by closing the
// channel, normally right after a chunk with Done or Err set.
type Chunk struct {
	Delta     string
	ToolCalls []ToolCall
	Done      bool
	Err       error
}

// Provider holds multi-turn conversations with tool calling.
type Provider interface {
	Chat(ctx context.Context, req Request) (<-chan Chunk, error)
}

// Responder answers a single prompt under fixed instructions.
type Responder interface {
	Respond(ctx context.Context, instructions, prompt string) (<-chan Chunk, error)
}
