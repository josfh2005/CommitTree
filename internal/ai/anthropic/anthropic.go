// Package anthropic adapts the Anthropic Messages API to the app's
// provider-neutral interfaces.
package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"git-ui/internal/ai"
)

// MaxTokens is generous because answers carry resolved file regions; the
// request streams, so a large ceiling costs nothing when unused.
const MaxTokens = 64000

type Client struct {
	sdk sdk.Client
}

type Option func(*[]option.RequestOption)

// WithBaseURL points the client at another host; tests use it.
func WithBaseURL(u string) Option {
	return func(opts *[]option.RequestOption) { *opts = append(*opts, option.WithBaseURL(u)) }
}

func New(apiKey string, opts ...Option) *Client {
	req := []option.RequestOption{option.WithAPIKey(apiKey)}
	for _, o := range opts {
		o(&req)
	}
	return &Client{sdk: sdk.NewClient(req...)}
}

// Chat streams one assistant turn. Text arrives as deltas; a tool call is
// emitted once its block closes, because its arguments stream as partial
// JSON that is only valid when complete.
func (c *Client) Chat(ctx context.Context, req ai.Request) (<-chan ai.Chunk, error) {
	params := sdk.MessageNewParams{
		Model:     sdk.Model(req.Model),
		MaxTokens: MaxTokens,
		Thinking:  sdk.ThinkingConfigParamUnion{OfAdaptive: &sdk.ThinkingConfigAdaptiveParam{}},
		Messages:  messages(req.Messages),
	}
	if req.System != "" {
		params.System = []sdk.TextBlockParam{{Text: req.System}}
	}
	for _, t := range req.Tools {
		params.Tools = append(params.Tools, sdk.ToolUnionParam{OfTool: &sdk.ToolParam{
			Name:        t.Name,
			Description: sdk.String(t.Description),
			InputSchema: sdk.ToolInputSchemaParam{
				Properties: t.Parameters["properties"],
				Required:   requiredFields(t.Parameters),
			},
		}})
	}

	stream := c.sdk.Messages.NewStreaming(ctx, params)
	ch := make(chan ai.Chunk)
	go func() {
		defer close(ch)
		defer stream.Close()
		message := sdk.Message{}
		emitted := map[int]bool{}
		for stream.Next() {
			event := stream.Current()
			if err := message.Accumulate(event); err != nil {
				send(ctx, ch, ai.Chunk{Err: fmt.Errorf("anthropic: %w", err)})
				return
			}
			switch e := event.AsAny().(type) {
			case sdk.ContentBlockDeltaEvent:
				if d, ok := e.Delta.AsAny().(sdk.TextDelta); ok && d.Text != "" {
					if !send(ctx, ch, ai.Chunk{Delta: d.Text}) {
						return
					}
				}
			case sdk.ContentBlockStopEvent:
				if call, ok := toolCall(message, int(e.Index)); ok {
					emitted[int(e.Index)] = true
					if !send(ctx, ch, ai.Chunk{ToolCalls: []ai.ToolCall{call}}) {
						return
					}
				}
			}
		}
		if err := stream.Err(); err != nil {
			send(ctx, ch, ai.Chunk{Err: classify(err)})
			return
		}
		// A tool_use block whose content_block_stop never arrived (the
		// stream ended right after message_stop) would otherwise be
		// silently dropped; emit it here from the accumulated message.
		for i := range message.Content {
			if emitted[i] {
				continue
			}
			if call, ok := toolCall(message, i); ok {
				if !send(ctx, ch, ai.Chunk{ToolCalls: []ai.ToolCall{call}}) {
					return
				}
			}
		}
		send(ctx, ch, ai.Chunk{Done: true})
	}()
	return ch, nil
}

// requiredFields reads the "required" entry of a JSON Schema parameters
// object. Callers populate it as []string (internal/ai/tools) or []any (a
// schema decoded from JSON), so both are handled.
func requiredFields(params map[string]any) []string {
	switch v := params["required"].(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, e := range v {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// toolCall reads the accumulated block at index when it is a completed tool
// use.
func toolCall(m sdk.Message, index int) (ai.ToolCall, bool) {
	if index < 0 || index >= len(m.Content) {
		return ai.ToolCall{}, false
	}
	block, ok := m.Content[index].AsAny().(sdk.ToolUseBlock)
	if !ok {
		return ai.ToolCall{}, false
	}
	args := map[string]any{}
	if len(block.Input) > 0 {
		if err := json.Unmarshal(block.Input, &args); err != nil {
			args = map[string]any{}
		}
	}
	return ai.ToolCall{ID: block.ID, Name: block.Name, Args: args}, true
}

// leadWithUser makes sure the list handed to the API starts with a user
// message, as the Messages API requires. agent.Trim caps history at a fixed
// window and only skips leading RoleTool messages; one agent run has
// exactly one RoleUser message followed by ~2*MergeMaxSteps
// assistant/tool-result messages, so once a run is long enough (a merge
// resolution, whose MergeMaxSteps appends ~60 messages), Trim's window
// slides past that single RoleUser entirely and starts on RoleAssistant -
// there is no RoleUser left to slice to. If one survives, start there,
// dropping anything before it (a leading assistant's now-orphaned tool
// calls included - their RoleTool results are dropped separately in
// messages, since they'd otherwise reference a tool_use no longer sent).
// If none survives, synthesize a leading user turn rather than send an
// empty or user-less list, both of which the API also rejects.
func leadWithUser(history []ai.Message) []ai.Message {
	for i, m := range history {
		if m.Role == ai.RoleUser {
			return history[i:]
		}
	}
	if len(history) == 0 {
		return history
	}
	return append([]ai.Message{syntheticLead(history)}, history...)
}

// syntheticLead stands in for a RoleUser message that Trim's window no
// longer contains. It is deterministic - no summarizing - and just quotes
// the first remaining assistant text (if any) so the model has some anchor
// for what it was doing, plus a note that earlier turns were trimmed.
func syntheticLead(history []ai.Message) ai.Message {
	for _, m := range history {
		if m.Role == ai.RoleAssistant && m.Content != "" {
			return ai.Message{Role: ai.RoleUser, Content: fmt.Sprintf(
				"[Earlier turns were trimmed from this conversation. The assistant had last said: %q. Continue the work.]",
				m.Content,
			)}
		}
	}
	return ai.Message{Role: ai.RoleUser, Content: "[Earlier turns were trimmed from this conversation. Continue the work.]"}
}

// messages converts the app's history. Every tool result for one assistant
// turn goes into a single user message, which is what the API expects. A
// RoleTool message carries only the tool's name (see ai.Message), so its
// tool_use_id is recovered positionally from the ToolCalls of the assistant
// message that precedes it — the agent always appends results in call
// order, immediately after that assistant turn.
func messages(history []ai.Message) []sdk.MessageParam {
	history = leadWithUser(history)
	var out []sdk.MessageParam
	var pendingResults []sdk.ContentBlockParamUnion
	var pendingCalls []ai.ToolCall
	flush := func() {
		if len(pendingResults) > 0 {
			out = append(out, sdk.NewUserMessage(pendingResults...))
			pendingResults = nil
		}
	}
	for _, m := range history {
		switch m.Role {
		case ai.RoleTool:
			// A tool result with no pending call means its assistant turn
			// was dropped from the window (or, now, replaced by the
			// synthetic lead) - sending it would reference a tool_use id
			// the request never includes, which the API rejects. Drop it
			// rather than guess an id from the tool's name.
			if len(pendingCalls) == 0 {
				continue
			}
			id := pendingCalls[0].ID
			pendingCalls = pendingCalls[1:]
			pendingResults = append(pendingResults, sdk.NewToolResultBlock(id, m.Content, false))
		case ai.RoleUser:
			flush()
			pendingCalls = nil
			// The API rejects an empty text block; drop an empty user turn
			// rather than send one.
			if m.Content != "" {
				out = append(out, sdk.NewUserMessage(sdk.NewTextBlock(m.Content)))
			}
		case ai.RoleAssistant:
			flush()
			pendingCalls = append([]ai.ToolCall(nil), m.ToolCalls...)
			blocks := []sdk.ContentBlockParamUnion{}
			if m.Content != "" {
				blocks = append(blocks, sdk.NewTextBlock(m.Content))
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, sdk.ContentBlockParamUnion{OfToolUse: &sdk.ToolUseBlockParam{
					ID: tc.ID, Name: tc.Name, Input: tc.Args,
				}})
			}
			if len(blocks) > 0 {
				out = append(out, sdk.NewAssistantMessage(blocks...))
			}
		}
	}
	flush()
	return out
}

// Responder adapts the client to single-prompt tasks.
func (c *Client) Responder(model string) ai.Responder { return responder{c: c, model: model} }

type responder struct {
	c     *Client
	model string
}

func (r responder) Respond(ctx context.Context, instructions, prompt string) (<-chan ai.Chunk, error) {
	return r.c.Chat(ctx, ai.Request{
		Model:    r.model,
		System:   instructions,
		Messages: []ai.Message{{Role: ai.RoleUser, Content: prompt}},
	})
}

// ListModels returns the model ids the key can use, newest first as the API
// returns them. It pages through the full list via the SDK's auto-pager.
func (c *Client) ListModels(ctx context.Context) ([]string, error) {
	out := []string{}
	iter := c.sdk.Models.ListAutoPaging(ctx, sdk.ModelListParams{})
	for iter.Next() {
		out = append(out, iter.Current().ID)
	}
	if err := iter.Err(); err != nil {
		return nil, classify(err)
	}
	return out, nil
}

// classify turns an SDK error into one the chat can show without leaking the
// request or the key.
func classify(err error) error {
	var apierr *sdk.Error
	if !errors.As(err, &apierr) {
		return fmt.Errorf("anthropic: %w", err)
	}
	// Out of credit arrives as a 400 like any other rejected request, whose
	// raw body would otherwise be pasted into the chat verbatim.
	if strings.Contains(strings.ToLower(apierr.Error()), "credit balance") {
		return errors.New("anthropic reports no available credit.")
	}
	switch apierr.StatusCode {
	case 401, 403:
		return errors.New("anthropic rejected the API key")
	case 429:
		return errors.New("anthropic is rate limiting; try again in a moment")
	case 400:
		return fmt.Errorf("anthropic rejected the request: %s", apierr.Error())
	default:
		return fmt.Errorf("anthropic returned %d", apierr.StatusCode)
	}
}

func send(ctx context.Context, ch chan<- ai.Chunk, c ai.Chunk) bool {
	select {
	case ch <- c:
		return true
	case <-ctx.Done():
		return false
	}
}
