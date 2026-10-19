package refs

import (
	"context"
	"strings"

	"git-ui/internal/gitcmd"
)

// Tracking is what TrackTwin did for a local branch. Twin is its remote
// branch of the same name ("origin/x"), "" when there is none; Tracked says
// whether the branch now tracks it — false with a Twin means the two have
// diverged, so the upstream was left unset.
type Tracking struct {
	Twin    string `json:"twin"`
	Tracked bool   `json:"tracked"`
}

// Twin is the remote-tracking branch a local branch called name naturally
// tracks: origin's when origin has one, else the only remote's that has
// one; "" when no remote has it, or several do and none is origin.
func Twin(ctx context.Context, dir, name string) string {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "remote")
	if err != nil {
		return ""
	}
	var found []string
	for _, remote := range strings.Fields(out) {
		if !refExists(ctx, dir, "refs/remotes/"+remote+"/"+name) {
			continue
		}
		if remote == "origin" {
			return "origin/" + name
		}
		found = append(found, remote+"/"+name)
	}
	if len(found) == 1 {
		return found[0]
	}
	return ""
}

// TrackTwin sets a local branch's upstream to its Twin, unless it already
// has an upstream, has no twin, or has diverged from it (each side has
// commits the other lacks): tracking is set only when one contains the
// other, so a pull fast-forwards or a push is accepted.
func TrackTwin(ctx context.Context, dir, branch string) (Tracking, error) {
	if hasUpstream(ctx, dir, branch) {
		return Tracking{}, nil
	}
	twin := Twin(ctx, dir, branch)
	if twin == "" {
		return Tracking{}, nil
	}
	local, remote := "refs/heads/"+branch, "refs/remotes/"+twin
	if !isAncestor(ctx, dir, local, remote) && !isAncestor(ctx, dir, remote, local) {
		return Tracking{Twin: twin}, nil
	}
	if err := SetUpstream(ctx, dir, branch, twin); err != nil {
		return Tracking{}, err
	}
	return Tracking{Twin: twin, Tracked: true}, nil
}

// SetUpstream makes branch track the remote-tracking branch upstream
// ("origin/x").
func SetUpstream(ctx context.Context, dir, branch, upstream string) error {
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "branch", "--set-upstream-to=refs/remotes/"+upstream, "--", branch)
	return err
}

// hasUpstream reports whether branch has an upstream configured, whether
// or not that upstream still exists.
func hasUpstream(ctx context.Context, dir, branch string) bool {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "for-each-ref", "--format=%(upstream)", "refs/heads/"+branch)
	return err == nil && strings.TrimSpace(out) != ""
}

func refExists(ctx context.Context, dir, ref string) bool {
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", ref)
	return err == nil
}

func isAncestor(ctx context.Context, dir, a, b string) bool {
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "merge-base", "--is-ancestor", a, b)
	return err == nil
}
