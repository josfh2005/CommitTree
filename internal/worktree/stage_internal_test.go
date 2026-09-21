package worktree

import "testing"

// deleteUntracked tolerates a path that is already gone by the time it runs
// (e.g. a race between Status and Discard, or a caller retrying): there is
// nothing left to discard, so it is not an error.
func TestDeleteUntrackedOfAnAlreadyVanishedPathReturnsNil(t *testing.T) {
	dir := t.TempDir()
	if err := deleteUntracked(dir, "never-existed.txt"); err != nil {
		t.Errorf("deleteUntracked of a missing file = %v, want nil", err)
	}
}
