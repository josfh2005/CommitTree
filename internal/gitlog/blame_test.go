package gitlog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git-ui/internal/testrepo"
)

const h1 = "1111111111111111111111111111111111111111"
const h2 = "2222222222222222222222222222222222222222"

func TestGetBlameSHA256Repository(t *testing.T) {
	r := testrepo.New(t)
	if err := os.RemoveAll(filepath.Join(r.Dir, ".git")); err != nil {
		t.Fatal(err)
	}
	r.Git("init", "-q", "--object-format=sha256", "-b", "main")
	r.Git("config", "user.name", "Test User")
	r.Git("config", "user.email", "test@example.com")
	r.Git("config", "commit.gpgsign", "false")
	r.WriteFile("a.txt", "one\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-m", "add")
	head := r.Git("rev-parse", "HEAD")
	r.WriteFile("a.txt", "one\ntwo\n")

	b, err := GetBlame(context.Background(), r.Dir, "", "a.txt", BlameOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(head) != 64 || len(b.Blocks) != 2 || b.Blocks[0].Hash != head || b.Blocks[0].Short != head[:7] {
		t.Fatalf("head %q, blocks = %+v", head, b.Blocks)
	}
	if !b.Blocks[1].Uncommitted || b.Blocks[1].Author != "" {
		t.Fatalf("uncommitted block = %+v", b.Blocks[1])
	}
}

func TestParsePorcelainRepeatsMetadata(t *testing.T) {
	out := strings.Join([]string{
		h1 + " 1 1 1",
		"author Ana", "author-mail <ana@x>", "author-time 1700000000", "author-tz +0200",
		"summary first", "filename a.txt",
		"\tone",
		h2 + " 1 2 1",
		"author Bea", "author-mail <bea@x>", "author-time 1700000100", "author-tz -0300",
		"summary second", "previous " + h1 + " old.txt", "filename a.txt",
		"\ttwo",
		h1 + " 2 3 1",
		"filename a.txt",
		"\tthree",
		"",
	}, "\n")
	b, err := parsePorcelain(out, 100)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(b.Lines, ",") != "one,two,three" || b.StartLine != 1 {
		t.Fatalf("lines = %v start %d", b.Lines, b.StartLine)
	}
	if len(b.Blocks) != 3 {
		t.Fatalf("blocks = %+v", b.Blocks)
	}
	last := b.Blocks[2]
	if last.Hash != h1 || last.Author != "Ana" || last.Email != "ana@x" || last.Summary != "first" || last.Start != 3 || last.Count != 1 {
		t.Fatalf("repeated block = %+v", last)
	}
	mid := b.Blocks[1]
	if mid.Previous != h1 || mid.PrevPath != "old.txt" || mid.Short != "2222222" {
		t.Fatalf("previous = %+v", mid)
	}
	if _, off := b.Blocks[0].Date.Zone(); off != 2*3600 || b.Blocks[0].Date.Unix() != 1700000000 {
		t.Fatalf("date = %v", b.Blocks[0].Date)
	}
}

func TestParsePorcelainUnquotesNonASCIIPaths(t *testing.T) {
	out := strings.Join([]string{
		h1 + " 1 1 1",
		"author Ana", "author-mail <ana@x>", "author-time 1700000000", "author-tz +0000",
		"summary first", `filename "a\303\261o.txt"`,
		"\tone",
		h2 + " 1 2 1",
		"author Bea", "author-mail <bea@x>", "author-time 1700000100", "author-tz +0000",
		"summary second", `previous ` + h1 + ` "a\303\261o.txt"`, `filename "a\303\261o.txt"`,
		"\ttwo",
		"",
	}, "\n")
	b, err := parsePorcelain(out, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Blocks) != 2 {
		t.Fatalf("blocks = %+v", b.Blocks)
	}
	if b.Blocks[0].Filename != "año.txt" {
		t.Fatalf("filename = %q", b.Blocks[0].Filename)
	}
	if b.Blocks[1].PrevPath != "año.txt" {
		t.Fatalf("prevPath = %q", b.Blocks[1].PrevPath)
	}
}

func TestGetBlameStripsCRLF(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile("win.txt", "one\r\ntwo\r\n")
	r.Git("add", "win.txt")
	r.Git("commit", "-q", "-m", "crlf")
	b, err := GetBlame(context.Background(), r.Dir, "HEAD", "win.txt", BlameOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(b.Lines, ",") != "one,two" {
		t.Fatalf("lines = %q", b.Lines)
	}
}

func TestParsePorcelainGroupsConsecutiveLines(t *testing.T) {
	out := h1 + " 1 1 2\nauthor A\nauthor-mail <a@x>\nauthor-time 1\nauthor-tz +0000\nsummary s\nboundary\nfilename f\n\ta\n" +
		h1 + " 2 2\nfilename f\n\tb\n"
	b, err := parsePorcelain(out, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Blocks) != 1 || b.Blocks[0].Count != 2 || !b.Blocks[0].Boundary {
		t.Fatalf("blocks = %+v", b.Blocks)
	}
}

func TestParsePorcelainTruncatesAndRejectsBinary(t *testing.T) {
	out := h1 + " 1 1 3\nauthor A\nauthor-mail <a@x>\nauthor-time 1\nauthor-tz +0000\nsummary s\nfilename f\n\ta\n" +
		h1 + " 2 2\nfilename f\n\tb\n" + h1 + " 3 3\nfilename f\n\tc\n"
	b, err := parsePorcelain(out, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !b.Truncated || len(b.Lines) != 2 || b.Blocks[0].Count != 2 {
		t.Fatalf("truncated = %+v", b)
	}
	bin := h1 + " 1 1 1\nauthor A\nauthor-mail <a@x>\nauthor-time 1\nauthor-tz +0000\nsummary s\nfilename f\n\tx\x00y\n"
	if _, err := parsePorcelain(bin, 10); !errors.Is(err, ErrBinaryBlame) {
		t.Fatalf("binary err = %v", err)
	}
}

func TestGetBlameAtRevisionAndPrevious(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile("a.txt", "one\ntwo\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-m", "add a")
	first := r.Git("rev-parse", "HEAD")
	r.WriteFile("a.txt", "one\nTWO\nthree\n")
	r.Git("commit", "-q", "-am", "edit a")
	second := r.Git("rev-parse", "HEAD")

	b, err := GetBlame(context.Background(), r.Dir, second, "a.txt", BlameOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if b.Rev != second || b.Path != "a.txt" || strings.Join(b.Lines, ",") != "one,TWO,three" {
		t.Fatalf("blame = %+v", b)
	}
	if len(b.Blocks) != 2 || b.Blocks[0].Hash != first || b.Blocks[1].Hash != second || b.Blocks[1].Start != 2 || b.Blocks[1].Count != 2 {
		t.Fatalf("blocks = %+v", b.Blocks)
	}
	if b.Blocks[0].Boundary {
		t.Fatal("root commit must not be a boundary (--root)")
	}
	if b.Blocks[1].Previous != first || b.Blocks[1].PrevPath != "a.txt" {
		t.Fatalf("previous = %+v", b.Blocks[1])
	}
}

func TestGetBlameRange(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile("a.txt", "1\n2\n3\n4\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-m", "add")
	b, err := GetBlame(context.Background(), r.Dir, "HEAD", "a.txt", BlameOptions{Start: 2, End: 3})
	if err != nil {
		t.Fatal(err)
	}
	if b.StartLine != 2 || strings.Join(b.Lines, ",") != "2,3" || b.Blocks[0].Start != 2 || b.Blocks[0].Count != 2 {
		t.Fatalf("range blame = %+v", b)
	}
}

func TestGetBlameWorkingTreeRename(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile("a.txt", "x\ny\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-m", "add")
	r.Git("mv", "a.txt", "b.txt")
	r.WriteFile("b.txt", "x\nz\n")
	b, err := GetBlame(context.Background(), r.Dir, "", "b.txt", BlameOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Blocks) != 2 || b.Blocks[0].Filename != "a.txt" {
		t.Fatalf("blocks = %+v", b.Blocks)
	}
	u := b.Blocks[1]
	if !u.Uncommitted || u.Author != "" || u.Summary != "" || u.Previous != "" || u.PrevPath != "" {
		t.Fatalf("uncommitted block = %+v", u)
	}
}

func TestGetBlameNonASCIIFilename(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile("año.txt", "uno\n")
	r.Git("add", "año.txt")
	r.Git("commit", "-q", "-m", "add")
	r.WriteFile("año.txt", "uno\ndos\n")
	r.Git("commit", "-q", "-am", "edit")

	b, err := GetBlame(context.Background(), r.Dir, "", "año.txt", BlameOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Blocks) == 0 {
		t.Fatalf("blocks = %+v", b.Blocks)
	}
	for _, blk := range b.Blocks {
		if blk.Filename != "año.txt" {
			t.Fatalf("filename = %q, want año.txt (block %+v)", blk.Filename, blk)
		}
	}

	r.Git("mv", "año.txt", "b.txt")
	r.WriteFile("b.txt", "uno\ndos\ntres\n")
	renamed, err := GetBlame(context.Background(), r.Dir, "", "b.txt", BlameOptions{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, blk := range renamed.Blocks {
		if blk.PrevPath == "año.txt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no block with prevPath año.txt: %+v", renamed.Blocks)
	}
}

func TestGetBlameIgnoreWhitespace(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile("a.txt", "if x {\nfoo()\n}\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-m", "add")
	first := r.Git("rev-parse", "HEAD")
	r.WriteFile("a.txt", "if x {\n\tfoo()\n}\n")
	r.Git("commit", "-q", "-am", "indent")

	plain, err := GetBlame(context.Background(), r.Dir, "HEAD", "a.txt", BlameOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ws, err := GetBlame(context.Background(), r.Dir, "HEAD", "a.txt", BlameOptions{IgnoreWhitespace: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(plain.Blocks) != 3 || len(ws.Blocks) != 1 || ws.Blocks[0].Hash != first {
		t.Fatalf("plain %d blocks, -w %+v", len(plain.Blocks), ws.Blocks)
	}
}

func TestGetBlameErrors(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	ctx := context.Background()
	if _, err := GetBlame(ctx, r.Dir, "--output=x", "file-1.txt", BlameOptions{}); err == nil {
		t.Fatal("want error for an option-looking rev")
	}
	if _, err := GetBlame(ctx, r.Dir, "HEAD", "", BlameOptions{}); err == nil {
		t.Fatal("want error for an empty path")
	}
	if _, err := GetBlame(ctx, r.Dir, "HEAD", "nope.txt", BlameOptions{}); err == nil {
		t.Fatal("want error for a missing path")
	}
	if _, err := GetBlame(ctx, r.Dir, "", "../outside.txt", BlameOptions{}); err == nil {
		t.Fatal("want error for a path outside the repository")
	}
	r.WriteFile("bin.dat", "a\x00b\n")
	r.Git("add", "bin.dat")
	r.Git("commit", "-q", "-m", "bin")
	if _, err := GetBlame(ctx, r.Dir, "HEAD", "bin.dat", BlameOptions{}); !errors.Is(err, ErrBinaryBlame) {
		t.Fatalf("binary err = %v", err)
	}
}

func TestGetBlameCap(t *testing.T) {
	old := maxBlameLines
	maxBlameLines = 2
	defer func() { maxBlameLines = old }()
	r := testrepo.New(t)
	r.WriteFile("a.txt", "1\n2\n3\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-m", "add")
	b, err := GetBlame(context.Background(), r.Dir, "HEAD", "a.txt", BlameOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !b.Truncated || len(b.Lines) != 2 {
		t.Fatalf("cap = %+v", b)
	}
}
