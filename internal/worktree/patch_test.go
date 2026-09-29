package worktree_test

import (
	"errors"
	"strings"
	"testing"

	"git-ui/internal/worktree"
)

const header = "diff --git a/f.txt b/f.txt\nindex 1111111..2222222 100644\n--- a/f.txt\n+++ b/f.txt\n"

// sample has two hunks. Hunk 0 body: 0 " one", 1 "-two", 2 "+TWO",
// 3 "+two-and-a-half", 4 " three". Hunk 1 body: 0 " ten", 1 "-eleven", 2 " twelve".
const sample = header +
	"@@ -1,3 +1,4 @@ func main()\n one\n-two\n+TWO\n+two-and-a-half\n three\n" +
	"@@ -10,3 +11,2 @@\n ten\n-eleven\n twelve\n"

func parse(t *testing.T, text string) worktree.ParsedDiff {
	t.Helper()
	d, err := worktree.ParseDiff(text)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestParseDiff(t *testing.T) {
	d := parse(t, sample)
	if len(d.Header) != 4 || len(d.Hunks) != 2 {
		t.Fatalf("header %d lines, %d hunks; want 4 and 2", len(d.Header), len(d.Hunks))
	}
	h := d.Hunks[1]
	if h.OldStart != 10 || h.NewStart != 11 || len(h.Lines) != 3 || h.Lines[1].Kind != worktree.LineDel || h.Lines[1].Text != "eleven" {
		t.Fatalf("hunk 1 = %+v", h)
	}
	if d.Hunks[0].Section != " func main()" {
		t.Errorf("section = %q", d.Hunks[0].Section)
	}
}

// Review Focus 2: an added "++ x" shows as "+++ x" and a removed "-- y" as
// "--- y"; inside a hunk they are changes, not file headers.
func TestParseDiffKeepsHeaderLookalikesInsideAHunk(t *testing.T) {
	d := parse(t, header+"@@ -1,2 +1,2 @@\n--- y\n+++ x\n keep\n")
	lines := d.Hunks[0].Lines
	if len(d.Header) != 4 || len(lines) != 3 || lines[0].Kind != worktree.LineDel || lines[0].Text != "-- y" || lines[1].Kind != worktree.LineAdd || lines[1].Text != "++ x" {
		t.Fatalf("header = %q, lines = %+v", d.Header, lines)
	}
}

func TestParseDiffMarksNoNewline(t *testing.T) {
	d := parse(t, header+"@@ -1,2 +1,2 @@\n a\n-b\n\\ No newline at end of file\n+c\n\\ No newline at end of file\n")
	lines := d.Hunks[0].Lines
	if len(lines) != 3 || !lines[1].NoNewline || !lines[2].NoNewline || lines[0].NoNewline {
		t.Fatalf("lines = %+v", lines)
	}
}

func TestBuildPatch(t *testing.T) {
	tests := []struct {
		name    string
		diff    string
		sel     worktree.Selection
		reverse bool
		want    string
	}{
		{
			name: "whole second hunk, forward",
			diff: sample, sel: worktree.Selection{{Hunk: 1}},
			want: header + "@@ -10,3 +10,2 @@\n ten\n-eleven\n twelve\n",
		},
		{
			name: "forward: an unselected - becomes context, an unselected + is dropped",
			diff: sample, sel: worktree.Selection{{Hunk: 0, Lines: []int{2}}},
			want: header + "@@ -1,3 +1,4 @@ func main()\n one\n two\n+TWO\n three\n",
		},
		{
			name: "forward: the next hunk's new start follows the lines this patch removes",
			diff: sample, sel: worktree.Selection{{Hunk: 1}, {Hunk: 0, Lines: []int{1}}},
			want: header + "@@ -1,3 +1,2 @@ func main()\n one\n-two\n three\n@@ -10,3 +9,2 @@\n ten\n-eleven\n twelve\n",
		},
		{
			name: "reverse: an unselected + becomes context, an unselected - is dropped",
			diff: sample, sel: worktree.Selection{{Hunk: 0, Lines: []int{2}}}, reverse: true,
			want: header + "@@ -1,3 +1,4 @@ func main()\n one\n+TWO\n two-and-a-half\n three\n",
		},
		{
			name: "reverse: the next hunk's old start follows the lines this patch restores",
			diff: sample, sel: worktree.Selection{{Hunk: 0, Lines: []int{1}}, {Hunk: 1}}, reverse: true,
			want: header + "@@ -1,5 +1,4 @@ func main()\n one\n-two\n TWO\n two-and-a-half\n three\n@@ -12,3 +11,2 @@\n ten\n-eleven\n twelve\n",
		},
		{
			name: "a side left empty uses the line-before convention",
			diff: header + "@@ -1 +1 @@\n-a\n+b\n", sel: worktree.Selection{{Hunk: 0, Lines: []int{0}}},
			want: header + "@@ -1,1 +0,0 @@\n-a\n",
		},
		{
			name: "no-newline markers survive a whole hunk",
			diff: header + "@@ -1,2 +1,2 @@\n a\n-b\n\\ No newline at end of file\n+c\n\\ No newline at end of file\n",
			sel:  worktree.Selection{{Hunk: 0}},
			want: header + "@@ -1,2 +1,2 @@\n a\n-b\n\\ No newline at end of file\n+c\n\\ No newline at end of file\n",
		},
		{
			name: "removing only the last line without a newline",
			diff: header + "@@ -1,2 +1,2 @@\n a\n-b\n\\ No newline at end of file\n+c\n\\ No newline at end of file\n",
			sel:  worktree.Selection{{Hunk: 0, Lines: []int{1}}},
			want: header + "@@ -1,2 +1,1 @@\n a\n-b\n\\ No newline at end of file\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := worktree.BuildPatch(parse(t, tt.diff), tt.sel, tt.reverse)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("patch =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

func TestBuildPatchRefuses(t *testing.T) {
	noNewline := parse(t, header+"@@ -1,2 +1,2 @@\n a\n-b\n\\ No newline at end of file\n+c\n\\ No newline at end of file\n")
	tests := []struct {
		name string
		d    worktree.ParsedDiff
		sel  worktree.Selection
		want error
	}{
		{"nothing selected", parse(t, sample), nil, worktree.ErrEmptySelection},
		{"a context line", parse(t, sample), worktree.Selection{{Hunk: 0, Lines: []int{0}}}, nil},
		{"a hunk that does not exist", parse(t, sample), worktree.Selection{{Hunk: 2}}, nil},
		{"a line out of range", parse(t, sample), worktree.Selection{{Hunk: 1, Lines: []int{3}}}, nil},
		// Review Focus 3: keeping "b" (no newline) as context before "+c" is not a valid file.
		{"splitting a last line without a newline", noNewline, worktree.Selection{{Hunk: 0, Lines: []int{2}}}, worktree.ErrSplitsLastLine},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := worktree.BuildPatch(tt.d, tt.sel, false)
			if err == nil {
				t.Fatal("want an error")
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if strings.Contains(err.Error(), "%!") {
				t.Errorf("badly formatted error %q", err)
			}
		})
	}
}
