package agent

import (
	"encoding/json"
	"strings"

	"git-ui/internal/ai"
)

// recoverCalls scans text for JSON tool calls a model wrote as plain text
// instead of returning them as structured tool_calls. Some Ollama models do
// this: they answer with the call's JSON in the message content, sometimes
// inside a fenced code block, sometimes surrounded by prose.
//
// It finds bare JSON objects anywhere in text by trying to decode a JSON
// value starting at each '{'; a decoder handles nested braces correctly, so
// no manual brace counting is needed. On a successful decode it advances
// past the object using the decoder's InputOffset; on failure it moves on
// one byte and keeps scanning, so a fenced block's surrounding prose does
// not stop it from finding the object inside.
//
// attempted reports whether the text looked like it contained a tool call
// that recoverCalls could not turn into a valid, runnable call: a decoded
// object with a "name" key plus an "arguments"/"parameters" key naming a
// tool that isn't in tools, or - when no object decoded at all - text that
// mentions both "name" and "arguments"/"parameters", which is the simple
// heuristic for "this was probably malformed call JSON".
func recoverCalls(text string, tools []ai.ToolSpec) (calls []ai.ToolCall, attempted bool) {
	known := make(map[string]bool, len(tools))
	for _, t := range tools {
		known[t.Name] = true
	}

	i := 0
	for i < len(text) {
		if text[i] != '{' {
			i++
			continue
		}
		dec := json.NewDecoder(strings.NewReader(text[i:]))
		var v map[string]any
		if err := dec.Decode(&v); err != nil {
			i++
			continue
		}
		if name, args, ok := asCall(v); ok {
			attempted = true
			if known[name] {
				calls = append(calls, ai.ToolCall{Name: name, Args: args})
			}
		}
		i += int(dec.InputOffset())
	}

	if len(calls) == 0 && !attempted {
		if strings.Contains(text, `"name"`) && (strings.Contains(text, `"arguments"`) || strings.Contains(text, `"parameters"`)) {
			attempted = true
		}
	}
	return calls, attempted
}

// asCall reports whether m has exactly the shape of a tool call: a string
// "name" plus an object "arguments" (or "parameters", since some models
// alternate between the two across a run).
func asCall(m map[string]any) (name string, args map[string]any, ok bool) {
	if len(m) != 2 {
		return "", nil, false
	}
	n, hasName := m["name"].(string)
	if !hasName {
		return "", nil, false
	}
	a, hasArgs := m["arguments"].(map[string]any)
	if !hasArgs {
		a, hasArgs = m["parameters"].(map[string]any)
	}
	if !hasArgs {
		return "", nil, false
	}
	return n, a, true
}
