// Package merge runs git merges and edits the conflict markers they leave
// behind. The marker surgery (Parse, Splice) is pure text handling with no
// git involved, so it can be tested exhaustively.
package merge

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// ContextLines is how many lines above and below a conflict are shown with it.
const ContextLines = 6

// ErrBadConflict reports a file whose markers don't nest the way git writes
// them; refusing is safer than guessing where the sides end.
var ErrBadConflict = errors.New("merge: malformed conflict markers")

// Hunk is one conflicted region of a file. HasBase is false when the file
// was written in the two-way style, which carries no common ancestor; with
// it true, an empty Base means the ancestor had nothing there — both sides
// added lines at the same place.
type Hunk struct {
	Index   int
	Ours    string
	Theirs  string
	Base    string
	HasBase bool
	Before  string
	After   string

	// ID names the region by its content, so the name survives the
	// renumbering every resolve causes; the AI resolver and the Merge
	// view's buttons both use it.
	ID string
	// Start and End are the line indices of the marker block; End is
	// exclusive. BaseAt is the ||||||| line (-1 in the two-way style) and
	// Sep the ======= line, so a view can tell the sides apart without
	// telling git's markers from look-alike content itself.
	Start, End  int
	BaseAt, Sep int
}

// Parse returns the conflicted regions of content, in file order.
func Parse(content string) ([]Hunk, error) {
	lines := splitLines(content)
	hunks := []Hunk{}
	for i := 0; i < len(lines); i++ {
		if !marker(lines[i], "<<<<<<<") {
			continue
		}
		h := Hunk{Index: len(hunks), Start: i, BaseAt: -1}
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
				h.HasBase = true
				h.BaseAt = i
			case marker(line, "=======") && side != &theirs:
				side = &theirs
				h.Sep = i
			case marker(line, ">>>>>>>") && side == &theirs:
				h.End = i + 1
				closed = true
			default:
				*side = append(*side, line)
			}
		}
		if !closed {
			return nil, fmt.Errorf("%w: unterminated conflict at line %d", ErrBadConflict, h.Start+1)
		}
		i = h.End - 1
		h.Ours, h.Base, h.Theirs = strings.Join(ours, ""), strings.Join(base, ""), strings.Join(theirs, "")
		h.Before = strings.Join(lines[max(0, h.Start-ContextLines):h.Start], "")
		hunks = append(hunks, h)
	}
	// After is filled second: it stops at the next conflict, which isn't
	// known until the whole file has been walked.
	for i := range hunks {
		limit := len(lines)
		if i+1 < len(hunks) {
			limit = hunks[i+1].Start
		}
		hunks[i].After = strings.Join(lines[hunks[i].End:min(limit, hunks[i].End+ContextLines)], "")
	}
	// A region's id is its content, so it survives the renumbering other
	// resolves cause. Regions with the same content also carry their line:
	// resolving one moves the other, so a stale id — a second click, the
	// AI retrying — finds nothing rather than landing on the twin.
	count := map[string]int{}
	for i := range hunks {
		sum := sha1.Sum([]byte(hunks[i].Ours + "\x00" + hunks[i].Base + "\x00" + hunks[i].Theirs))
		hunks[i].ID = hex.EncodeToString(sum[:4])
		count[hunks[i].ID]++
	}
	for i := range hunks {
		if count[hunks[i].ID] > 1 {
			hunks[i].ID = fmt.Sprintf("%s@%d", hunks[i].ID, hunks[i].Start)
		}
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

var (
	// ErrNoSuchHunk means the file no longer has a conflict at that index —
	// usually because it was already resolved.
	ErrNoSuchHunk = errors.New("merge: no such conflict")
	// ErrMarkersLeft means a proposed resolution still contains markers,
	// which would leave the file conflicted after staging.
	ErrMarkersLeft = errors.New("merge: the resolution still contains conflict markers")
)

// Splice replaces the marker block of hunk index with resolved and returns
// the new content. Every other byte of content is preserved, including line
// endings and a missing final newline. It re-parses content on each call, so
// resolving hunks one at a time needs no offset bookkeeping from the caller.
func Splice(content string, index int, resolved string) (string, error) {
	hunks, err := Parse(content)
	if err != nil {
		return "", err
	}
	if index < 0 || index >= len(hunks) {
		return "", fmt.Errorf("%w: asked for %d, the file has %d", ErrNoSuchHunk, index, len(hunks))
	}
	if HasMarkers(resolved) {
		return "", ErrMarkersLeft
	}
	lines := splitLines(content)
	h := hunks[index]
	tail := strings.Join(lines[h.End:], "")
	body := resolved
	// The block replaced whole lines, so the replacement ends a line too —
	// unless it sits at the end of a file that never had a final newline.
	if body != "" && !strings.HasSuffix(body, "\n") && (tail != "" || strings.HasSuffix(content, "\n")) {
		body += "\n"
	}
	return strings.Join(lines[:h.Start], "") + body + tail, nil
}
