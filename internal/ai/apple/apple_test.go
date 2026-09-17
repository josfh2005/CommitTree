package apple_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git-ui/internal/ai"
	"git-ui/internal/ai/apple"
)

// fakeHelper writes an executable shell script and returns its path.
func fakeHelper(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "git-ui-apple")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func collect(ch <-chan ai.Chunk) []ai.Chunk {
	var out []ai.Chunk
	for c := range ch {
		out = append(out, c)
	}
	return out
}

func TestStatus(t *testing.T) {
	ok := apple.New(fakeHelper(t, `echo '{"available":true}'`)).Status(context.Background())
	if !ok.Available {
		t.Fatalf("status = %+v", ok)
	}
	off := apple.New(fakeHelper(t, `echo '{"available":false,"reason":"appleIntelligenceNotEnabled"}'`)).Status(context.Background())
	if off.Available || off.Reason != "appleIntelligenceNotEnabled" {
		t.Fatalf("status = %+v", off)
	}
	broken := apple.New(fakeHelper(t, `exit 3`)).Status(context.Background())
	if broken.Available || broken.Reason != "helperFailed" {
		t.Fatalf("status = %+v", broken)
	}
	missing := apple.New("").Status(context.Background())
	if missing.Available || missing.Reason != "helperNotFound" {
		t.Fatalf("status = %+v", missing)
	}
}

func TestRespondStreamsAndSendsInput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.json")
	helper := fakeHelper(t, `cat > '`+input+`'
echo '{"delta":"Hola"}'
echo '{"delta":" mundo"}'
echo '{"done":true}'
`)

	ch, err := apple.New(helper).Respond(context.Background(), "be brief", "commit text")
	if err != nil {
		t.Fatal(err)
	}
	chunks := collect(ch)
	if len(chunks) != 3 || chunks[0].Delta != "Hola" || chunks[1].Delta != " mundo" || !chunks[2].Done {
		t.Fatalf("chunks = %#v", chunks)
	}
	var sent map[string]string
	data, _ := os.ReadFile(input)
	if err := json.Unmarshal(data, &sent); err != nil || sent["instructions"] != "be brief" || sent["prompt"] != "commit text" {
		t.Fatalf("input = %s, err %v", data, err)
	}
}

func TestRespondErrorLine(t *testing.T) {
	helper := fakeHelper(t, `cat >/dev/null
echo '{"error":"too large"}'
exit 1
`)
	ch, err := apple.New(helper).Respond(context.Background(), "i", "p")
	if err != nil {
		t.Fatal(err)
	}
	chunks := collect(ch)
	if len(chunks) != 1 || chunks[0].Err == nil || chunks[0].Err.Error() != "too large" {
		t.Fatalf("chunks = %#v", chunks)
	}
}

func TestRespondExitWithoutDone(t *testing.T) {
	helper := fakeHelper(t, `cat >/dev/null
echo '{"delta":"partial"}'
exit 1
`)
	ch, _ := apple.New(helper).Respond(context.Background(), "i", "p")
	chunks := collect(ch)
	last := chunks[len(chunks)-1]
	if last.Err == nil {
		t.Fatalf("chunks = %#v", chunks)
	}
}

func TestRespondTimeout(t *testing.T) {
	helper := fakeHelper(t, `cat >/dev/null
exec sleep 5
`)
	c := apple.New(helper)
	c.Timeout = 200 * time.Millisecond
	start := time.Now()
	ch, err := c.Respond(context.Background(), "i", "p")
	if err != nil {
		t.Fatal(err)
	}
	chunks := collect(ch)
	if time.Since(start) > 3*time.Second {
		t.Fatal("timeout not enforced")
	}
	last := chunks[len(chunks)-1]
	if !errors.Is(last.Err, context.DeadlineExceeded) {
		t.Fatalf("last = %#v", last)
	}
}

func TestRespondMissingHelper(t *testing.T) {
	if _, err := apple.New("").Respond(context.Background(), "i", "p"); !errors.Is(err, apple.ErrHelperNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestLocateFindsBuiltHelperFromRepoRoot(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "helpers", "apple", ".build", "release", "git-ui-apple")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	if got := apple.Locate(); !strings.HasSuffix(got, "helpers/apple/.build/release/git-ui-apple") {
		t.Fatalf("Locate = %q", got)
	}
}
