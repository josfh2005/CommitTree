// Package gitlog reads commit history.
package gitlog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"git-ui/internal/gitcmd"
)

// Format is the git log --format string: NUL-separated fields, each record
// terminated by an ASCII record separator.
const Format = "%H%x00%h%x00%P%x00%an%x00%ae%x00%aI%x00%D%x00%s%x1e"

type RefKind string

const (
	RefHead   RefKind = "head"
	RefLocal  RefKind = "local"
	RefRemote RefKind = "remote"
	RefTag    RefKind = "tag"
)

type Ref struct {
	Name string  `json:"name"`
	Kind RefKind `json:"kind"`
}

type Commit struct {
	Hash    string    `json:"hash"`
	Short   string    `json:"short"`
	Parents []string  `json:"parents"`
	Author  string    `json:"author"`
	Email   string    `json:"email"`
	Date    time.Time `json:"date"`
	Subject string    `json:"subject"`
	Refs    []Ref     `json:"refs"`
}

func (c Commit) IsHead() bool {
	for _, r := range c.Refs {
		if r.Kind == RefHead {
			return true
		}
	}
	return false
}

type Filters struct {
	Text   string   `json:"text"`
	Branch string   `json:"branch"`
	Author string   `json:"author"`
	Since  string   `json:"since"`
	Until  string   `json:"until"`
	Paths  []string `json:"paths"`
}

// GraphVisible reports whether a graph can be drawn. Text, author and date
// filters drop commits without rewriting parents, which would leave lines
// that never end.
func (f Filters) GraphVisible() bool {
	return f.Text == "" && f.Author == "" && f.Since == "" && f.Until == ""
}

// Order selects how commits are walked. OrderTopo (the default) keeps each
// line of development together, so a merged branch's own history is not
// interleaved with the trunk by date; OrderDate is a strict walk by commit
// date. Callers pass this explicitly rather than the package reading any
// setting itself — the same pattern ops.Pull follows for its strategy.
const (
	OrderTopo = "topo"
	OrderDate = "date"
)

func Args(f Filters, order string, skip, limit int) []string {
	orderFlag := "--topo-order"
	if order == OrderDate {
		orderFlag = "--date-order"
	}
	args := []string{"log", orderFlag, "--parents", "--decorate=full", "--format=" + Format,
		fmt.Sprintf("--skip=%d", skip), fmt.Sprintf("-n%d", limit)}
	if f.Author != "" {
		args = append(args, "--author="+f.Author)
	}
	if f.Text != "" {
		args = append(args, "--grep="+f.Text)
	}
	if f.Author != "" || f.Text != "" {
		args = append(args, "-i", "--fixed-strings")
	}
	if f.Since != "" {
		args = append(args, "--since="+f.Since)
	}
	if f.Until != "" {
		args = append(args, "--until="+f.Until)
	}
	if f.Branch == "" {
		args = append(args, "--all")
	} else {
		args = append(args, "--end-of-options", f.Branch)
	}
	if len(f.Paths) > 0 {
		args = append(args, "--")
		args = append(args, f.Paths...)
	}
	return args
}

func Parse(out string) ([]Commit, error) {
	commits := []Commit{}
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		f := strings.Split(rec, "\x00")
		if len(f) != 8 {
			return nil, fmt.Errorf("gitlog: record has %d fields, want 8", len(f))
		}
		date, err := time.Parse(time.RFC3339, f[5])
		if err != nil {
			return nil, fmt.Errorf("gitlog: bad date %q: %w", f[5], err)
		}
		parents := strings.Fields(f[2])
		if parents == nil {
			parents = []string{}
		}
		commits = append(commits, Commit{
			Hash: f[0], Short: f[1], Parents: parents, Author: f[3], Email: f[4],
			Date: date, Subject: f[7], Refs: parseRefs(f[6]),
		})
	}
	return commits, nil
}

func parseRefs(decoration string) []Ref {
	refs := []Ref{}
	for _, part := range strings.Split(decoration, ", ") {
		if part == "" {
			continue
		}
		if part == "HEAD" {
			refs = append(refs, Ref{Name: "HEAD", Kind: RefHead})
			continue
		}
		if rest, ok := strings.CutPrefix(part, "HEAD -> "); ok {
			refs = append(refs, Ref{Name: "HEAD", Kind: RefHead})
			part = rest
		}
		part = strings.TrimPrefix(part, "tag: ")
		switch {
		case strings.HasPrefix(part, "refs/heads/"):
			refs = append(refs, Ref{Name: strings.TrimPrefix(part, "refs/heads/"), Kind: RefLocal})
		case strings.HasPrefix(part, "refs/tags/"):
			refs = append(refs, Ref{Name: strings.TrimPrefix(part, "refs/tags/"), Kind: RefTag})
		case strings.HasPrefix(part, "refs/remotes/"):
			name := strings.TrimPrefix(part, "refs/remotes/")
			if !strings.HasSuffix(name, "/HEAD") {
				refs = append(refs, Ref{Name: name, Kind: RefRemote})
			}
		}
	}
	return refs
}

func Get(ctx context.Context, dir string, f Filters, order string, skip, limit int) ([]Commit, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, Args(f, order, skip, limit)...)
	if err != nil {
		return nil, err
	}
	return Parse(out)
}

func ResolveCommit(ctx context.Context, dir, rev string) (string, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout,
		"rev-parse", "--verify", "--quiet", "--end-of-options", rev+"^{commit}")
	var gerr *gitcmd.Error
	if errors.As(err, &gerr) && gerr.ExitCode == 1 {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func IsShallow(ctx context.Context, dir string) (bool, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--is-shallow-repository")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "true", nil
}

func Authors(ctx context.Context, dir string) ([]string, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "shortlog", "-sn", "--all")
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, line := range strings.Split(out, "\n") {
		if _, name, ok := strings.Cut(strings.TrimSpace(line), "\t"); ok {
			names = append(names, name)
		}
	}
	return names, nil
}
