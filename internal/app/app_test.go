package app

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"git-ui/internal/gitlog"
	"git-ui/internal/repos"
	"git-ui/internal/testrepo"
)

func newTestApp(t *testing.T) (*App, string) {
	t.Helper()
	store, err := repos.Open(filepath.Join(t.TempDir(), "repos.json"))
	if err != nil {
		t.Fatal(err)
	}
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("switch", "-q", "-c", "feature")
	r.Commit("feature work")
	r.Git("switch", "-q", "main")
	r.Commit("main work")
	r.Git("merge", "-q", "--no-ff", "-m", "Merge feature", "feature")
	repo, err := store.Add(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	return New(store), repo.ID
}

func TestGetLogPagesMatchSinglePage(t *testing.T) {
	a, id := newTestApp(t)

	whole, err := a.GetLog(id, gitlog.Filters{}, gitlog.OrderTopo, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	first, err := a.GetLog(id, gitlog.Filters{}, gitlog.OrderTopo, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.GetLog(id, gitlog.Filters{}, gitlog.OrderTopo, 2, 100)
	if err != nil {
		t.Fatal(err)
	}

	if len(whole.Rows) != 4 || !whole.GraphVisible || whole.HasMore {
		t.Fatalf("whole = %+v", whole)
	}
	if !first.HasMore || second.HasMore {
		t.Fatalf("hasMore: first %v second %v", first.HasMore, second.HasMore)
	}
	got := append(first.Rows, second.Rows...)
	if !reflect.DeepEqual(got, whole.Rows) {
		t.Fatalf("paged rows differ:\n got  %+v\n want %+v", got, whole.Rows)
	}
	top := whole.Rows[0]
	if !top.IsMerge || !top.IsHead || top.Subject != "Merge feature" {
		t.Fatalf("top = %+v", top)
	}
}

func TestGetLogRejectsStalePage(t *testing.T) {
	a, id := newTestApp(t)
	if _, err := a.GetLog(id, gitlog.Filters{}, gitlog.OrderTopo, 0, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := a.GetLog(id, gitlog.Filters{Author: "Test"}, gitlog.OrderTopo, 2, 2); !errors.Is(err, ErrStalePage) {
		t.Fatalf("different filters: want ErrStalePage, got %v", err)
	}
	if _, err := a.GetLog(id, gitlog.Filters{}, gitlog.OrderTopo, 3, 2); !errors.Is(err, ErrStalePage) {
		t.Fatalf("wrong offset: want ErrStalePage, got %v", err)
	}
}

func TestGetLogOrderChangeRejectsStalePage(t *testing.T) {
	a, id := newTestApp(t)
	if _, err := a.GetLog(id, gitlog.Filters{}, gitlog.OrderTopo, 0, 2); err != nil {
		t.Fatal(err)
	}
	// A page fetched for the topo order cannot be followed by an offset page
	// requested under date order: the ordering changed, so the log must
	// restart from the first page just like a filter change would.
	if _, err := a.GetLog(id, gitlog.Filters{}, gitlog.OrderDate, 2, 2); !errors.Is(err, ErrStalePage) {
		t.Fatalf("order change: want ErrStalePage, got %v", err)
	}
	// Starting over at offset 0 under the new order is accepted.
	if _, err := a.GetLog(id, gitlog.Filters{}, gitlog.OrderDate, 0, 2); err != nil {
		t.Fatalf("restart under new order: %v", err)
	}
}

func TestGetLogHidesGraphForAuthorFilter(t *testing.T) {
	a, id := newTestApp(t)
	page, err := a.GetLog(id, gitlog.Filters{Author: "Test User"}, gitlog.OrderTopo, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if page.GraphVisible || len(page.Rows) != 4 {
		t.Fatalf("page = %+v", page)
	}
	for _, r := range page.Rows {
		if r.Lane != 0 || len(r.Edges) != 0 || r.Edges == nil {
			t.Fatalf("row has graph data: %+v", r)
		}
	}
}

func TestWriteRejectsConcurrentOperation(t *testing.T) {
	a, id := newTestApp(t)
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan error)
	go func() {
		done <- a.write(id, func(ctx context.Context, dir string) error {
			close(started)
			<-release
			return nil
		})
	}()
	<-started

	if err := a.Checkout(id, "feature"); !errors.Is(err, ErrBusy) {
		t.Fatalf("want ErrBusy, got %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestCheckoutAndRefsThroughApp(t *testing.T) {
	a, id := newTestApp(t)
	if err := a.Checkout(id, "feature"); err != nil {
		t.Fatal(err)
	}
	got, err := a.GetRefs(id)
	if err != nil || got.Head != "feature" {
		t.Fatalf("refs = %+v, err %v", got, err)
	}
	items := a.ListRepos()
	if len(items) != 1 || items[0].Branch != "feature" {
		t.Fatalf("items = %+v", items)
	}
	if _, err := a.GetRefs("unknown"); !errors.Is(err, repos.ErrUnknownRepo) {
		t.Fatalf("unknown repo: %v", err)
	}
}
