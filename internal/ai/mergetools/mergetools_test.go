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
	if !strings.Contains(out, "0 conflict") {
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
