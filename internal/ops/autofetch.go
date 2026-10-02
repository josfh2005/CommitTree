package ops

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"git-ui/internal/gitcmd"
)

// AutoFetchTimeout bounds a background fetch: long enough for a slow
// remote, short enough that a hung one does not hold the write lock long.
const AutoFetchTimeout = 60 * time.Second

// NoPromptEnv makes every way git could ask the user fail instead:
// askpass programs (git's and ssh's) and Git Credential Manager's UI.
// Helpers that answer silently — osxkeychain, ssh-agent — still work.
var NoPromptEnv = []string{
	"GIT_ASKPASS=false",
	"SSH_ASKPASS=false",
	"SSH_ASKPASS_REQUIRE=never",
	"GCM_INTERACTIVE=never",
}

// AutoFetchResult is what one background fetch brought.
type AutoFetchResult struct {
	Skipped     bool   `json:"skipped"`
	Branch      string `json:"branch"`
	Upstream    string `json:"upstream"`
	NewCommits  int    `json:"newCommits"`
	RefsChanged bool   `json:"refsChanged"`
	// AuthFailed names the remotes this fetch could not reach for want of
	// credentials; the caller pauses them.
	AuthFailed []string `json:"authFailed"`
}

var authMarkers = []string{
	"authentication failed",
	"could not read username",
	"could not read password",
	"permission denied (publickey",
	"host key verification failed",
	"terminal prompts disabled",
}

// IsAuthError reports whether err is a git command that failed for want
// of credentials, as opposed to the network, a timeout or a cancel.
func IsAuthError(err error) bool {
	var gerr *gitcmd.Error
	if !errors.As(err, &gerr) {
		return false
	}
	s := strings.ToLower(gerr.Stderr)
	for _, m := range authMarkers {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// AutoFetch fetches each remote paused does not report, one at a time,
// without ever prompting, and reports the commits the checked-out branch's
// upstream gained in this fetch that HEAD lacks. Only this fetch's change
// counts, so commits a manual Fetch already brought are never reported
// again. A remote failing for want of credentials lands in AuthFailed;
// any other failure of one remote is left to the Commands panel. Both let
// the next remote run; a cancel stops them all.
func AutoFetch(ctx context.Context, dir string, paused func(remote string) bool) (AutoFetchResult, error) {
	var res AutoFetchResult
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "remote")
	if err != nil {
		return res, err
	}
	var remotes []string
	for _, r := range strings.Fields(out) {
		if paused == nil || !paused(r) {
			remotes = append(remotes, r)
		}
	}
	if len(remotes) == 0 {
		res.Skipped = true
		return res, nil
	}
	if b, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "symbolic-ref", "-q", "--short", "HEAD"); err == nil {
		res.Branch = strings.TrimSpace(b)
	}
	if res.Branch != "" {
		if u, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"); err == nil {
			res.Upstream = strings.TrimSpace(u)
		}
	}
	oldTip := upstreamTip(ctx, dir, res.Upstream)
	before, err := remoteRefs(ctx, dir)
	if err != nil {
		return res, err
	}
	for _, remote := range remotes {
		_, err := gitcmd.RunEnv(ctx, dir, AutoFetchTimeout, NoPromptEnv, "fetch", "--prune", remote)
		switch {
		case err == nil:
		case ctx.Err() != nil:
			return res, err
		case IsAuthError(err):
			res.AuthFailed = append(res.AuthFailed, remote)
		}
	}
	after, err := remoteRefs(ctx, dir)
	if err != nil {
		return res, err
	}
	res.RefsChanged = before != after
	newTip := upstreamTip(ctx, dir, res.Upstream)
	if oldTip != "" && newTip != "" && oldTip != newTip {
		n, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-list", "--count", newTip, "^"+oldTip, "^HEAD")
		if err == nil {
			res.NewCommits, _ = strconv.Atoi(strings.TrimSpace(n))
		}
	}
	return res, nil
}

// upstreamTip is the upstream's commit, "" when there is none or it is gone.
func upstreamTip(ctx context.Context, dir, upstream string) string {
	if upstream == "" {
		return ""
	}
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "-q", "@{upstream}^{commit}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// remoteRefs is every remote-tracking ref with its commit, one per line.
func remoteRefs(ctx context.Context, dir string) (string, error) {
	return gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "for-each-ref", "--format=%(refname) %(objectname)", "refs/remotes")
}
