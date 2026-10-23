// Package hosting is CommitTree's view of the code host behind a repository:
// Bitbucket Cloud and GitHub. This file holds the host-neutral types, the
// remote URL parsing and the Client interface the two host clients implement
// (docs/superpowers/specs/2026-10-23-hosting-integration-design.md).
package hosting

import (
	"context"
	"net/url"
	"sort"
	"strings"
	"time"

	"git-ui/internal/ops"
)

// Host is a supported code host.
type Host string

const (
	Bitbucket Host = "bitbucket"
	GitHub    Host = "github"
)

// Repo is a repository on a host, found through one of the git remotes.
type Repo struct {
	Host   Host   `json:"host"`
	Owner  string `json:"owner"`
	Name   string `json:"name"`
	Remote string `json:"remote"` // git remote name
}

// FullName is "owner/name".
func (r Repo) FullName() string { return r.Owner + "/" + r.Name }

// WebURL is the repository's page in the browser.
func (r Repo) WebURL() string {
	switch r.Host {
	case Bitbucket:
		return "https://bitbucket.org/" + r.FullName()
	case GitHub:
		return "https://github.com/" + r.FullName()
	}
	return ""
}

// PullsURL is the repository's pull request list in the browser.
func (r Repo) PullsURL() string {
	switch r.Host {
	case Bitbucket:
		return r.WebURL() + "/pull-requests"
	case GitHub:
		return r.WebURL() + "/pulls"
	}
	return ""
}

func hostOf(name string) (Host, bool) {
	switch strings.ToLower(name) {
	case "bitbucket.org":
		return Bitbucket, true
	case "github.com":
		return GitHub, true
	}
	return "", false
}

// ParseRemoteURL reads the host, owner and name out of an https://, ssh:// or
// scp-like (git@host:owner/name) remote URL. Only bitbucket.org and
// github.com are recognised; SSH aliases and other hosts are not. Credentials
// in the URL are ignored. Remote is left empty.
func ParseRemoteURL(raw string) (Repo, bool) {
	raw = strings.TrimSpace(raw)
	var host, path string
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return Repo{}, false
		}
		switch strings.ToLower(u.Scheme) {
		case "https", "http", "ssh", "git":
		default:
			return Repo{}, false
		}
		host, path = u.Hostname(), u.Path
	} else {
		// scp-like: [user@]host:path
		at := strings.LastIndex(raw, "@")
		rest := raw[at+1:]
		h, p, ok := strings.Cut(rest, ":")
		if !ok || h == "" || strings.Contains(h, "/") {
			return Repo{}, false
		}
		host, path = h, p
	}
	h, ok := hostOf(host)
	if !ok {
		return Repo{}, false
	}
	path = strings.Trim(path, "/")
	path = strings.TrimSuffix(path, ".git")
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return Repo{}, false
	}
	return Repo{Host: h, Owner: parts[0], Name: parts[1]}, true
}

// Detect picks the hosted repository behind the remotes: origin when it is on
// a supported host, otherwise the first supported remote in name order. Repo.
// Remote is the remote it came from. The fetch URL is read, falling back to
// the push URL.
func Detect(remotes []ops.Remote) (Repo, bool) {
	sorted := append([]ops.Remote(nil), remotes...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i].Name, sorted[j].Name
		if (a == "origin") != (b == "origin") {
			return a == "origin"
		}
		return a < b
	})
	for _, rm := range sorted {
		for _, u := range []string{rm.FetchURL, rm.PushURL} {
			if r, ok := ParseRemoteURL(u); ok {
				r.Remote = rm.Name
				return r, true
			}
		}
	}
	return Repo{}, false
}

// Filter is which pull requests a list shows.
type Filter string

const (
	FilterOpen   Filter = "open"
	FilterMine   Filter = "mine"
	FilterReview Filter = "review"
)

// Account is the signed-in user on a host. ID is the Bitbucket uuid or the
// GitHub login.
type Account struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Login string `json:"login"`
}

// PullRequest is one row of the pull request list.
type PullRequest struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Draft     bool      `json:"draft"`
	Author    string    `json:"author"`
	AuthorID  string    `json:"authorID"`
	Source    string    `json:"source"`
	Target    string    `json:"target"`
	HeadHash  string    `json:"headHash"`
	URL       string    `json:"url"`
	Fork      bool      `json:"fork"`
	Approvals int       `json:"approvals"`
	Comments  int       `json:"comments"`
	Updated   time.Time `json:"updated"`
	State     State     `json:"state"` // head commit's aggregate; "" when unknown
}

// Reviewer is one reviewer and where they stand. Verdict is "approved",
// "changes" or "pending".
type Reviewer struct {
	Name    string `json:"name"`
	Verdict string `json:"verdict"`
}

// Comment is one comment of a pull request, general or on a file line.
type Comment struct {
	ID      string    `json:"id"`
	Parent  string    `json:"parent"`
	Author  string    `json:"author"`
	Created time.Time `json:"created"`
	Body    string    `json:"body"`
	Path    string    `json:"path"`
	Line    int       `json:"line"`
	Verdict string    `json:"verdict"`
}

// PRCommit is one commit of a pull request.
type PRCommit struct {
	Hash    string `json:"hash"`
	Short   string `json:"short"`
	Subject string `json:"subject"`
	Author  string `json:"author"`
}

// PullRequestDetail is a pull request opened on its own.
type PullRequestDetail struct {
	PullRequest
	Description  string       `json:"description"`
	Created      time.Time    `json:"created"`
	Reviewers    []Reviewer   `json:"reviewers"`
	Commits      []PRCommit   `json:"commits"`
	Comments     []Comment    `json:"comments"`
	CommentsMore bool         `json:"commentsMore"`
	Checks       CommitStatus `json:"checks"`
}

// PRFile is one changed file of a pull request. NoDiff is set when the host
// gave no patch for it.
type PRFile struct {
	Path    string `json:"path"`
	OldPath string `json:"oldPath"`
	Status  string `json:"status"`
	Patch   string `json:"patch"`
	NoDiff  bool   `json:"noDiff"`
}

// Count is a number that may be a lower bound: More is set when there are
// more than N.
type Count struct {
	N    int  `json:"n"`
	More bool `json:"more"`
}

// Client is what a host offers CommitTree. The Bitbucket and GitHub clients
// implement it.
type Client interface {
	User(ctx context.Context) (Account, error)
	Statuses(ctx context.Context, r Repo, hashes []string) (map[string]CommitStatus, error)
	Checks(ctx context.Context, r Repo, hash string) (CommitStatus, error)
	PullRequests(ctx context.Context, r Repo, f Filter, me Account) (prs []PullRequest, more bool, err error)
	PullRequestCount(ctx context.Context, r Repo) (Count, error)
	PullRequest(ctx context.Context, r Repo, number int) (PullRequestDetail, error)
	PullRequestFiles(ctx context.Context, r Repo, number int) ([]PRFile, error)
}
