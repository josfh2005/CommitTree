package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"git-ui/internal/ai"
)

const (
	suggestTurns     = 6
	suggestTurnChars = 1500
	maxReplies       = 3
	maxReplyChars    = 60
)

// ErrNoReplies is returned when the model's answer holds no usable reply.
var ErrNoReplies = errors.New("no suggested replies")

// SuggestReplies asks r for the replies the user is likely to send next in
// the conversation history, and returns at most three short ones.
func SuggestReplies(ctx context.Context, r ai.Responder, instructions string, history []ai.Message) ([]string, error) {
	stream, err := r.Respond(ctx, instructions, SuggestContext(history))
	if err != nil {
		return nil, err
	}
	var text strings.Builder
	for chunk := range stream {
		if chunk.Err != nil {
			err = chunk.Err
		}
		text.WriteString(chunk.Delta)
	}
	if err != nil {
		return nil, err
	}
	return ParseReplies(text.String())
}

// SuggestContext is the end of the conversation as "User:"/"Assistant:"
// turns: the last suggestTurns messages with text, oldest first, each cut to
// suggestTurnChars characters. Tool calls and tool results are left out.
func SuggestContext(history []ai.Message) string {
	var turns []string
	for i := len(history) - 1; i >= 0 && len(turns) < suggestTurns; i-- {
		m := history[i]
		text := strings.TrimSpace(m.Content)
		if text == "" || (m.Role != ai.RoleUser && m.Role != ai.RoleAssistant) {
			continue
		}
		label := "User"
		if m.Role == ai.RoleAssistant {
			label = "Assistant"
		}
		turns = append(turns, label+": "+cutRunes(text, suggestTurnChars))
	}
	slices.Reverse(turns)
	return strings.Join(turns, "\n\n")
}

// ParseReplies reads the JSON array of replies out of a model's answer,
// tolerating code fences and prose around it: it keeps non-empty strings of
// at most maxReplyChars characters, drops case-insensitive duplicates and
// returns at most maxReplies.
func ParseReplies(text string) ([]string, error) {
	start, end := strings.Index(text, "["), strings.LastIndex(text, "]")
	if start < 0 || end < start {
		return nil, ErrNoReplies
	}
	var raw []any
	if err := json.Unmarshal([]byte(text[start:end+1]), &raw); err != nil {
		return nil, ErrNoReplies
	}
	seen := map[string]bool{}
	var out []string
	for _, v := range raw {
		s, ok := v.(string)
		s = strings.TrimSpace(s)
		key := strings.ToLower(s)
		if !ok || s == "" || utf8.RuneCountInString(s) > maxReplyChars || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
		if len(out) == maxReplies {
			break
		}
	}
	if len(out) == 0 {
		return nil, ErrNoReplies
	}
	return out, nil
}

func cutRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
