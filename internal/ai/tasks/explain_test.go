package tasks_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"git-ui/internal/ai"
	"git-ui/internal/ai/tasks"
	"git-ui/internal/testrepo"
)

func bigCommit(t *testing.T) (*testrepo.Repo, string) {
	t.Helper()
	r := testrepo.New(t)
	r.Commit("base")
	var b strings.Builder
	for i := 0; i < 800; i++ {
		fmt.Fprintf(&b, "config line number %d\n", i)
	}
	r.WriteFile("config.txt", b.String())
	r.Git("add", "config.txt")
	r.Git("commit", "-q", "-m", "feat: add config", "-m", "Needed for NEXO-7.")
	return r, r.Git("rev-parse", "HEAD")
}

func TestExplainContextTruncatesPerBudget(t *testing.T) {
	r, hash := bigCommit(t)
	ctx := context.Background()

	apple, err := tasks.ExplainContext(ctx, r.Dir, hash, tasks.AppleDiffBudget)
	if err != nil {
		t.Fatal(err)
	}
	ollama, err := tasks.ExplainContext(ctx, r.Dir, hash, tasks.OllamaDiffBudget)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Commit " + hash[:7], "Subject: feat: add config", "Needed for NEXO-7.", "A config.txt", "Diff:", "[truncated]"} {
		if !strings.Contains(apple, want) {
			t.Errorf("context missing %q", want)
		}
	}
	appleDiff := apple[strings.Index(apple, "Diff:"):]
	ollamaDiff := ollama[strings.Index(ollama, "Diff:"):]
	if len(appleDiff) > tasks.AppleDiffBudget+40 || len(ollamaDiff) > tasks.OllamaDiffBudget+40 {
		t.Fatalf("diff over budget: apple %d, ollama %d", len(appleDiff), len(ollamaDiff))
	}
	if len(ollamaDiff) <= len(appleDiff) {
		t.Fatal("Ollama budget should include more of the diff")
	}
}

type fakeResponder struct{ instructions, prompt string }

func (f *fakeResponder) Respond(ctx context.Context, instructions, prompt string) (<-chan ai.Chunk, error) {
	f.instructions, f.prompt = instructions, prompt
	ch := make(chan ai.Chunk, 2)
	ch <- ai.Chunk{Delta: "- adds config"}
	ch <- ai.Chunk{Done: true}
	close(ch)
	return ch, nil
}

func TestExplainPassesInstructionsAndContext(t *testing.T) {
	r, hash := bigCommit(t)
	f := &fakeResponder{}

	ch, err := tasks.Explain(context.Background(), f, "explain it", r.Dir, hash, tasks.OllamaDiffBudget)
	if err != nil {
		t.Fatal(err)
	}
	var text string
	for c := range ch {
		text += c.Delta
	}
	if text != "- adds config" || f.instructions != "explain it" || !strings.Contains(f.prompt, "Subject: feat: add config") {
		t.Fatalf("text %q, instructions %q, prompt %q", text, f.instructions, f.prompt)
	}
}

func TestExplainUnknownCommit(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	if _, err := tasks.Explain(context.Background(), &fakeResponder{}, "i", r.Dir, "deadbeefdeadbeef", 100); err == nil {
		t.Fatal("expected error for unknown commit")
	}
}
