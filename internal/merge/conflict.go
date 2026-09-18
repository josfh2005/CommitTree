// Package merge runs git merges and edits the conflict markers they leave
// behind. The marker surgery (Parse, Splice) is pure text handling with no
// git involved, so it can be tested exhaustively.
package merge

import (
	"errors"
	"fmt"
	"strings"
)

// ContextLines is how many lines above and below a conflict are shown with it.
const ContextLines = 6

// ErrBadConflict reports a file whose markers don't nest the way git writes
// them; refusing is safer than guessing where the sides end.
var ErrBadConflict = errors.New("merge: malformed conflict markers")

// Hunk is one conflicted region of a file. Base is empty when the file was
// written in the two-way style, which carries no common ancestor.
type Hunk struct {
	Index  int
	Ours   string
	Theirs string
	Base   string
	Before string
	After  string

	start, end int // line indices of the marker block; end is exclusive
}

// Parse returns the conflicted regions of content, in file order.
func Parse(content string) ([]Hunk, error) {
	lines := splitLines(content)
	hunks := []Hunk{}
	for i := 0; i < len(lines); i++ {
		if !marker(lines[i], "<<<<<<<") {
			continue
		}
		h := Hunk{Index: len(hunks), start: i}
		var ours, base, theirs []string
		side := &ours
		closed := false
		for i++; i < len(lines) && !closed; i++ {
			line := lines[i]
			switch {
			case marker(line, "<<<<<<<"):
				return nil, fmt.Errorf("%w: conflict reopened at line %d", ErrBadConflict, i+1)
			case marker(line, "|||||||") && side == &ours:
				side = &base
			case marker(line, "=======") && side != &theirs:
				side = &theirs
			case marker(line, ">>>>>>>") && side == &theirs:
				h.end = i + 1
				closed = true
			default:
				*side = append(*side, line)
			}
		}
		if !closed {
			return nil, fmt.Errorf("%w: unterminated conflict at line %d", ErrBadConflict, h.start+1)
		}
		i = h.end - 1
		h.Ours, h.Base, h.Theirs = strings.Join(ours, ""), strings.Join(base, ""), strings.Join(theirs, "")
		h.Before = strings.Join(lines[max(0, h.start-ContextLines):h.start], "")
		hunks = append(hunks, h)
	}
	// After is filled second: it stops at the next conflict, which isn't
	// known until the whole file has been walked.
	for i := range hunks {
		limit := len(lines)
		if i+1 < len(hunks) {
			limit = hunks[i+1].start
		}
		hunks[i].After = strings.Join(lines[hunks[i].end:min(limit, hunks[i].end+ContextLines)], "")
	}
	return hunks, nil
}

// HasMarkers reports whether s still contains conflict markers.
func HasMarkers(s string) bool {
	for _, line := range splitLines(s) {
		if marker(line, "<<<<<<<") || marker(line, "=======") || marker(line, ">>>>>>>") {
			return true
		}
	}
	return false
}

// marker reports whether line is a conflict marker of the given kind: the
// seven-character sign followed by a space or the end of the line. Markdown
// underlines ("========") are longer, so they don't match.
func marker(line, sign string) bool {
	if !strings.HasPrefix(line, sign) {
		return false
	}
	rest := strings.TrimSuffix(strings.TrimSuffix(line[len(sign):], "\n"), "\r")
	return rest == "" || strings.HasPrefix(rest, " ")
}

// splitLines keeps each line's own terminator, so joining the result
// reproduces the input byte for byte.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.SplitAfter(s, "\n")
	if n := len(lines); lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}
