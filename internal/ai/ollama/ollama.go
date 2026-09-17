// Package ollama is a client for the native Ollama HTTP API.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"git-ui/internal/ai"
)

var (
	ErrUnreachable   = errors.New("ollama is not reachable")
	ErrModelNotFound = errors.New("model not found")
	ErrNoToolSupport = errors.New("model does not support tools")
)

type Model struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type Client struct {
	baseURL string
	http    *http.Client
}

func New(baseURL string) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: &http.Client{}}
}

type wireFunction struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type wireToolCall struct {
	ID       string       `json:"id,omitempty"`
	Function wireFunction `json:"function"`
}

type wireMessage struct {
	Role      string         `json:"role"`
	Content   string         `json:"content"`
	ToolCalls []wireToolCall `json:"tool_calls,omitempty"`
	ToolName  string         `json:"tool_name,omitempty"`
}

type wireTool struct {
	Type     string      `json:"type"`
	Function ai.ToolSpec `json:"function"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []wireMessage `json:"messages"`
	Tools    []wireTool    `json:"tools,omitempty"`
	Stream   bool          `json:"stream"`
}

type chatLine struct {
	Message wireMessage `json:"message"`
	Done    bool        `json:"done"`
	Error   string      `json:"error"`
}

func (c *Client) ListModels(ctx context.Context) ([]Model, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var body struct {
		Models []Model `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("ollama: decode models: %w", err)
	}
	if body.Models == nil {
		body.Models = []Model{}
	}
	return body.Models, nil
}

func (c *Client) Chat(ctx context.Context, req ai.Request) (<-chan ai.Chunk, error) {
	body := chatRequest{Model: req.Model, Stream: true}
	if req.System != "" {
		body.Messages = append(body.Messages, wireMessage{Role: string(ai.RoleSystem), Content: req.System})
	}
	for _, m := range req.Messages {
		wm := wireMessage{Role: string(m.Role), Content: m.Content, ToolName: m.ToolName}
		for _, tc := range m.ToolCalls {
			args := tc.Args
			if args == nil {
				args = map[string]any{}
			}
			wm.ToolCalls = append(wm.ToolCalls, wireToolCall{ID: tc.ID, Function: wireFunction{Name: tc.Name, Arguments: args}})
		}
		body.Messages = append(body.Messages, wm)
	}
	for _, t := range req.Tools {
		body.Tools = append(body.Tools, wireTool{Type: "function", Function: t})
	}
	resp, err := c.post(ctx, "/api/chat", body)
	if err != nil {
		return nil, err
	}

	ch := make(chan ai.Chunk)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		dec := json.NewDecoder(resp.Body)
		for {
			var line chatLine
			if err := dec.Decode(&line); err != nil {
				if ctx.Err() != nil {
					err = ctx.Err()
				} else if errors.Is(err, io.EOF) {
					err = errors.New("ollama: response ended unexpectedly")
				}
				send(ctx, ch, ai.Chunk{Err: err})
				return
			}
			if line.Error != "" {
				send(ctx, ch, ai.Chunk{Err: classify(line.Error)})
				return
			}
			chunk := ai.Chunk{Delta: line.Message.Content, Done: line.Done}
			for _, tc := range line.Message.ToolCalls {
				chunk.ToolCalls = append(chunk.ToolCalls, ai.ToolCall{ID: tc.ID, Name: tc.Function.Name, Args: tc.Function.Arguments})
			}
			if chunk.Delta != "" || len(chunk.ToolCalls) > 0 || chunk.Done {
				if !send(ctx, ch, chunk) {
					return
				}
			}
			if line.Done {
				return
			}
		}
	}()
	return ch, nil
}

func (c *Client) Pull(ctx context.Context, name string, progress func(status string, completed, total int64)) error {
	resp, err := c.post(ctx, "/api/pull", map[string]any{"model": name, "stream": true})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	dec := json.NewDecoder(resp.Body)
	for {
		var line struct {
			Status    string `json:"status"`
			Total     int64  `json:"total"`
			Completed int64  `json:"completed"`
			Error     string `json:"error"`
		}
		if err := dec.Decode(&line); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, io.EOF) {
				return errors.New("ollama: download ended before it finished")
			}
			return err
		}
		if line.Error != "" {
			return classify(line.Error)
		}
		progress(line.Status, line.Completed, line.Total)
		if line.Status == "success" {
			return nil
		}
	}
}

// Responder adapts the client to single-prompt tasks with the given model.
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

func (c *Client) post(ctx context.Context, path string, body any) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(ctx, req)
}

func (c *Client) do(ctx context.Context, req *http.Request) (*http.Response, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w at %s: %v", ErrUnreachable, c.baseURL, err)
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return nil, classify(e.Error)
	}
	return resp, nil
}

func classify(msg string) error {
	switch {
	case strings.Contains(msg, "does not support tools"):
		return fmt.Errorf("%w: %s", ErrNoToolSupport, msg)
	case strings.Contains(msg, "not found"):
		return fmt.Errorf("%w: %s", ErrModelNotFound, msg)
	}
	return fmt.Errorf("ollama: %s", msg)
}

func send(ctx context.Context, ch chan<- ai.Chunk, c ai.Chunk) bool {
	select {
	case ch <- c:
		return true
	case <-ctx.Done():
		return false
	}
}
