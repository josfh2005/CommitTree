package app

import "testing"

func TestGetBlame(t *testing.T) {
	a, id := newTestApp(t)
	b, err := a.GetBlame(id, "HEAD", "file-1.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Lines) != 1 || b.Lines[0] != "base" || len(b.Blocks) != 1 || b.Blocks[0].Summary != "base" {
		t.Fatalf("blame = %+v", b)
	}
	if _, err := a.GetBlame("nope", "HEAD", "file-1.txt", false); err == nil {
		t.Fatal("want error for an unknown repository")
	}
}
