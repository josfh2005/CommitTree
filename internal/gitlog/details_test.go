package gitlog_test

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"git-ui/internal/gitlog"
	"git-ui/internal/testrepo"
)

func TestGetDetailsAndDiff(t *testing.T) {
	r := testrepo.New(t)
	root := r.Commit("first")
	r.WriteFile("notes.md", "hello\n")
	r.Git("add", "notes.md")
	r.Git("mv", "file-1.txt", "renamed.txt")
	r.Git("commit", "-q", "-m", "subject line", "-m", "body text")
	head := r.Git("rev-parse", "HEAD")

	d, err := gitlog.GetDetails(ctx, r.Dir, head)
	if err != nil {
		t.Fatal(err)
	}
	if d.Subject != "subject line" || d.Body != "body text" || d.Committer != "Test User" {
		t.Fatalf("details = %+v", d)
	}
	wantFiles := []gitlog.FileChange{
		{Status: "A", Path: "notes.md"},
		{Status: "R", Path: "renamed.txt", OldPath: "file-1.txt"},
	}
	if !reflect.DeepEqual(d.Files, wantFiles) {
		t.Fatalf("files = %+v", d.Files)
	}

	rootDetails, err := gitlog.GetDetails(ctx, r.Dir, root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rootDetails.Files, []gitlog.FileChange{{Status: "A", Path: "file-1.txt"}}) {
		t.Fatalf("root files = %+v", rootDetails.Files)
	}

	patch, err := gitlog.Diff(ctx, r.Dir, root, head, []string{"notes.md"})
	if err != nil || !strings.Contains(patch, "+hello") {
		t.Fatalf("patch = %q, err %v", patch, err)
	}
	rootPatch, err := gitlog.Diff(ctx, r.Dir, "", root, []string{"file-1.txt"})
	if err != nil || !strings.Contains(rootPatch, "+first") {
		t.Fatalf("root patch = %q, err %v", rootPatch, err)
	}
}

func TestParseRaw(t *testing.T) {
	out := ":160000 160000 aaa bbb M\x00lib\x00:100644 100644 ccc ddd M\x00a.txt\x00:100644 100644 eee fff R090\x00old\x00new\x00"
	want := []gitlog.FileChange{
		{Status: "M", Path: "lib", Submodule: true},
		{Status: "M", Path: "a.txt"},
		{Status: "R", Path: "new", OldPath: "old"},
	}
	if got := gitlog.ParseRaw(out); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	if got := gitlog.ParseRaw(""); got == nil || len(got) != 0 {
		t.Fatalf("empty = %#v", got)
	}
}

// withSub returns a parent repo with lib (from a separate repo) added at
// path, copied from internal/submodules/submodules_test.go's helper.
func withSub(t *testing.T, path string) (*testrepo.Repo, *testrepo.Repo) {
	t.Helper()
	lib := testrepo.New(t)
	lib.WriteFile("a.txt", "a")
	lib.Git("add", "a.txt")
	lib.Git("commit", "-q", "-m", "lib one")
	parent := testrepo.New(t)
	parent.WriteFile("p.txt", "p")
	parent.Commit("parent one")
	parent.Git("-c", "protocol.file.allow=always", "submodule", "add", "-q", lib.Dir, path)
	parent.Git("commit", "-q", "-m", "add submodule")
	return parent, lib
}

// A commit that only moves a submodule's pointer shows Submodule:true in the
// details' file list, and its diff is the --submodule=log summary, not the
// raw "Subproject commit" line a plain diff would show.
func TestGetDetailsAndDiffOfASubmodulePointerMove(t *testing.T) {
	parent, _ := withSub(t, "lib")
	before := parent.Git("rev-parse", "HEAD")
	subDir := parent.Dir + "/lib"
	if err := os.WriteFile(subDir+"/a.txt", []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	parent.Git("-C", subDir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-am", "moved")
	parent.Git("add", "lib")
	parent.Git("commit", "-q", "-m", "move submodule")
	head := parent.Git("rev-parse", "HEAD")

	d, err := gitlog.GetDetails(ctx, parent.Dir, head)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Files) != 1 || d.Files[0].Path != "lib" || d.Files[0].Status != "M" || !d.Files[0].Submodule {
		t.Fatalf("files = %+v", d.Files)
	}

	patch, err := gitlog.Diff(ctx, parent.Dir, before, head, []string{"lib"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(patch, "Submodule lib ") {
		t.Errorf("patch = %q, want the submodule=log summary", patch)
	}
	if strings.Contains(patch, "Subproject commit") {
		t.Errorf("patch = %q, want no raw Subproject commit line", patch)
	}
}
