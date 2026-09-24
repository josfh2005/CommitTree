package merge

import (
	"context"
	"errors"
	"strings"
	"testing"

	"git-ui/internal/testrepo"
)

func TestCherryPickClean(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("switch", "-q", "-c", "feature")
	picked := r.Commit("on feature")
	r.Git("switch", "-q", "main")
	got, err := CherryPick(context.Background(), r.Dir, picked)
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != Picked {
		t.Fatalf("outcome = %v, want Picked", got.Outcome)
	}
	if s := r.Git("log", "-1", "--format=%s"); s != "on feature" {
		t.Errorf("HEAD subject = %q", s)
	}
}

func TestCherryPickConflicted(t *testing.T) {
	r := conflicting(t) // on main
	got, err := CherryPick(context.Background(), r.Dir, "feature")
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != Conflicted || strings.Join(got.Conflicts, ",") != "greeting.txt" {
		t.Fatalf("result = %+v", got)
	}
	if st := status(t, r.Dir); st.Kind != KindCherryPick {
		t.Fatalf("kind = %q", st.Kind)
	}
}

func TestCherryPickOfChangesAlreadyThereIsNothingToApply(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("switch", "-q", "-c", "feature")
	r.WriteFile("a.txt", "same\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-m", "add a on feature")
	r.Git("switch", "-q", "main")
	r.WriteFile("a.txt", "same\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-m", "add a on main")
	before := r.Git("rev-parse", "HEAD")

	got, err := CherryPick(context.Background(), r.Dir, "feature")
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != NothingToApply {
		t.Fatalf("outcome = %v, want NothingToApply", got.Outcome)
	}
	if st := status(t, r.Dir); st.Merging {
		t.Fatalf("state = %+v, want no cherry-pick left in progress", st)
	}
	if r.Git("rev-parse", "HEAD") != before {
		t.Error("HEAD moved")
	}
}

func TestCherryPickRefusesAMergeCommit(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("switch", "-q", "-c", "side")
	r.Commit("on side")
	r.Git("switch", "-q", "main")
	r.Commit("on main")
	r.Git("merge", "-q", "--no-ff", "--no-edit", "side")
	mergeCommit := r.Git("rev-parse", "HEAD")
	r.Git("switch", "-q", "-c", "other", "HEAD~1")
	if _, err := CherryPick(context.Background(), r.Dir, mergeCommit); !errors.Is(err, ErrMergeCommit) {
		t.Fatalf("err = %v, want ErrMergeCommit", err)
	}
}

func TestErrMergeCommitText(t *testing.T) {
	if ErrMergeCommit.Error() != "Cherry-picking a merge commit isn't supported" {
		t.Fatalf("ErrMergeCommit.Error() = %q", ErrMergeCommit.Error())
	}
}

func TestCherryPickRefusesTrackedChanges(t *testing.T) {
	r := conflicting(t)
	r.WriteFile("greeting.txt", "dirty\n")
	if _, err := CherryPick(context.Background(), r.Dir, "feature"); !errors.Is(err, ErrDirtyWorktree) {
		t.Fatalf("err = %v, want ErrDirtyWorktree", err)
	}
}

func TestSkipACherryPick(t *testing.T) {
	r := conflicting(t)
	before := r.Git("rev-parse", "HEAD")
	if _, err := CherryPick(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	if err := Skip(context.Background(), r.Dir); err != nil {
		t.Fatal(err)
	}
	if st := status(t, r.Dir); st.Merging {
		t.Fatalf("state = %+v", st)
	}
	if r.Git("rev-parse", "HEAD") != before {
		t.Error("a skipped cherry-pick moved HEAD")
	}
}

func TestSkipARebaseStep(t *testing.T) {
	r := conflicting(t)
	r.Git("switch", "-q", "feature")
	if _, err := Rebase(context.Background(), r.Dir, "main"); err != nil {
		t.Fatal(err)
	}
	if err := Skip(context.Background(), r.Dir); err != nil {
		t.Fatal(err)
	}
	if st := status(t, r.Dir); st.Merging {
		t.Fatalf("state = %+v", st)
	}
	// The only feature commit was skipped: feature now sits on main.
	if r.Git("rev-parse", "HEAD") != r.Git("rev-parse", "main") {
		t.Error("want feature == main after skipping its only commit")
	}
}

func TestSkipARebaseStepStopsOnTheNextConflict(t *testing.T) {
	r := twoStepRebase(t)
	if err := Skip(context.Background(), r.Dir); err != nil {
		t.Fatalf("Skip() = %v, want nil: the sequencer moved to step 2's conflict", err)
	}
	if st := status(t, r.Dir); st.Kind != KindRebase || st.Step != 2 {
		t.Fatalf("state = %+v, want stopped on step 2", st)
	}
}

func TestSkipRefusesAMerge(t *testing.T) {
	r := conflicting(t)
	if _, err := Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	if err := Skip(context.Background(), r.Dir); !errors.Is(err, ErrNothingToSkip) {
		t.Fatalf("err = %v, want ErrNothingToSkip", err)
	}
}

// Taking the base's side wholesale empties the replayed commit. Depending on
// git's version and backend, Continue then either finishes or refuses with
// "No changes"; either way Continue followed by Skip must get the user out.
func TestAnEmptiedRebaseStepCanAlwaysBeLeft(t *testing.T) {
	r := conflicting(t)
	r.Git("switch", "-q", "feature")
	if _, err := Rebase(context.Background(), r.Dir, "main"); err != nil {
		t.Fatal(err)
	}
	r.WriteFile("greeting.txt", "hi\n") // main's version
	r.Git("add", "greeting.txt")
	if err := Continue(context.Background(), r.Dir); err != nil {
		if err := Skip(context.Background(), r.Dir); err != nil {
			t.Fatalf("Continue failed and Skip failed too: %v", err)
		}
	}
	if st := status(t, r.Dir); st.Merging {
		t.Fatalf("state = %+v, want the rebase finished", st)
	}
}
