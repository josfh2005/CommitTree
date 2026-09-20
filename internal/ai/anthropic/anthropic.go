// Package anthropic adapts the Anthropic Messages API to the app's
// provider-neutral interfaces.
package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"git-ui/internal/ai"
)

// MaxTokens is generous because answers carry resolved file regions; the
// request streams, so a large ceiling costs nothing when unused.
const MaxTokens = 64000

// defaultBaseURL is the production Anthropic API host; WithBaseURL overrides
// it for tests.
const defaultBaseURL = "https://api.anthropic.com"

type Client struct {
	sdk     sdk.Client
	apiKey  string
	baseURL string
	http    *http.Client
}

type Option func(*Client, *[]option.RequestOption)

// WithBaseURL points the client at another host; tests use it.
func WithBaseURL(u string) Option {
	return func(c *Client, opts *[]option.RequestOption) {
		c.baseURL = strings.TrimRight(u, "/")
		*opts = append(*opts, option.WithBaseURL(u))
	}
}

func New(apiKey string, opts ...Option) *Client {
	c := &Client{apiKey: apiKey, baseURL: defaultBaseURL, http: &http.Client{}}
	req := []option.RequestOption{option.WithAPIKey(apiKey)}
	for _, o := range opts {
		o(c, &req)
	}
	c.sdk = sdk.NewClient(req...)
	return c
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
			InputSchema: sdk.ToolInputSchemaParam{Properties: t.Parameters["properties"]},
		}})
	}

	stream := c.sdk.Messages.NewStreaming(ctx, params)
	ch := make(chan ai.Chunk)
	go func() {
		defer close(ch)
		message := sdk.Message{}
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
		send(ctx, ch, ai.Chunk{Done: true})
	}()
	return ch, nil
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

// messages converts the app's history. Every tool result for one assistant
// turn goes into a single user message, which is what the API expects.
func messages(history []ai.Message) []sdk.MessageParam {
	var out []sdk.MessageParam
	var pendingResults []sdk.ContentBlockParamUnion
	flush := func() {
		if len(pendingResults) > 0 {
			out = append(out, sdk.NewUserMessage(pendingResults...))
			pendingResults = nil
		}
	}
	for _, m := range history {
		switch m.Role {
		case ai.RoleTool:
			pendingResults = append(pendingResults, sdk.NewToolResultBlock(m.ToolName, m.Content, false))
		case ai.RoleUser:
			flush()
			out = append(out, sdk.NewUserMessage(sdk.NewTextBlock(m.Content)))
		case ai.RoleAssistant:
			flush()
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
// returns them.
//
// This bypasses the SDK's typed Models.List call: the SDK's JSON decoder
// requires an exact "application/json" content-type header, which the real
// API always sends but which is not guaranteed by every proxy or test
// double in front of it. A plain HTTP GET decoded by hand is more robust.
func (c *Client) ListModels(ctx context.Context) ([]string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("anthropic: %w", err)
	}
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: %w", err)
	}
	defer resp.Body.Close()

	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("anthropic: decode models: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, classifyStatus(resp.StatusCode, body.Error.Message)
	}

	out := []string{}
	for _, m := range body.Data {
		out = append(out, m.ID)
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

// classifyStatus turns a raw HTTP status and API error message into an
// error the chat can show without leaking the request or the key.
func classifyStatus(statusCode int, msg string) error {
	switch statusCode {
	case 401, 403:
		return errors.New("anthropic rejected the API key")
	case 429:
		return errors.New("anthropic is rate limiting; try again in a moment")
	case 400:
		return fmt.Errorf("anthropic rejected the request: %s", msg)
	default:
		return fmt.Errorf("anthropic returned %d", statusCode)
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
