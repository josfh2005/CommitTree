package hosting

import (
	"strings"
	"testing"
)

const twoFileDiff = `diff --git a/src/a.go b/src/a.go
index 111..222 100644
--- a/src/a.go
+++ b/src/a.go
@@ -1,2 +1,2 @@
-old
+new
 same
diff --git a/old.txt b/old.txt
deleted file mode 100644
index 333..000
--- a/old.txt
+++ /dev/null
@@ -1 +0,0 @@
-gone
`

func TestSplitUnifiedDiff(t *testing.T) {
	got := SplitUnifiedDiff(twoFileDiff)
	if len(got) != 2 {
		t.Fatalf("want 2 files, got %d: %v", len(got), got)
	}
	a, ok := got["src/a.go"]
	if !ok || !strings.HasPrefix(a, "diff --git a/src/a.go") || !strings.Contains(a, "+new") || strings.Contains(a, "gone") {
		t.Errorf("src/a.go: %q", a)
	}
	d, ok := got["old.txt"]
	if !ok || !strings.HasPrefix(d, "diff --git a/old.txt") || !strings.Contains(d, "-gone") {
		t.Errorf("old.txt: %q", d)
	}
}

func TestSplitUnifiedDiffRenameAndEmpty(t *testing.T) {
	if got := SplitUnifiedDiff(""); len(got) != 0 {
		t.Errorf("empty diff: %v", got)
	}
	got := SplitUnifiedDiff("diff --git a/x.txt b/y.txt\nsimilarity index 100%\nrename from x.txt\nrename to y.txt\n")
	if _, ok := got["y.txt"]; !ok || len(got) != 1 {
		t.Errorf("rename: %v", got)
	}
}

func TestSplitUnifiedDiffPathWithSpacesAndBPrefix(t *testing.T) {
	got := SplitUnifiedDiff("diff --git a/my dir b/x.txt b/my dir b/x.txt\n--- a/my dir b/x.txt\n+++ b/my dir b/x.txt\n@@ -1 +1 @@\n-a\n+b\n")
	if _, ok := got["my dir b/x.txt"]; !ok || len(got) != 1 {
		t.Errorf("got %v", got)
	}
}

func TestPatchShowsChanges(t *testing.T) {
	cases := map[string]struct {
		patch string
		want  bool
	}{
		"hunk":        {"diff --git a/a b/a\n--- a/a\n+++ b/a\n@@ -1 +1 @@\n-x\n+y\n", true},
		"binary":      {"diff --git a/i.png b/i.png\nnew file mode 100644\nBinary files /dev/null and b/i.png differ\n", false},
		"git binary":  {"diff --git a/i.png b/i.png\nindex 1..2\nGIT binary patch\nliteral 3\n", false},
		"pure rename": {"diff --git a/x b/y\nsimilarity index 100%\nrename from x\nrename to y\n", true},
		"mode only":   {"diff --git a/x b/x\nold mode 100644\nnew mode 100755\n", false},
		"empty":       {"", false},
	}
	for name, c := range cases {
		if got := PatchShowsChanges(c.patch); got != c.want {
			t.Errorf("%s: got %v, want %v", name, got, c.want)
		}
	}
}
