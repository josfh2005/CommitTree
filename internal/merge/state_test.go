package merge

import (
	"context"
	"testing"

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
