package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"git-ui/internal/cmdlog"
	"git-ui/internal/testrepo"
)

func TestAutoFetchThroughTheAppLayerIsAuto(t *testing.T) {
	a, r, id := newPlainApp(t)
	bare := testrepo.NewBareFrom(t, r)
	r.Git("remote", "add", "origin", bare)
	r.Git("push", "-q", "-u", "origin", "main")
	other := testrepo.Clone(t, bare)
	other.Commit("new")
	other.Git("push", "-q", "origin", "main")

	res, err := a.AutoFetch(id)
	if err != nil {
		t.Fatal(err)
	}
	if res.NewCommits != 1 || res.Upstream != "origin/main" {
		t.Errorf("res = %+v", res)
	}
	view, _ := a.CommandLog(id)
	found := false
	for _, e := range view.Entries {
		if len(e.Args) > 0 && e.Args[0] == "fetch" {
			found = true
			if e.Origin != cmdlog.OriginAuto {
				t.Errorf("fetch origin = %q, want auto", e.Origin)
			}
		}
	}
	if !found {
		t.Error("the background fetch is not in the command log")
	}
	if _, ok := a.autoFetches.Load(id); ok {
		t.Error("the cancel is still registered after the fetch")
	}
}

func TestAutoFetchSkipsWhileAUserWriteRuns(t *testing.T) {
	a, r, id := newPlainApp(t)
	r.Git("remote", "add", "origin", testrepo.NewBareFrom(t, r))
	mu := a.writeMutex(id)
	mu.Lock()
	defer mu.Unlock()

	res, err := a.AutoFetch(id)
	if err != nil || !res.Skipped {
		t.Errorf("res = %+v, err = %v; want Skipped", res, err)
	}
}

func TestUserWriteCancelsABackgroundFetch(t *testing.T) {
	a, _, id := newPlainApp(t)
	mu := a.writeMutex(id)
	mu.Lock() // the background fetch holds the lock …
	cancelled := make(chan struct{})
	a.autoFetches.Store(id, context.CancelFunc(func() {
		close(cancelled)
		go func() { // … and lets go once git has stopped.
			time.Sleep(20 * time.Millisecond)
			a.autoFetches.Delete(id)
			mu.Unlock()
		}()
	}))

	ran := false
	if err := a.write(id, func(context.Context, string) error { ran = true; return nil }); err != nil {
		t.Fatalf("write = %v, want it to run", err)
	}
	select {
	case <-cancelled:
	default:
		t.Error("the background fetch was not cancelled")
	}
	if !ran {
		t.Error("the user write did not run")
	}
}

func TestUserWriteStillRefusedBehindAnotherUserWrite(t *testing.T) {
	a, _, id := newPlainApp(t)
	mu := a.writeMutex(id)
	mu.Lock()
	defer mu.Unlock()
	if err := a.write(id, func(context.Context, string) error { return nil }); !errors.Is(err, ErrBusy) {
		t.Errorf("write = %v, want ErrBusy", err)
	}
	if err := a.writeAll([]string{id}, func(context.Context) error { return nil }); !errors.Is(err, ErrBusy) {
		t.Errorf("writeAll = %v, want ErrBusy", err)
	}
}
