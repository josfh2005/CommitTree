// Package refs lists and edits branches and tags.
package refs

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"git-ui/internal/gitcmd"
)

type Branch struct {
	Name     string `json:"name"`
	Remote   string `json:"remote"`
	Hash     string `json:"hash"`
	Current  bool   `json:"current"`
	Upstream string `json:"upstream"`
	// Ahead and Behind compare a local branch with its upstream as the last
	// fetch left it (%(upstream:track)); zero without an upstream.
	Ahead  int `json:"ahead,omitempty"`
	Behind int `json:"behind,omitempty"`
	// UpstreamGone: the upstream is configured but its remote branch no
	// longer exists. UpstreamLocal: the upstream is another local branch.
	UpstreamGone  bool `json:"upstreamGone,omitempty"`
	UpstreamLocal bool `json:"upstreamLocal,omitempty"`
	// Official: a local branch the "Push main branches" scope pushes —
	// main, master, develop and release/*, or the git-flow names
	// (OfficialRule). Never set on remote-tracking branches.
	Official bool `json:"official"`
	// Worktree is the path of another worktree that has this local branch
	// checked out, or "" (filled in by the app, not by List).
	Worktree string `json:"worktree,omitempty"`
	// WorktreeGone: that worktree's directory no longer exists, but git
	// still counts the branch as checked out there until it is pruned.
	WorktreeGone bool `json:"worktreeGone,omitempty"`
}

type Remote struct {
	Name     string   `json:"name"`
	Branches []Branch `json:"branches"`
}

type Tag struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
}

type Refs struct {
	Head     string   `json:"head"`
	HeadHash string   `json:"headHash"`
	Detached bool     `json:"detached"`
	Local    []Branch `json:"local"`
	Remotes  []Remote `json:"remotes"`
	Tags     []Tag    `json:"tags"`
}

const refFormat = "%(refname)%00%(objectname)%00%(*objectname)%00%(HEAD)%00%(upstream:short)%00%(upstream:track,nobracket)%00%(upstream:remotename)"

func List(ctx context.Context, dir string) (Refs, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout,
		"for-each-ref", "--format="+refFormat, "refs/heads", "refs/remotes", "refs/tags")
	if err != nil {
		return Refs{}, err
	}
	remoteOut, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "remote")
	if err != nil {
		return Refs{}, err
	}

	rule, err := ReadOfficialRule(ctx, dir)
	if err != nil {
		return Refs{}, err
	}

	r := Refs{Local: []Branch{}, Remotes: []Remote{}, Tags: []Tag{}}
	remoteNames := strings.Fields(remoteOut)
	for _, name := range remoteNames {
		r.Remotes = append(r.Remotes, Remote{Name: name, Branches: []Branch{}})
	}

	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Split(line, "\x00")
		if len(f) != 7 {
			continue
		}
		ref, hash, peeled, head, upstream, track, upstreamRemote := f[0], f[1], f[2], f[3], f[4], f[5], f[6]
		switch {
		case strings.HasPrefix(ref, "refs/heads/"):
			ahead, behind, gone := ParseTrack(track)
			r.Local = append(r.Local, Branch{Name: strings.TrimPrefix(ref, "refs/heads/"),
				Hash: hash, Current: head == "*", Upstream: upstream,
				Ahead: ahead, Behind: behind, UpstreamGone: gone,
				UpstreamLocal: upstream != "" && upstreamRemote == ".",
				Official:      rule.IsOfficial(strings.TrimPrefix(ref, "refs/heads/"))})
		case strings.HasPrefix(ref, "refs/tags/"):
			if peeled != "" {
				hash = peeled
			}
			r.Tags = append(r.Tags, Tag{Name: strings.TrimPrefix(ref, "refs/tags/"), Hash: hash})
		case strings.HasPrefix(ref, "refs/remotes/"):
			i, name := splitRemote(strings.TrimPrefix(ref, "refs/remotes/"), remoteNames)
			if i < 0 || name == "HEAD" {
				continue
			}
			r.Remotes[i].Branches = append(r.Remotes[i].Branches,
				Branch{Name: name, Remote: remoteNames[i], Hash: hash})
		}
	}

	head, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "symbolic-ref", "-q", "--short", "HEAD")
	var gerr *gitcmd.Error
	switch {
	case err == nil:
		r.Head = strings.TrimSpace(head)
	case errors.As(err, &gerr) && gerr.ExitCode == 1:
		r.Detached = true
	default:
		return Refs{}, err
	}
	if hash, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", "HEAD"); err == nil {
		r.HeadHash = strings.TrimSpace(hash)
	}
	return r, nil
}

// splitRemote finds the remote a remote-tracking ref belongs to, preferring
// the longest matching remote name.
func splitRemote(rest string, remotes []string) (int, string) {
	best, name := -1, ""
	for i, remote := range remotes {
		if strings.HasPrefix(rest, remote+"/") && (best < 0 || len(remote) > len(remotes[best])) {
			best, name = i, strings.TrimPrefix(rest, remote+"/")
		}
	}
	return best, name
}

// ParseTrack reads %(upstream:track,nobracket): "ahead 2", "behind 1",
// "ahead 2, behind 1", "gone" or "" (up to date, or no upstream).
func ParseTrack(s string) (ahead, behind int, gone bool) {
	if s == "gone" {
		return 0, 0, true
	}
	for _, part := range strings.Split(s, ", ") {
		var n int
		if _, err := fmt.Sscanf(part, "ahead %d", &n); err == nil {
			ahead = n
		} else if _, err := fmt.Sscanf(part, "behind %d", &n); err == nil {
			behind = n
		}
	}
	return ahead, behind, false
}

func CurrentLabel(ctx context.Context, dir string) string {
	if out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "symbolic-ref", "-q", "--short", "HEAD"); err == nil {
		return strings.TrimSpace(out)
	}
	if out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--short", "HEAD"); err == nil {
		return strings.TrimSpace(out)
	}
	return ""
}

func Fingerprint(ctx context.Context, dir string) (string, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "for-each-ref", "--format=%(refname) %(objectname)")
	if err != nil {
		return "", err
	}
	head, _ := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "symbolic-ref", "-q", "HEAD")
	hash, _ := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", "HEAD")
	sum := sha1.Sum([]byte(out + "\x00" + head + "\x00" + hash))
	return hex.EncodeToString(sum[:]), nil
}
