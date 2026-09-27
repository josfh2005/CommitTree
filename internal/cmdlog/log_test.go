package cmdlog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"git-ui/internal/gitcmd"
)

func rec(dir string, args ...string) gitcmd.Record {
	return gitcmd.Record{Ctx: context.Background(), Dir: dir, Args: args, Start: time.Unix(100, 0), Duration: 42 * time.Millisecond}
}

func TestAddFillsEntry(t *testing.T) {
	l := New()
	r := rec("/r/", "push", "https://bob:hunter22@h/r.git")
	r.Err = &gitcmd.Error{ExitCode: 128}
	r.ExitCode = 128
	r.Stderr = "fatal: https://bob:hunter22@h/r.git denied"
	e := l.Add(r)

	if e.Repo != "/r" || e.Kind != KindWrite || e.Origin != OriginYou || e.Outcome != OutcomeFailed || e.ExitCode != 128 || e.DurationMs != 42 {
		t.Fatalf("entry wrong: %+v", e)
	}
	if strings.Contains(strings.Join(e.Args, " "), "hunter22") {
		t.Fatalf("args not redacted: %v", e.Args)
	}
	out, err := l.Output("/r", e.ID)
	if err != nil || strings.Contains(out.Stderr, "hunter22") {
		t.Fatalf("output not redacted: %+v %v", out, err)
	}
}

func TestOriginFallbackAndContext(t *testing.T) {
	l := New()
	if e := l.Add(rec("/r", "status")); e.Origin != OriginAuto || e.Kind != KindRead {
		t.Fatalf("read without origin: %+v", e)
	}
	if e := l.Add(rec("/r", "commit", "-m", "x")); e.Origin != OriginYou {
		t.Fatalf("write without origin: %+v", e)
	}
	r := rec("/r", "status")
	r.Ctx = WithOrigin(context.Background(), OriginAI)
	if e := l.Add(r); e.Origin != OriginAI {
		t.Fatalf("ctx origin ignored: %+v", e)
	}
}

func TestOutcomeTimeout(t *testing.T) {
	l := New()
	r := rec("/r", "fetch")
	r.Err = &gitcmd.Error{ExitCode: -1, Err: gitcmd.ErrTimeout}
	r.ExitCode = -1
	if e := l.Add(r); e.Outcome != OutcomeTimeout {
		t.Fatalf("got %s", e.Outcome)
	}
}

func TestListNewestFirstPerRepo(t *testing.T) {
	l := New()
	a1 := l.Add(rec("/a", "status"))
	l.Add(rec("/b", "status"))
	a2 := l.Add(rec("/a", "log"))
	got := l.List("/a")
	if len(got) != 2 || got[0].ID != a2.ID || got[1].ID != a1.ID {
		t.Fatalf("got %+v", got)
	}
	if len(l.List("/nope")) != 0 {
		t.Fatal("unknown repo must be empty")
	}
}

func TestRingKeepsLast500(t *testing.T) {
	l := New()
	var first, last Entry
	for i := 0; i < MaxEntries+10; i++ {
		e := l.Add(rec("/r", "status", fmt.Sprint(i)))
		if i == 0 {
			first = e
		}
		last = e
	}
	got := l.List("/r")
	if len(got) != MaxEntries || got[0].ID != last.ID {
		t.Fatalf("len %d, newest %d", len(got), got[0].ID)
	}
	if _, err := l.Output("/r", first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("evicted entry: got %v", err)
	}
}

func TestOutputTruncatedOnRuneBoundary(t *testing.T) {
	l := New()
	r := rec("/r", "log")
	r.Stdout = "a" + strings.Repeat("é", MaxStream) // the cap falls inside an é
	e := l.Add(r)
	out, _ := l.Output("/r", e.ID)
	if !e.OutputTruncated || len(out.Stdout) > MaxStream || !utf8.ValidString(out.Stdout) {
		t.Fatalf("truncated=%v len=%d valid=%v", e.OutputTruncated, len(out.Stdout), utf8.ValidString(out.Stdout))
	}
}

func TestLargeOutputMaskedNearStartAndTruncated(t *testing.T) {
	l := New()
	url := "https://bob:hunter22@h/r.git"
	r := rec("/r", "clone", url)
	// The secret sits well before MaxStream; the bulk of the output goes
	// past MaxStream+4096, so a naive cut before masking would either miss
	// the secret (impossible here, it's near the start) or, if masking ran
	// on the whole multi-megabyte buffer, would be slow. This checks
	// correctness: near-start secrets stay masked even though most of the
	// output is discarded before masking runs.
	r.Stdout = "cloning " + url + "\n" + strings.Repeat("x", MaxStream+8192)
	e := l.Add(r)
	if !e.OutputTruncated {
		t.Fatal("want OutputTruncated for output past MaxStream")
	}
	out, err := l.Output("/r", e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.Stdout, "hunter22") {
		t.Fatalf("secret near the start not masked: %q", out.Stdout[:min(80, len(out.Stdout))])
	}
	if len(out.Stdout) > MaxStream {
		t.Fatalf("not capped to MaxStream: %d", len(out.Stdout))
	}
}

func TestOutputBudgetDropsOldestOutputs(t *testing.T) {
	l := New()
	big := strings.Repeat("x", MaxStream)
	n := MaxOutputBytes/MaxStream + 5
	var ids []int64
	for i := 0; i < n; i++ {
		r := rec("/r", "log")
		r.Stdout = big
		ids = append(ids, l.Add(r).ID)
	}
	list := l.List("/r")
	oldest, newest := list[len(list)-1], list[0]
	if !oldest.OutputDropped || newest.OutputDropped {
		t.Fatalf("oldest dropped=%v newest dropped=%v", oldest.OutputDropped, newest.OutputDropped)
	}
	if out, err := l.Output("/r", ids[0]); err != nil || out.Stdout != "" {
		t.Fatalf("dropped output: %q %v", out.Stdout, err)
	}
	if out, _ := l.Output("/r", ids[n-1]); out.Stdout != big {
		t.Fatal("newest output must be kept")
	}
}

func TestClear(t *testing.T) {
	l := New()
	l.Add(rec("/r", "status"))
	l.Clear("/r")
	if len(l.List("/r")) != 0 {
		t.Fatal("not cleared")
	}
}

func start(dir string, cancel func(), args ...string) gitcmd.Start {
	return gitcmd.Start{Ctx: context.Background(), Dir: dir, Args: args, Start: time.Unix(100, 0), Cancel: cancel}
}

func TestBeginWriteIsRunningThenReplacedInPlace(t *testing.T) {
	l := New()
	before := l.Add(rec("/r", "commit", "-m", "a"))
	b, shown := l.Begin(start("/r/", func() {}, "push"))
	if !shown || b.Outcome != OutcomeRunning || b.Origin != OriginYou || b.Kind != KindWrite || b.Repo != "/r" || b.ID <= before.ID {
		t.Fatalf("running entry wrong: %+v %v", b, shown)
	}
	if got := l.List("/r"); len(got) != 2 || got[0].ID != b.ID || got[0].Outcome != OutcomeRunning {
		t.Fatalf("list: %+v", got)
	}
	r := rec("/r", "push")
	r.ID = b.ID
	r.Stdout = "done"
	e := l.Add(r)
	got := l.List("/r")
	if e.ID != b.ID || len(got) != 2 || got[0].ID != b.ID || got[0].Outcome != OutcomeOK {
		t.Fatalf("not replaced in place: %+v", got)
	}
	if out, err := l.Output("/r", b.ID); err != nil || out.Stdout != "done" {
		t.Fatalf("output: %+v %v", out, err)
	}
}

func TestBeginReadStoresNothing(t *testing.T) {
	l := New()
	b, shown := l.Begin(start("/r", func() {}, "status"))
	if shown || b.ID == 0 || len(l.List("/r")) != 0 {
		t.Fatalf("a running read must not be listed: %+v %v", b, shown)
	}
	if err := l.Cancel("/r", b.ID); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("a read cannot be cancelled: %v", err)
	}
	r := rec("/r", "status")
	r.ID = b.ID
	if e := l.Add(r); e.ID != b.ID || len(l.List("/r")) != 1 {
		t.Fatalf("read end: %+v", e)
	}
}

func TestBeginKeepsOriginAndRedacts(t *testing.T) {
	l := New()
	s := start("/r", func() {}, "push", "https://bob:hunter22@h/r.git")
	s.Ctx = WithOrigin(context.Background(), OriginAI)
	b, _ := l.Begin(s)
	if b.Origin != OriginAI || strings.Contains(strings.Join(b.Args, " "), "hunter22") {
		t.Fatalf("got %+v", b)
	}
}

func TestCancel(t *testing.T) {
	l := New()
	called := 0
	b, _ := l.Begin(start("/r", func() { called++ }, "push"))
	if err := l.Cancel("/other", b.ID); !errors.Is(err, ErrNotRunning) || called != 0 {
		t.Fatalf("another repository's cancel: %v, called %d", err, called)
	}
	if err := l.Cancel("/r/", b.ID); err != nil || called != 1 {
		t.Fatalf("cancel: %v, called %d", err, called)
	}
	r := rec("/r", "push")
	r.ID = b.ID
	r.ExitCode = -1
	r.Err = &gitcmd.Error{ExitCode: -1, Err: gitcmd.ErrCancelled}
	if e := l.Add(r); e.Outcome != OutcomeCancelled {
		t.Fatalf("got %s", e.Outcome)
	}
	if err := l.Cancel("/r", b.ID); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("finished command: %v", err)
	}
	if err := l.Cancel("/r", 999); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("unknown id: %v", err)
	}
}

func TestClearKeepsRunning(t *testing.T) {
	l := New()
	l.Add(rec("/r", "commit", "-m", "x"))
	b, _ := l.Begin(start("/r", func() {}, "push"))
	l.Clear("/r")
	if got := l.List("/r"); len(got) != 1 || got[0].ID != b.ID {
		t.Fatalf("got %+v", got)
	}
}
