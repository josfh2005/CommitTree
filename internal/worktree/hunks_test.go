package worktree_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"git-ui/internal/testrepo"
	"git-ui/internal/worktree"
)

// numbered is "line 1\n" … "line n\n", with some lines replaced.
func numbered(n int, change map[int]string) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		line := fmt.Sprintf("line %d", i)
		if c, ok := change[i]; ok {
			line = c
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// twoHunks commits f.txt (12 numbered lines) and changes line 2 to
// "TWO\nTWO-B" and line 11 to "ELEVEN": the unstaged diff has two hunks.
// Hunk 0 body: 0 " line 1", 1 "-line 2", 2 "+TWO", 3 "+TWO-B", 4-6 context.
func twoHunks(t *testing.T) *testrepo.Repo {
	t.Helper()
	r := testrepo.New(t)
	r.WriteFile("f.txt", numbered(12, nil))
	r.Git("add", "f.txt")
	r.Git("commit", "-q", "-m", "f")
	r.WriteFile("f.txt", numbered(12, map[int]string{2: "TWO\nTWO-B", 11: "ELEVEN"}))
	return r
}

func diffOf(t *testing.T, r *testrepo.Repo, staged bool) string {
	t.Helper()
	d, err := worktree.FileDiff(ctx, r.Dir, "f.txt", staged)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func apply(t *testing.T, r *testrepo.Repo, staged bool, sel worktree.Selection, action worktree.Action) string {
	t.Helper()
	patch, err := worktree.ApplySelection(ctx, r.Dir, "f.txt", staged, worktree.DiffHash(diffOf(t, r, staged)), sel, action)
	if err != nil {
		t.Fatal(err)
	}
	return patch
}

func TestApplySelectionStagesOneHunk(t *testing.T) {
	r := twoHunks(t)
	apply(t, r, false, worktree.Selection{{Hunk: 0}}, worktree.ActionStage)

	cached, unstaged := r.Git("diff", "--cached"), r.Git("diff")
	if !strings.Contains(cached, "+TWO") || strings.Contains(cached, "+ELEVEN") {
		t.Errorf("staged diff = %s", cached)
	}
	if strings.Contains(unstaged, "+TWO") || !strings.Contains(unstaged, "+ELEVEN") {
		t.Errorf("unstaged diff = %s", unstaged)
	}
}

func TestApplySelectionStagesSomeLines(t *testing.T) {
	r := twoHunks(t)
	apply(t, r, false, worktree.Selection{{Hunk: 0, Lines: []int{2}}}, worktree.ActionStage)

	if got, want := r.Git("show", ":f.txt"), strings.TrimSuffix(numbered(12, map[int]string{2: "line 2\nTWO"}), "\n"); got != want {
		t.Errorf("index =\n%s\nwant\n%s", got, want)
	}
	if got := read(t, r.Dir, "f.txt"); got != numbered(12, map[int]string{2: "TWO\nTWO-B", 11: "ELEVEN"}) {
		t.Errorf("working tree changed:\n%s", got)
	}
}

func TestApplySelectionUnstagesOneHunk(t *testing.T) {
	r := twoHunks(t)
	r.Git("add", "f.txt")
	apply(t, r, true, worktree.Selection{{Hunk: 1}}, worktree.ActionUnstage)

	cached := r.Git("diff", "--cached")
	if !strings.Contains(cached, "+TWO") || strings.Contains(cached, "+ELEVEN") {
		t.Errorf("staged diff = %s", cached)
	}
	if !strings.Contains(read(t, r.Dir, "f.txt"), "ELEVEN") {
		t.Error("unstage touched the working tree")
	}
}

func TestApplySelectionDiscardsSomeLines(t *testing.T) {
	r := twoHunks(t)
	patch := apply(t, r, false, worktree.Selection{{Hunk: 0, Lines: []int{3}}}, worktree.ActionDiscard)

	if got, want := read(t, r.Dir, "f.txt"), numbered(12, map[int]string{2: "TWO", 11: "ELEVEN"}); got != want {
		t.Errorf("file =\n%s\nwant\n%s", got, want)
	}
	if cached := r.Git("diff", "--cached"); cached != "" {
		t.Errorf("discard touched the index: %s", cached)
	}
	if !strings.Contains(patch, "+TWO-B") {
		t.Errorf("returned patch = %s", patch)
	}
}

func TestApplySelectionOnAPartiallyStagedFile(t *testing.T) {
	r := twoHunks(t)
	r.Git("add", "f.txt")
	r.WriteFile("f.txt", numbered(12, map[int]string{2: "TWO\nTWO-B", 6: "SIX", 11: "ELEVEN"}))
	apply(t, r, false, worktree.Selection{{Hunk: 0}}, worktree.ActionDiscard)

	if got, want := read(t, r.Dir, "f.txt"), numbered(12, map[int]string{2: "TWO\nTWO-B", 11: "ELEVEN"}); got != want {
		t.Errorf("file =\n%s\nwant the staged content\n%s", got, want)
	}
	if cached := r.Git("diff", "--cached"); !strings.Contains(cached, "+ELEVEN") {
		t.Errorf("staged part lost: %s", cached)
	}
}

func TestApplySelectionRefusesAStaleDiff(t *testing.T) {
	r := twoHunks(t)
	hash := worktree.DiffHash(diffOf(t, r, false))
	r.WriteFile("f.txt", numbered(12, map[int]string{2: "OTHER"}))

	_, err := worktree.ApplySelection(ctx, r.Dir, "f.txt", false, hash, worktree.Selection{{Hunk: 0}}, worktree.ActionStage)
	if !errors.Is(err, worktree.ErrDiffChanged) {
		t.Fatalf("err = %v, want ErrDiffChanged", err)
	}
	if cached := r.Git("diff", "--cached"); cached != "" {
		t.Errorf("something was staged: %s", cached)
	}
}

func TestReapplyUndoesADiscard(t *testing.T) {
	r := twoHunks(t)
	changed := read(t, r.Dir, "f.txt")
	patch := apply(t, r, false, worktree.Selection{{Hunk: 0}}, worktree.ActionDiscard)

	if err := worktree.Reapply(ctx, r.Dir, patch); err != nil {
		t.Fatal(err)
	}
	if got := read(t, r.Dir, "f.txt"); got != changed {
		t.Errorf("after undo =\n%s\nwant\n%s", got, changed)
	}
}

func TestReapplyRefusesWhenTheLinesChangedAgain(t *testing.T) {
	r := twoHunks(t)
	patch := apply(t, r, false, worktree.Selection{{Hunk: 0}}, worktree.ActionDiscard)
	r.WriteFile("f.txt", numbered(12, map[int]string{1: "ONE", 2: "OTHER", 3: "THREE", 11: "ELEVEN"}))
	before := read(t, r.Dir, "f.txt")

	if err := worktree.Reapply(ctx, r.Dir, patch); !errors.Is(err, worktree.ErrUndoStale) {
		t.Fatalf("err = %v, want ErrUndoStale", err)
	}
	if read(t, r.Dir, "f.txt") != before {
		t.Error("a failed undo changed the file")
	}
}

func TestApplySelectionRefusesWholeFileOnlyPaths(t *testing.T) {
	r := twoHunks(t)
	r.WriteFile("new.txt", "new\n")
	for _, tc := range []struct {
		path   string
		staged bool
		action worktree.Action
		want   error
	}{
		{"new.txt", false, worktree.ActionStage, worktree.ErrNotPatchable},
		{"f.txt", false, worktree.ActionUnstage, worktree.ErrWrongSection},
		{"f.txt", true, worktree.ActionStage, worktree.ErrWrongSection},
		{"f.txt", true, worktree.ActionDiscard, worktree.ErrWrongSection},
	} {
		d, _ := worktree.FileDiff(ctx, r.Dir, tc.path, tc.staged)
		_, err := worktree.ApplySelection(ctx, r.Dir, tc.path, tc.staged, worktree.DiffHash(d), worktree.Selection{{Hunk: 0}}, tc.action)
		if !errors.Is(err, tc.want) {
			t.Errorf("%s staged=%v %s: err = %v, want %v", tc.path, tc.staged, tc.action, err, tc.want)
		}
	}
	if _, err := worktree.ApplySelection(ctx, r.Dir, "nope.txt", false, "", worktree.Selection{{Hunk: 0}}, worktree.ActionStage); err == nil {
		t.Error("an unlisted path was accepted")
	}
}

func TestApplySelectionRefusesARename(t *testing.T) {
	r := twoHunks(t)
	r.Git("checkout", "--", "f.txt")
	r.Git("mv", "f.txt", "g.txt")
	d, err := worktree.FileDiff(ctx, r.Dir, "g.txt", true)
	if err != nil {
		t.Fatal(err)
	}
	_, err = worktree.ApplySelection(ctx, r.Dir, "g.txt", true, worktree.DiffHash(d), worktree.Selection{{Hunk: 0}}, worktree.ActionUnstage)
	if !errors.Is(err, worktree.ErrNotPatchable) {
		t.Fatalf("err = %v, want ErrNotPatchable", err)
	}
}

// Review Focus 1: a user's diff settings must not change what gets applied.
func TestApplySelectionIgnoresTheUsersDiffConfig(t *testing.T) {
	r := twoHunks(t)
	r.Git("config", "diff.noprefix", "true")
	r.Git("config", "diff.mnemonicPrefix", "true")
	r.Git("config", "diff.upper.textconv", "tr a-z A-Z")
	r.WriteFile(".gitattributes", "f.txt diff=upper\n")
	apply(t, r, false, worktree.Selection{{Hunk: 1}}, worktree.ActionStage)

	if cached := r.Git("diff", "--cached", "--no-textconv"); !strings.Contains(cached, "+ELEVEN") || strings.Contains(cached, "+TWO") {
		t.Errorf("staged diff = %s", cached)
	}
}

// Review Focus 3: a file whose last line has no newline.
func TestApplySelectionOnALastLineWithoutNewline(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile("f.txt", "a\nb")
	r.Git("add", "f.txt")
	r.Git("commit", "-q", "-m", "f")
	r.WriteFile("f.txt", "a\nc")
	apply(t, r, false, worktree.Selection{{Hunk: 0}}, worktree.ActionDiscard)
	if got := read(t, r.Dir, "f.txt"); got != "a\nb" {
		t.Errorf("file = %q, want %q", got, "a\nb")
	}
}

// Review Focus 4: CRLF line endings are kept byte for byte.
func TestApplySelectionKeepsCRLF(t *testing.T) {
	r := testrepo.New(t)
	r.Git("config", "core.autocrlf", "false")
	r.WriteFile("f.txt", "one\r\ntwo\r\nthree\r\n")
	r.Git("add", "f.txt")
	r.Git("commit", "-q", "-m", "f")
	r.WriteFile("f.txt", "one\r\nTWO\r\nthree\r\n")
	apply(t, r, false, worktree.Selection{{Hunk: 0}}, worktree.ActionStage)

	if got := r.Git("cat-file", "-p", ":f.txt"); got != "one\r\nTWO\r\nthree" {
		t.Errorf("index blob = %q", got)
	}
}
