package worktrees

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"git-ui/internal/testrepo"
)

func TestParsePorcelain(t *testing.T) {
	out := "worktree /r\x00HEAD aaa\x00branch refs/heads/main\x00\x00" +
		"worktree /tmp/with space/wt\x00HEAD bbb\x00branch refs/heads/one\x00\x00" +
		"worktree /tmp/det\x00HEAD ccc\x00detached\x00\x00" +
		"worktree /tmp/gone\x00HEAD ddd\x00branch refs/heads/two\x00prunable gitdir file points to non-existent location\x00\x00"
	got := parse(out)
	want := []Worktree{
		{Path: "/r", Head: "aaa", Branch: "main", Main: true},
		{Path: "/tmp/with space/wt", Head: "bbb", Branch: "one"},
		{Path: "/tmp/det", Head: "ccc", Detached: true},
		{Path: "/tmp/gone", Head: "ddd", Branch: "two", Prunable: true},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d worktrees: %+v", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("worktree %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestListReadsRealWorktrees(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	tmp := t.TempDir()
	wt := filepath.Join(tmp, "with space")
	det := filepath.Join(tmp, "det")
	r.Git("worktree", "add", "-q", wt, "-b", "one")
	r.Git("worktree", "add", "-q", "--detach", det)

	list, err := List(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	// git lists the main tree first and linked worktrees sorted by path.
	byName := func(list []Worktree, name string) Worktree {
		for _, w := range list {
			if filepath.Base(w.Path) == name {
				return w
			}
		}
		t.Fatalf("no worktree %q in %+v", name, list)
		return Worktree{}
	}
	if len(list) != 3 || !list[0].Main || byName(list, "with space").Branch != "one" || !byName(list, "det").Detached {
		t.Fatalf("list = %+v", list)
	}

	if err := os.RemoveAll(det); err != nil {
		t.Fatal(err)
	}
	list, err = List(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || !byName(list, "det").Prunable || byName(list, "with space").Prunable {
		t.Fatalf("after removing the directory: %+v", list)
	}
}
