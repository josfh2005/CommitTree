// Package openai adapts the OpenAI chat completions API to the app's
// provider-neutral interfaces.
package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	sdk "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"

	"git-ui/internal/ai"
)

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

// pending accumulates one streamed tool call: the arguments arrive as JSON
// fragments split across chunks and are only valid once joined.
type pending struct {
	id   string
	name string
	args strings.Builder
}

// Chat streams one assistant turn. Text arrives as deltas; tool calls are
// accumulated across chunks (their arguments stream as JSON fragments) and
// emitted once the stream reports a finish reason.
func (c *Client) Chat(ctx context.Context, req ai.Request) (<-chan ai.Chunk, error) {
	params := sdk.ChatCompletionNewParams{
		Model:    sdk.ChatModel(req.Model),
		Messages: messages(req.System, req.Messages),
	}
	for _, t := range req.Tools {
		params.Tools = append(params.Tools, sdk.ChatCompletionToolParam{
			Function: sdk.FunctionDefinitionParam{
				Name:        t.Name,
				Description: sdk.String(t.Description),
				Parameters:  sdk.FunctionParameters(t.Parameters),
			},
		})
	}

	stream := c.sdk.Chat.Completions.NewStreaming(ctx, params)
	ch := make(chan ai.Chunk)
	go func() {
		defer close(ch)
		defer stream.Close()
		calls := map[int64]*pending{}
		order := []int64{}
		finished := false
		for stream.Next() {
			chunk := stream.Current()
			if len(chunk.Choices) == 0 {
				continue
			}
			delta := chunk.Choices[0].Delta
			if delta.Content != "" {
				if !send(ctx, ch, ai.Chunk{Delta: delta.Content}) {
					return
				}
			}
			for _, tc := range delta.ToolCalls {
				p := calls[tc.Index]
				if p == nil {
					p = &pending{}
					calls[tc.Index] = p
					order = append(order, tc.Index)
				}
				if tc.ID != "" {
					p.id = tc.ID
				}
				if tc.Function.Name != "" {
					p.name = tc.Function.Name
				}
				p.args.WriteString(tc.Function.Arguments)
			}
			if chunk.Choices[0].FinishReason != "" {
				finished = true
				for _, call := range finish(calls, order) {
					if !send(ctx, ch, ai.Chunk{ToolCalls: []ai.ToolCall{call}}) {
						return
					}
				}
				send(ctx, ch, ai.Chunk{Done: true})
				return
			}
		}
		if err := stream.Err(); err != nil {
			send(ctx, ch, ai.Chunk{Err: classify(err)})
			return
		}
		// The stream ended without a finish_reason (dropped connection,
		// server bug); any tool call the model was in the middle of asking
		// for would otherwise be silently lost.
		if !finished {
			for _, call := range finish(calls, order) {
				if !send(ctx, ch, ai.Chunk{ToolCalls: []ai.ToolCall{call}}) {
					return
				}
			}
		}
		send(ctx, ch, ai.Chunk{Done: true})
	}()
	return ch, nil
}

// finish turns the accumulated fragments into calls, in the order their
// index first appeared.
func finish(calls map[int64]*pending, order []int64) []ai.ToolCall {
	out := []ai.ToolCall{}
	for _, i := range order {
		p := calls[i]
		if p == nil || p.name == "" {
			continue
		}
		args := map[string]any{}
		if s := p.args.String(); s != "" {
			if err := json.Unmarshal([]byte(s), &args); err != nil {
				args = map[string]any{}
			}
		}
		out = append(out, ai.ToolCall{ID: p.id, Name: p.name, Args: args})
	}
	return out
}

// messages converts the app's history. A RoleTool message carries only the
// tool's name (see ai.Message), so its tool_call_id is recovered
// positionally from the ToolCalls of the assistant message that precedes it
// — the agent always appends results in call order, immediately after that
// assistant turn. Matching by name instead would pair both results of a
// two-call turn to the same id when the calls share a name.
func messages(system string, history []ai.Message) []sdk.ChatCompletionMessageParamUnion {
	out := []sdk.ChatCompletionMessageParamUnion{}
	if system != "" {
		out = append(out, sdk.SystemMessage(system))
	}
	var pendingCalls []ai.ToolCall
	for _, m := range history {
		switch m.Role {
		case ai.RoleUser:
			pendingCalls = nil
			out = append(out, sdk.UserMessage(m.Content))
		case ai.RoleTool:
			id := m.ToolName
			if len(pendingCalls) > 0 {
				id = pendingCalls[0].ID
				pendingCalls = pendingCalls[1:]
			}
			out = append(out, sdk.ToolMessage(m.Content, id))
		case ai.RoleAssistant:
			pendingCalls = append([]ai.ToolCall(nil), m.ToolCalls...)
			msg := sdk.ChatCompletionAssistantMessageParam{}
			if m.Content != "" {
				msg.Content.OfString = sdk.String(m.Content)
			}
			for _, tc := range m.ToolCalls {
				args, _ := json.Marshal(tc.Args)
				msg.ToolCalls = append(msg.ToolCalls, sdk.ChatCompletionMessageToolCallParam{
					ID: tc.ID,
					Function: sdk.ChatCompletionMessageToolCallFunctionParam{
						Name: tc.Name, Arguments: string(args),
					},
				})
			}
			// An assistant message with neither content nor tool calls
			// serializes as a bare {"role":"assistant"}; the agent saves
			// exactly that when a run is stopped before the first delta.
			// Skip it rather than send it.
			if m.Content != "" || len(m.ToolCalls) > 0 {
				out = append(out, sdk.ChatCompletionMessageParamUnion{OfAssistant: &msg})
			}
		}
	}
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

// ListModels returns the chat models the key can use. The raw list also holds
// embedding, image and audio models, which would only clutter the dropdown.
func (c *Client) ListModels(ctx context.Context) ([]string, error) {
	page, err := c.sdk.Models.List(ctx)
	if err != nil {
		return nil, classify(err)
	}
	out := []string{}
	for _, m := range page.Data {
		if isChatModel(m.ID) {
			out = append(out, m.ID)
		}
	}
	return out, nil
}

// isChatModel keeps the families that can hold a conversation with tools.
// o1 and o3 are excluded even though they can chat: messages() always emits
// a "system" role message when req.System is set, and the older o-series
// rejects that role outright (it wants "developer" or no system message at
// all), so listing them here would only offer a model that errors on the
// first turn. o4 accepts "system" and stays.
func isChatModel(id string) bool {
	for _, prefix := range []string{"gpt-", "o4", "chatgpt-"} {
		if strings.HasPrefix(id, prefix) {
			return true
		}
	}
	return false
}

// classify turns an SDK error into one the chat can show without leaking the
// request or the key.
func classify(err error) error {
	var apierr *sdk.Error
	if !errors.As(err, &apierr) {
		return fmt.Errorf("openai: %w", err)
	}
	// Out of credit arrives as a 429 like a rate limit, but "try again in a
	// moment" is wrong for something that never resolves on its own.
	if apierr.Code == "insufficient_quota" {
		return errors.New("openai reports no available credit.")
	}
	switch apierr.StatusCode {
	case 401, 403:
		return errors.New("openai rejected the API key")
	case 429:
		return errors.New("openai is rate limiting; try again in a moment")
	case 400:
		return fmt.Errorf("openai rejected the request: %s", apierr.Error())
	default:
		return fmt.Errorf("openai returned %d", apierr.StatusCode)
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
