package submodules_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"git-ui/internal/submodules"
	"git-ui/internal/testrepo"
)

// withSub returns a parent repo with lib (from a separate repo) added at path.
func withSub(t *testing.T, path string) (*testrepo.Repo, *testrepo.Repo) {
	t.Helper()
	lib := testrepo.New(t)
	lib.WriteFile("a.txt", "a")
	// Tracked explicitly (not via Commit, which only adds its own generated
	// file) so later tests can modify a.txt and see it as a tracked change.
	lib.Git("add", "a.txt")
	lib.Git("commit", "-q", "-m", "lib one")
	parent := testrepo.New(t)
	parent.WriteFile("p.txt", "p")
	parent.Commit("parent one")
	parent.Git("-c", "protocol.file.allow=always", "submodule", "add", "-q", lib.Dir, path)
	parent.Git("commit", "-q", "-m", "add submodule")
	return parent, lib
}

func find(t *testing.T, list []submodules.Submodule, path string) submodules.Submodule {
	t.Helper()
	for _, s := range list {
		if s.Path == path {
			return s
		}
	}
	t.Fatalf("%s not in %+v", path, list)
	return submodules.Submodule{}
}

func TestListInSync(t *testing.T) {
	parent, _ := withSub(t, "vendor/lib")
	list, err := submodules.List(context.Background(), parent.Dir)
	if err != nil {
		t.Fatal(err)
	}
	s := find(t, list, "vendor/lib")
	if !s.Initialised || !s.Configured || s.Moved || s.Modified || s.Untracked || s.Recorded == "" || s.CheckedOut != s.Recorded || s.Name != "vendor/lib" {
		t.Fatalf("unexpected %+v", s)
	}
}

func TestListMovedModifiedUntracked(t *testing.T) {
	parent, _ := withSub(t, "lib")
	sub := filepath.Join(parent.Dir, "lib")
	os.WriteFile(filepath.Join(sub, "a.txt"), []byte("changed"), 0o644)
	// The submodule clone has no identity of its own; pass one per command.
	run := func(args ...string) {
		parent.Git(append([]string{"-C", sub, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
	}
	run("commit", "-q", "-am", "moved")
	os.WriteFile(filepath.Join(sub, "a.txt"), []byte("dirty"), 0o644)
	os.WriteFile(filepath.Join(sub, "new.txt"), []byte("n"), 0o644)
	list, _ := submodules.List(context.Background(), parent.Dir)
	s := find(t, list, "lib")
	if !s.Moved || !s.Modified || !s.Untracked || s.Branch == "" {
		t.Fatalf("unexpected %+v", s)
	}
}

func TestListUninitialised(t *testing.T) {
	parent, _ := withSub(t, "lib")
	clone := testrepo.Clone(t, parent.Dir) // clones without --recurse-submodules
	list, err := submodules.List(context.Background(), clone.Dir)
	if err != nil {
		t.Fatal(err)
	}
	s := find(t, list, "lib")
	if s.Initialised || s.CheckedOut != "" || s.Recorded == "" || !s.Configured {
		t.Fatalf("unexpected %+v", s)
	}
}

func TestListNestedIsFlat(t *testing.T) {
	inner := testrepo.New(t)
	inner.WriteFile("z.txt", "z")
	inner.Commit("inner")
	mid := testrepo.New(t)
	mid.WriteFile("m.txt", "m")
	mid.Commit("mid")
	mid.Git("-c", "protocol.file.allow=always", "submodule", "add", "-q", inner.Dir, "deps/zlib")
	mid.Git("commit", "-q", "-m", "add inner")
	top := testrepo.New(t)
	top.WriteFile("t.txt", "t")
	top.Commit("top")
	top.Git("-c", "protocol.file.allow=always", "submodule", "add", "-q", mid.Dir, "vendor/lib")
	top.Git("-c", "protocol.file.allow=always", "submodule", "update", "--init", "--recursive", "-q")
	top.Git("commit", "-q", "-m", "add mid")
	list, _ := submodules.List(context.Background(), top.Dir)
	if len(list) != 2 || list[0].Path != "vendor/lib" || list[1].Path != "vendor/lib/deps/zlib" || !list[1].Initialised {
		t.Fatalf("unexpected %+v", list)
	}
}

func TestListPathWithSpace(t *testing.T) {
	parent, _ := withSub(t, "third party/lib")
	list, _ := submodules.List(context.Background(), parent.Dir)
	if s := find(t, list, "third party/lib"); !s.Initialised {
		t.Fatalf("unexpected %+v", s)
	}
}

func TestListNotConfigured(t *testing.T) {
	parent, _ := withSub(t, "lib")
	parent.Git("config", "-f", ".gitmodules", "--remove-section", "submodule.lib")
	list, err := submodules.List(context.Background(), parent.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if s := find(t, list, "lib"); s.Configured {
		t.Fatalf("unexpected %+v", s)
	}
}

func TestListNone(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile("a", "a")
	r.Commit("one")
	list, err := submodules.List(context.Background(), r.Dir)
	if err != nil || len(list) != 0 || submodules.HasAny(r.Dir) {
		t.Fatalf("%v %v", list, err)
	}
}

// TestListConflict covers an unmerged gitlink: List must not fail (unlike
// `git submodule status`, which aborts) and must report Conflict with no
// Recorded commit, since there is no single recorded stage.
func TestListConflict(t *testing.T) {
	parent, lib := withSub(t, "lib")
	sub := filepath.Join(parent.Dir, "lib")
	run := func(args ...string) string {
		return parent.Git(append([]string{"-C", sub, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
	}
	_ = lib
	base := run("rev-parse", "HEAD")

	// A and B are made siblings of base (not ancestor/descendant of each
	// other): git's submodule merge heuristic silently fast-forwards a
	// gitlink when one recorded commit is an ancestor of the other, so a
	// genuine conflict needs diverging submodule history.
	parent.Git("checkout", "-q", "-b", "x")
	os.WriteFile(filepath.Join(sub, "a.txt"), []byte("a1"), 0o644)
	run("commit", "-q", "-am", "a")
	parent.Git("add", "lib")
	parent.Git("commit", "-q", "-m", "record a on x")

	parent.Git("checkout", "-q", "main")
	run("checkout", "-q", base)
	os.WriteFile(filepath.Join(sub, "a.txt"), []byte("a2"), 0o644)
	run("commit", "-q", "-am", "b")
	parent.Git("add", "lib")
	parent.Git("commit", "-q", "-m", "record b on main")

	parent.GitFails("merge", "x")

	list, err := submodules.List(context.Background(), parent.Dir)
	if err != nil {
		t.Fatal(err)
	}
	s := find(t, list, "lib")
	if !s.Conflict || s.Recorded != "" {
		t.Fatalf("unexpected %+v", s)
	}
}
