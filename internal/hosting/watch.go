package hosting

import (
	"context"
	"errors"
	"time"
)

// WatchEvent is one change in the state of a commit being watched after a
// push. Final is set on the last event of a watch: the pipeline reached
// success, failed or stopped.
type WatchEvent struct {
	RepoID string `json:"repoID"`
	Branch string `json:"branch"`
	Hash   string `json:"hash"`
	State  State  `json:"state"`
	Final  bool   `json:"final"`
	Noun   string `json:"noun"` // "Pipelines" | "Checks"
}

// Watcher polls one commit's checks until they finish. Zero Interval, MaxAge
// and NoCheckAge mean 30 s, 60 min and 5 min.
type Watcher struct {
	Interval, MaxAge, NoCheckAge time.Duration
	// Fetch reads the commit's current state from the host.
	Fetch func(ctx context.Context, repoID, hash string) (CommitStatus, error)
	// Emit is called on every change of state (nil: ignored).
	Emit func(WatchEvent)
	// Sleep waits d, or returns early with an error when ctx ends. Nil waits
	// on a timer. Tests replace it.
	Sleep func(ctx context.Context, d time.Duration) error
	// Now is the clock (nil: time.Now).
	Now func() time.Time
}

const (
	watchInterval   = 30 * time.Second
	watchMaxAge     = 60 * time.Minute
	watchNoCheckAge = 5 * time.Minute
)

func sleepTimer(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func orDefault(d, def time.Duration) time.Duration {
	if d <= 0 {
		return def
	}
	return d
}

// Watch blocks, so run it in a goroutine. It emits when the commit's state
// changes (starting from "no checks yet", which is not a change) and, once,
// with Final set, when it reaches a final state with no check of the commit
// still running (a failed commit with a sibling running is announced, not
// final, and the watch goes on), then returns. It also
// returns, with nothing final emitted, when ctx ends, when the host rejects
// the token or there is no account, after MaxAge, or when the commit has had
// no checks for NoCheckAge. A failed fetch (network, request limit) is not a
// change of state: the next poll tries again.
func (w *Watcher) Watch(ctx context.Context, repoID, branch, hash, noun string) {
	interval := orDefault(w.Interval, watchInterval)
	maxAge := orDefault(w.MaxAge, watchMaxAge)
	noCheckAge := orDefault(w.NoCheckAge, watchNoCheckAge)
	now, sleep := w.Now, w.Sleep
	if now == nil {
		now = time.Now
	}
	if sleep == nil {
		sleep = sleepTimer
	}
	start := now()
	noneSince := start // since when there have been no checks
	last := None
	for {
		st, err := w.Fetch(ctx, repoID, hash)
		switch {
		case ctx.Err() != nil:
			return
		case errors.Is(err, ErrRejected) || errors.Is(err, ErrNoAccount):
			return
		case err == nil:
			settled := st.Settled()
			if st.State != last || settled {
				last = st.State
				if w.Emit != nil {
					w.Emit(WatchEvent{RepoID: repoID, Branch: branch, Hash: hash, State: st.State, Final: settled, Noun: noun})
				}
				if settled {
					return
				}
			}
			if st.State != None {
				noneSince = now()
			}
		}
		t := now()
		if t.Sub(start) >= maxAge || (last == None && t.Sub(noneSince) >= noCheckAge) {
			return
		}
		if sleep(ctx, interval) != nil {
			return
		}
	}
}
