package hosting

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestLatestKeepsNewestPerKey(t *testing.T) {
	t1 := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	got := Latest([]Check{
		{Key: "build", Name: "build", State: Failed, Created: t1},
		{Key: "build", Name: "build", State: Success, Created: t2},
	})
	if len(got) != 1 || got[0].State != Success {
		t.Fatalf("got %+v", got)
	}
}

func TestLatestOrdersByName(t *testing.T) {
	got := Latest([]Check{{Key: "b", Name: "zeta"}, {Key: "a", Name: "alpha"}})
	if len(got) != 2 || got[0].Name != "alpha" || got[1].Name != "zeta" {
		t.Fatalf("got %+v", got)
	}
}

func TestAggregate(t *testing.T) {
	t1 := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	cases := []struct {
		name   string
		checks []Check
		want   State
	}{
		{"failed+running", []Check{{Key: "a", State: Failed}, {Key: "b", State: Running}}, Failed},
		{"running+success", []Check{{Key: "a", State: Running}, {Key: "b", State: Success}}, Running},
		{"stopped+success", []Check{{Key: "a", State: Stopped}, {Key: "b", State: Success}}, Stopped},
		{"success+success", []Check{{Key: "a", State: Success}, {Key: "b", State: Success}}, Success},
		{"empty", nil, None},
		{"rerun green", []Check{
			{Key: "a", State: Failed, Created: t1},
			{Key: "a", State: Success, Created: t2},
		}, Success},
	}
	for _, c := range cases {
		if got := Aggregate(c.checks); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

func TestStateFinal(t *testing.T) {
	for s, want := range map[State]bool{Success: true, Failed: true, Stopped: true, Running: false, None: false} {
		if s.Final() != want {
			t.Errorf("%s.Final() = %v, want %v", s, s.Final(), want)
		}
	}
}

func TestLatestQueuedRerunWins(t *testing.T) {
	// A queued GitHub check run has no start time yet. As a re-run it is the
	// newest run of its check, not the oldest.
	t1 := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	checks := []Check{
		{Key: "build", Name: "build", State: Failed, Created: t1},
		{Key: "build", Name: "build", State: Running},
	}
	got := Latest(checks)
	if len(got) != 1 || got[0].State != Running {
		t.Fatalf("Latest = %+v, want the queued re-run", got)
	}
	if Aggregate(checks) != Running {
		t.Fatalf("Aggregate = %s, want running", Aggregate(checks))
	}
}

func TestSettled(t *testing.T) {
	cases := []struct {
		name string
		st   CommitStatus
		want bool
	}{
		{"success", CommitStatus{State: Success, Checks: []Check{{Key: "a", State: Success}}}, true},
		{"failed alone", CommitStatus{State: Failed, Checks: []Check{{Key: "a", State: Failed}}}, true},
		{"failed with a running sibling", CommitStatus{State: Failed, Checks: []Check{{Key: "a", State: Failed}, {Key: "b", State: Running}}}, false},
		{"final without checks", CommitStatus{State: Success}, true},
		{"running", CommitStatus{State: Running, Checks: []Check{{Key: "a", State: Running}}}, false},
		{"none", CommitStatus{State: None}, false},
	}
	for _, c := range cases {
		if got := c.st.Settled(); got != c.want {
			t.Errorf("%s: Settled = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestCheckJSONOmitsZeroTimes(t *testing.T) {
	// A zero time.Time would be sent as "0001-01-01T00:00:00Z", which the
	// frontend would read as a real moment.
	b, err := json.Marshal(Check{Key: "k", Name: "n", State: Running})
	if err != nil {
		t.Fatal(err)
	}
	if s := string(b); strings.Contains(s, "0001") || strings.Contains(s, "started") || strings.Contains(s, "created") {
		t.Fatalf("json = %s", s)
	}
	when := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	b, _ = json.Marshal(Check{Started: when, Created: when})
	if s := string(b); !strings.Contains(s, `"started":"2026-10-01T10:00:00Z"`) || !strings.Contains(s, `"created":`) {
		t.Fatalf("json = %s", s)
	}
}
