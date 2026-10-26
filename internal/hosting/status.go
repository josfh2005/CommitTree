package hosting

import (
	"sort"
	"time"
)

// State is how a check, or all the checks of a commit, stand.
type State string

const (
	Success State = "success"
	Failed  State = "failed"
	Running State = "running"
	Stopped State = "stopped"
	None    State = "none"
)

// Final reports whether the state will not change on its own: success,
// failed or stopped.
func (s State) Final() bool {
	return s == Success || s == Failed || s == Stopped
}

// Check is one pipeline or check run of a commit.
type Check struct {
	Key         string    `json:"key"`
	Name        string    `json:"name"`
	State       State     `json:"state"`
	URL         string    `json:"url"`
	Started     time.Time `json:"started,omitzero"`
	DurationSec int       `json:"durationSec"`
	Created     time.Time `json:"created,omitzero"`
}

// CommitStatus is a commit's aggregate state and the checks behind it.
type CommitStatus struct {
	Hash   string  `json:"hash"`
	State  State   `json:"state"`
	Checks []Check `json:"checks"`
}

// Settled reports whether nothing about the commit will change on its own: the
// aggregate is final and no check is still running. A commit can be failed
// while another of its checks runs; that is not settled.
func (s CommitStatus) Settled() bool {
	if !s.State.Final() {
		return false
	}
	for _, c := range s.Checks {
		if c.State == Running {
			return false
		}
	}
	return true
}

// Latest keeps the newest check (by Created) of each Key, so a run that
// failed and was re-run green counts once, as green. A running check with no
// time yet (a queued re-run) is the newest. Checks without a Key are keyed by
// their Name. The result is ordered by Name, then Key.
func Latest(checks []Check) []Check {
	keyOf := func(c Check) string {
		if c.Key != "" {
			return c.Key
		}
		return c.Name
	}
	newest := map[string]Check{}
	for _, c := range checks {
		k := keyOf(c)
		if old, ok := newest[k]; !ok || newer(c, old) {
			newest[k] = c
		}
	}
	out := make([]Check, 0, len(newest))
	for _, c := range newest {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return keyOf(out[i]) < keyOf(out[j])
	})
	return out
}

// Aggregate is the commit's state over the latest run of each check: any
// failed gives failed; else any running gives running; else any stopped gives
// stopped; else any success gives success; no checks give none.
func Aggregate(checks []Check) State {
	var running, stopped, success bool
	for _, c := range Latest(checks) {
		switch c.State {
		case Failed:
			return Failed
		case Running:
			running = true
		case Stopped:
			stopped = true
		case Success:
			success = true
		}
	}
	switch {
	case running:
		return Running
	case stopped:
		return Stopped
	case success:
		return Success
	}
	return None
}

// newer reports whether check a is a later run than b. A running check
// without a time is a queued run, later than any dated one.
func newer(a, b Check) bool {
	if a.Created.IsZero() != b.Created.IsZero() {
		return a.Created.IsZero() && a.State == Running
	}
	return a.Created.After(b.Created)
}
