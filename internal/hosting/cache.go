package hosting

import (
	"sync"
	"time"
)

const (
	noneTTL    = 10 * time.Minute
	runningTTL = 30 * time.Second
)

// Cache remembers commit statuses per repository for the session. A final
// state (success, failed, stopped) never expires; none is asked again after
// 10 minutes and running after 30 seconds. It is safe for concurrent use.
type Cache struct {
	mu      sync.Mutex
	now     func() time.Time
	entries map[string]map[string]entry // repoKey → hash → entry
}

type entry struct {
	st  CommitStatus
	got time.Time
}

// NewCache makes an empty cache. now is the clock (nil: time.Now).
func NewCache(now func() time.Time) *Cache {
	if now == nil {
		now = time.Now
	}
	return &Cache{now: now, entries: map[string]map[string]entry{}}
}

// RepoKey is the cache key of a repository. It carries the host and the
// owner/name, so a remote that is edited to point elsewhere starts afresh.
func RepoKey(r Repo) string { return string(r.Host) + ":" + r.FullName() }

func (e entry) fresh(now time.Time) bool {
	switch {
	case e.st.Settled():
		return true
	case e.st.State.Final(): // failed while a sibling check still runs
		return now.Sub(e.got) < runningTTL
	case e.st.State == None:
		return now.Sub(e.got) < noneTTL
	case e.st.State == Running:
		return now.Sub(e.got) < runningTTL
	}
	return false
}

// Split sorts the hashes into the ones with a fresh entry (have) and the ones
// to ask the host about (missing, in the given order, without repeats).
func (c *Cache) Split(repoKey string, hashes []string) (have map[string]CommitStatus, missing []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	have = map[string]CommitStatus{}
	seen := map[string]bool{}
	for _, h := range hashes {
		if seen[h] {
			continue
		}
		seen[h] = true
		if e, ok := c.entries[repoKey][h]; ok && e.fresh(now) {
			have[h] = e.st
		} else {
			missing = append(missing, h)
		}
	}
	return have, missing
}

// Put stores statuses, keyed by their hash in the map, as of now.
func (c *Cache) Put(repoKey string, sts map[string]CommitStatus) {
	c.mu.Lock()
	defer c.mu.Unlock()
	m := c.entries[repoKey]
	if m == nil {
		m = map[string]entry{}
		c.entries[repoKey] = m
	}
	now := c.now()
	for h, st := range sts {
		m[h] = entry{st: st, got: now}
	}
}

// Get returns a fresh entry.
func (c *Cache) Get(repoKey, hash string) (CommitStatus, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[repoKey][hash]; ok && e.fresh(c.now()) {
		return e.st, true
	}
	return CommitStatus{}, false
}
