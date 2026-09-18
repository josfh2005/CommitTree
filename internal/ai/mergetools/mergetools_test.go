package mergetools_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git-ui/internal/ai"
	"git-ui/internal/ai/mergetools"
	"git-ui/internal/merge"
	"git-ui/internal/testrepo"
)

func conflicted(t *testing.T) *testrepo.Repo {
	t.Helper()
	r := testrepo.New(t)
	r.WriteFile("greeting.txt", "hello\n")
	r.Git("add", "greeting.txt")
	r.Git("commit", "-q", "-m", "add greeting")
	r.Git("switch", "-q", "-c", "feature")
	r.WriteFile("greeting.txt", "hola\n")
	r.Git("commit", "-q", "-am", "spanish")
	r.Git("switch", "-q", "main")
	r.WriteFile("greeting.txt", "hi\n")
	r.Git("commit", "-q", "-am", "informal")
	if _, err := merge.Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	return r
}

func call(name string, args map[string]any) ai.ToolCall {
	return ai.ToolCall{ID: "c1", Name: name, Args: args}
}

func TestSpecsCoverTheFourTools(t *testing.T) {
	names := map[string]bool{}
	for _, s := range mergetools.Specs() {
		names[s.Name] = true
	}
	for _, want := range []string{"list_conflicts", "read_conflict", "resolve_hunk", "stage_file"} {
		if !names[want] {
			t.Errorf("missing tool %q", want)
		}
	}
}

func TestListConflicts(t *testing.T) {
	r := conflicted(t)
	out, changed := mergetools.Run(context.Background(), r.Dir, call("list_conflicts", nil))
	if changed {
		t.Error("changed = true, but listing writes nothing")
	}
	if !strings.Contains(out, "greeting.txt") || !strings.Contains(out, "1 conflict") {
		t.Errorf("out = %q", out)
	}
}

func TestReadConflictShowsAllThreeSides(t *testing.T) {
	r := conflicted(t)
	out, _ := mergetools.Run(context.Background(), r.Dir, call("read_conflict", map[string]any{"path": "greeting.txt", "hunk": float64(0)}))
	for _, want := range []string{"hi", "hola", "hello"} {
		if !strings.Contains(out, want) {
			t.Errorf("out = %q, want it to contain %q", out, want)
		}
	}
}

func TestResolveHunkWritesTheFile(t *testing.T) {
	r := conflicted(t)
	out, changed := mergetools.Run(context.Background(), r.Dir,
		call("resolve_hunk", map[string]any{"path": "greeting.txt", "hunk": float64(0), "resolved": "hi / hola\n"}))
	if !changed {
		t.Error("changed = false, want true")
	}
	if !strings.Contains(out, "no conflicts left") {
		t.Errorf("out = %q, want it to report none left", out)
	}
	data, err := os.ReadFile(filepath.Join(r.Dir, "greeting.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hi / hola\n" {
		t.Errorf("file = %q", data)
	}
}

// A path with `-merge` in .gitattributes gets no text merge at all: git
// leaves OUR side in the worktree with no markers. merge.Status must file it
// under Manual, so stage_file (which only opens Conflicts paths) must refuse
// it rather than silently staging OUR side over THEIRS.
func TestStageFileRefusesANoMergeAttributePath(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile(".gitattributes", "*.lock -merge\n")
	r.WriteFile("deps.lock", "base\n")
	r.Git("add", ".gitattributes", "deps.lock")
	r.Git("commit", "-q", "-m", "add deps.lock")
	r.Git("switch", "-q", "-c", "feature")
	r.WriteFile("deps.lock", "theirs\n")
	r.Git("commit", "-q", "-am", "their lock")
	r.Git("switch", "-q", "main")
	r.WriteFile("deps.lock", "ours\n")
	r.Git("commit", "-q", "-am", "our lock")
	if _, err := merge.Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}

	out, changed := mergetools.Run(context.Background(), r.Dir, call("stage_file", map[string]any{"path": "deps.lock"}))
	if changed {
		t.Errorf("changed = true, want stage_file to refuse a Manual path; out = %q", out)
	}

	st, err := merge.Status(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range st.Manual {
		if p == "deps.lock" {
			found = true
		}
	}
	if !found {
		t.Errorf("manual = %v, conflicts = %v, want deps.lock still unmerged in manual", st.Manual, st.Conflicts)
	}
}

// B: OUR side had `-merge` in effect when git wrote the conflicted file, but
// the incoming branch dropped that .gitattributes line, so the post-merge
// worktree attributes alone would say the path is an ordinary text conflict.
// merge.Status must still union in HEAD's attributes and file it under
// Manual, so stage_file must refuse it.
func TestStageFileRefusesANoMergeAttributePathEvenWhenTheirsDropsTheAttribute(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile(".gitattributes", "*.lock -merge\n")
	r.WriteFile("deps.lock", "base\n")
	r.Git("add", ".gitattributes", "deps.lock")
	r.Git("commit", "-q", "-m", "add deps.lock")
	r.Git("switch", "-q", "-c", "feature")
	r.WriteFile(".gitattributes", "")
	r.WriteFile("deps.lock", "theirs\n")
	r.Git("commit", "-q", "-am", "drop the -merge rule and change deps.lock")
	r.Git("switch", "-q", "main")
	r.WriteFile("deps.lock", "ours\n")
	r.Git("commit", "-q", "-am", "our lock")
	if _, err := merge.Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}

	out, changed := mergetools.Run(context.Background(), r.Dir, call("stage_file", map[string]any{"path": "deps.lock"}))
	if changed {
		t.Errorf("changed = true, want stage_file to refuse a Manual path; out = %q", out)
	}

	st, err := merge.Status(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range st.Manual {
		if p == "deps.lock" {
			found = true
		}
	}
	if !found {
		t.Errorf("manual = %v, conflicts = %v, want deps.lock still unmerged in manual", st.Manual, st.Conflicts)
	}
}

func TestResolveHunkRefusesMarkers(t *testing.T) {
	r := conflicted(t)
	out, changed := mergetools.Run(context.Background(), r.Dir,
		call("resolve_hunk", map[string]any{"path": "greeting.txt", "hunk": float64(0), "resolved": "<<<<<<< HEAD\nhi\n"}))
	if changed {
		t.Error("changed = true, but nothing should have been written")
	}
	if !strings.Contains(strings.ToLower(out), "marker") {
		t.Errorf("out = %q, want the refusal to mention markers", out)
	}
}

// The agent must not reach outside the repository, nor touch files that
// aren't part of this merge.
func TestToolsRefuseAPathThatIsNotConflicted(t *testing.T) {
	r := conflicted(t)
	r.WriteFile("untouched.txt", "keep me\n")
	for _, path := range []string{"untouched.txt", "../escape.txt", "/etc/hosts"} {
		out, changed := mergetools.Run(context.Background(), r.Dir,
			call("resolve_hunk", map[string]any{"path": path, "hunk": float64(0), "resolved": "x\n"}))
		if changed {
			t.Fatalf("%s: changed = true", path)
		}
		if !strings.Contains(out, "not a conflicted file") {
			t.Errorf("%s: out = %q", path, out)
		}
	}
	if data, _ := os.ReadFile(filepath.Join(r.Dir, "untouched.txt")); string(data) != "keep me\n" {
		t.Errorf("untouched.txt was modified: %q", data)
	}
}

func TestStageFileRefusesWhileMarkersRemain(t *testing.T) {
	r := conflicted(t)
	out, _ := mergetools.Run(context.Background(), r.Dir, call("stage_file", map[string]any{"path": "greeting.txt"}))
	if !strings.Contains(strings.ToLower(out), "marker") {
		t.Errorf("out = %q", out)
	}
}

func TestStageFileAfterResolving(t *testing.T) {
	r := conflicted(t)
	mergetools.Run(context.Background(), r.Dir,
		call("resolve_hunk", map[string]any{"path": "greeting.txt", "hunk": float64(0), "resolved": "hi / hola\n"}))
	out, changed := mergetools.Run(context.Background(), r.Dir, call("stage_file", map[string]any{"path": "greeting.txt"}))
	if !changed {
		t.Error("changed = false, want true")
	}
	if !strings.Contains(out, "Staged") {
		t.Errorf("out = %q", out)
	}
	st, err := merge.Status(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Conflicts) != 0 {
		t.Errorf("conflicts = %v, want none", st.Conflicts)
	}
}

func TestToolsRefuseAnUnmergedSymlink(t *testing.T) {
	r := conflicted(t)
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("do not touch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Stand greeting.txt's path on a symlink pointing out of the repository.
	link := filepath.Join(r.Dir, "greeting.txt")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	out, changed := mergetools.Run(context.Background(), r.Dir,
		call("resolve_hunk", map[string]any{"path": "greeting.txt", "hunk": float64(0), "resolved": "pwned\n"}))
	if changed {
		t.Error("changed = true, want false")
	}
	// Status now files a symlinked path under Manual, so the membership check
	// refuses it ("not a conflicted file") before open's own symlink check
	// ("symbolic link") is reached. Either refusal is correct; what matters is
	// that nothing was written and the file outside is untouched.
	if !strings.Contains(out, "not a conflicted file") && !strings.Contains(out, "symbolic link") {
		t.Errorf("out = %q, want a refusal", out)
	}
	if data, _ := os.ReadFile(outside); string(data) != "do not touch\n" {
		t.Fatalf("the file outside the repository was modified: %q", data)
	}
}

// A conflicted file may be named like a glob. Staging it must stage exactly
// that file: git reads a bare path as a pattern, so "*.txt" would otherwise
// sweep in every other .txt file — conflicted or untracked — and end the
// conflict on them without anyone resolving it.
func TestStageFileTreatsThePathLiterally(t *testing.T) {
	r := testrepo.New(t)
	glob := filepath.Join(r.Dir, "*.txt")
	write := func(globText, otherText string) {
		t.Helper()
		if err := os.WriteFile(glob, []byte(globText), 0o644); err != nil {
			t.Fatal(err)
		}
		r.WriteFile("other.txt", otherText)
	}
	write("base glob\n", "base other\n")
	r.Git("add", "-A")
	r.Git("commit", "-q", "-m", "base")
	r.Git("switch", "-q", "-c", "feature")
	write("their glob\n", "their other\n")
	r.Git("commit", "-q", "-am", "theirs")
	r.Git("switch", "-q", "main")
	write("our glob\n", "our other\n")
	r.Git("commit", "-q", "-am", "ours")
	if _, err := merge.Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	r.WriteFile("untracked.txt", "not part of the merge\n")

	if _, changed := mergetools.Run(context.Background(), r.Dir,
		call("resolve_hunk", map[string]any{"path": "*.txt", "hunk": float64(0), "resolved": "merged glob\n"})); !changed {
		t.Fatal("resolve_hunk on *.txt changed nothing")
	}
	if data, _ := os.ReadFile(glob); string(data) != "merged glob\n" {
		t.Fatalf("*.txt = %q", data)
	}
	out, changed := mergetools.Run(context.Background(), r.Dir, call("stage_file", map[string]any{"path": "*.txt"}))
	if !changed {
		t.Fatalf("stage_file(*.txt) changed = false: %q", out)
	}

	st, err := merge.Status(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	stillConflicted := false
	for _, p := range append(append([]string{}, st.Conflicts...), st.Manual...) {
		if p == "other.txt" {
			stillConflicted = true
		}
	}
	if !stillConflicted {
		t.Errorf("other.txt is no longer a conflict (conflicts = %v, manual = %v): staging *.txt swept it in", st.Conflicts, st.Manual)
	}
	if staged := r.Git("diff", "--cached", "--name-only"); strings.Contains(staged, "untracked.txt") {
		t.Errorf("staged = %q: staging *.txt added untracked.txt", staged)
	}
}

// A modify/delete conflict has no markers, but staging it would still settle
// it — keeping one side and dropping the other on the model's say-so. Only a
// human decides that.
func TestStageFileRefusesAModifyDeleteConflict(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile("gone.txt", "base\n")
	r.Git("add", "gone.txt")
	r.Git("commit", "-q", "-m", "add gone")
	r.Git("switch", "-q", "-c", "feature")
	r.WriteFile("gone.txt", "edited on feature\n")
	r.Git("commit", "-q", "-am", "edit gone")
	r.Git("switch", "-q", "main")
	r.Git("rm", "-q", "gone.txt")
	r.Git("commit", "-q", "-m", "delete gone")
	if _, err := merge.Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}

	out, changed := mergetools.Run(context.Background(), r.Dir, call("stage_file", map[string]any{"path": "gone.txt"}))
	if changed {
		t.Error("changed = true, want false")
	}
	if !strings.Contains(out, "not a conflicted file") {
		t.Errorf("out = %q, want a refusal", out)
	}
	st, err := merge.Status(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Manual) != 1 || st.Manual[0] != "gone.txt" {
		t.Errorf("manual = %v, want gone.txt still unmerged", st.Manual)
	}
}

// Once every region is resolved, the file is still the agent's to stage, and
// list_conflicts must say so rather than telling it to leave the file alone.
func TestListConflictsAsksToStageAResolvedFile(t *testing.T) {
	r := conflicted(t)
	mergetools.Run(context.Background(), r.Dir,
		call("resolve_hunk", map[string]any{"path": "greeting.txt", "hunk": float64(0), "resolved": "hi / hola\n"}))
	out, _ := mergetools.Run(context.Background(), r.Dir, call("list_conflicts", nil))
	if !strings.Contains(out, "greeting.txt — 0 conflict(s) left; call stage_file") {
		t.Errorf("out = %q, want greeting.txt listed as ready to stage", out)
	}
	if strings.Contains(out, "leave it for the user") {
		t.Errorf("out = %q, the agent's own resolved file is described as the user's", out)
	}
}

// twoRegions is a merge whose one conflicted file, pair.txt, has two regions
// far enough apart that git keeps them separate.
func twoRegions(t *testing.T) *testrepo.Repo {
	t.Helper()
	r := testrepo.New(t)
	middle := "1\n2\n3\n4\n5\n6\n7\n8\n"
	r.WriteFile("pair.txt", "a\n"+middle+"b\n")
	r.Git("add", "pair.txt")
	r.Git("commit", "-q", "-m", "add pair")
	r.Git("switch", "-q", "-c", "feature")
	r.WriteFile("pair.txt", "a-theirs\n"+middle+"b-theirs\n")
	r.Git("commit", "-q", "-am", "theirs")
	r.Git("switch", "-q", "main")
	r.WriteFile("pair.txt", "a-ours\n"+middle+"b-ours\n")
	r.Git("commit", "-q", "-am", "ours")
	if _, err := merge.Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	return r
}

// Regions renumber after each resolve. A model that asks for region 1 once
// region 0 is gone must be told the one left is now region 0.
func TestResolveHunkSaysTheNextRegionIsZero(t *testing.T) {
	r := twoRegions(t)
	out, _ := mergetools.Run(context.Background(), r.Dir, call("resolve_hunk", map[string]any{"path": "pair.txt", "hunk": 0, "resolved": "a\n"}))
	if !strings.Contains(out, "1 conflict(s) left") || !strings.Contains(out, "region 0") {
		t.Errorf("out = %q, want the count and that the next one is region 0", out)
	}
}

func TestReadConflictPastTheEndNamesTheValidRegions(t *testing.T) {
	r := twoRegions(t)
	mergetools.Run(context.Background(), r.Dir, call("resolve_hunk", map[string]any{"path": "pair.txt", "hunk": 0, "resolved": "a\n"}))
	out, _ := mergetools.Run(context.Background(), r.Dir, call("read_conflict", map[string]any{"path": "pair.txt", "hunk": 1}))
	if !strings.Contains(out, "no region 1") || !strings.Contains(out, "numbered 0 to 0") {
		t.Errorf("out = %q, want it to name the valid range", out)
	}
}

func TestResolveHunkPastTheEndNamesTheValidRegions(t *testing.T) {
	r := twoRegions(t)
	out, changed := mergetools.Run(context.Background(), r.Dir, call("resolve_hunk", map[string]any{"path": "pair.txt", "hunk": 2, "resolved": "x\n"}))
	if changed {
		t.Error("changed = true for a region that does not exist")
	}
	if !strings.Contains(out, "no region 2") || !strings.Contains(out, "numbered 0 to 1") {
		t.Errorf("out = %q, want it to name the valid range", out)
	}
}

func TestResolveHunkOnTheLastRegionSaysToStage(t *testing.T) {
	r := conflicted(t)
	out, _ := mergetools.Run(context.Background(), r.Dir, call("resolve_hunk", map[string]any{"path": "greeting.txt", "hunk": 0, "resolved": "hi\n"}))
	if !strings.Contains(out, "no conflicts left") || !strings.Contains(out, "stage_file") {
		t.Errorf("out = %q, want it to say to stage the file", out)
	}
}
