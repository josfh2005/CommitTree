package worktree_test

import (
	"context"
	"strings"
	"testing"

	"git-ui/internal/worktree"
)

func TestFileDiff(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "one\ntwo\n")
	r.WriteFile("b.txt", "new\n")
	r.Git("add", "b.txt")
	r.WriteFile("c.txt", "fresh\n")
	ctx := context.Background()

	cases := []struct {
		path   string
		staged bool
		want   string
	}{
		{"a.txt", false, "+two"},
		{"b.txt", true, "+new"},
		{"c.txt", false, "+fresh"},
	}
	for _, c := range cases {
		got, err := worktree.FileDiff(ctx, r.Dir, c.path, c.staged)
		if err != nil || !strings.Contains(got, c.want) {
			t.Errorf("FileDiff(%s, staged=%v) = %q, %v; want it to contain %q", c.path, c.staged, got, err, c.want)
		}
	}
	if _, err := worktree.FileDiff(ctx, r.Dir, "nope.txt", false); err == nil || !strings.Contains(err.Error(), `"nope.txt" is not a changed file`) {
		t.Fatalf("err = %v, want an unlisted path refused", err)
	}
}
