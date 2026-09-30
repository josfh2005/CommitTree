package merge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"git-ui/internal/testrepo"
)

// textConflict merges feature into main with name conflicting (diff3).
func textConflict(t *testing.T, name, base, ours, theirs string) *testrepo.Repo {
	t.Helper()
	r := testrepo.New(t)
	r.Git("config", "merge.conflictStyle", "diff3")
	r.WriteFile(name, base)
	r.Git("add", name)
	r.Git("commit", "-q", "-m", "base")
	r.Git("switch", "-q", "-c", "feature")
	r.WriteFile(name, theirs)
	r.Git("commit", "-q", "-am", "theirs")
	r.Git("switch", "-q", "main")
	r.WriteFile(name, ours)
	r.Git("commit", "-q", "-am", "ours")
	if _, err := Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	return r
}

const twoRegions = "a\nb\nc\nd\ne\nf\ng\nh\n"

func twoRegionConflict(t *testing.T) *testrepo.Repo {
	return textConflict(t, "f.txt", twoRegions,
		strings.NewReplacer("b\n", "B-ours\n", "g\n", "G-ours\n").Replace(twoRegions),
		strings.NewReplacer("b\n", "B-theirs\n", "g\n", "G-theirs\n").Replace(twoRegions))
}

func regions(t *testing.T, dir string) []Hunk {
	t.Helper()
	hs, err := Parse(readFile(t, dir, "f.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return hs
}

func TestParseGivesStableIDsAndSpans(t *testing.T) {
	r := twoRegionConflict(t)
	hs := regions(t, r.Dir)
	if len(hs) != 2 || hs[0].ID == "" || hs[0].ID == hs[1].ID {
		t.Fatalf("hunks = %+v", hs)
	}
	lines := strings.SplitAfter(readFile(t, r.Dir, "f.txt"), "\n")
	if !strings.HasPrefix(lines[hs[0].Start], "<<<<<<<") || !strings.HasPrefix(lines[hs[0].End-1], ">>>>>>>") {
		t.Fatalf("span %d..%d does not cover the markers", hs[0].Start, hs[0].End)
	}
	second := hs[1].ID
	if _, err := ResolveRegion(context.Background(), r.Dir, "f.txt", hs[0].ID, "B\n"); err != nil {
		t.Fatal(err)
	}
	if after := regions(t, r.Dir); len(after) != 1 || after[0].ID != second {
		t.Fatalf("the other region's id changed: %+v", after)
	}
}

func TestRegionIDsTellRepeatsApart(t *testing.T) {
	hs, err := Parse("<<<<<<< a\nx\n=======\ny\n>>>>>>> b\nmid\n<<<<<<< a\nx\n=======\ny\n>>>>>>> b\n")
	if err != nil {
		t.Fatal(err)
	}
	if hs[0].ID == hs[1].ID {
		t.Fatalf("ids = %q, %q", hs[0].ID, hs[1].ID)
	}
}

// Resolving one of two identical regions must not let a stale id — a
// second click on it, or the AI retrying — land on the other one.
func TestAStaleIDOfARepeatedRegionNeverHitsItsTwin(t *testing.T) {
	same := func(s string) string { return strings.ReplaceAll(s, "X", "same") }
	base := "a\nX\nb\nc\nd\ne\nf\ng\nh\nX\ni\n"
	r := textConflict(t, "f.txt", same(base), strings.ReplaceAll(same(base), "same", "ours"), strings.ReplaceAll(same(base), "same", "theirs"))
	hs := regions(t, r.Dir)
	if len(hs) != 2 {
		t.Fatalf("want two identical regions, got %+v", hs)
	}
	first, second := hs[0].ID, hs[1].ID
	ctx := context.Background()
	if _, err := ResolveRegion(ctx, r.Dir, "f.txt", first, "one\n"); err != nil {
		t.Fatal(err)
	}
	before := readFile(t, r.Dir, "f.txt")
	for _, id := range []string{first, second} {
		if _, err := ResolveRegion(ctx, r.Dir, "f.txt", id, "stale\n"); !errors.Is(err, ErrNoSuchRegion) {
			t.Errorf("stale id %s: err = %v", id, err)
		}
	}
	if readFile(t, r.Dir, "f.txt") != before {
		t.Fatal("a stale id wrote into the twin region")
	}
}

func TestRegionText(t *testing.T) {
	h := Hunk{Ours: "o\n", Theirs: "t\n", Base: "b\n"}
	for choice, want := range map[string]string{"ours": "o\n", "theirs": "t\n", "both": "o\nt\n"} {
		if got, err := RegionText(h, choice); err != nil || got != want {
			t.Errorf("%s: %q, %v", choice, got, err)
		}
	}
	if _, err := RegionText(h, "base"); err == nil {
		t.Error("unknown choice accepted")
	}
}

func TestResolveRegionStaleIDWritesNothing(t *testing.T) {
	r := twoRegionConflict(t)
	id := regions(t, r.Dir)[0].ID
	if _, err := ResolveRegion(context.Background(), r.Dir, "f.txt", id, "B\n"); err != nil {
		t.Fatal(err)
	}
	before := readFile(t, r.Dir, "f.txt")
	_, err := ResolveRegion(context.Background(), r.Dir, "f.txt", id, "B again\n")
	if !errors.Is(err, ErrNoSuchRegion) {
		t.Fatalf("err = %v", err)
	}
	if readFile(t, r.Dir, "f.txt") != before {
		t.Fatal("a stale id changed the file")
	}
}

func TestResolveRegionReportsWhatIsLeftAndKeepsTheMode(t *testing.T) {
	r := twoRegionConflict(t)
	full := filepath.Join(r.Dir, "f.txt")
	if err := os.Chmod(full, 0o755); err != nil {
		t.Fatal(err)
	}
	hs := regions(t, r.Dir)
	left, err := ResolveRegion(context.Background(), r.Dir, "f.txt", hs[0].ID, "B\n")
	if err != nil || left != 1 {
		t.Fatalf("left %d, err %v", left, err)
	}
	left, err = ResolveRegion(context.Background(), r.Dir, "f.txt", hs[1].ID, "G\n")
	if err != nil || left != 0 {
		t.Fatalf("left %d, err %v", left, err)
	}
	if got := readFile(t, r.Dir, "f.txt"); got != "a\nB\nc\nd\ne\nf\nG\nh\n" {
		t.Fatalf("file = %q", got)
	}
	if info, _ := os.Stat(full); info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v", info.Mode())
	}
}

func TestResolveRegionKeepsBytesAround(t *testing.T) {
	r := textConflict(t, "crlf.txt", "a\r\nb\r\nc", "a\r\nB1\r\nc", "a\r\nB2\r\nc")
	hs, err := Parse(readFile(t, r.Dir, "crlf.txt"))
	if err != nil || len(hs) != 1 {
		t.Fatalf("%v %+v", err, hs)
	}
	if _, err := ResolveRegion(context.Background(), r.Dir, "crlf.txt", hs[0].ID, "B\r\n"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, r.Dir, "crlf.txt"); got != "a\r\nB\r\nc" {
		t.Fatalf("file = %q", got)
	}
}

func TestResolveRegionRefusesWhatIsNotATextConflict(t *testing.T) {
	r := twoRegionConflict(t)
	id := regions(t, r.Dir)[0].ID
	if _, err := ResolveRegion(context.Background(), r.Dir, "other.txt", id, "x\n"); !errors.Is(err, ErrNotInMerge) {
		t.Errorf("unknown path: %v", err)
	}
	if _, err := ResolveRegion(context.Background(), r.Dir, "f.txt", id, "<<<<<<< x\n"); !errors.Is(err, ErrMarkersLeft) {
		t.Errorf("markers: %v", err)
	}
}

// A resolved and staged file comes back with its markers, unstaged; a file
// the merge settled cleanly is never offered and is refused.
func TestRestartBringsBackAStagedFile(t *testing.T) {
	r := twoRegionConflict(t)
	ctx := context.Background()
	original := readFile(t, r.Dir, "f.txt")
	for _, h := range regions(t, r.Dir) {
		if _, err := ResolveRegion(ctx, r.Dir, "f.txt", h.ID, "done\n"); err != nil {
			t.Fatal(err)
		}
	}
	if err := Stage(ctx, r.Dir, "f.txt"); err != nil {
		t.Fatal(err)
	}
	can, err := Restartable(ctx, r.Dir)
	if err != nil || !slices.Contains(can, "f.txt") {
		t.Fatalf("restartable = %v, %v", can, err)
	}
	if err := Restart(ctx, r.Dir, "f.txt"); err != nil {
		t.Fatal(err)
	}
	// git checkout -m cannot know the original marker labels (HEAD, the
	// base commit, the branch name) and writes ours/base/theirs instead;
	// the regions themselves, and so their ids, come back unchanged.
	if got := markerLabelsDropped(readFile(t, r.Dir, "f.txt")); got != markerLabelsDropped(original) {
		t.Fatalf("file = %q, want the original conflict %q", got, original)
	}
	if st := status(t, r.Dir); !slices.Contains(st.Conflicts, "f.txt") {
		t.Fatalf("not conflicted again: %+v", st)
	}
}

func TestRestartRefusesAFileThatWasNeverConflicted(t *testing.T) {
	r := twoRegionConflict(t)
	if err := Restart(context.Background(), r.Dir, "not-in-the-merge.txt"); !errors.Is(err, ErrNotInMerge) {
		t.Fatalf("err = %v", err)
	}
}

func markerLabelsDropped(s string) string {
	lines := strings.SplitAfter(s, "\n")
	for i, l := range lines {
		for _, m := range []string{"<<<<<<<", "|||||||", ">>>>>>>"} {
			if strings.HasPrefix(l, m) {
				lines[i] = m + "\n"
			}
		}
	}
	return strings.Join(lines, "")
}

// A modify/delete file, once taken, has a resolve-undo record but no pair
// of sides to merge again: Restart must not be offered for it.
func TestRestartIsNotOfferedForATakenModifyDelete(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile("gone.txt", "base\n")
	r.Git("add", "gone.txt")
	r.Git("commit", "-q", "-m", "base")
	r.Git("switch", "-q", "-c", "feature")
	r.Git("rm", "-q", "gone.txt")
	r.Git("commit", "-q", "-m", "delete")
	r.Git("switch", "-q", "main")
	r.WriteFile("gone.txt", "changed\n")
	r.Git("commit", "-q", "-am", "change")
	ctx := context.Background()
	if _, err := Start(ctx, r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	if err := Take(ctx, r.Dir, "gone.txt", Theirs); err != nil {
		t.Fatal(err)
	}
	can, err := Restartable(ctx, r.Dir)
	if err != nil || slices.Contains(can, "gone.txt") {
		t.Fatalf("restartable = %v, %v", can, err)
	}
}
