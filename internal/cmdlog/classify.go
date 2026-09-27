// Package cmdlog keeps the log of git commands the app ran, per repository,
// for the Commands panel.
package cmdlog

import (
	"context"
	"strings"
)

// Origin is who asked for a command.
type Origin string

const (
	OriginYou  Origin = "you"
	OriginAI   Origin = "ai"
	OriginAuto Origin = "auto"
)

// Kind tells a command that only reads the repository from one that may
// change it (or talk to a remote).
type Kind string

const (
	KindRead  Kind = "read"
	KindWrite Kind = "write"
)

type originKey struct{}

// WithOrigin marks every git command run with ctx as asked for by o.
func WithOrigin(ctx context.Context, o Origin) context.Context {
	return context.WithValue(ctx, originKey{}, o)
}

// OriginFrom is the origin WithOrigin put in ctx, if any.
func OriginFrom(ctx context.Context) (Origin, bool) {
	if ctx == nil {
		return "", false
	}
	o, ok := ctx.Value(originKey{}).(Origin)
	return o, ok
}

// alwaysRead are subcommands that never change the repository.
var alwaysRead = map[string]bool{
	"log": true, "show": true, "status": true, "diff": true, "rev-parse": true,
	"rev-list": true, "for-each-ref": true, "show-ref": true, "cat-file": true,
	"blame": true, "ls-files": true, "ls-tree": true, "merge-base": true,
	"name-rev": true, "describe": true, "diff-tree": true, "diff-index": true,
	"diff-files": true, "check-ignore": true, "check-attr": true, "var": true,
	"version": true, "shortlog": true, "grep": true, "check-ref-format": true,
}

// Classify says whether args (git's arguments after -C dir) only read.
// Anything not known to be a read is a write, so it is always shown: a
// wrong "write" only makes the panel noisier, a wrong "read" would hide a
// change.
func Classify(args []string) Kind {
	sub, rest := subcommand(args)
	pos := positional(rest)
	read := false
	switch {
	case alwaysRead[sub]:
		read = true
	case sub == "symbolic-ref":
		read = len(pos) <= 1
	case sub == "worktree":
		read = len(pos) > 0 && pos[0] == "list"
	case sub == "submodule":
		read = len(pos) == 0 || pos[0] == "status"
	case sub == "stash":
		read = len(pos) > 0 && (pos[0] == "list" || pos[0] == "show")
	case sub == "reflog":
		read = len(pos) == 0 || pos[0] == "show"
	case sub == "remote":
		read = len(pos) == 0 || pos[0] == "get-url"
	case sub == "config":
		read = has(rest, "--get", "--get-all", "--get-regexp", "--get-urlmatch", "--list", "-l") ||
			(len(pos) > 0 && (pos[0] == "get" || pos[0] == "list"))
	case sub == "branch":
		read = has(rest, "--list", "-l") || (len(pos) == 0 && !has(rest,
			"-d", "-D", "--delete", "-m", "-M", "--move", "-c", "-C", "--copy",
			"-u", "--unset-upstream", "--edit-description", "-f", "--force") && !prefixed(rest, "--set-upstream-to"))
	case sub == "tag":
		read = has(rest, "--list", "-l") || (len(pos) == 0 && !has(rest, "-d", "--delete"))
	}
	if read {
		return KindRead
	}
	return KindWrite
}

// subcommand skips git's global options (-c key=value, --no-pager, …) and
// returns the subcommand and what follows it.
func subcommand(args []string) (string, []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-c" {
			i++
			continue
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		return a, args[i+1:]
	}
	return "", nil
}

// positional is rest without its options.
func positional(rest []string) []string {
	var out []string
	for _, a := range rest {
		if !strings.HasPrefix(a, "-") {
			out = append(out, a)
		}
	}
	return out
}

func has(rest []string, flags ...string) bool {
	for _, a := range rest {
		for _, f := range flags {
			if a == f {
				return true
			}
		}
	}
	return false
}

func prefixed(rest []string, prefix string) bool {
	for _, a := range rest {
		if strings.HasPrefix(a, prefix) {
			return true
		}
	}
	return false
}
