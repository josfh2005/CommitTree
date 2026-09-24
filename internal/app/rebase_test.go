package app

import (
	"testing"

	"git-ui/internal/merge"
)

func TestRebaseOntoLeavesConflictsForTheView(t *testing.T) {
	a, r, id := newMergeApp(t)
	r.Git("switch", "-q", "feature")
	result, err := a.RebaseOnto(id, "main")
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != merge.Conflicted {
		t.Fatalf("outcome = %v", result.Outcome)
	}
	st, err := a.GetMergeState(id)
	if err != nil || st.Kind != merge.KindRebase {
		t.Fatalf("state = %+v, %v", st, err)
	}
	if err := a.SkipStep(id); err != nil {
		t.Fatal(err)
	}
	if st, _ := a.GetMergeState(id); st.Merging {
		t.Fatalf("state after skip = %+v", st)
	}
}

func TestCherryPickAndPreviewThroughTheApp(t *testing.T) {
	a, r, id := newMergeApp(t) // on main; feature conflicts with it
	p, err := a.GetRebasePreview(id, "feature")
	if err != nil || p.Commits != 1 {
		t.Fatalf("preview = %+v, %v", p, err)
	}
	contained, err := a.IsAncestorOfHead(id, "feature")
	if err != nil || contained {
		t.Fatalf("IsAncestorOfHead(feature) = %v, %v", contained, err)
	}
	result, err := a.CherryPick(id, "feature")
	if err != nil || result.Outcome != merge.Conflicted {
		t.Fatalf("result = %+v, %v", result, err)
	}
	_ = r
}
