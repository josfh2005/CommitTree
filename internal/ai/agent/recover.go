package agent

import (
	"encoding/json"
	"fmt"
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
		v, n, ok := decodeObject(text[i:])
		if !ok {
			i++
			continue
		}
		if name, args, ok := asCall(v); ok {
			attempted = true
			if known[name] {
				calls = append(calls, ai.ToolCall{Name: name, Args: args})
			}
		}
		i += n
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

// decodeObject decodes the JSON object at the start of s and returns it with
// the number of bytes of s it used. Models writing code into a call often put
// raw tabs and newlines inside strings, which JSON forbids; when the plain
// decode fails and s starts like an object with a key, it retries with those
// control characters escaped, mapping the length back onto the original s.
func decodeObject(s string) (map[string]any, int, bool) {
	var v map[string]any
	dec := json.NewDecoder(strings.NewReader(s))
	if err := dec.Decode(&v); err == nil {
		return v, int(dec.InputOffset()), true
	}
	if !strings.HasPrefix(strings.TrimLeft(s[1:], " \t\r\n"), `"`) {
		return nil, 0, false
	}
	fixed, origin := escapeControlInStrings(s)
	v = nil
	dec = json.NewDecoder(strings.NewReader(fixed))
	if err := dec.Decode(&v); err != nil {
		return nil, 0, false
	}
	return v, origin[dec.InputOffset()], true
}

// escapeControlInStrings escapes control characters found inside JSON string
// literals. origin[j] is the index in s of the byte that produced fixed[j];
// origin[len(fixed)] is len(s).
func escapeControlInStrings(s string) (string, []int) {
	var b strings.Builder
	origin := make([]int, 0, len(s)+1)
	emit := func(str string, at int) {
		b.WriteString(str)
		for range len(str) {
			origin = append(origin, at)
		}
	}
	inString, escaped := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case !inString:
			inString = c == '"'
		case escaped:
			escaped = false
		case c == '\\':
			escaped = true
		case c == '"':
			inString = false
		case c < 0x20:
			switch c {
			case '\t':
				emit(`\t`, i)
			case '\n':
				emit(`\n`, i)
			case '\r':
				emit(`\r`, i)
			default:
				emit(fmt.Sprintf(`\u%04x`, c), i)
			}
			continue
		}
		b.WriteByte(c)
		origin = append(origin, i)
	}
	origin = append(origin, len(s))
	return b.String(), origin
}
