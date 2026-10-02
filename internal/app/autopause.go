package app

import (
	"sync"

	"git-ui/internal/cmdlog"
	"git-ui/internal/gitcmd"
)

// autoPause is the remotes background fetches skip because they failed
// for want of credentials, by repository (cmdlog.RepoKey). In memory only:
// a restart retries them.
type autoPause struct {
	mu      sync.Mutex
	remotes map[string]map[string]bool
}

func (p *autoPause) paused(key, remote string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.remotes[key][remote]
}

func (p *autoPause) pause(key string, remotes []string) {
	if len(remotes) == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.remotes == nil {
		p.remotes = map[string]map[string]bool{}
	}
	if p.remotes[key] == nil {
		p.remotes[key] = map[string]bool{}
	}
	for _, r := range remotes {
		p.remotes[key][r] = true
	}
}

func (p *autoPause) resume(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.remotes, key)
}

// noteRemoteUpdate unpauses a repository's background fetches when a fetch
// or pull of it that was not itself a background fetch succeeds — from the
// toolbar, the AI or git-flow alike: credentials work again.
func (a *App) noteRemoteUpdate(r gitcmd.Record) {
	if r.ExitCode != 0 || r.Ctx == nil || !cmdlog.UpdatesFromRemote(r.Args) {
		return
	}
	if o, ok := cmdlog.OriginFrom(r.Ctx); ok && o == cmdlog.OriginAuto {
		return
	}
	a.paused.resume(cmdlog.RepoKey(r.Dir))
}
