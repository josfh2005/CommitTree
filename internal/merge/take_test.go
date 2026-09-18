package merge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"git-ui/internal/testrepo"
)

// binaryConflict is a merge whose one conflict, logo.bin, is binary: Manual.
func binaryConflict(t *testing.T) *testrepo.Repo {
	t.Helper()
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
	return r
}

func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestTakeTheirsOnABinaryConflictStagesTheirVersion(t *testing.T) {
	r := binaryConflict(t)
	if err := Take(context.Background(), r.Dir, "logo.bin", Theirs); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, r.Dir, "logo.bin"); got != "\x00\x01theirs\n" {
		t.Errorf("logo.bin = %q, want theirs", got)
	}
	st := status(t, r.Dir)
	if len(st.Manual) != 0 || !slices.Equal(st.Staged, []string{"logo.bin"}) {
		t.Errorf("manual = %v, staged = %v", st.Manual, st.Staged)
	}
}

// Taking ours leaves the file as our branch has it, so there is nothing
// left to show: it is settled and matches HEAD.
func TestTakeOursOnABinaryConflictSettlesIt(t *testing.T) {
	r := binaryConflict(t)
	if err := Take(context.Background(), r.Dir, "logo.bin", Ours); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, r.Dir, "logo.bin"); got != "\x00\x01ours\n" {
		t.Errorf("logo.bin = %q, want ours", got)
	}
	st := status(t, r.Dir)
	if len(st.Manual) != 0 || len(st.Conflicts) != 0 || len(st.Unstaged) != 0 {
		t.Errorf("state = %+v, want it settled", st)
	}
}

// In modifyDelete our side deleted gone.txt and theirs edited it.
func TestTakeTheirsOnModifyDeleteKeepsTheirEdit(t *testing.T) {
	r := modifyDelete(t)
	if err := Take(context.Background(), r.Dir, "gone.txt", Theirs); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, r.Dir, "gone.txt"); got != "edited on feature\n" {
		t.Errorf("gone.txt = %q, want their edit", got)
	}
	if st := status(t, r.Dir); len(st.Manual) != 0 || !slices.Equal(st.Staged, []string{"gone.txt"}) {
		t.Errorf("manual = %v, staged = %v", st.Manual, st.Staged)
	}
}

func TestTakeTheSideThatDeletedRemovesTheFile(t *testing.T) {
	r := modifyDelete(t)
	if err := Take(context.Background(), r.Dir, "gone.txt", Ours); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(r.Dir, "gone.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("gone.txt still exists: %v", err)
	}
	if st := status(t, r.Dir); len(st.Manual) != 0 || len(st.Staged) != 0 {
		t.Errorf("manual = %v, staged = %v, want it settled as deleted", st.Manual, st.Staged)
	}
}

// Only Manual files: a text conflict is the agent's or the editor's, and
// pathspec magic or a stranger path must never reach git checkout.
func TestTakeAcceptsOnlyManualPaths(t *testing.T) {
	r := binaryConflict(t)
	r.WriteFile("other.txt", "x\n")
	for _, path := range []string{":(glob)*", "*", "other.txt", "", "../x"} {
		if err := Take(context.Background(), r.Dir, path, Theirs); !errors.Is(err, ErrNotInMerge) {
			t.Errorf("Take(%q) = %v, want ErrNotInMerge", path, err)
		}
	}
	c := conflicting(t)
	if _, err := Start(context.Background(), c.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	if err := Take(context.Background(), c.Dir, "greeting.txt", Theirs); !errors.Is(err, ErrNotInMerge) {
		t.Errorf("Take on a text conflict = %v, want ErrNotInMerge", err)
	}
	if st := status(t, r.Dir); !slices.Equal(st.Manual, []string{"logo.bin"}) {
		t.Errorf("manual = %v, want logo.bin untouched", st.Manual)
	}
}

func TestTakeRejectsAnUnknownSide(t *testing.T) {
	r := binaryConflict(t)
	if err := Take(context.Background(), r.Dir, "logo.bin", Side("both")); err == nil {
		t.Error("want an error for an unknown side")
	}
}
