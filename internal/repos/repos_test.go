package repos_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"git-ui/internal/repos"
	"git-ui/internal/testrepo"
)

var ctx = context.Background()

func realPath(t *testing.T, p string) string {
	t.Helper()
	out, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestAddListRemovePersist(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("init")
	sub := filepath.Join(r.Dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "cfg", "repos.json")

	s, err := repos.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	added, err := s.Add(ctx, sub)
	if err != nil {
		t.Fatal(err)
	}
	top := realPath(t, r.Dir)
	if added.Path != top || added.Name != filepath.Base(top) || added.ID == "" {
		t.Fatalf("added = %+v, want path %s", added, top)
	}

	again, err := s.Add(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != added.ID || len(s.List()) != 1 {
		t.Fatalf("duplicate add created a new entry: %+v", s.List())
	}

	reopened, err := repos.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	list := reopened.List()
	if len(list) != 1 || list[0].ID != added.ID || list[0].Missing {
		t.Fatalf("reopened list = %+v", list)
	}

	if err := reopened.Remove(added.ID); err != nil {
		t.Fatal(err)
	}
	final, _ := repos.Open(file)
	if len(final.List()) != 0 {
		t.Fatalf("after remove = %+v", final.List())
	}
	if err := final.Remove("nope"); !errors.Is(err, repos.ErrUnknownRepo) {
		t.Fatalf("remove unknown: %v", err)
	}
}

func TestAddRejectsNonRepo(t *testing.T) {
	s, _ := repos.Open(filepath.Join(t.TempDir(), "repos.json"))
	_, err := s.Add(ctx, t.TempDir())
	if !errors.Is(err, repos.ErrNotRepo) {
		t.Fatalf("want ErrNotRepo, got %v", err)
	}
}

func TestListFlagsMissing(t *testing.T) {
	r := testrepo.New(t)
	s, _ := repos.Open(filepath.Join(t.TempDir(), "repos.json"))
	if _, err := s.Add(ctx, r.Dir); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(r.Dir); err != nil {
		t.Fatal(err)
	}
	if list := s.List(); !list[0].Missing {
		t.Fatalf("want missing, got %+v", list)
	}
}

func TestAddAfterRelocateGetsUniqueID(t *testing.T) {
	dir1 := testrepo.New(t)
	dir2 := testrepo.New(t)
	s, _ := repos.Open(filepath.Join(t.TempDir(), "repos.json"))

	added, err := s.Add(ctx, dir1.Dir)
	if err != nil {
		t.Fatal(err)
	}
	relocated, err := s.Relocate(ctx, added.ID, dir2.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if relocated.ID != added.ID {
		t.Fatalf("relocate changed ID: %+v", relocated)
	}

	readded, err := s.Add(ctx, dir1.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if readded.ID == relocated.ID {
		t.Fatalf("re-added entry reused ID %q", readded.ID)
	}

	list := s.List()
	if len(list) != 2 {
		t.Fatalf("want 2 entries, got %+v", list)
	}
	gotRelocated, ok := s.Get(relocated.ID)
	if !ok || gotRelocated.Path != relocated.Path {
		t.Fatalf("Get(relocated) = %+v %v", gotRelocated, ok)
	}
	gotReadded, ok := s.Get(readded.ID)
	if !ok || gotReadded.Path != readded.Path {
		t.Fatalf("Get(readded) = %+v %v", gotReadded, ok)
	}
}

func TestRelocateKeepsID(t *testing.T) {
	a := testrepo.New(t)
	b := testrepo.New(t)
	s, _ := repos.Open(filepath.Join(t.TempDir(), "repos.json"))
	added, _ := s.Add(ctx, a.Dir)

	moved, err := s.Relocate(ctx, added.ID, b.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if moved.ID != added.ID || moved.Path != realPath(t, b.Dir) {
		t.Fatalf("moved = %+v", moved)
	}
	got, ok := s.Get(added.ID)
	if !ok || got.Path != moved.Path {
		t.Fatalf("Get = %+v %v", got, ok)
	}
}

func TestSetGroupPersists(t *testing.T) {
	r := testrepo.New(t)
	file := filepath.Join(t.TempDir(), "repos.json")
	s, _ := repos.Open(file)
	added, err := s.Add(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if added.Group != "" {
		t.Fatalf("new repo group = %q, want empty", added.Group)
	}

	if err := s.SetGroup(added.ID, "work"); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Get(added.ID)
	if !ok || got.Group != "work" {
		t.Fatalf("Get after SetGroup = %+v %v", got, ok)
	}

	reopened, err := repos.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	list := reopened.List()
	if len(list) != 1 || list[0].Group != "work" {
		t.Fatalf("reopened list = %+v", list)
	}

	// Empty string clears the group again.
	if err := reopened.SetGroup(added.ID, ""); err != nil {
		t.Fatal(err)
	}
	got, ok = reopened.Get(added.ID)
	if !ok || got.Group != "" {
		t.Fatalf("Get after clearing group = %+v %v", got, ok)
	}

	if err := reopened.SetGroup("nope", "x"); !errors.Is(err, repos.ErrUnknownRepo) {
		t.Fatalf("SetGroup unknown: %v", err)
	}
}

func TestOpenWithoutGroupFieldLoadsUngrouped(t *testing.T) {
	s, err := repos.Open("testdata/repos_no_group.json")
	if err != nil {
		t.Fatal(err)
	}
	list := s.List()
	if len(list) != 1 || list[0].ID != "abc123def456" || list[0].Group != "" {
		t.Fatalf("list = %+v, want one ungrouped entry", list)
	}
}
