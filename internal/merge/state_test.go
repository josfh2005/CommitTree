package merge

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"git-ui/internal/testrepo"
)

func TestStatusWhenNotMerging(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")

	st, err := Status(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Merging {
		t.Fatalf("merging = true, want false: %+v", st)
	}
}

func TestStatusDuringAConflict(t *testing.T) {
	r := conflicting(t)
	if _, err := Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}

	st, err := Status(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Merging {
		t.Fatal("merging = false, want true")
	}
	if st.Into != "main" || st.From != "feature" {
		t.Errorf("merging %q into %q, want feature into main", st.From, st.Into)
	}
	if len(st.Conflicts) != 1 || st.Conflicts[0] != "greeting.txt" {
		t.Errorf("conflicts = %v", st.Conflicts)
	}
	if len(st.Manual) != 0 {
		t.Errorf("manual = %v, want none", st.Manual)
	}
}

// A binary conflict has no markers to splice, so it belongs in Manual.
func TestStatusPutsAMarkerlessConflictInManual(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile("logo.bin", "\x00\x01base\n")
	r.Git("add", "logo.bin")
	r.Git("commit", "-q", "-m", "add logo")
	r.Git("switch", "-q", "-c", "feature")
	r.WriteFile("logo.bin", "\x00\x01theirs\n")
	r.Git("commit", "-q", "-am", "their logo")
	r.Git("switch", "-q", "main")
	r.WriteFile("logo.bin", "\x00\x01ours\n")
	r.Git("commit", "-q", "-am", "our logo")

	if _, err := Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	st, err := Status(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Manual) != 1 || st.Manual[0] != "logo.bin" {
		t.Fatalf("manual = %v, conflicts = %v", st.Manual, st.Conflicts)
	}
}

func TestStatusAfterStagingTheResolution(t *testing.T) {
	r := conflicting(t)
	if _, err := Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	r.WriteFile("greeting.txt", "hi there\n")
	r.Git("add", "greeting.txt")

	st, err := Status(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Merging {
		t.Error("merging = false: the merge is still open until it is committed")
	}
	if len(st.Conflicts) != 0 {
		t.Errorf("conflicts = %v, want none left", st.Conflicts)
	}
}

func TestStatusNamesTheSourceByHashWhenTheMessageHasNoBranch(t *testing.T) {
	r := conflicting(t)
	hash := strings.TrimSpace(r.Git("rev-parse", "feature"))
	// Merging a raw commit gives a MERGE_MSG with no quoted branch name.
	Start(context.Background(), r.Dir, hash)

	st, err := Status(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Merging {
		t.Fatal("merging = false, want true")
	}
	if st.From == "" || !strings.HasPrefix(hash, st.From) {
		t.Errorf("from = %q, want a short prefix of %s", st.From, hash)
	}
}

// modifyDelete builds a merge where main deleted gone.txt and feature edited
// it: git leaves it unmerged with no stage 2 and no markers to splice.
func modifyDelete(t *testing.T) *testrepo.Repo {
	t.Helper()
	r := testrepo.New(t)
	r.WriteFile("gone.txt", "base\n")
	r.Git("add", "gone.txt")
	r.Git("commit", "-q", "-m", "add gone")
	r.Git("switch", "-q", "-c", "feature")
	r.WriteFile("gone.txt", "edited on feature\n")
	r.Git("commit", "-q", "-am", "edit gone")
	r.Git("switch", "-q", "main")
	r.Git("rm", "-q", "gone.txt")
	r.Git("commit", "-q", "-m", "delete gone")
	if _, err := Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestStatusPutsAModifyDeleteConflictInManual(t *testing.T) {
	r := modifyDelete(t)
	st, err := Status(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Manual) != 1 || st.Manual[0] != "gone.txt" {
		t.Fatalf("manual = %v, conflicts = %v, want gone.txt in manual", st.Manual, st.Conflicts)
	}
	if len(st.Conflicts) != 0 {
		t.Errorf("conflicts = %v, want none", st.Conflicts)
	}
}

// Removing every marker does not settle a file; staging does. Until then it
// is still the agent's to stage, not a human's to settle.
func TestStatusKeepsAResolvedButUnstagedFileInConflicts(t *testing.T) {
	r := conflicting(t)
	if _, err := Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	r.WriteFile("greeting.txt", "hi there\n")

	st, err := Status(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Conflicts) != 1 || st.Conflicts[0] != "greeting.txt" {
		t.Errorf("conflicts = %v, want greeting.txt", st.Conflicts)
	}
	if len(st.Manual) != 0 {
		t.Errorf("manual = %v, want none", st.Manual)
	}
}

// A worktree path that is a FIFO must be classified without being read:
// opening one blocks until a writer appears, and Status runs on every repo
// selection, so a read would make the repository unopenable.
func TestStatusDoesNotReadAFifo(t *testing.T) {
	r := conflicting(t)
	if _, err := Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(r.Dir, "greeting.txt")
	if err := os.Remove(fifo); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Fatal(err)
	}
	// If Status does block on the FIFO, open its write end on the way out so
	// the stuck reader wakes and the goroutine ends instead of leaking.
	t.Cleanup(func() {
		if f, err := os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			f.Close()
		}
	})

	type result struct {
		st  State
		err error
	}
	done := make(chan result, 1)
	go func() {
		st, err := Status(context.Background(), r.Dir)
		done <- result{st, err}
	}()
	select {
	case res := <-done:
		if res.err != nil {
			t.Fatal(res.err)
		}
		if len(res.st.Manual) != 1 || res.st.Manual[0] != "greeting.txt" {
			t.Errorf("manual = %v, conflicts = %v, want the FIFO in manual", res.st.Manual, res.st.Conflicts)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Status did not return within 5s: it is blocked reading the FIFO")
	}
}

// A path with `-merge` (or `merge=binary`) in .gitattributes gets no text
// merge at all: git leaves OUR side in the worktree, writes no markers, and
// records the path as unmerged. Without markers to prove it, it must not be
// treated as resolved, so it belongs in Manual, not Conflicts.
func TestStatusPutsANoMergeAttributeConflictInManual(t *testing.T) {
	for _, attr := range []string{"*.lock -merge", "*.lock merge=binary"} {
		t.Run(attr, func(t *testing.T) {
			r := testrepo.New(t)
			r.WriteFile(".gitattributes", attr+"\n")
			r.WriteFile("deps.lock", "base\n")
			r.Git("add", ".gitattributes", "deps.lock")
			r.Git("commit", "-q", "-m", "add deps.lock")
			r.Git("switch", "-q", "-c", "feature")
			r.WriteFile("deps.lock", "theirs\n")
			r.Git("commit", "-q", "-am", "their lock")
			r.Git("switch", "-q", "main")
			r.WriteFile("deps.lock", "ours\n")
			r.Git("commit", "-q", "-am", "our lock")

			if _, err := Start(context.Background(), r.Dir, "feature"); err != nil {
				t.Fatal(err)
			}
			st, err := Status(context.Background(), r.Dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(st.Manual) != 1 || st.Manual[0] != "deps.lock" {
				t.Fatalf("manual = %v, conflicts = %v, want deps.lock in manual", st.Manual, st.Conflicts)
			}
			if len(st.Conflicts) != 0 {
				t.Errorf("conflicts = %v, want none", st.Conflicts)
			}
		})
	}
}

// A custom conflict-marker-size writes markers our parser does not
// recognise (only the standard 7-character form is), so the file parses as
// having zero hunks even though it is still full of markers. It must land
// in Manual.
func TestStatusPutsACustomMarkerSizeConflictInManual(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile(".gitattributes", "*.txt conflict-marker-size=10\n")
	r.WriteFile("greeting.txt", "hello\n")
	r.Git("add", ".gitattributes", "greeting.txt")
	r.Git("commit", "-q", "-m", "add greeting")
	r.Git("switch", "-q", "-c", "feature")
	r.WriteFile("greeting.txt", "hola\n")
	r.Git("commit", "-q", "-am", "spanish")
	r.Git("switch", "-q", "main")
	r.WriteFile("greeting.txt", "hi\n")
	r.Git("commit", "-q", "-am", "informal")

	if _, err := Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	st, err := Status(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Manual) != 1 || st.Manual[0] != "greeting.txt" {
		t.Fatalf("manual = %v, conflicts = %v, want greeting.txt in manual", st.Manual, st.Conflicts)
	}
	if len(st.Conflicts) != 0 {
		t.Errorf("conflicts = %v, want none", st.Conflicts)
	}
}

func TestParseCheckAttrZ(t *testing.T) {
	// path\0attr\0value\0, in triples, with a path that itself contains a
	// space to make sure the split is by NUL, not whitespace.
	raw := "a b.txt\x00merge\x00unset\x00a b.txt\x00conflict-marker-size\x00unspecified\x00" +
		"plain.txt\x00merge\x00text\x00plain.txt\x00conflict-marker-size\x00" + "10\x00"
	got := parseCheckAttrZ(raw)
	want := map[string]map[string]string{
		"a b.txt":   {"merge": "unset", "conflict-marker-size": "unspecified"},
		"plain.txt": {"merge": "text", "conflict-marker-size": "10"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d paths, want %d: %+v", len(got), len(want), got)
	}
	for path, attrs := range want {
		for k, v := range attrs {
			if got[path][k] != v {
				t.Errorf("got[%q][%q] = %q, want %q", path, k, got[path][k], v)
			}
		}
	}
}

// B: OUR side had `-merge` when git wrote the conflicted file (so it kept
// OUR content with no markers), but the incoming branch also deleted that
// .gitattributes line. Checking only the post-merge worktree attributes
// would miss the rule that was actually in effect, so the path must still
// land in Manual.
func TestStatusPutsANoMergeAttributeConflictInManualWhenTheirsDropsTheAttribute(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile(".gitattributes", "*.lock -merge\n")
	r.WriteFile("deps.lock", "base\n")
	r.Git("add", ".gitattributes", "deps.lock")
	r.Git("commit", "-q", "-m", "add deps.lock")
	r.Git("switch", "-q", "-c", "feature")
	r.WriteFile(".gitattributes", "")
	r.WriteFile("deps.lock", "theirs\n")
	r.Git("commit", "-q", "-am", "drop the -merge rule and change deps.lock")
	r.Git("switch", "-q", "main")
	r.WriteFile("deps.lock", "ours\n")
	r.Git("commit", "-q", "-am", "our lock")

	if _, err := Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	st, err := Status(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Manual) != 1 || st.Manual[0] != "deps.lock" {
		t.Fatalf("manual = %v, conflicts = %v, want deps.lock in manual", st.Manual, st.Conflicts)
	}
	if len(st.Conflicts) != 0 {
		t.Errorf("conflicts = %v, want none", st.Conflicts)
	}
}

// B4: same shape as B, but with conflict-marker-size=10 instead of -merge.
func TestStatusPutsACustomMarkerSizeConflictInManualWhenTheirsDropsTheAttribute(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile(".gitattributes", "*.txt conflict-marker-size=10\n")
	r.WriteFile("greeting.txt", "hello\n")
	r.Git("add", ".gitattributes", "greeting.txt")
	r.Git("commit", "-q", "-m", "add greeting")
	r.Git("switch", "-q", "-c", "feature")
	r.WriteFile(".gitattributes", "")
	r.WriteFile("greeting.txt", "hola\n")
	r.Git("commit", "-q", "-am", "drop the marker-size rule and change greeting")
	r.Git("switch", "-q", "main")
	r.WriteFile("greeting.txt", "hi\n")
	r.Git("commit", "-q", "-am", "informal")

	if _, err := Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	st, err := Status(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Manual) != 1 || st.Manual[0] != "greeting.txt" {
		t.Fatalf("manual = %v, conflicts = %v, want greeting.txt in manual", st.Manual, st.Conflicts)
	}
	if len(st.Conflicts) != 0 {
		t.Errorf("conflicts = %v, want none", st.Conflicts)
	}
}

// A rebase that conflicts is Kind rebase, with step info from git's own
// rebase-merge bookkeeping, not Kind merge — MERGE_HEAD never exists here.
func TestStatusReportsARebaseInProgress(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("switch", "-q", "-c", "feature")
	r.WriteFile("a.txt", "feature change\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-am", "feature change")
	r.Git("switch", "-q", "main")
	r.WriteFile("a.txt", "main change\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-am", "main change")
	r.Git("switch", "-q", "feature")
	r.GitFails("rebase", "main")

	st := status(t, r.Dir)
	if st.Kind != KindRebase || !st.Merging {
		t.Fatalf("state = %+v, want Kind rebase and Merging true", st)
	}
	if st.Step != 1 || st.Total != 1 {
		t.Errorf("step/total = %d/%d, want 1/1", st.Step, st.Total)
	}
	if st.Subject != "feature change" {
		t.Errorf("subject = %q", st.Subject)
	}
	if st.From != "feature" {
		t.Errorf("from = %q, want feature", st.From)
	}
	if !slices.Contains(st.Conflicts, "a.txt") {
		t.Errorf("conflicts = %v, want a.txt", st.Conflicts)
	}
}

// A stash pop that conflicts has no MERGE_HEAD and no rebase directory —
// unmerged entries alone are the signal.
func TestStatusReportsAStashConflict(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile("a.txt", "one\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-m", "base")
	r.WriteFile("a.txt", "stashed change\n")
	r.Git("stash", "push", "-q", "-m", "wip")
	r.WriteFile("a.txt", "conflicting change\n")
	r.Git("commit", "-q", "-am", "conflicting change")
	r.GitFails("stash", "pop")

	st := status(t, r.Dir)
	if st.Kind != KindStash || !st.Merging {
		t.Fatalf("state = %+v, want Kind stash and Merging true", st)
	}
	if !slices.Contains(st.Conflicts, "a.txt") {
		t.Errorf("conflicts = %v, want a.txt", st.Conflicts)
	}
}

// A conflicted cherry-pick has unmerged entries and no MERGE_HEAD, exactly
// like a conflicted stash pop — CHERRY_PICK_HEAD is the only thing telling
// them apart, and getting this wrong makes Continue/Abort silent no-ops on
// a repository the user cannot then finish or abort from the app.
func TestStatusTellsACherryPickApartFromAStashConflict(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("switch", "-q", "-c", "feature")
	r.WriteFile("a.txt", "feature change\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-am", "feature change")
	r.Git("switch", "-q", "main")
	r.WriteFile("a.txt", "main change\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-am", "main change")
	r.GitFails("cherry-pick", "feature")

	st := status(t, r.Dir)
	if st.Kind != KindCherryPick || !st.Merging {
		t.Fatalf("state = %+v, want Kind cherry-pick and Merging true", st)
	}
	if st.Subject != "feature change" {
		t.Errorf("subject = %q, want the picked commit's subject", st.Subject)
	}
	if !slices.Contains(st.Conflicts, "a.txt") {
		t.Errorf("conflicts = %v, want a.txt", st.Conflicts)
	}
}

// A conflicted revert is the same shape, under REVERT_HEAD.
func TestStatusReportsARevertInProgress(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile("a.txt", "one\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-m", "base")
	r.WriteFile("a.txt", "two\n")
	r.Git("commit", "-q", "-am", "second")
	r.WriteFile("a.txt", "three\n")
	r.Git("commit", "-q", "-am", "third")
	r.GitFails("revert", "--no-edit", "HEAD~1")

	st := status(t, r.Dir)
	if st.Kind != KindRevert || !st.Merging {
		t.Fatalf("state = %+v, want Kind revert and Merging true", st)
	}
}
