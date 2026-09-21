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

// A stash index is not stable across the async gap a conflicted Pop opens:
// dropping some other stash before the conflict is resolved shifts every
// index below it down by one. The owed drop must still land on the entry
// that actually conflicted, not on whatever now sits at its old index —
// StashDrop needs no clean tree, so it is reachable mid-conflict, unlike
// StashPush.
func TestStashPopConflictSurvivesAnIndexShiftBeforeResolution(t *testing.T) {
	a, r, id := newPlainApp(t)
	r.WriteFile("f.txt", "one\n")
	r.Git("add", "f.txt")
	r.Git("commit", "-q", "-m", "base f")

	r.WriteFile("keep.txt", "keep me\n")
	r.Git("add", "keep.txt")
	r.Git("stash", "push", "-q", "-m", "keep")

	r.WriteFile("f.txt", "stashed\n")
	r.Git("stash", "push", "-q", "-m", "wip")

	r.WriteFile("shifter.txt", "drop me\n")
	r.Git("add", "shifter.txt")
	r.Git("stash", "push", "-q", "-m", "shifter")

	r.WriteFile("f.txt", "conflicting\n")
	r.Git("commit", "-q", "-am", "conflicting")

	// stash@{0} is "shifter", stash@{1} is "wip", stash@{2} is "keep";
	// popping index 1 ("wip") conflicts and leaves all three in place.
	if err := a.StashPop(id, 1); err != nil {
		t.Fatal(err)
	}
	entries, err := a.GetStashEntries(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %+v, want all three stashes still present", entries)
	}
	owedHash := entries[1].Hash
	keepHash := entries[2].Hash
	if owed := a.OwedStashDrop(id); owed != 1 {
		t.Fatalf("owed = %d, want 1 before the shift", owed)
	}

	// Drop "shifter" at index 0 mid-conflict — reachable because StashDrop
	// needs no clean tree. This shifts "wip" (the owed entry) from index 1
	// to 0, and "keep" from index 2 to 1.
	if err := a.StashDrop(id, 0); err != nil {
		t.Fatal(err)
	}
	entries, err = a.GetStashEntries(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Hash != owedHash || entries[1].Hash != keepHash {
		t.Fatalf("entries = %+v, want wip at 0 and keep at 1 after the shift", entries)
	}
	if owed := a.OwedStashDrop(id); owed != 0 {
		t.Errorf("owed = %d, want 0 after the shift", owed)
	}

	r.WriteFile("f.txt", "resolved\n")
	if err := a.StageMergeFile(id, "f.txt"); err != nil {
		t.Fatal(err)
	}
	if owed := a.OwedStashDrop(id); owed != -1 {
		t.Errorf("owed = %d, want -1 once resolved", owed)
	}

	// The original owed entry ("wip") must be the one gone, and "keep" —
	// which never conflicted and was never the target of any drop — must
	// have survived. A naive index-keyed reminder would instead drop
	// whatever now sits at index 0, which by this point is "keep".
	entries, err = a.GetStashEntries(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want exactly one stash left", entries)
	}
	if entries[0].Hash != keepHash {
		t.Errorf("surviving entry = %+v, want %q (keep) to survive, not the owed one dropped by mistake", entries[0], keepHash)
	}
	if entries[0].Hash == owedHash {
		t.Errorf("the owed stash (%q) survived; it should have been dropped", owedHash)
	}
}
