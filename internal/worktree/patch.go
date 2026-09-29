package worktree

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// LineKind is what one body line of a hunk does.
type LineKind int

const (
	LineContext LineKind = iota
	LineAdd
	LineDel
)

// DiffLine is one body line of a hunk, without its leading ' ', '+' or '-'.
// NoNewline marks a line followed by "\ No newline at end of file".
type DiffLine struct {
	Kind      LineKind
	Text      string
	NoNewline bool
}

// Hunk is one "@@" block. Section is what git printed after the second "@@"
// (the enclosing function), kept so a rebuilt header reads the same.
type Hunk struct {
	OldStart, NewStart int
	Section            string
	Lines              []DiffLine
}

// ParsedDiff is one file's unified diff: the lines before the first hunk
// ("diff --git", "index", "---", "+++") and its hunks.
type ParsedDiff struct {
	Header []string
	Hunks  []Hunk
}

// HunkPick selects lines of one hunk by their index among its body lines
// (context lines count too); no Lines means the whole hunk.
type HunkPick struct {
	Hunk  int   `json:"hunk"`
	Lines []int `json:"lines"`
}

// Selection is what the diff pane sends: the picked hunks, in any order.
type Selection []HunkPick

var (
	ErrEmptySelection = errors.New("worktree: nothing selected")
	// ErrSplitsLastLine refuses a selection that would keep a last line with
	// no trailing newline in the middle of the result, which no file can be.
	ErrSplitsLastLine = errors.New("worktree: this selection splits the file's last line, which has no newline; act on the whole hunk")
)

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@(.*)$`)

// ParseDiff splits one file's diff, as FileDiff returns it, into its header
// and hunks. Inside a hunk every line is read by its first character, so an
// added "++ x" (shown "+++ x") is a change, not a header.
func ParseDiff(text string) (ParsedDiff, error) {
	var d ParsedDiff
	if text == "" {
		return d, nil
	}
	for _, l := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		if m := hunkHeader.FindStringSubmatch(l); m != nil {
			oldStart, _ := strconv.Atoi(m[1])
			newStart, _ := strconv.Atoi(m[2])
			d.Hunks = append(d.Hunks, Hunk{OldStart: oldStart, NewStart: newStart, Section: m[3]})
			continue
		}
		if len(d.Hunks) == 0 {
			d.Header = append(d.Header, l)
			continue
		}
		h := &d.Hunks[len(d.Hunks)-1]
		switch {
		case strings.HasPrefix(l, `\`):
			if n := len(h.Lines); n > 0 {
				h.Lines[n-1].NoNewline = true
			}
		case strings.HasPrefix(l, "+"):
			h.Lines = append(h.Lines, DiffLine{Kind: LineAdd, Text: l[1:]})
		case strings.HasPrefix(l, "-"):
			h.Lines = append(h.Lines, DiffLine{Kind: LineDel, Text: l[1:]})
		case strings.HasPrefix(l, " "), l == "":
			h.Lines = append(h.Lines, DiffLine{Kind: LineContext, Text: strings.TrimPrefix(l, " ")})
		default:
			return ParsedDiff{}, fmt.Errorf("worktree: unexpected diff line %q", l)
		}
	}
	return d, nil
}

// BuildPatch keeps only the selected changes of d. Unselected lines must stay
// as they are in the content the patch is applied to: forward (staging, onto
// the index, which has the - lines) an unselected - becomes context and an
// unselected + is dropped; reverse (unstaging and discarding, applied with -R
// onto content that has the + lines) it is the other way round. Header counts
// are recomputed, and the start of the side that moves is shifted by the net
// change of the hunks before it.
func BuildPatch(d ParsedDiff, sel Selection, reverse bool) (string, error) {
	picked := map[int]map[int]bool{} // hunk → selected body lines; a nil map is the whole hunk
	for _, p := range sel {
		if p.Hunk < 0 || p.Hunk >= len(d.Hunks) {
			return "", fmt.Errorf("worktree: no hunk %d", p.Hunk)
		}
		if len(p.Lines) == 0 {
			picked[p.Hunk] = nil
			continue
		}
		lines, seen := picked[p.Hunk]
		if seen && lines == nil {
			continue // already taken whole
		}
		if lines == nil {
			lines = map[int]bool{}
			picked[p.Hunk] = lines
		}
		for _, i := range p.Lines {
			if i < 0 || i >= len(d.Hunks[p.Hunk].Lines) || d.Hunks[p.Hunk].Lines[i].Kind == LineContext {
				return "", fmt.Errorf("worktree: line %d of hunk %d is not a change", i, p.Hunk)
			}
			lines[i] = true
		}
	}

	var b strings.Builder
	for _, l := range d.Header {
		// A mode change belongs to the whole file, not to part of it.
		if strings.HasPrefix(l, "old mode ") || strings.HasPrefix(l, "new mode ") {
			continue
		}
		b.WriteString(l)
		b.WriteByte('\n')
	}
	shift, changed := 0, false
	for hi, h := range d.Hunks {
		lines, ok := picked[hi]
		if !ok {
			continue
		}
		var body []DiffLine
		oldCount, newCount, changes := 0, 0, 0
		for li, l := range h.Lines {
			kind := l.Kind
			if kind != LineContext && lines != nil && !lines[li] {
				if (kind == LineDel) != reverse {
					kind = LineContext
				} else {
					continue
				}
			}
			if kind != LineContext {
				changes++
			}
			if kind != LineAdd {
				oldCount++
			}
			if kind != LineDel {
				newCount++
			}
			body = append(body, DiffLine{Kind: kind, Text: l.Text, NoNewline: l.NoNewline})
		}
		if changes == 0 {
			continue
		}
		if splitsLastLine(body) {
			return "", ErrSplitsLastLine
		}
		changed = true

		// The anchor is the side the patch is applied to, which keeps its
		// original start; the other side lands shifted by earlier hunks.
		anchor, anchorCount, otherCount := h.OldStart, oldCount, newCount
		if reverse {
			anchor, anchorCount, otherCount = h.NewStart, newCount, oldCount
		}
		first := anchor
		if anchorCount == 0 {
			first++ // an empty side's start is the line before it
		}
		other := first + shift
		if otherCount == 0 {
			other--
		}
		shift += otherCount - anchorCount
		oldStart, newStart := anchor, other
		if reverse {
			oldStart, newStart = other, anchor
		}

		fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@%s\n", oldStart, oldCount, newStart, newCount, h.Section)
		for _, l := range body {
			b.WriteByte(" +-"[l.Kind])
			b.WriteString(l.Text)
			b.WriteByte('\n')
			if l.NoNewline {
				b.WriteString("\\ No newline at end of file\n")
			}
		}
	}
	if !changed {
		return "", ErrEmptySelection
	}
	return b.String(), nil
}

// splitsLastLine reports whether a line marked "no newline at end of file"
// would be followed by more lines on the same side (old: context and -, new:
// context and +); git apply would glue them together.
func splitsLastLine(body []DiffLine) bool {
	for _, skip := range []LineKind{LineAdd, LineDel} {
		marked := false
		for _, l := range body {
			if l.Kind == skip {
				continue
			}
			if marked {
				return true
			}
			marked = l.NoNewline
		}
	}
	return false
}
