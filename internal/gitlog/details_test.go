package gitlog_test

import (
	"reflect"
	"strings"
	"testing"

	"git-ui/internal/gitlog"
	"git-ui/internal/testrepo"
)

func TestParseNameStatus(t *testing.T) {
	out := "M\x00a.go\x00R087\x00old.go\x00new.go\x00A\x00b c.txt\x00"
	want := []gitlog.FileChange{
		{Status: "M", Path: "a.go"},
		{Status: "R", Path: "new.go", OldPath: "old.go"},
		{Status: "A", Path: "b c.txt"},
	}
	if got := gitlog.ParseNameStatus(out); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	if got := gitlog.ParseNameStatus(""); got == nil || len(got) != 0 {
		t.Fatalf("empty = %#v", got)
	}
}

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
