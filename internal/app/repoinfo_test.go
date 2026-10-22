package app

import (
	"errors"
	"testing"
)

func TestCollectGarbagePacksAndIsAWrite(t *testing.T) {
	a, id := newTestApp(t)
	before, err := a.RepoInfo(id)
	if err != nil {
		t.Fatal(err)
	}
	if before.Loose == 0 {
		t.Fatalf("before = %+v, want loose objects from the test commits", before)
	}

	unlock, err := a.lockWrite(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.CollectGarbage(id); !errors.Is(err, ErrBusy) {
		t.Errorf("CollectGarbage during another write: err = %v, want ErrBusy", err)
	}
	unlock()

	if err := a.CollectGarbage(id); err != nil {
		t.Fatal(err)
	}
	after, err := a.RepoInfo(id)
	if err != nil {
		t.Fatal(err)
	}
	if after.Loose != 0 || after.Packs == 0 {
		t.Fatalf("after = %+v, want everything packed", after)
	}
	if size, err := a.WorktreeSize(id); err != nil || size == 0 {
		t.Fatalf("WorktreeSize = %d, %v; want the test repo's files", size, err)
	}
}
