package hosting

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// watchRig is a Watcher over a scripted Fetch and a fake clock that Sleep
// advances. After the script runs out, Fetch keeps returning the last answer.
type watchRig struct {
	w      *Watcher
	clk    *fakeClock
	script []State
	calls  int
	events []WatchEvent
	err    func(call int) error
}

func newWatchRig(script ...State) *watchRig {
	r := &watchRig{clk: newClock(), script: script}
	r.w = &Watcher{
		Interval: 30 * time.Second, MaxAge: 60 * time.Minute, NoCheckAge: 5 * time.Minute,
		Now: r.clk.Now,
		Sleep: func(_ context.Context, d time.Duration) error {
			r.clk.Advance(d)
			return nil
		},
		Emit: func(e WatchEvent) { r.events = append(r.events, e) },
		Fetch: func(_ context.Context, _, hash string) (CommitStatus, error) {
			i := r.calls
			r.calls++
			if r.err != nil {
				if err := r.err(i); err != nil {
					return CommitStatus{}, err
				}
			}
			if i >= len(r.script) {
				i = len(r.script) - 1
			}
			return CommitStatus{Hash: hash, State: r.script[i]}, nil
		},
	}
	return r
}

func (r *watchRig) run() { r.w.Watch(context.Background(), "repo1", "main", "abc", "Pipelines") }

func TestWatchStopsAtFinal(t *testing.T) {
	r := newWatchRig(Running, Running, Failed, Success)
	r.run()
	if r.calls != 3 {
		t.Fatalf("Fetch called %d times, want 3", r.calls)
	}
	var finals []WatchEvent
	for _, e := range r.events {
		if e.Final {
			finals = append(finals, e)
		}
	}
	want := WatchEvent{RepoID: "repo1", Branch: "main", Hash: "abc", State: Failed, Final: true, Noun: "Pipelines"}
	if len(finals) != 1 || finals[0] != want {
		t.Fatalf("final events %+v, want exactly %+v", finals, want)
	}
}

func TestWatchGivesUpWithoutChecks(t *testing.T) {
	r := newWatchRig(None)
	r.run()
	if len(r.events) != 0 {
		t.Fatalf("events %+v, want none", r.events)
	}
	if got := r.clk.Now().Sub(time.Unix(1_800_000_000, 0)); got < 5*time.Minute || got > 6*time.Minute {
		t.Fatalf("gave up after %v, want about 5 min", got)
	}
}

func TestWatchNoCheckClockRestartsWhenChecksAppear(t *testing.T) {
	// Checks show up at 4 min and finish at 8 min: the 5 min limit counts
	// from the last time there were none.
	script := make([]State, 0, 20)
	for i := 0; i < 8; i++ {
		script = append(script, None)
	}
	script = append(script, Running)
	for i := 0; i < 8; i++ {
		script = append(script, Running)
	}
	script = append(script, Success)
	r := newWatchRig(script...)
	r.run()
	if n := len(r.events); n != 2 || !r.events[1].Final || r.events[1].State != Success {
		t.Fatalf("events %+v", r.events)
	}
}

func TestWatchMaxAge(t *testing.T) {
	r := newWatchRig(Running)
	r.run()
	for _, e := range r.events {
		if e.Final {
			t.Fatalf("final event after running out the clock: %+v", e)
		}
	}
	if got := r.clk.Now().Sub(time.Unix(1_800_000_000, 0)); got < 60*time.Minute || got > 61*time.Minute {
		t.Fatalf("stopped after %v, want about 60 min", got)
	}
}

func TestWatchEmitsChangesOnly(t *testing.T) {
	r := newWatchRig(None, Running, Running, Success)
	r.run()
	if len(r.events) != 2 || r.events[0].State != Running || r.events[0].Final || r.events[1].State != Success || !r.events[1].Final {
		t.Fatalf("events %+v, want running then final success", r.events)
	}
}

func TestWatchFetchErrorsKeepPolling(t *testing.T) {
	r := newWatchRig(Running, Running, Failed)
	r.err = func(call int) error {
		if call == 1 {
			return &RateLimitError{Host: GitHub}
		}
		if call == 2 {
			return errors.New("network down")
		}
		return nil
	}
	r.run()
	if r.calls != 4 {
		t.Fatalf("Fetch called %d times, want 4 (errors do not end the watch)", r.calls)
	}
	if last := r.events[len(r.events)-1]; !last.Final || last.State != Failed {
		t.Fatalf("events %+v", r.events)
	}
}

func TestWatchStopsSilentlyWhenRejectedOrNoAccount(t *testing.T) {
	for _, e := range []error{ErrRejected, ErrNoAccount} {
		r := newWatchRig(Running)
		r.err = func(call int) error {
			if call == 1 {
				return e
			}
			return nil
		}
		r.run()
		if r.calls != 2 {
			t.Fatalf("%v: Fetch called %d times, want 2", e, r.calls)
		}
		if len(r.events) != 1 || r.events[0].Final {
			t.Fatalf("%v: events %+v, want only the running change", e, r.events)
		}
	}
}

func TestWatchStopsWhenContextEnds(t *testing.T) {
	r := newWatchRig(Running)
	ctx, cancel := context.WithCancel(context.Background())
	r.w.Sleep = func(ctx context.Context, d time.Duration) error {
		cancel()
		return ctx.Err()
	}
	r.w.Watch(ctx, "repo1", "main", "abc", "Checks")
	if r.calls != 1 {
		t.Fatalf("Fetch called %d times, want 1", r.calls)
	}
}

func TestWatchDefaults(t *testing.T) {
	w := &Watcher{
		Fetch: func(context.Context, string, string) (CommitStatus, error) {
			return CommitStatus{State: Success}, nil
		},
	}
	var got []WatchEvent
	w.Emit = func(e WatchEvent) { got = append(got, e) }
	w.Watch(context.Background(), "r", "b", "h", "Checks") // zero fields: real clock, first poll is final
	if len(got) != 1 || !got[0].Final {
		t.Fatalf("events %+v", got)
	}
}

func TestWatchKeepsGoingWhileASiblingRuns(t *testing.T) {
	// failed arrives while another check still runs: the state is announced,
	// but not as final, and the watch goes on until everything has finished.
	r := newWatchRig(Running)
	failedRunning := CommitStatus{State: Failed, Checks: []Check{{Key: "a", State: Failed}, {Key: "b", State: Running}}}
	failedDone := CommitStatus{State: Failed, Checks: []Check{{Key: "a", State: Failed}, {Key: "b", State: Success}}}
	answers := []CommitStatus{{State: Running}, failedRunning, failedRunning, failedDone}
	r.w.Fetch = func(_ context.Context, _, hash string) (CommitStatus, error) {
		i := r.calls
		r.calls++
		return answers[i], nil
	}
	r.run()
	if r.calls != 4 {
		t.Fatalf("Fetch called %d times, want 4", r.calls)
	}
	var got []string
	for _, e := range r.events {
		got = append(got, string(e.State)+map[bool]string{true: "!", false: ""}[e.Final])
	}
	if want := "running failed failed!"; strings.Join(got, " ") != want {
		t.Fatalf("events %v, want %s", got, want)
	}
}
