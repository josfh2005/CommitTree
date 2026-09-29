package repos_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

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

func TestRenameGroupMovesEveryRepoWithOldName(t *testing.T) {
	a := testrepo.New(t)
	b := testrepo.New(t)
	c := testrepo.New(t)
	file := filepath.Join(t.TempDir(), "repos.json")
	s, _ := repos.Open(file)
	ra, err := s.Add(ctx, a.Dir)
	if err != nil {
		t.Fatal(err)
	}
	rb, err := s.Add(ctx, b.Dir)
	if err != nil {
		t.Fatal(err)
	}
	rc, err := s.Add(ctx, c.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetGroup(ra.ID, "work"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetGroup(rb.ID, "work"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetGroup(rc.ID, "personal"); err != nil {
		t.Fatal(err)
	}

	if err := s.RenameGroup("work", "job"); err != nil {
		t.Fatal(err)
	}

	gotA, _ := s.Get(ra.ID)
	gotB, _ := s.Get(rb.ID)
	gotC, _ := s.Get(rc.ID)
	if gotA.Group != "job" || gotB.Group != "job" {
		t.Fatalf("renamed repos = %+v, %+v", gotA, gotB)
	}
	if gotC.Group != "personal" {
		t.Fatalf("unrelated repo group changed: %+v", gotC)
	}

	reopened, err := repos.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range reopened.List() {
		if (r.ID == ra.ID || r.ID == rb.ID) && r.Group != "job" {
			t.Fatalf("persisted group not renamed: %+v", r)
		}
		if r.ID == rc.ID && r.Group != "personal" {
			t.Fatalf("persisted unrelated group changed: %+v", r)
		}
	}
}

func TestRenameGroupIntoExistingNameMerges(t *testing.T) {
	a := testrepo.New(t)
	b := testrepo.New(t)
	s, _ := repos.Open(filepath.Join(t.TempDir(), "repos.json"))
	ra, err := s.Add(ctx, a.Dir)
	if err != nil {
		t.Fatal(err)
	}
	rb, err := s.Add(ctx, b.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetGroup(ra.ID, "work"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetGroup(rb.ID, "personal"); err != nil {
		t.Fatal(err)
	}

	if err := s.RenameGroup("work", "personal"); err != nil {
		t.Fatal(err)
	}

	gotA, _ := s.Get(ra.ID)
	gotB, _ := s.Get(rb.ID)
	if gotA.Group != "personal" || gotB.Group != "personal" {
		t.Fatalf("merge result = %+v, %+v", gotA, gotB)
	}
}

func TestRenameGroupSameNameDoesNotWrite(t *testing.T) {
	a := testrepo.New(t)
	file := filepath.Join(t.TempDir(), "repos.json")
	s, _ := repos.Open(file)
	ra, err := s.Add(ctx, a.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetGroup(ra.ID, "work"); err != nil {
		t.Fatal(err)
	}
	info1, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)

	if err := s.RenameGroup("work", "work"); err != nil {
		t.Fatal(err)
	}

	info2, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Fatalf("no-op rename touched the file: %v -> %v", info1.ModTime(), info2.ModTime())
	}
	got, ok := s.Get(ra.ID)
	if !ok || got.Group != "work" {
		t.Fatalf("Get after no-op rename = %+v %v", got, ok)
	}
}

func TestRenameGroupWithNoMatchesIsNoop(t *testing.T) {
	a := testrepo.New(t)
	s, _ := repos.Open(filepath.Join(t.TempDir(), "repos.json"))
	ra, err := s.Add(ctx, a.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetGroup(ra.ID, "work"); err != nil {
		t.Fatal(err)
	}

	if err := s.RenameGroup("nonexistent", "job"); err != nil {
		t.Fatal(err)
	}

	got, ok := s.Get(ra.ID)
	if !ok || got.Group != "work" {
		t.Fatalf("unrelated repo group changed: %+v", got)
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

// Reorder rewrites the stored order, which is the sidebar's manual order:
// unknown ids are ignored, ids left out keep their relative order at the end,
// and the order survives a reopen.
func TestReorderPersists(t *testing.T) {
	file := filepath.Join(t.TempDir(), "repos.json")
	s, _ := repos.Open(file)
	var ids []string
	for range 3 {
		added, err := s.Add(ctx, testrepo.New(t).Dir)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, added.ID)
	}
	a, b, c := ids[0], ids[1], ids[2]

	if err := s.Reorder([]string{c, "nope", a}); err != nil {
		t.Fatal(err)
	}
	order := func(list []repos.Repo) []string {
		out := []string{}
		for _, r := range list {
			out = append(out, r.ID)
		}
		return out
	}
	want := []string{c, a, b}
	if got := order(s.List()); !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	reopened, err := repos.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	if got := order(reopened.List()); !slices.Equal(got, want) {
		t.Fatalf("reopened order = %v, want %v", got, want)
	}
}
