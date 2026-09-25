package gitlog

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"git-ui/internal/gitcmd"
)

var (
	ErrBinaryBlame  = errors.New("binary file — blame not available")
	ErrBlameTimeout = errors.New("blame took too long")
)

// maxBlameLines caps how much of a file a blame returns; a variable so tests
// can lower it.
var maxBlameLines = 20000

const zeroHash = "0000000000000000000000000000000000000000"

type BlameOptions struct {
	IgnoreWhitespace bool
	Start, End       int // 1-based inclusive line range; 0, 0 = whole file
}

// BlameBlock is a run of consecutive lines last changed by the same commit.
type BlameBlock struct {
	Hash        string    `json:"hash"`
	Short       string    `json:"short"`
	Author      string    `json:"author"`
	Email       string    `json:"email"`
	Date        time.Time `json:"date"`
	Summary     string    `json:"summary"`
	Filename    string    `json:"filename"` // the file's path in that commit
	Start       int       `json:"start"`    // first line number in the blamed file
	Count       int       `json:"count"`
	Previous    string    `json:"previous,omitempty"`
	PrevPath    string    `json:"prevPath,omitempty"`
	Boundary    bool      `json:"boundary,omitempty"`
	Uncommitted bool      `json:"uncommitted,omitempty"`
}

type Blame struct {
	Path      string       `json:"path"`
	Rev       string       `json:"rev"` // "" = working tree
	StartLine int          `json:"startLine"`
	Lines     []string     `json:"lines"`
	Blocks    []BlameBlock `json:"blocks"`
	Truncated bool         `json:"truncated"`
}

// GetBlame blames path at rev ("" = the working tree). A rev that looks like
// an option is refused rather than passed after --end-of-options, which git
// blame does not accept in front of a revision.
func GetBlame(ctx context.Context, dir, rev, path string, opts BlameOptions) (Blame, error) {
	if path == "" {
		return Blame{}, errors.New("gitlog: blame needs a path")
	}
	if strings.HasPrefix(rev, "-") {
		return Blame{}, fmt.Errorf("gitlog: invalid revision %q", rev)
	}
	args := []string{"blame", "--porcelain", "--root"}
	if opts.IgnoreWhitespace {
		args = append(args, "-w")
	}
	if opts.Start > 0 && opts.End >= opts.Start {
		args = append(args, "-L", fmt.Sprintf("%d,%d", opts.Start, opts.End))
	}
	if rev != "" {
		args = append(args, rev)
	}
	args = append(args, "--", path)
	out, err := gitcmd.Run(ctx, dir, gitcmd.BlameTimeout, args...)
	if err != nil {
		if errors.Is(err, gitcmd.ErrTimeout) {
			return Blame{}, ErrBlameTimeout
		}
		return Blame{}, err
	}
	b, err := parsePorcelain(out, maxBlameLines)
	if err != nil {
		return Blame{}, err
	}
	b.Path, b.Rev = path, rev
	return b, nil
}

type blameMeta struct {
	author, email, summary, filename, previous, prevPath, tz string
	time                                                     int64
	boundary                                                 bool
}

// parsePorcelain reads `git blame --porcelain`. Git prints a commit's
// headers only the first time the commit appears, so they are kept per hash
// and copied into every block of that commit.
func parsePorcelain(out string, limit int) (Blame, error) {
	var b Blame
	metas := map[string]*blameMeta{}
	var cur *blameMeta
	var hash string
	var final int
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "\t"):
			if cur == nil {
				return Blame{}, errors.New("gitlog: blame line before its header")
			}
			// A CRLF file's lines keep their carriage return; drop it so it
			// is not shown or sent to the model as part of the line.
			text := strings.TrimSuffix(line[1:], "\r")
			if strings.IndexByte(text, 0) >= 0 {
				return Blame{}, ErrBinaryBlame
			}
			if len(b.Lines) == limit {
				b.Truncated = true
				return b, nil
			}
			if len(b.Lines) == 0 {
				b.StartLine = final
			}
			b.Lines = append(b.Lines, text)
			if n := len(b.Blocks) - 1; n >= 0 && b.Blocks[n].Hash == hash && b.Blocks[n].Start+b.Blocks[n].Count == final {
				b.Blocks[n].Count++
			} else {
				b.Blocks = append(b.Blocks, cur.block(hash, final))
			}
			cur = nil
		case line == "":
		case cur == nil:
			f := strings.Fields(line)
			if len(f) < 3 || len(f[0]) != 40 {
				return Blame{}, fmt.Errorf("gitlog: unexpected blame header %q", line)
			}
			n, err := strconv.Atoi(f[2])
			if err != nil {
				return Blame{}, fmt.Errorf("gitlog: bad blame line number in %q", line)
			}
			hash, final = f[0], n
			if metas[hash] == nil {
				metas[hash] = &blameMeta{}
			}
			cur = metas[hash]
		default:
			key, val, _ := strings.Cut(line, " ")
			switch key {
			case "author":
				cur.author = val
			case "author-mail":
				cur.email = strings.TrimSuffix(strings.TrimPrefix(val, "<"), ">")
			case "author-time":
				cur.time, _ = strconv.ParseInt(val, 10, 64)
			case "author-tz":
				cur.tz = val
			case "summary":
				cur.summary = val
			case "filename":
				cur.filename = unquoteGitPath(val)
			case "previous":
				var prevPath string
				cur.previous, prevPath, _ = strings.Cut(val, " ")
				cur.prevPath = unquoteGitPath(prevPath)
			case "boundary":
				cur.boundary = true
			}
		}
	}
	return b, nil
}

func (m *blameMeta) block(hash string, start int) BlameBlock {
	blk := BlameBlock{
		Hash: hash, Short: hash[:7], Author: m.author, Email: m.email, Summary: m.summary,
		Filename: m.filename, Date: time.Unix(m.time, 0).In(tzOffset(m.tz)),
		Start: start, Count: 1, Previous: m.previous, PrevPath: m.prevPath, Boundary: m.boundary,
	}
	if hash == zeroHash {
		// Git labels these "Not Committed Yet" and may add a previous line
		// pointing at HEAD; neither means anything for lines no commit has.
		blk.Uncommitted = true
		blk.Author, blk.Email, blk.Summary, blk.Previous, blk.PrevPath = "", "", "", "", ""
	}
	return blk
}

// unquoteGitPath decodes a path that git porcelain quoted because it has
// non-ASCII or otherwise unusual bytes, e.g. `"a\303\261o.txt"`. Paths that
// were not quoted are returned unchanged.
func unquoteGitPath(s string) string {
	if len(s) >= 2 && s[0] == '"' {
		if u, err := strconv.Unquote(s); err == nil {
			return u
		}
	}
	return s
}

// tzOffset turns git's "+0200" into a fixed zone.
func tzOffset(tz string) *time.Location {
	if len(tz) != 5 {
		return time.UTC
	}
	h, err1 := strconv.Atoi(tz[1:3])
	m, err2 := strconv.Atoi(tz[3:5])
	if err1 != nil || err2 != nil {
		return time.UTC
	}
	secs := h*3600 + m*60
	if tz[0] == '-' {
		secs = -secs
	}
	return time.FixedZone(tz, secs)
}
