package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
	if h, _ := a.lockFor(id).state(); h != holderFree {
		t.Errorf("lock holder = %v after the fetch, want free", h)
	}
}

func TestAutoFetchSkipsWhileAUserWriteRuns(t *testing.T) {
	a, r, id := newPlainApp(t)
	r.Git("remote", "add", "origin", testrepo.NewBareFrom(t, r))
	l := a.lockFor(id)
	if err := l.lockUser(); err != nil {
		t.Fatal(err)
	}
	defer l.unlock()

	res, err := a.AutoFetch(id)
	if err != nil || !res.Skipped {
		t.Errorf("res = %+v, err = %v; want Skipped", res, err)
	}
}

func TestUserWriteCancelsABackgroundFetch(t *testing.T) {
	a, _, id := newPlainApp(t)
	l := a.lockFor(id)
	cancelled := make(chan struct{})
	l.tryLockAuto(func() { // the background fetch holds the lock …
		close(cancelled)
		go func() { // … and lets go once git has stopped.
			time.Sleep(20 * time.Millisecond)
			l.unlock()
		}()
	})

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
	l := a.lockFor(id)
	if err := l.lockUser(); err != nil {
		t.Fatal(err)
	}
	defer l.unlock()
	if err := a.write(id, func(context.Context, string) error { return nil }); !errors.Is(err, ErrBusy) {
		t.Errorf("write = %v, want ErrBusy", err)
	}
	if err := a.writeAll([]string{id}, func(context.Context) error { return nil }); !errors.Is(err, ErrBusy) {
		t.Errorf("writeAll = %v, want ErrBusy", err)
	}
}

func TestUserWriteCancelsARealBackgroundFetchAsCancelled(t *testing.T) {
	a, r, id := newPlainApp(t)
	arrived := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		select {
		case arrived <- struct{}{}:
		default:
		}
		<-req.Context().Done() // a remote that never answers
	}))
	defer srv.Close()
	r.Git("remote", "add", "origin", srv.URL+"/repo.git")

	done := make(chan error, 1)
	go func() { _, err := a.AutoFetch(id); done <- err }()
	select {
	case <-arrived:
	case <-time.After(10 * time.Second):
		t.Fatal("the background fetch never reached the remote")
	}
	if err := a.write(id, func(context.Context, string) error { return nil }); err != nil {
		t.Fatalf("write = %v", err)
	}
	<-done
	view, _ := a.CommandLog(id)
	for _, e := range view.Entries {
		if len(e.Args) > 0 && e.Args[0] == "fetch" {
			if e.Outcome != cmdlog.OutcomeCancelled {
				t.Errorf("fetch outcome = %q, want cancelled", e.Outcome)
			}
			return
		}
	}
	t.Error("no fetch in the command log")
}

func TestOnlyOneUserWriteWaitsForABackgroundFetch(t *testing.T) {
	a, _, id := newPlainApp(t)
	l := a.lockFor(id)
	l.tryLockAuto(func() {}) // a background fetch that takes a while to stop
	first := make(chan error, 1)
	go func() { first <- a.write(id, func(context.Context, string) error { return nil }) }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, w := l.state(); w {
			break
		}
		if time.Now().After(deadline) {
			l.unlock()
			t.Fatal("the first write did not start waiting for the background fetch")
		}
		time.Sleep(time.Millisecond)
	}
	second := make(chan error, 1)
	go func() { second <- a.write(id, func(context.Context, string) error { return nil }) }()
	select {
	case err := <-second:
		if !errors.Is(err, ErrBusy) {
			t.Errorf("second write = %v, want ErrBusy", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("the second write waited instead of being refused")
	}
	l.unlock()
	if err := <-first; err != nil {
		t.Errorf("first write = %v", err)
	}
}
