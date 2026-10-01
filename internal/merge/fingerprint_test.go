package merge

import (
	"context"
	"strings"
	"testing"

	"git-ui/internal/testrepo"
)

// twoStepRebase stops a rebase of feature onto main on its first of two
// conflicting commits.
func twoStepRebase(t *testing.T) *testrepo.Repo {
	t.Helper()
	r := conflicting(t) // feature: "greet in spanish"; main: "greet informally"
	r.Git("switch", "-q", "feature")
	r.WriteFile("greeting.txt", "hola!!\n")
	r.Git("commit", "-q", "-am", "shout in spanish")
	r.GitFails("rebase", "main")
	return r
}

func TestFingerprintIsEmptyWithNothingInProgress(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	if fp := Fingerprint(context.Background(), r.Dir); fp != "" {
		t.Fatalf("fingerprint = %q", fp)
	}
}

func TestFingerprintNamesTheKindAndCommit(t *testing.T) {
	r := conflicting(t)
	feature := r.Git("rev-parse", "feature")
	r.GitFails("merge", "feature")
	if fp := Fingerprint(context.Background(), r.Dir); fp != "merge:"+feature {
		t.Errorf("merge fingerprint = %q", fp)
	}
	r.Git("merge", "--abort")
	r.GitFails("cherry-pick", "feature")
	if fp := Fingerprint(context.Background(), r.Dir); fp != "cherry-pick:"+feature {
		t.Errorf("cherry-pick fingerprint = %q", fp)
	}
}

func TestFingerprintChangesBetweenRebaseSteps(t *testing.T) {
	r := twoStepRebase(t)
	first := Fingerprint(context.Background(), r.Dir)
	if !strings.HasPrefix(first, "rebase:") {
		t.Fatalf("fingerprint = %q", first)
	}
	r.WriteFile("greeting.txt", "resolved one\n")
	r.Git("add", "greeting.txt")
	if err := Continue(context.Background(), r.Dir, ""); err != nil {
		t.Fatal(err)
	}
	if st := status(t, r.Dir); st.Kind != KindRebase || st.Step != 2 {
		t.Fatalf("state = %+v, want stopped on step 2", st)
	}
	if second := Fingerprint(context.Background(), r.Dir); second == first || !strings.HasPrefix(second, "rebase:") {
		t.Fatalf("second fingerprint = %q, first = %q", second, first)
	}
}

func TestStatusNamesTheSides(t *testing.T) {
	t.Run("merge", func(t *testing.T) {
		r := conflicting(t)
		r.GitFails("merge", "feature")
		st := status(t, r.Dir)
		if st.OursLabel != "main" || st.TheirsLabel != "feature" {
			t.Fatalf("labels = %q / %q", st.OursLabel, st.TheirsLabel)
		}
		if !strings.Contains(st.OursDescription, "merging into") || !strings.Contains(st.TheirsDescription, "being merged") {
			t.Fatalf("descriptions = %q / %q", st.OursDescription, st.TheirsDescription)
		}
	})
	t.Run("rebase", func(t *testing.T) {
		r := conflicting(t)
		r.Git("switch", "-q", "feature")
		short := r.Git("rev-parse", "--short", "feature")
		r.GitFails("rebase", "main")
		st := status(t, r.Dir)
		if st.OursLabel != "main" || st.TheirsLabel != short+" greet in spanish" {
			t.Fatalf("labels = %q / %q", st.OursLabel, st.TheirsLabel)
		}
		if !strings.Contains(st.OursDescription, "rebased onto") || !strings.Contains(st.TheirsDescription, "being replayed") {
			t.Fatalf("descriptions = %q / %q", st.OursDescription, st.TheirsDescription)
		}
	})
	t.Run("cherry-pick", func(t *testing.T) {
		r := conflicting(t)
		short := r.Git("rev-parse", "--short", "feature")
		r.GitFails("cherry-pick", "feature")
		st := status(t, r.Dir)
		if st.OursLabel != "main" || st.TheirsLabel != short+" greet in spanish" {
			t.Fatalf("labels = %q / %q", st.OursLabel, st.TheirsLabel)
		}
		if !strings.Contains(st.TheirsDescription, "being cherry-picked") {
			t.Fatalf("description = %q", st.TheirsDescription)
		}
	})
}
