# Chat Token Counts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show tokens in/out per chat answer (answer row) and per conversation (composer bar, after the model picker).

**Architecture:** Each provider puts a neutral `ai.Usage` on its final `Done` chunk. The agent loop (and the explanation path) stores it on the assistant message of that model call and emits `chat:usage`. The frontend sums per answer item and per conversation. Persistence is free: usage lives in the stored messages.

**Tech Stack:** Go (Wails backend, `internal/`), openai-go v1.12.0, anthropic-sdk-go v1.74.0, Svelte + TypeScript frontend (`frontend/src`), vitest.

**Spec:** `docs/superpowers/specs/2026-10-01-chat-token-counts-design.md`

## Global Constraints

- "In" = everything the model read on a call, cache included (Anthropic: input + cache_read + cache_creation; OpenAI: prompt_tokens; Ollama: prompt_eval_count).
- Unknown is not zero: no usage → nil / absent, left out of sums, never shown as 0.
- Counted: chat turns (every agent model call), explanations, conflict resolver. Not counted: suggested replies, commit messages.
- No money cost. Cache detail and call count only in tooltips.
- Number format: `<1000` as is, then `1.2k`, then `1.2M`, one decimal, trailing `.0` dropped.
- Behaviour change updates `docs/spec/06-ai.md` in the same commit (Task 7).
- Merges into main use `git merge --no-ff`. After the change, rebuild and reopen the app with `make dev` (node 22).
- Commits carry no `Co-Authored-By` line.

## Review Focus

- OpenAI: a stream error *after* `finish_reason` must still deliver the answer and tool calls (Done, nil usage), not an error. Pinned in Task 2.
- OpenAI-compatible servers that never send a usage chunk: Done arrives with nil usage. Pinned in Task 2.
- Old conversations stored before this change (assistant messages without `usage`): composer total hidden or flagged `missing`, never 0. Pinned in Task 6.
- Ollama final line without count fields → nil, not `{0,0}`. Pinned in Task 1.
- Live run: `chat:usage` events for a run that is not the panel's current run (other repo, stale run) must be ignored. Pinned in Task 6.

---

### Task 1: Neutral usage type + Ollama

**Files:**
- Modify: `internal/ai/ai.go` (types `Message`, `Chunk`)
- Modify: `internal/ai/ollama/ollama.go` (`chatLine`, `Chat` goroutine)
- Test: `internal/ai/ollama/ollama_test.go`

**Interfaces:**
- Produces: `ai.Usage{Input, Output, CacheRead, CacheWrite int}`; `ai.Chunk.Usage *ai.Usage`; `ai.Message.Usage *ai.Usage` (`json:"usage,omitempty"`).

- [ ] **Step 1: Write the failing tests** (append to `ollama_test.go`)

```go
func TestChatReportsUsageOnTheFinalLine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"message":{"role":"assistant","content":"hi"},"done":false}`)
		fmt.Fprintln(w, `{"message":{"content":""},"done":true,"prompt_eval_count":120,"eval_count":7}`)
	}))
	defer srv.Close()
	ch, err := ollama.New(srv.URL).Chat(context.Background(), ai.Request{Model: "m", Messages: []ai.Message{{Role: ai.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	var usage *ai.Usage
	for c := range ch {
		if c.Done {
			usage = c.Usage
		}
	}
	if usage == nil || *usage != (ai.Usage{Input: 120, Output: 7}) {
		t.Fatalf("usage = %#v", usage)
	}
}

func TestChatWithoutCountsReportsNoUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"message":{"role":"assistant","content":"hi"},"done":true}`)
	}))
	defer srv.Close()
	ch, err := ollama.New(srv.URL).Chat(context.Background(), ai.Request{Model: "m", Messages: []ai.Message{{Role: ai.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	for c := range ch {
		if c.Done && c.Usage != nil {
			t.Fatalf("usage = %#v, want nil", c.Usage)
		}
	}
}
```

Add `"fmt"` to the imports if missing.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/ai/ollama/ -run Usage`
Expected: compile error, `c.Usage undefined`.

- [ ] **Step 3: Implement**

In `internal/ai/ai.go` add after `ToolSpec`:

```go
// Usage is what one model call consumed, in tokens. Input is everything the
// model read, cached tokens included; CacheRead and CacheWrite are the parts
// of Input served from or written to the provider's prompt cache.
type Usage struct {
	Input      int `json:"input"`
	Output     int `json:"output"`
	CacheRead  int `json:"cacheRead,omitempty"`
	CacheWrite int `json:"cacheWrite,omitempty"`
}
```

In `Message`, after `At`:

```go
	// Usage is what the model call that produced an assistant message
	// consumed; nil when the provider reported nothing or the message was
	// stored before it was recorded.
	Usage *Usage `json:"usage,omitempty"`
```

In `Chunk`, after `Done`:

```go
	// Usage is set on the Done chunk when the provider reported it.
	Usage *Usage
```

In `ollama.go`, `chatLine` gains:

```go
	PromptEvalCount *int `json:"prompt_eval_count"`
	EvalCount       *int `json:"eval_count"`
```

and in `Chat`, right after `chunk := ai.Chunk{Delta: line.Message.Content, Done: line.Done}`:

```go
			if line.Done && (line.PromptEvalCount != nil || line.EvalCount != nil) {
				chunk.Usage = &ai.Usage{Input: deref(line.PromptEvalCount), Output: deref(line.EvalCount)}
			}
```

with, at file bottom:

```go
func deref(n *int) int {
	if n == nil {
		return 0
	}
	return *n
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ai/...`
Expected: PASS (existing agent/app tests unaffected: nil usage everywhere else).

- [ ] **Step 5: Commit**

```bash
git add internal/ai/ai.go internal/ai/ollama/
git commit -m "feat(ai): usage on the final chunk; Ollama reports its eval counts"
```

---

### Task 2: OpenAI usage

**Files:**
- Modify: `internal/ai/openai/openai.go` (`Chat`)
- Test: `internal/ai/openai/openai_test.go`

**Interfaces:**
- Consumes: `ai.Usage`, `ai.Chunk.Usage` (Task 1).

- [ ] **Step 1: Write the failing tests** (append)

```go
// The usage chunk arrives after finish_reason, with no choices.
func TestChatReportsUsageAfterTheFinishReason(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		chunks(w,
			`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"list_refs","arguments":"{}"}}]}}]}`,
			`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
			`{"id":"1","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":900,"completion_tokens":40,"total_tokens":940,"prompt_tokens_details":{"cached_tokens":512}}}`,
		)
	}))
	defer srv.Close()
	ch, err := openai.New("sk-test", openai.WithBaseURL(srv.URL)).Chat(context.Background(), ai.Request{
		Model: "gpt-4.1", Messages: []ai.Message{{Role: ai.RoleUser, Content: "hi"}},
		Tools: []ai.ToolSpec{{Name: "list_refs", Parameters: map[string]any{"type": "object"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var calls []ai.ToolCall
	var usage *ai.Usage
	done := false
	for c := range ch {
		calls = append(calls, c.ToolCalls...)
		if c.Done {
			done, usage = true, c.Usage
		}
		if c.Err != nil {
			t.Fatal(c.Err)
		}
	}
	if !done || len(calls) != 1 || calls[0].ID != "call_1" {
		t.Fatalf("done %v calls %#v", done, calls)
	}
	if usage == nil || *usage != (ai.Usage{Input: 900, Output: 40, CacheRead: 512}) {
		t.Fatalf("usage = %#v", usage)
	}
	opts, _ := body["stream_options"].(map[string]any)
	if opts["include_usage"] != true {
		t.Fatalf("stream_options = %#v", body["stream_options"])
	}
}

// An OpenAI-compatible server may ignore include_usage.
func TestChatWithoutAUsageChunkReportsNoUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunks(w,
			`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"hi"}}]}`,
			`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		)
	}))
	defer srv.Close()
	ch, err := openai.New("sk-test", openai.WithBaseURL(srv.URL)).Chat(context.Background(), ai.Request{Model: "gpt-4.1", Messages: []ai.Message{{Role: ai.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	done := false
	for c := range ch {
		if c.Done {
			done = true
			if c.Usage != nil {
				t.Fatalf("usage = %#v, want nil", c.Usage)
			}
		}
	}
	if !done {
		t.Fatal("no Done chunk")
	}
}

// The connection drops after finish_reason, before the usage chunk: the
// answer is complete, so it still ends Done (without usage), not in error.
func TestChatErrorAfterTheFinishReasonStillEndsDone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: {not json\n\n")
	}))
	defer srv.Close()
	ch, err := openai.New("sk-test", openai.WithBaseURL(srv.URL)).Chat(context.Background(), ai.Request{Model: "gpt-4.1", Messages: []ai.Message{{Role: ai.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	text, _, done, err := collect(t, ch)
	if err != nil || !done || text != "hi" {
		t.Fatalf("text %q done %v err %v", text, done, err)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/ai/openai/ -run 'Usage|AfterTheFinish'`
Expected: FAIL — usage nil / `stream_options` missing (the error test may already pass; if it fails with a non-nil err that is also expected until Step 3).

- [ ] **Step 3: Implement**

In `Chat`, after `params` is built (before the tools loop or after it):

```go
	params.StreamOptions = sdk.ChatCompletionStreamOptionsParam{IncludeUsage: sdk.Bool(true)}
```

Replace the goroutine's loop and tail with:

```go
		calls := map[int64]*pending{}
		order := []int64{}
		finished := false
		var usage *ai.Usage
		for stream.Next() {
			chunk := stream.Current()
			if chunk.JSON.Usage.Valid() {
				usage = &ai.Usage{
					Input:     int(chunk.Usage.PromptTokens),
					Output:    int(chunk.Usage.CompletionTokens),
					CacheRead: int(chunk.Usage.PromptTokensDetails.CachedTokens),
				}
			}
			if len(chunk.Choices) == 0 || finished {
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
				// Keep reading: the usage chunk comes after the finish.
				finished = true
			}
		}
		// After finish_reason the answer is complete; a failure while
		// waiting for the usage chunk only loses the count.
		if err := stream.Err(); err != nil && !finished {
			send(ctx, ch, ai.Chunk{Err: classify(err)})
			return
		}
		// Without a finish_reason (dropped connection, server bug) a tool
		// call the model was in the middle of asking for would otherwise be
		// silently lost, so pending calls are emitted either way.
		for _, call := range finish(calls, order) {
			if !send(ctx, ch, ai.Chunk{ToolCalls: []ai.ToolCall{call}}) {
				return
			}
		}
		send(ctx, ch, ai.Chunk{Done: true, Usage: usage})
```

Check that `sdk.Bool` exists in openai-go v1.12.0 (`grep -n "^func Bool" $(go env GOMODCACHE)/github.com/openai/openai-go@v1.12.0/*.go`); if it is named differently use `param.NewOpt(true)` from `github.com/openai/openai-go/packages/param`. Update the comment above `Chat` to say tool calls and usage are emitted when the stream ends. If a canceled context makes `stream.Err()` non-nil after finish, `send` already returns false on a done ctx, so no extra handling.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ai/openai/`
Expected: PASS, including the existing `TestChatEmitsToolCallWhenStreamEndsWithoutFinishReason`.

- [ ] **Step 5: Commit**

```bash
git add internal/ai/openai/
git commit -m "feat(ai): OpenAI asks for usage and reads it after the finish reason"
```

---

### Task 3: Anthropic usage

**Files:**
- Modify: `internal/ai/anthropic/anthropic.go` (`Chat`, final send)
- Test: `internal/ai/anthropic/anthropic_test.go`

**Interfaces:**
- Consumes: `ai.Usage`, `ai.Chunk.Usage` (Task 1).

- [ ] **Step 1: Write the failing test** (append)

```go
func TestChatReportsUsageWithCacheInInput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sse(w,
			`message_start {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5","content":[],"stop_reason":null,"usage":{"input_tokens":100,"cache_read_input_tokens":2000,"cache_creation_input_tokens":300,"output_tokens":1}}}`,
			`content_block_start {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`content_block_delta {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hola"}}`,
			`content_block_stop {"type":"content_block_stop","index":0}`,
			`message_delta {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":55}}`,
			`message_stop {"type":"message_stop"}`,
		)
	}))
	defer srv.Close()
	ch, err := anthropic.New("sk-test", anthropic.WithBaseURL(srv.URL)).Chat(context.Background(), ai.Request{Model: "claude-opus-5", Messages: []ai.Message{{Role: ai.RoleUser, Content: "hola"}}})
	if err != nil {
		t.Fatal(err)
	}
	var usage *ai.Usage
	for c := range ch {
		if c.Done {
			usage = c.Usage
		}
	}
	want := ai.Usage{Input: 2400, Output: 55, CacheRead: 2000, CacheWrite: 300}
	if usage == nil || *usage != want {
		t.Fatalf("usage = %#v, want %#v", usage, want)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/ai/anthropic/ -run Usage`
Expected: FAIL, `usage = (*ai.Usage)(nil)`.

- [ ] **Step 3: Implement**

Replace the final `send(ctx, ch, ai.Chunk{Done: true})` in `Chat` with:

```go
		u := message.Usage
		send(ctx, ch, ai.Chunk{Done: true, Usage: &ai.Usage{
			Input:      int(u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens),
			Output:     int(u.OutputTokens),
			CacheRead:  int(u.CacheReadInputTokens),
			CacheWrite: int(u.CacheCreationInputTokens),
		}})
```

If the accumulated message_delta usage does not overwrite `output_tokens` (test fails with Output 1), read the SDK's `Message.Accumulate` for `MessageDeltaEvent` and take `OutputTokens` from the last `MessageDeltaEvent` seen in the loop instead.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ai/anthropic/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ai/anthropic/
git commit -m "feat(ai): Anthropic reports usage, cache counted in input"
```

---

### Task 4: Agent stores usage and emits `chat:usage`

**Files:**
- Modify: `internal/ai/agent/agent.go` (constants, new `UsageEvent`, `Execute`)
- Test: `internal/ai/agent/agent_test.go`

**Interfaces:**
- Consumes: `ai.Chunk.Usage`, `ai.Message.Usage` (Task 1).
- Produces: `agent.EventUsage = "chat:usage"`; `agent.UsageEvent{RepoID string "repoID"; RunID string "runID"; Usage ai.Usage "usage"}`.

- [ ] **Step 1: Write the failing tests** (append)

```go
func TestEachModelCallKeepsItsUsage(t *testing.T) {
	u1 := &ai.Usage{Input: 100, Output: 10}
	u2 := &ai.Usage{Input: 180, Output: 25, CacheRead: 90}
	p := &scripted{turns: [][]ai.Chunk{
		{{ToolCalls: []ai.ToolCall{{ID: "c1", Name: "list_refs", Args: map[string]any{}}}}, {Done: true, Usage: u1}},
		{{Delta: "On main."}, {Done: true, Usage: u2}},
	}}
	rec := &recorder{}
	got, err := agent.Execute(context.Background(), baseRun(p, rec), user)
	if err != nil {
		t.Fatal(err)
	}
	if got[1].Usage == nil || *got[1].Usage != *u1 || got[3].Usage == nil || *got[3].Usage != *u2 {
		t.Fatalf("usages = %#v / %#v", got[1].Usage, got[3].Usage)
	}
	var usages []agent.UsageEvent
	for i, n := range rec.names {
		if n == agent.EventUsage {
			usages = append(usages, rec.data[i].(agent.UsageEvent))
		}
	}
	want := []agent.UsageEvent{{RepoID: "repo1", RunID: "run1", Usage: *u1}, {RepoID: "repo1", RunID: "run1", Usage: *u2}}
	if !reflect.DeepEqual(usages, want) {
		t.Fatalf("usage events = %#v", usages)
	}
}

func TestNoUsageEmitsNothing(t *testing.T) {
	p := &scripted{turns: [][]ai.Chunk{{{Delta: "hi"}, {Done: true}}}}
	rec := &recorder{}
	got, err := agent.Execute(context.Background(), baseRun(p, rec), user)
	if err != nil {
		t.Fatal(err)
	}
	if got[1].Usage != nil {
		t.Fatalf("usage = %#v", got[1].Usage)
	}
	for _, n := range rec.names {
		if n == agent.EventUsage {
			t.Fatal("chat:usage emitted without a usage")
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/ai/agent/ -run 'Usage'`
Expected: compile error, `agent.EventUsage undefined`.

- [ ] **Step 3: Implement**

In the const block add `EventUsage = "chat:usage"` after `EventSuggestions`. After `DeltaEvent` add:

```go
// UsageEvent is what one model call of a run consumed; a run with tool
// rounds sends one per call.
type UsageEvent struct {
	RepoID string   `json:"repoID"`
	RunID  string   `json:"runID"`
	Usage  ai.Usage `json:"usage"`
}
```

In `Execute`, declare `var usage *ai.Usage` next to `done := false`; in the chunk loop:

```go
			if chunk.Done {
				done = true
				usage = chunk.Usage
			}
```

Right after the `if streamErr != nil || !done { ... }` block (the call finished):

```go
		if usage != nil {
			r.Emit(EventUsage, UsageEvent{RepoID: r.RepoID, RunID: r.RunID, Usage: *usage})
		}
```

and change the append of the assistant message to:

```go
		msgs = append(msgs, ai.Message{Role: ai.RoleAssistant, Content: text.String(), ToolCalls: calls, Usage: usage})
```

Check `stampAnswer` in `internal/app/ai.go` (and the resolver path in `internal/app/merge.go`) only sets Provider/Model/At and does not rebuild messages, so `Usage` survives; if it rebuilds, copy `Usage` across.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ai/... ./internal/app/`
Expected: PASS (existing DeepEqual tests use nil usage).

- [ ] **Step 5: Commit**

```bash
git add internal/ai/agent/ internal/app/
git commit -m "feat(ai): agent keeps each model call's usage and emits chat:usage"
```

---

### Task 5: Explanations store usage

**Files:**
- Modify: `internal/app/ai.go` (`streamIntoString`, its caller in `explainTask` ~:607-613)
- Modify: `internal/app/ai_test.go` (`fakeOllama` final lines, explain test)

**Interfaces:**
- Consumes: `agent.EventUsage`, `agent.UsageEvent` (Task 4).
- Produces: `streamIntoString(...) (string, *ai.Usage, error)`.

- [ ] **Step 1: Write the failing test**

In `fakeOllama`, change the text answer's final line to carry counts:

```go
			writeLines(w, `{"message":{"role":"assistant","content":"Hay "},"done":false}`, `{"message":{"content":"ramas."},"done":false}`, `{"message":{"content":""},"done":true,"prompt_eval_count":300,"eval_count":12}`)
```

In `TestExplainInChatWritesTheAnswerToTheConversation`, after `done := ev.wait(t, agent.EventDone)...`:

```go
	usage := ev.wait(t, agent.EventUsage).data.(agent.UsageEvent)
	if usage.RunID != "exp-1" || usage.Usage != (ai.Usage{Input: 300, Output: 12}) {
		t.Fatalf("usage event = %#v", usage)
	}
```

and extend the final answer check with `|| history[1].Usage == nil || *history[1].Usage != (ai.Usage{Input: 300, Output: 12})`.

Also in `TestSendChatRunsToolsStreamsAndSaves` assert the saved final assistant message has `Usage` `{300,12}` (the chat path through the agent), so the app layer is pinned end to end.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/app/ -run 'ExplainInChatWrites|SendChatRunsTools'`
Expected: the explain test FAILS (no usage event / nil Usage); the SendChat assertion should already PASS after Task 4.

- [ ] **Step 3: Implement**

```go
func streamIntoString(ctx context.Context, a *App, repoID, runID string, r ai.Responder, instructions, prompt string) (string, *ai.Usage, error) {
	stream, err := r.Respond(ctx, instructions, prompt)
	if err != nil {
		return "", nil, err
	}
	var text strings.Builder
	var streamErr error
	var usage *ai.Usage
	for chunk := range stream {
		switch {
		case chunk.Err != nil:
			streamErr = chunk.Err
		case chunk.Delta != "":
			text.WriteString(chunk.Delta)
			a.emit(agent.EventDelta, agent.DeltaEvent{RepoID: repoID, RunID: runID, Text: chunk.Delta})
		}
		if chunk.Done && chunk.Usage != nil {
			usage = chunk.Usage
			a.emit(agent.EventUsage, agent.UsageEvent{RepoID: repoID, RunID: runID, Usage: *usage})
		}
	}
	if ctx.Err() != nil {
		return text.String(), nil, ctx.Err()
	}
	return text.String(), usage, streamErr
}
```

Caller: `answer, usage, runErr := streamIntoString(...)` and add `Usage: usage,` to the saved assistant message.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/app/
git commit -m "feat(ai): explanations keep their usage and emit chat:usage"
```

---

### Task 6: Frontend token state

**Files:**
- Modify: `frontend/src/lib/types.ts` (`AIMessage`, new `AIUsage`, `ChatUsageEvent`)
- Modify: `frontend/src/lib/chat.ts` (`ChatItem`, `fromMessages`, `CHAT_EVENTS`, `Payload`, `applyEvent`, new helpers)
- Test: `frontend/src/lib/chat.test.ts`

**Interfaces:**
- Consumes: event `chat:usage` with `{repoID, runID, usage: {input, output, cacheRead?, cacheWrite?}}` (Task 4/5).
- Produces (all exported from `lib/chat.ts`):
  - `interface TokenCount { input: number; output: number; cacheRead: number; cacheWrite: number; calls: number }`
  - `ChatItem.usage?: TokenCount`
  - `conversationTokens(state: ChatState): { total: TokenCount; missing: boolean } | null`
  - `formatTokens(n: number): string`
  - `tokensText(t: TokenCount): string` → `"8.2k in · 640 out"`
  - `tokensTitle(t: TokenCount): string` → `"2 model calls · cache read 512 · cache written 300"` (cache parts only when non-zero; "1 model call" singular)

- [ ] **Step 1: Write the failing tests** (append to `chat.test.ts`; add the new names to the import from `./chat`)

```ts
describe('token counts', () => {
  const u = (input: number, output: number, cacheRead = 0) => ({ input, output, ...(cacheRead ? { cacheRead } : {}) })

  it('sums the usage of every model call folded into an answer', () => {
    const s = fromMessages('r', [
      { role: 'user', content: 'q' },
      { role: 'assistant', content: '', toolCalls: [{ id: 'c1', name: 'list_refs', args: {} }], usage: u(100, 10) },
      { role: 'tool', content: 'main', toolName: 'list_refs' },
      { role: 'assistant', content: 'On main.', usage: u(180, 25, 90) },
    ])
    expect(s.items[1].usage).toEqual({ input: 280, output: 35, cacheRead: 90, cacheWrite: 0, calls: 2 })
  })

  it('leaves an answer stored without usage uncounted', () => {
    const s = fromMessages('r', [{ role: 'user', content: 'q' }, { role: 'assistant', content: 'a' }])
    expect(s.items[1].usage).toBeUndefined()
    expect(conversationTokens(s)).toBeNull()
  })

  it('totals the conversation and flags answers without a count', () => {
    const s = fromMessages('r', [
      { role: 'user', content: 'old' },
      { role: 'assistant', content: 'old answer' },
      { role: 'user', content: 'q' },
      { role: 'assistant', content: 'a', usage: u(1000, 50) },
    ])
    expect(conversationTokens(s)).toEqual({ total: { input: 1000, output: 50, cacheRead: 0, cacheWrite: 0, calls: 1 }, missing: true })
  })

  it('adds live usage to the running answer and ignores other runs', () => {
    let s = startRun(emptyChat('r'), 'q', 'run1')
    s = applyEvent(s, 'chat:usage', { repoID: 'r', runID: 'run1', usage: u(100, 10) })
    s = applyEvent(s, 'chat:usage', { repoID: 'r', runID: 'run1', usage: u(150, 20, 100) })
    s = applyEvent(s, 'chat:usage', { repoID: 'r', runID: 'other', usage: u(9, 9) })
    s = applyEvent(s, 'chat:usage', { repoID: 'x', runID: 'run1', usage: u(9, 9) })
    expect(s.items[1].usage).toEqual({ input: 250, output: 30, cacheRead: 100, cacheWrite: 0, calls: 2 })
  })

  it('does not count the running answer as missing', () => {
    let s = startRun(emptyChat('r'), 'q', 'run1')
    s = applyEvent(s, 'chat:delta', { repoID: 'r', runID: 'run1', text: 'partial' })
    expect(conversationTokens(s)).toBeNull()
  })

  it('formats token numbers', () => {
    expect(formatTokens(0)).toBe('0')
    expect(formatTokens(999)).toBe('999')
    expect(formatTokens(1000)).toBe('1k')
    expect(formatTokens(1234)).toBe('1.2k')
    expect(formatTokens(12000)).toBe('12k')
    expect(formatTokens(999_999)).toBe('1M')
    expect(formatTokens(1_250_000)).toBe('1.3M')
  })

  it('describes a count', () => {
    const t = { input: 8200, output: 640, cacheRead: 512, cacheWrite: 0, calls: 2 }
    expect(tokensText(t)).toBe('8.2k in · 640 out')
    expect(tokensTitle(t)).toBe('2 model calls · cache read 512')
    expect(tokensTitle({ ...t, calls: 1, cacheRead: 0 })).toBe('1 model call')
  })
})
```

Note `formatTokens(999_999)`: 999.999k rounds to `1000k`; the rule promotes it to `1M`. Check that `emptyChat` and `startRun` are exported (they are used by existing tests; import them if not already imported).

- [ ] **Step 2: Run to verify it fails**

Run: `cd frontend && npx vitest run src/lib/chat.test.ts`
Expected: FAIL, missing exports.

- [ ] **Step 3: Implement**

`types.ts`:

```ts
export interface AIUsage { input: number; output: number; cacheRead?: number; cacheWrite?: number }
```

add `usage?: AIUsage` to `AIMessage`, and next to `ChatDoneEvent`:

```ts
export interface ChatUsageEvent { repoID: string; runID: string; usage: AIUsage }
```

`chat.ts`:

```ts
// TokenCount is what the model calls of an answer (or a conversation)
// consumed. Calls without a reported usage are not in it.
export interface TokenCount { input: number; output: number; cacheRead: number; cacheWrite: number; calls: number }

function addUsage(t: TokenCount | undefined, u: AIUsage): TokenCount {
  return {
    input: (t?.input ?? 0) + u.input,
    output: (t?.output ?? 0) + u.output,
    cacheRead: (t?.cacheRead ?? 0) + (u.cacheRead ?? 0),
    cacheWrite: (t?.cacheWrite ?? 0) + (u.cacheWrite ?? 0),
    calls: (t?.calls ?? 0) + 1,
  }
}
```

- `ChatItem` gains `usage?: TokenCount` with a comment ("what the answer's model calls consumed; absent when none reported").
- In `fromMessages`, in the assistant branch after `if (m.at) last.at = m.at`: `if (m.usage) last.usage = addUsage(last.usage, m.usage)`.
- `CHAT_EVENTS` gains `'chat:usage'`; `Payload` gains `ChatUsageEvent`.
- In `applyEvent`'s switch (after the runID guard):

```ts
    case 'chat:usage':
      last.usage = addUsage(last.usage, (payload as ChatUsageEvent).usage)
      return { ...state, items }
```

Helpers:

```ts
/** conversationTokens totals the conversation's counted calls, or null when
 *  none is counted. missing: a finished answer has no count (stored before
 *  counts existed, or its provider gave none); the running one never is. */
export function conversationTokens(state: ChatState): { total: TokenCount; missing: boolean } | null {
  let total: TokenCount | undefined
  let missing = false
  state.items.forEach((item, i) => {
    if (item.role !== 'assistant') return
    if (item.usage) {
      total = { input: (total?.input ?? 0) + item.usage.input, output: (total?.output ?? 0) + item.usage.output,
        cacheRead: (total?.cacheRead ?? 0) + item.usage.cacheRead, cacheWrite: (total?.cacheWrite ?? 0) + item.usage.cacheWrite,
        calls: (total?.calls ?? 0) + item.usage.calls }
    } else if (item.text && !(state.runID !== null && i === state.items.length - 1)) {
      missing = true
    }
  })
  return total ? { total, missing } : null
}

/** formatTokens: 999, 1.2k, 12k, 1.3M. */
export function formatTokens(n: number): string {
  const short = (v: number) => String(Math.round(v * 10) / 10)
  if (n < 1000) return String(n)
  if (Math.round(n / 100) < 10_000) return `${short(n / 1000)}k`
  return `${short(n / 1_000_000)}M`
}

export function tokensText(t: TokenCount): string {
  return `${formatTokens(t.input)} in · ${formatTokens(t.output)} out`
}

export function tokensTitle(t: TokenCount): string {
  const parts = [`${t.calls} model call${t.calls === 1 ? '' : 's'}`]
  if (t.cacheRead) parts.push(`cache read ${formatTokens(t.cacheRead)}`)
  if (t.cacheWrite) parts.push(`cache written ${formatTokens(t.cacheWrite)}`)
  return parts.join(' · ')
}
```

(`Math.round(n / 100) < 10_000` means "rounds to under 1000.0k", so 999 999 → `1M`.) Import `AIUsage`, `ChatUsageEvent` types.

- [ ] **Step 4: Run tests**

Run: `cd frontend && npx vitest run && npx svelte-check --threshold error` (or the repo's `npm run check` if present)
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/
git commit -m "feat(chat): token counts in chat state (per answer, per conversation, live)"
```

---

### Task 7: Views, spec, rebuild

**Files:**
- Modify: `frontend/src/components/ChatPanel.svelte` (answer row ~:311-315, composer bar ~:338-340, styles ~:391-397)
- Modify: `docs/spec/06-ai.md` (chat panel section, answer-row paragraph ~:123 and after the model picker paragraph ~:133-139)

**Interfaces:**
- Consumes: `conversationTokens`, `tokensText`, `tokensTitle` from `lib/chat.ts` (Task 6).

- [ ] **Step 1: Answer row**

Import the three helpers. After the `answeredBy` span:

```svelte
              {#if item.usage}<span class="tokens" title={tokensTitle(item.usage)}>{tokensText(item.usage)}</span>{/if}
```

- [ ] **Step 2: Composer bar**

In the script: `$: tokens = conversationTokens(state)`. After `<ModelPicker … />`:

```svelte
      {#if tokens}
        <span class="tokens" title={`This conversation: ${tokensTitle(tokens.total)}${tokens.missing ? '\nSome answers have no count' : ''}`}>{tokensText(tokens.total)}</span>
      {/if}
```

Style: `.tokens { flex: none; font-size: 11px; color: var(--faint); white-space: nowrap; font-variant-numeric: tabular-nums; }`. The composer bar span must not push Stop/Send out at narrow panel widths: the `.spacer` stays after it; check the panel at its minimum width.

- [ ] **Step 3: Spec**

In `docs/spec/06-ai.md`, in the answer-row paragraph, after the provider/model sentence, add:

> When the provider reported it, the row ends with the tokens the answer used, "8.2k in · 640 out": "in" is everything the model read across all of the answer's model calls (one per tool round), cached tokens included; the tooltip gives the number of model calls and the cached part. An answer whose provider reported nothing, or stored before counts were recorded, shows none — never 0.

After the model picker paragraph, add:

> Next to the picker, the conversation's total, "12.4k in · 1.1k out", sums every counted answer — chat turns, explanations and the conflict resolver; suggested replies and commit messages are not part of it. It grows while an answer runs, is stored with the conversation, starts again with a new conversation and is hidden while nothing is counted. Its tooltip gives the model calls, the cached part, and says when some answers have no count. The counts come from the provider: Ollama's eval counts, OpenAI's usage (asked for on every stream) and Anthropic's usage, with cache reads and writes counted as input. No cost is shown.

- [ ] **Step 4: Verify**

Run: `go test ./... && cd frontend && npx vitest run`
Expected: PASS.
Run: `make dev` (node 22). Ask a question with each configured provider; check the answer row, the bar growing live during a tool round, the tooltip, "New chat" hiding the total, and an app restart keeping it.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/ChatPanel.svelte docs/spec/06-ai.md
git commit -m "feat(chat): show tokens in/out per answer and per conversation"
```

---

### After the tasks

- Final whole-branch review (most capable model).
- Merge into main with `git merge --no-ff claude/chat-token-counts-display-2c253e -m "Merge claude/chat-token-counts-display-2c253e: tokens in/out per answer and per conversation in the chat"`, then rebuild and reopen with `make dev`.
- Update memory `git-ui-chat-token-counts.md` (merged, commit, manual pass state) and its `MEMORY.md` line.
