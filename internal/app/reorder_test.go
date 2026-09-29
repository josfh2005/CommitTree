package app

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"git-ui/internal/repos"
	"git-ui/internal/testrepo"
)

// The sidebar's manual order is the order ListRepos returns stored
// repositories in, which ReorderRepos rewrites.
func TestReorderReposChangesTheListedOrder(t *testing.T) {
	store, err := repos.Open(filepath.Join(t.TempDir(), "repos.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for range 2 {
		r := testrepo.New(t)
		r.Commit("init")
		added, err := store.Add(context.Background(), r.Dir)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, added.ID)
	}
	a := New(store)
	if err := a.ReorderRepos([]string{ids[1], ids[0]}); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, item := range a.ListRepos() {
		got = append(got, item.ID)
	}
	if want := []string{ids[1], ids[0]}; !slices.Equal(got, want) {
		t.Fatalf("listed order = %v, want %v", got, want)
	}
}
