package gitlog_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"git-ui/internal/gitlog"
	"git-ui/internal/testrepo"
)

var ctx = context.Background()

func TestArgsDefaultsToAllRefs(t *testing.T) {
	got := gitlog.Args(gitlog.Filters{}, 0, 500)
	want := []string{"log", "--topo-order", "--parents", "--decorate=full", "--format=" + gitlog.Format,
		"--skip=0", "-n500", "--all"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestArgsWithAllFilters(t *testing.T) {
	f := gitlog.Filters{Text: "fix", Branch: "refs/heads/develop", Author: "ana",
		Since: "2026-01-01", Until: "2026-02-01", Paths: []string{"a.go", "b/"}}
	got := gitlog.Args(f, 500, 500)
	want := []string{"log", "--topo-order", "--parents", "--decorate=full", "--format=" + gitlog.Format,
		"--skip=500", "-n500", "--author=ana", "--grep=fix", "-i", "--fixed-strings",
		"--since=2026-01-01", "--until=2026-02-01",
		"--end-of-options", "refs/heads/develop", "--", "a.go", "b/"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestGraphVisible(t *testing.T) {
	cases := map[string]struct {
		f    gitlog.Filters
		want bool
	}{
		"none":   {gitlog.Filters{}, true},
		"branch": {gitlog.Filters{Branch: "refs/heads/x", Paths: []string{"a"}}, true},
		"text":   {gitlog.Filters{Text: "x"}, false},
		"author": {gitlog.Filters{Author: "x"}, false},
		"since":  {gitlog.Filters{Since: "2026-01-01"}, false},
		"until":  {gitlog.Filters{Until: "2026-01-01"}, false},
	}
	for name, c := range cases {
		if got := c.f.GraphVisible(); got != c.want {
			t.Errorf("%s: got %v", name, got)
		}
	}
}

func TestParse(t *testing.T) {
	out := "aaaa\x00aa\x00bbbb cccc\x00Ana\x00ana@x.io\x002026-03-01T10:00:00+01:00\x00" +
		"HEAD -> refs/heads/main, refs/remotes/origin/main, refs/remotes/origin/HEAD, tag: refs/tags/v1.0\x00" +
		"Merge: things ✓\x1e\n" +
		"bbbb\x00bb\x00\x00Bo\x00bo@x.io\x002026-02-01T10:00:00Z\x00\x00root\x1e\n"

	got, err := gitlog.Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d commits", len(got))
	}
	want0 := gitlog.Commit{
		Hash: "aaaa", Short: "aa", Parents: []string{"bbbb", "cccc"},
		Author: "Ana", Email: "ana@x.io",
		Date:    time.Date(2026, 3, 1, 10, 0, 0, 0, time.FixedZone("", 3600)),
		Subject: "Merge: things ✓",
		Refs: []gitlog.Ref{
			{Name: "HEAD", Kind: gitlog.RefHead},
			{Name: "main", Kind: gitlog.RefLocal},
			{Name: "origin/main", Kind: gitlog.RefRemote},
			{Name: "v1.0", Kind: gitlog.RefTag},
		},
	}
	if !got[0].Date.Equal(want0.Date) {
		t.Fatalf("date = %v", got[0].Date)
	}
	got[0].Date = want0.Date
	if !reflect.DeepEqual(got[0], want0) {
		t.Fatalf("got  %+v\nwant %+v", got[0], want0)
	}
	if !got[0].IsHead() || got[1].IsHead() {
		t.Fatal("IsHead wrong")
	}
	if len(got[1].Parents) != 0 || got[1].Parents == nil || len(got[1].Refs) != 0 || got[1].Refs == nil {
		t.Fatalf("root commit = %+v", got[1])
	}
}

func TestParseDetachedHead(t *testing.T) {
	out := "aaaa\x00aa\x00\x00A\x00a@x\x002026-01-01T00:00:00Z\x00HEAD, refs/heads/feat\x00s\x1e"
	got, err := gitlog.Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	want := []gitlog.Ref{{Name: "HEAD", Kind: gitlog.RefHead}, {Name: "feat", Kind: gitlog.RefLocal}}
	if !reflect.DeepEqual(got[0].Refs, want) {
		t.Fatalf("refs = %+v", got[0].Refs)
	}
}

func TestGetReadsRealRepo(t *testing.T) {
	r := testrepo.New(t)
	base := r.Commit("base")
	r.Git("switch", "-q", "-c", "feature")
	feat := r.Commit("feature work")
	r.Git("switch", "-q", "main")
	r.Commit("main work")
	r.Git("merge", "-q", "--no-ff", "-m", "Merge feature", "feature")
	r.Git("tag", "v1.0")

	all, err := gitlog.Get(ctx, r.Dir, gitlog.Filters{}, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 {
		t.Fatalf("got %d commits", len(all))
	}
	top := all[0]
	if top.Subject != "Merge feature" || len(top.Parents) != 2 || !top.IsHead() {
		t.Fatalf("top = %+v", top)
	}
	wantRefs := []gitlog.Ref{
		{Name: "HEAD", Kind: gitlog.RefHead},
		{Name: "main", Kind: gitlog.RefLocal},
		{Name: "v1.0", Kind: gitlog.RefTag},
	}
	if !reflect.DeepEqual(top.Refs, wantRefs) {
		t.Fatalf("top refs = %+v", top.Refs)
	}
	if all[3].Hash != base {
		t.Fatalf("last = %s, want base %s", all[3].Hash, base)
	}

	page, err := gitlog.Get(ctx, r.Dir, gitlog.Filters{}, 2, 2)
	if err != nil || len(page) != 2 || page[0].Hash != all[2].Hash {
		t.Fatalf("page = %+v, err %v", page, err)
	}

	onlyFeature, err := gitlog.Get(ctx, r.Dir, gitlog.Filters{Branch: "refs/heads/feature"}, 0, 100)
	if err != nil || len(onlyFeature) != 2 || onlyFeature[0].Hash != feat {
		t.Fatalf("feature log = %+v, err %v", onlyFeature, err)
	}
}

func TestGetWithPathFilterRewritesParents(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile("a", "1\n")
	r.Git("add", "a")
	r.Git("commit", "-q", "-m", "commit1")
	c1 := r.Git("rev-parse", "HEAD")

	r.WriteFile("b", "1\n")
	r.Git("add", "b")
	r.Git("commit", "-q", "-m", "commit2")

	r.WriteFile("a", "2\n")
	r.Git("add", "a")
	r.Git("commit", "-q", "-m", "commit3")
	c3 := r.Git("rev-parse", "HEAD")

	got, err := gitlog.Get(ctx, r.Dir, gitlog.Filters{Paths: []string{"a"}}, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d commits, want 2: %+v", len(got), got)
	}
	if got[0].Hash != c3 || got[1].Hash != c1 {
		t.Fatalf("got hashes %s, %s; want %s, %s", got[0].Hash, got[1].Hash, c3, c1)
	}
	if len(got[0].Parents) != 1 || got[0].Parents[0] != c1 {
		t.Fatalf("newer commit parents = %+v, want [%s]", got[0].Parents, c1)
	}
}

func TestGetEmptyRepo(t *testing.T) {
	r := testrepo.New(t)
	got, err := gitlog.Get(ctx, r.Dir, gitlog.Filters{}, 0, 100)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %+v, err %v", got, err)
	}
}

func TestResolveCommit(t *testing.T) {
	r := testrepo.New(t)
	hash := r.Commit("one")

	got, err := gitlog.ResolveCommit(ctx, r.Dir, hash[:7])
	if err != nil || got != hash {
		t.Fatalf("got %q, err %v", got, err)
	}
	got, err = gitlog.ResolveCommit(ctx, r.Dir, "deadbeef")
	if err != nil || got != "" {
		t.Fatalf("unknown: got %q, err %v", got, err)
	}
}

func TestAuthorsAndShallow(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("one")

	authors, err := gitlog.Authors(ctx, r.Dir)
	if err != nil || !reflect.DeepEqual(authors, []string{"Test User"}) {
		t.Fatalf("authors = %q, err %v", authors, err)
	}
	shallow, err := gitlog.IsShallow(ctx, r.Dir)
	if err != nil || shallow {
		t.Fatalf("shallow = %v, err %v", shallow, err)
	}
}
