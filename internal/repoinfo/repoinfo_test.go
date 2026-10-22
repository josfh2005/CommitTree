package repoinfo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git-ui/internal/testrepo"
)

var ctx = context.Background()

func TestParseCountObjects(t *testing.T) {
	out := "count: 12\nsize: 48\nin-pack: 6343\npacks: 4\nsize-pack: 10025\nprune-packable: 3\ngarbage: 1\nsize-garbage: 2\n"
	got := parse(out)
	want := Info{Loose: 12, LooseSize: 48 * 1024, Packed: 6343, Packs: 4, PackSize: 10025 * 1024, PrunePackable: 3, Garbage: 1, GarbageSize: 2 * 1024,
		Size: (48 + 10025 + 2) * 1024}
	if got != want {
		t.Fatalf("parse = %+v\nwant    %+v", got, want)
	}
}

func TestGCReasonFollowsGitsAutoRule(t *testing.T) {
	cases := []struct {
		name               string
		loose, packs       int
		autoLimit, packLim int
		want               string
	}{
		{"nothing to do", 100, 4, 6700, 50, ""},
		{"too many loose objects", 6912, 4, 6700, 50, "6,912 loose objects (git suggests GC above 6,700)"},
		{"too many packs", 10, 53, 6700, 50, "53 packs (git suggests GC above 50)"},
		{"gc.auto=0 turns the rules off", 99999, 99, 0, 50, ""},
		{"gc.autoPackLimit=0 turns that rule off", 10, 99, 6700, 0, ""},
	}
	for _, c := range cases {
		if got := gcReason(c.loose, c.packs, c.autoLimit, c.packLim); got != c.want {
			t.Errorf("%s: gcReason = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestReadCountsLooseObjectsAndReadsTheThresholds(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("one")
	r.Commit("two")
	r.Git("config", "gc.auto", "3")

	info, err := Read(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Loose < 6 || info.Packs != 0 || info.AutoLimit != 3 || info.PackLimit != 50 {
		t.Fatalf("info = %+v; want 6+ loose objects (2 commits, trees, blobs), no packs, limits 3 and 50", info)
	}
	if !strings.Contains(info.GCReason, "loose objects") {
		t.Errorf("GCReason = %q, want the loose-object reason", info.GCReason)
	}
	if info.Size != info.LooseSize+info.PackSize+info.GarbageSize || info.Size == 0 {
		t.Errorf("Size = %d, want loose + pack + garbage", info.Size)
	}
}

func TestCollectPacksTheLooseObjects(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("one")
	r.Commit("two")

	if err := Collect(ctx, r.Dir); err != nil {
		t.Fatal(err)
	}
	info, err := Read(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Loose != 0 || info.Packs != 1 || info.Packed == 0 {
		t.Fatalf("after gc: %+v; want everything in one pack", info)
	}
}

func TestWorktreeSizeSkipsDotGitAndCountsIgnoredFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(rel string, n int) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt", 100)
	write("src/b.go", 200)
	write("node_modules/pkg/index.js", 300) // ignored by git, still on disk
	write(".git/objects/pack/big.pack", 5000)
	write("vendor/sub/.git/HEAD", 50) // a nested repository's .git
	if err := os.Symlink(filepath.Join(dir, "a.txt"), filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}

	got, err := WorktreeSize(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != 600 {
		t.Fatalf("WorktreeSize = %d, want 600 (a.txt + b.go + index.js; no .git, symlink not followed)", got)
	}
}

func TestWorktreeSizeStopsWhenCancelled(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := WorktreeSize(cancelled, dir); err == nil {
		t.Fatal("WorktreeSize ignored a cancelled context")
	}
}
