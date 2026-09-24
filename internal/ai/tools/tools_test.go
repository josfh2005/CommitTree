package tools_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"git-ui/internal/ai"
	"git-ui/internal/ai/tools"
	"git-ui/internal/gitlog"
	"git-ui/internal/testrepo"
)

var ctx = context.Background()

func run(dir, name string, args map[string]any) string {
	return tools.Run(ctx, dir, ai.ToolCall{ID: "c1", Name: name, Args: args})
}

func TestSpecsListAllTools(t *testing.T) {
	var names []string
	for _, s := range tools.Specs() {
		names = append(names, s.Name)
		if s.Description == "" || s.Parameters["type"] != "object" {
			t.Errorf("%s: incomplete spec %+v", s.Name, s)
		}
	}
	if strings.Join(names, ",") != "search_log,show_commit,diff_commit_file,list_refs,file_history,blame_file" {
		t.Fatalf("names = %v", names)
	}
}

func TestBlameFile(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile("a.txt", "one\ntwo\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-m", "add a")
	first := r.Git("rev-parse", "--short=7", "HEAD")
	r.WriteFile("a.txt", "one\nTWO\n")
	r.Git("commit", "-q", "-am", "edit a")
	second := r.Git("rev-parse", "--short=7", "HEAD")
	r.WriteFile("a.txt", "one\nTWO\nthree\n")

	got := run(r.Dir, "blame_file", map[string]any{"path": "a.txt"})
	want := []string{"L1-1  " + first, "add a", "L2-2  " + second, "edit a"}
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Fatalf("blame_file missing %q:\n%s", w, got)
		}
	}
	if strings.Contains(got, "not committed yet") {
		t.Fatalf("default rev is HEAD, not the working tree:\n%s", got)
	}

	ranged := run(r.Dir, "blame_file", map[string]any{"path": "a.txt", "rev": "HEAD", "start_line": float64(2), "end_line": float64(2)})
	if strings.Contains(ranged, "L1-1") || !strings.Contains(ranged, "L2-2  "+second) {
		t.Fatalf("ranged:\n%s", ranged)
	}
	if !strings.HasPrefix(run(r.Dir, "blame_file", map[string]any{}), "error: ") {
		t.Fatal("want an error without path")
	}
}

func TestBlameFileCapsBlocks(t *testing.T) {
	r := testrepo.New(t)
	var b strings.Builder
	for i := 0; i < tools.MaxBlameBlocks+5; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	r.WriteFile("big.txt", b.String())
	r.Git("add", "big.txt")
	r.Git("commit", "-q", "-m", "base")
	// Rewrite every other line in a second commit so blocks alternate.
	b.Reset()
	for i := 0; i < tools.MaxBlameBlocks+5; i++ {
		if i%2 == 0 {
			fmt.Fprintf(&b, "LINE %d\n", i)
		} else {
			fmt.Fprintf(&b, "line %d\n", i)
		}
	}
	r.WriteFile("big.txt", b.String())
	r.Git("commit", "-q", "-am", "edit")
	got := run(r.Dir, "blame_file", map[string]any{"path": "big.txt"})
	if !strings.Contains(got, "pass start_line/end_line to narrow") {
		t.Fatalf("want the cap note, got %d bytes", len(got))
	}
}

func TestFormatBlameUncommitted(t *testing.T) {
	got := tools.FormatBlame([]gitlog.BlameBlock{{Start: 3, Count: 2, Uncommitted: true}})
	if got != "L3-4  (not committed yet)" {
		t.Fatalf("got %q", got)
	}
}

func TestSearchLogShowCommitAndDiff(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("chore: base")
	r.WriteFile("notes.md", "hello\n")
	r.Git("add", "notes.md")
	r.Git("commit", "-q", "-m", "feat: add notes NEXO-42")
	hash := r.Git("rev-parse", "HEAD")
	short := hash[:7]
	r.Git("tag", "v1.0")

	log := run(r.Dir, "search_log", map[string]any{"text": "NEXO-42"})
	if !strings.Contains(log, short) || !strings.Contains(log, "feat: add notes NEXO-42") || strings.Contains(log, "chore: base") {
		t.Fatalf("search_log = %q", log)
	}
	if !strings.Contains(log, "Test User") || !strings.Contains(log, "v1.0") {
		t.Fatalf("search_log missing author or refs: %q", log)
	}

	show := run(r.Dir, "show_commit", map[string]any{"rev": short})
	for _, want := range []string{hash[:7], "feat: add notes NEXO-42", "A notes.md", "Test User"} {
		if !strings.Contains(show, want) {
			t.Errorf("show_commit missing %q: %q", want, show)
		}
	}

	diff := run(r.Dir, "diff_commit_file", map[string]any{"rev": "HEAD", "path": "notes.md"})
	if !strings.Contains(diff, "+hello") {
		t.Fatalf("diff = %q", diff)
	}
}

func TestListRefsAndFileHistory(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("one")
	r.Git("branch", "feature/x")
	r.WriteFile("a.txt", "1\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-m", "touch a once")
	r.WriteFile("a.txt", "2\n")
	r.Git("commit", "-q", "-am", "touch a twice")

	refs := run(r.Dir, "list_refs", nil)
	for _, want := range []string{"Current branch: main", "feature/x"} {
		if !strings.Contains(refs, want) {
			t.Errorf("list_refs missing %q: %q", want, refs)
		}
	}

	history := run(r.Dir, "file_history", map[string]any{"path": "a.txt"})
	if strings.Count(history, "\n")+1 != 2 || !strings.Contains(history, "touch a twice") || strings.Contains(history, "one") {
		t.Fatalf("file_history = %q", history)
	}
}

func TestDiffIsCappedInLines(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	var b strings.Builder
	for i := 0; i < 400; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	r.WriteFile("big.txt", b.String())
	r.Git("add", "big.txt")
	r.Git("commit", "-q", "-m", "big")

	diff := run(r.Dir, "diff_commit_file", map[string]any{"rev": "HEAD", "path": "big.txt"})
	if !strings.Contains(diff, "[diff truncated at 300 lines]") || strings.Contains(diff, "line 399") {
		t.Fatalf("diff not capped: %d chars", len(diff))
	}
}

func TestErrorsAreText(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"show_commit", map[string]any{"rev": "-x"}, "error: invalid revision"},
		{"show_commit", map[string]any{"rev": "deadbeef"}, "error: unknown revision"},
		{"show_commit", nil, "error: rev is required"},
		{"diff_commit_file", map[string]any{"rev": "HEAD"}, "error: path is required"},
		{"search_log", map[string]any{"branch": "--all"}, "error: invalid branch"},
		{"nope", nil, "error: unknown tool"},
	}
	for _, c := range cases {
		if got := run(r.Dir, c.name, c.args); !strings.HasPrefix(got, c.want) {
			t.Errorf("%s %v = %q, want prefix %q", c.name, c.args, got, c.want)
		}
	}
}

func TestLimitsAndTruncate(t *testing.T) {
	r := testrepo.New(t)
	for i := 0; i < 60; i++ {
		r.Commit(fmt.Sprintf("commit %d", i))
	}
	def := run(r.Dir, "search_log", nil)
	if n := strings.Count(def, "\n") + 1; n != 20 {
		t.Fatalf("default limit returned %d lines", n)
	}
	max := run(r.Dir, "search_log", map[string]any{"limit": float64(500)})
	if n := strings.Count(max, "\n") + 1; n != 50 {
		t.Fatalf("max limit returned %d lines", n)
	}

	long := strings.Repeat("é", tools.MaxOutput)
	cut := tools.Truncate(long, 10)
	if !strings.HasSuffix(cut, "\n[truncated]") || !strings.HasPrefix(cut, "éééé") {
		t.Fatalf("truncate = %q", cut)
	}
	if tools.Truncate("short", 10) != "short" {
		t.Fatal("short text changed")
	}
}
