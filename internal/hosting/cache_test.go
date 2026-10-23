package hosting

import (
	"testing"
	"time"
)

func newTestCache() (*Cache, *fakeClock) {
	clk := newClock()
	return NewCache(clk.Now), clk
}

func TestCacheFinalNeverExpires(t *testing.T) {
	c, clk := newTestCache()
	c.Put("k", map[string]CommitStatus{"a": {Hash: "a", State: Success}, "b": {Hash: "b", State: Failed}, "c": {Hash: "c", State: Stopped}})
	clk.Advance(24 * time.Hour)
	have, missing := c.Split("k", []string{"a", "b", "c"})
	if len(have) != 3 || len(missing) != 0 {
		t.Fatalf("have %v missing %v", have, missing)
	}
	if st, ok := c.Get("k", "a"); !ok || st.State != Success {
		t.Fatalf("Get = %v %v", st, ok)
	}
}

func TestCacheNoneExpiresAfter10Min(t *testing.T) {
	c, clk := newTestCache()
	c.Put("k", map[string]CommitStatus{"a": {Hash: "a", State: None}})
	clk.Advance(9*time.Minute + 59*time.Second)
	if have, missing := c.Split("k", []string{"a"}); len(have) != 1 || len(missing) != 0 {
		t.Fatalf("at 9m59s: have %v missing %v", have, missing)
	}
	clk.Advance(2 * time.Second)
	if have, missing := c.Split("k", []string{"a"}); len(have) != 0 || len(missing) != 1 {
		t.Fatalf("at 10m01s: have %v missing %v", have, missing)
	}
}

func TestCacheRunningAfter30s(t *testing.T) {
	c, clk := newTestCache()
	c.Put("k", map[string]CommitStatus{"a": {Hash: "a", State: Running}})
	clk.Advance(29 * time.Second)
	if have, _ := c.Split("k", []string{"a"}); len(have) != 1 {
		t.Fatalf("at 29s: have %v", have)
	}
	clk.Advance(2 * time.Second)
	if _, missing := c.Split("k", []string{"a"}); len(missing) != 1 {
		t.Fatalf("at 31s: missing %v", missing)
	}
}

func TestCacheKeysAreSeparateAndUnknownIsMissing(t *testing.T) {
	c, _ := newTestCache()
	c.Put("github:o/r", map[string]CommitStatus{"a": {Hash: "a", State: Success}})
	have, missing := c.Split("github:o/other", []string{"a", "a", "b"})
	if len(have) != 0 || len(missing) != 2 || missing[0] != "a" || missing[1] != "b" {
		t.Fatalf("have %v missing %v", have, missing)
	}
	if got := RepoKey(Repo{Host: GitHub, Owner: "o", Name: "r"}); got != "github:o/r" {
		t.Fatalf("RepoKey = %q", got)
	}
}
