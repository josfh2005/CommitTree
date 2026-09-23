// Package worktrees reads a repository's git worktrees.
package worktrees

import (
	"context"
	"strings"

	"git-ui/internal/gitcmd"
)

// Worktree is one entry of `git worktree list`.
type Worktree struct {
	Path     string `json:"path"`
	Head     string `json:"head"`
	Branch   string `json:"branch"` // short name; "" when detached
	Detached bool   `json:"detached"`
	Prunable bool   `json:"prunable"`
	Bare     bool   `json:"bare"`
	Main     bool   `json:"main"` // the first record: the main working tree
}

// List runs `git worktree list --porcelain -z` in dir. Any of a
// repository's working trees lists all of them, main tree first.
func List(ctx context.Context, dir string) ([]Worktree, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}
	return parse(out), nil
}

// parse reads -z porcelain: every field ends with NUL and a record ends with
// an empty field, so paths with spaces or newlines survive. Keys: worktree,
// HEAD, branch refs/heads/<x>, detached, bare, prunable [reason],
// locked [reason]. The trailing empty fields may already be trimmed.
func parse(out string) []Worktree {
	var list []Worktree
	var cur *Worktree
	for _, field := range strings.Split(out, "\x00") {
		if field == "" {
			if cur != nil {
				list = append(list, *cur)
				cur = nil
			}
			continue
		}
		key, value, _ := strings.Cut(field, " ")
		if key == "worktree" {
			cur = &Worktree{Path: value, Main: len(list) == 0}
			continue
		}
		if cur == nil {
			continue
		}
		switch key {
		case "HEAD":
			cur.Head = value
		case "branch":
			cur.Branch = strings.TrimPrefix(value, "refs/heads/")
		case "detached":
			cur.Detached = true
		case "bare":
			cur.Bare = true
		case "prunable":
			cur.Prunable = true
		}
	}
	if cur != nil {
		list = append(list, *cur)
	}
	return list
}
