package hosting

import (
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
