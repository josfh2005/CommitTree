package gitcmd

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestLineWriterSplitsOnCRAndLF(t *testing.T) {
	var buf bytes.Buffer
	var lines []string
	w := &lineWriter{buf: &buf, onLine: func(s string) { lines = append(lines, s) }}
	w.Write([]byte("Receiving objects:  10% (1/10)\rReceiving objects:  50% (5/10)\r"))
	w.Write([]byte("Receiving objects: 100% (10/10), done.\nResolving del"))
	w.Write([]byte("tas: 100% (2/2)   \n\n"))
	w.Write([]byte("tail without end"))
	w.flush()
	want := []string{
		"Receiving objects:  10% (1/10)",
		"Receiving objects:  50% (5/10)",
		"Receiving objects: 100% (10/10), done.",
		"Resolving deltas: 100% (2/2)",
		"tail without end",
	}
	if len(lines) != len(want) {
		t.Fatalf("lines = %q, want %q", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
	if !bytes.Contains(buf.Bytes(), []byte("tail without end")) {
		t.Fatal("buffer must keep everything written")
	}
}

// newBare makes a bare repository with one commit with plain exec, keeping
// this package's tests free of other internal packages.
func newBare(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	work := filepath.Join(dir, "work")
	bare := filepath.Join(dir, "bare.git")
	for _, args := range [][]string{
		{"init", "-q", "-b", "main", work},
		{"-C", work, "-c", "user.name=T", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "base"},
		{"clone", "-q", "--bare", work, bare},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return bare
}

func TestRunStreamPassesStderrLines(t *testing.T) {
	bare := newBare(t)
	parent := t.TempDir()
	var lines []string
	_, err := RunStream(context.Background(), parent, nil, 0, func(s string) { lines = append(lines, s) },
		"clone", "--progress", "file://"+bare, "copy")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) == 0 || lines[0] != "Cloning into 'copy'..." {
		t.Fatalf("lines = %q", lines)
	}
}

func TestRunStreamStallIsTimeout(t *testing.T) {
	parent := t.TempDir()
	start := time.Now()
	// ssh never answers: after "Cloning into 'x'..." git writes nothing.
	_, err := RunStream(context.Background(), parent, []string{"GIT_SSH_COMMAND=sleep 30;:"}, 300*time.Millisecond, nil,
		"clone", "--progress", "ssh://example.invalid/x.git", "x")
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("stall took %v", d)
	}
}

func TestRunStreamIsRecorded(t *testing.T) {
	var got []Record
	SetRecorder(&Recorder{End: func(r Record) { got = append(got, r) }})
	t.Cleanup(func() { SetRecorder(nil) })
	parent := t.TempDir()
	RunStream(context.Background(), parent, nil, 0, nil, "clone", "--progress", "file://"+newBare(t), "c")
	if len(got) != 1 || got[0].Args[0] != "clone" || got[0].Dir != parent {
		t.Fatalf("records = %+v", got)
	}
}

func TestRunStreamCancelViaStart(t *testing.T) {
	cancels := make(chan func(), 1)
	SetRecorder(&Recorder{Begin: func(s Start) int64 { cancels <- s.Cancel; return 1 }})
	t.Cleanup(func() { SetRecorder(nil) })
	done := make(chan error, 1)
	go func() {
		_, err := RunStream(context.Background(), t.TempDir(), []string{"GIT_SSH_COMMAND=sleep 30;:"}, 0, nil,
			"clone", "ssh://example.invalid/x.git", "x")
		done <- err
	}()
	select {
	case cancel := <-cancels:
		time.Sleep(300 * time.Millisecond)
		cancel()
	case <-time.After(10 * time.Second):
		t.Fatal("git never started")
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrCancelled) {
			t.Fatalf("err = %v, want ErrCancelled", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cancel did not stop git")
	}
}
