package app

import "testing"

func TestStashPushListApplyPopDrop(t *testing.T) {
	a, r, id := newPlainApp(t)
	r.WriteFile("f.txt", "changed\n")
	r.Git("add", "f.txt")

	if err := a.StashPush(id, "wip", false); err != nil {
		t.Fatal(err)
	}
	entries, err := a.GetStashEntries(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want one", entries)
	}
	diff, err := a.GetStashDiff(id, 0)
	if err != nil {
		t.Fatal(err)
	}
	if diff == "" {
		t.Error("diff is empty")
	}
	if err := a.StashApply(id, 0); err != nil {
		t.Fatal(err)
	}
	if err := a.StashDrop(id, 0); err != nil {
		t.Fatal(err)
	}
	entries, err = a.GetStashEntries(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %+v, want none after Drop", entries)
	}
}

// A Pop that conflicts leaves the stash entry in place and routes the
// repository into the shared conflict view as Kind stash; resolving it by
// staging the conflicted file drops the owed stash automatically.
func TestStashPopConflictDropsAutomaticallyOnceResolved(t *testing.T) {
	a, r, id := newPlainApp(t)
	r.WriteFile("f.txt", "one\n")
	r.Git("add", "f.txt")
	r.Git("commit", "-q", "-m", "base f")
	r.WriteFile("f.txt", "stashed\n")
	r.Git("stash", "push", "-q", "-m", "wip")
	r.WriteFile("f.txt", "conflicting\n")
	r.Git("commit", "-q", "-am", "conflicting")

	if err := a.StashPop(id, 0); err != nil {
		t.Fatal(err)
	}
	st, err := a.GetMergeState(id)
	if err != nil {
		t.Fatal(err)
	}
	if st.Kind != "stash" || len(st.Conflicts) == 0 {
		t.Fatalf("state = %+v, want a stash conflict on f.txt", st)
	}

	if owed := a.OwedStashDrop(id); owed != 0 {
		t.Errorf("owed = %d, want 0 — the conflicted Pop still owes a drop", owed)
	}

	r.WriteFile("f.txt", "resolved\n")
	if err := a.StageMergeFile(id, "f.txt"); err != nil {
		t.Fatal(err)
	}
	if owed := a.OwedStashDrop(id); owed != -1 {
		t.Errorf("owed = %d, want -1 once the conflict is resolved", owed)
	}
	entries, err := a.GetStashEntries(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %+v, want the owed stash dropped once resolved", entries)
	}
}
