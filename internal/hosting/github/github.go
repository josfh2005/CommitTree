// Package github is the GitHub client of the hosting integration: commit
// check statuses and the pull request list over GraphQL, a pull request's
// detail and files over REST (docs/superpowers/specs/2026-10-23-hosting-
// integration-design.md, §3 and §5). It talks to the API only through a
// hosting.Doer, which keeps the token on the API host.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"git-ui/internal/hosting"
)

const (
	statusBatch  = 50  // commits per GraphQL statuses query
	listCap      = 100 // pull requests per list
	commentCap   = 500
	filePages    = 30 // pages of 100 pull request files
	listPages    = 10 // pages of 100 for reviews and commits
	pageSize     = 100
	maxGQLDetail = 300
	apiVersion   = "2022-11-28"
	acceptHeader = "application/vnd.github+json"
	httpTimeout  = 20 * time.Second
)

type client struct{ d *hosting.Doer }

// New returns the GitHub client over d, whose BaseURL is
// https://api.github.com. Every request carries GitHub's Accept and API
// version headers; d itself is not modified.
func New(d *hosting.Doer) hosting.Client {
	dd := *d
	hc := &http.Client{Timeout: httpTimeout}
	if d.HTTP != nil {
		c := *d.HTTP
		hc = &c
	}
	hc.Transport = headerTransport{base: hc.Transport}
	dd.HTTP = hc
	return &client{d: &dd}
}

var _ hosting.Client = (*client)(nil)

// headerTransport sets GitHub's headers on every request, redirects included.
// The Doer sets a generic Accept before the request reaches it.
type headerTransport struct{ base http.RoundTripper }

func (t headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.Header.Set("Accept", acceptHeader)
	r.Header.Set("X-GitHub-Api-Version", apiVersion)
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(r)
}

// ---- helpers ----------------------------------------------------------

func repoPath(r hosting.Repo) string {
	return "/repos/" + url.PathEscape(r.Owner) + "/" + url.PathEscape(r.Name)
}

// access names the repository in a "no access" error, which the Doer leaves
// empty. Other errors pass through.
func access(r hosting.Repo, err error) error {
	var na *hosting.NoAccessError
	if errors.As(err, &na) && na.Repo == "" {
		na.Repo = r.FullName()
	}
	return err
}

// getAll reads a paginated REST list page by page (page=N, never a Link
// header) until a short page, maxItems items or maxPages pages.
func getAll[T any](ctx context.Context, c *client, r hosting.Repo, path string, maxItems, maxPages int) ([]T, error) {
	var out []T
	for n := 1; n <= maxPages; n++ {
		var pg []T
		target := path + "?" + url.Values{"per_page": {strconv.Itoa(pageSize)}, "page": {strconv.Itoa(n)}}.Encode()
		if _, err := c.d.JSON(ctx, "GET", target, nil, &pg); err != nil {
			return nil, access(r, err)
		}
		out = append(out, pg...)
		if len(pg) < pageSize || len(out) >= maxItems {
			break
		}
	}
	return out, nil
}

type wireLogin struct {
	Login string `json:"login"`
}

func loginOf(u *wireLogin) string {
	if u == nil || u.Login == "" {
		return "ghost" // a deleted account
	}
	return u.Login
}

// isOID reports whether s is a full commit id: 40 (SHA-1) or 64 (SHA-256)
// hex digits. Only these are ever put into a GraphQL query string.
func isOID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

// ---- GraphQL ----------------------------------------------------------

type gqlError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Path    []any  `json:"path"`
}

func (e gqlError) onRepository() bool {
	return len(e.Path) == 0 || (len(e.Path) == 1 && e.Path[0] == "repository")
}

// graphql posts a query for r (owner and name travel as variables) and
// decodes its data into out. A 200 answer with an errors array is mapped:
// NOT_FOUND on the repository is a *hosting.NoAccessError, RATE_LIMITED a
// *hosting.RateLimitError, anything else a plain error with the first message.
func (c *client) graphql(ctx context.Context, r hosting.Repo, query string, out any) error {
	body := map[string]any{
		"query":     query,
		"variables": map[string]string{"owner": r.Owner, "name": r.Name},
	}
	var resp struct {
		Data   json.RawMessage `json:"data"`
		Errors []gqlError      `json:"errors"`
	}
	if _, err := c.d.JSON(ctx, "POST", "/graphql", body, &resp); err != nil {
		return access(r, err)
	}
	if len(resp.Errors) > 0 {
		for _, e := range resp.Errors {
			if e.Type == "NOT_FOUND" && e.onRepository() {
				return &hosting.NoAccessError{Repo: r.FullName()}
			}
		}
		for _, e := range resp.Errors {
			if e.Type == "RATE_LIMITED" {
				if c.d.Limits != nil {
					if rl, ok := c.d.Limits.Blocked(hosting.GitHub); ok {
						return &rl
					}
				}
				return &hosting.RateLimitError{Host: hosting.GitHub}
			}
		}
		m := strings.TrimSpace(resp.Errors[0].Message)
		if len(m) > maxGQLDetail {
			m = m[:maxGQLDetail]
		}
		if m == "" {
			m = "the query failed"
		}
		return fmt.Errorf("GitHub: %s", m)
	}
	if len(resp.Data) == 0 || string(resp.Data) == "null" {
		return errors.New("GitHub: empty answer")
	}
	if err := json.Unmarshal(resp.Data, out); err != nil {
		return fmt.Errorf("GitHub: unreadable response: %w", err)
	}
	return nil
}

// ---- User -------------------------------------------------------------

func (c *client) User(ctx context.Context) (hosting.Account, error) {
	var u struct {
		Login string `json:"login"`
		Name  string `json:"name"`
	}
	if _, err := c.d.JSON(ctx, "GET", "/user", nil, &u); err != nil {
		return hosting.Account{}, err
	}
	name := u.Name
	if name == "" {
		name = u.Login
	}
	return hosting.Account{ID: u.Login, Login: u.Login, Name: name}, nil
}

// ---- Statuses and Checks ----------------------------------------------

type gqlContext struct {
	Typename    string    `json:"__typename"`
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	Conclusion  string    `json:"conclusion"`
	DetailsURL  string    `json:"detailsUrl"`
	StartedAt   time.Time `json:"startedAt"`
	CompletedAt time.Time `json:"completedAt"`
	Context     string    `json:"context"`
	State       string    `json:"state"`
	TargetURL   string    `json:"targetUrl"`
	CreatedAt   time.Time `json:"createdAt"`
}

type gqlCommit struct {
	StatusCheckRollup *struct {
		Contexts struct {
			Nodes []gqlContext `json:"nodes"`
		} `json:"contexts"`
	} `json:"statusCheckRollup"`
}

// toCheck maps a check run or a status context. ok is false for a node or a
// state this client does not know.
func (n gqlContext) toCheck() (hosting.Check, bool) {
	switch n.Typename {
	case "CheckRun":
		var st hosting.State
		switch {
		case n.Status != "COMPLETED":
			st = hosting.Running
		default:
			switch n.Conclusion {
			case "SUCCESS", "NEUTRAL", "SKIPPED":
				st = hosting.Success
			case "FAILURE", "TIMED_OUT", "STARTUP_FAILURE", "ACTION_REQUIRED":
				st = hosting.Failed
			case "CANCELLED", "STALE":
				st = hosting.Stopped
			default:
				return hosting.Check{}, false
			}
		}
		c := hosting.Check{Key: n.Name, Name: n.Name, State: st, URL: n.DetailsURL, Started: n.StartedAt, Created: n.StartedAt}
		if !n.StartedAt.IsZero() && !n.CompletedAt.IsZero() && n.CompletedAt.After(n.StartedAt) {
			c.DurationSec = int(n.CompletedAt.Sub(n.StartedAt) / time.Second)
		}
		return c, true
	case "StatusContext":
		var st hosting.State
		switch n.State {
		case "SUCCESS":
			st = hosting.Success
		case "FAILURE", "ERROR":
			st = hosting.Failed
		case "PENDING", "EXPECTED":
			st = hosting.Running
		default:
			return hosting.Check{}, false
		}
		return hosting.Check{Key: n.Context, Name: n.Context, State: st, URL: n.TargetURL, Started: n.CreatedAt, Created: n.CreatedAt}, true
	}
	return hosting.Check{}, false
}

func summarize(hash string, checks []hosting.Check) hosting.CommitStatus {
	if len(checks) == 0 {
		return hosting.CommitStatus{Hash: hash, State: hosting.None}
	}
	return hosting.CommitStatus{Hash: hash, State: hosting.Aggregate(checks), Checks: hosting.Latest(checks)}
}

const contextsSelection = `statusCheckRollup { contexts(first: 100) { nodes { __typename ` +
	`... on CheckRun { name status conclusion detailsUrl startedAt completedAt } ` +
	`... on StatusContext { context state targetUrl createdAt } } } }`

func statusesQuery(oids []string) string {
	var b strings.Builder
	b.WriteString(`query($owner: String!, $name: String!) { repository(owner: $owner, name: $name) { `)
	for i, oid := range oids {
		fmt.Fprintf(&b, `c%d: object(oid: %q) { ... on Commit { %s } } `, i, oid, contextsSelection)
	}
	b.WriteString(`} }`)
	return b.String()
}

// Statuses asks for the check runs and status contexts of the commits, 50 per
// GraphQL query. The aggregate is computed over the latest run of each check.
// A commit GitHub does not know, one without checks, and anything that is not
// a full commit id, are none.
func (c *client) Statuses(ctx context.Context, r hosting.Repo, hashes []string) (map[string]hosting.CommitStatus, error) {
	out := make(map[string]hosting.CommitStatus, len(hashes))
	var valid []string // distinct lower-case ids, in request order
	seen := map[string]bool{}
	for _, h := range hashes {
		if h == "" {
			continue
		}
		out[h] = hosting.CommitStatus{Hash: h, State: hosting.None}
		if lh := strings.ToLower(h); isOID(h) && !seen[lh] {
			seen[lh] = true
			valid = append(valid, lh)
		}
	}
	byOID := map[string]hosting.CommitStatus{}
	for start := 0; start < len(valid); start += statusBatch {
		batch := valid[start:min(start+statusBatch, len(valid))]
		var data struct {
			Repository map[string]*gqlCommit `json:"repository"`
		}
		if err := c.graphql(ctx, r, statusesQuery(batch), &data); err != nil {
			return nil, err
		}
		if data.Repository == nil {
			return nil, &hosting.NoAccessError{Repo: r.FullName()}
		}
		for i, oid := range batch {
			obj := data.Repository["c"+strconv.Itoa(i)]
			if obj == nil || obj.StatusCheckRollup == nil {
				continue
			}
			var checks []hosting.Check
			for _, n := range obj.StatusCheckRollup.Contexts.Nodes {
				if ck, ok := n.toCheck(); ok {
					checks = append(checks, ck)
				}
			}
			byOID[oid] = summarize(oid, checks)
		}
	}
	for h := range out {
		if st, ok := byOID[strings.ToLower(h)]; ok {
			st.Hash = h
			out[h] = st
		}
	}
	return out, nil
}

func (c *client) Checks(ctx context.Context, r hosting.Repo, hash string) (hosting.CommitStatus, error) {
	m, err := c.Statuses(ctx, r, []string{hash})
	if err != nil {
		return hosting.CommitStatus{}, err
	}
	if st, ok := m[hash]; ok {
		return st, nil
	}
	return hosting.CommitStatus{Hash: hash, State: hosting.None}, nil
}

// ---- Pull request list ------------------------------------------------

type gqlPR struct {
	Number            int        `json:"number"`
	Title             string     `json:"title"`
	IsDraft           bool       `json:"isDraft"`
	URL               string     `json:"url"`
	UpdatedAt         time.Time  `json:"updatedAt"`
	Author            *wireLogin `json:"author"`
	HeadRefName       string     `json:"headRefName"`
	BaseRefName       string     `json:"baseRefName"`
	HeadRefOid        string     `json:"headRefOid"`
	IsCrossRepository bool       `json:"isCrossRepository"`
	HeadRepository    *struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"headRepository"`
	Comments struct {
		TotalCount int `json:"totalCount"`
	} `json:"comments"`
	LatestReviews struct {
		Nodes []struct {
			Author *wireLogin `json:"author"`
			State  string     `json:"state"`
		} `json:"nodes"`
	} `json:"latestReviews"`
	ReviewRequests struct {
		Nodes []struct {
			RequestedReviewer *wireLogin `json:"requestedReviewer"`
		} `json:"nodes"`
	} `json:"reviewRequests"`
	Commits struct {
		Nodes []struct {
			Commit struct {
				StatusCheckRollup *struct {
					State string `json:"state"`
				} `json:"statusCheckRollup"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
}

const pullRequestsQuery = `query($owner: String!, $name: String!) { repository(owner: $owner, name: $name) { ` +
	`pullRequests(states: OPEN, first: 100, orderBy: {field: UPDATED_AT, direction: DESC}) { totalCount nodes { ` +
	`number title isDraft url updatedAt author { login } headRefName baseRefName headRefOid isCrossRepository ` +
	`headRepository { nameWithOwner } comments { totalCount } ` +
	`latestReviews(first: 50) { nodes { author { login } state } } ` +
	`reviewRequests(first: 50) { nodes { requestedReviewer { ... on User { login } } } } ` +
	`commits(last: 1) { nodes { commit { statusCheckRollup { state } } } } } } } }`

const pullRequestCountQuery = `query($owner: String!, $name: String!) { repository(owner: $owner, name: $name) { ` +
	`pullRequests(states: OPEN) { totalCount } } }`

func rollupState(s string) hosting.State {
	switch s {
	case "SUCCESS":
		return hosting.Success
	case "FAILURE", "ERROR":
		return hosting.Failed
	case "PENDING", "EXPECTED":
		return hosting.Running
	}
	return ""
}

func (w gqlPR) toPR() hosting.PullRequest {
	pr := hosting.PullRequest{
		Number:   w.Number,
		Title:    w.Title,
		Draft:    w.IsDraft,
		Author:   loginOf(w.Author),
		AuthorID: loginOf(w.Author),
		Source:   w.HeadRefName,
		Target:   w.BaseRefName,
		HeadHash: w.HeadRefOid,
		URL:      w.URL,
		Fork:     w.IsCrossRepository || w.HeadRepository == nil,
		Comments: w.Comments.TotalCount,
		Updated:  w.UpdatedAt,
	}
	for _, rv := range w.LatestReviews.Nodes {
		if rv.State == "APPROVED" {
			pr.Approvals++
		}
	}
	if len(w.Commits.Nodes) > 0 && w.Commits.Nodes[0].Commit.StatusCheckRollup != nil {
		pr.State = rollupState(w.Commits.Nodes[0].Commit.StatusCheckRollup.State)
	}
	return pr
}

func (w gqlPR) reviewRequestedFrom(login string) bool {
	for _, rr := range w.ReviewRequests.Nodes {
		if rr.RequestedReviewer != nil && strings.EqualFold(rr.RequestedReviewer.Login, login) {
			return true
		}
	}
	return false
}

func accountLogin(a hosting.Account) string {
	if a.Login != "" {
		return a.Login
	}
	return a.ID
}

// PullRequests lists the repository's open pull requests, newest update
// first, up to 100. The mine and review filters are applied here. more is set
// for the unfiltered list when there are more than 100 open.
func (c *client) PullRequests(ctx context.Context, r hosting.Repo, f hosting.Filter, me hosting.Account) ([]hosting.PullRequest, bool, error) {
	var data struct {
		Repository *struct {
			PullRequests struct {
				TotalCount int     `json:"totalCount"`
				Nodes      []gqlPR `json:"nodes"`
			} `json:"pullRequests"`
		} `json:"repository"`
	}
	if err := c.graphql(ctx, r, pullRequestsQuery, &data); err != nil {
		return nil, false, err
	}
	if data.Repository == nil {
		return nil, false, &hosting.NoAccessError{Repo: r.FullName()}
	}
	login := accountLogin(me)
	out := []hosting.PullRequest{}
	for _, w := range data.Repository.PullRequests.Nodes {
		switch f {
		case hosting.FilterMine:
			if w.Author == nil || !strings.EqualFold(w.Author.Login, login) {
				continue
			}
		case hosting.FilterReview:
			if !w.reviewRequestedFrom(login) {
				continue
			}
		}
		out = append(out, w.toPR())
		if len(out) == listCap {
			break
		}
	}
	more := f != hosting.FilterMine && f != hosting.FilterReview && data.Repository.PullRequests.TotalCount > listCap
	return out, more, nil
}

func (c *client) PullRequestCount(ctx context.Context, r hosting.Repo) (hosting.Count, error) {
	var data struct {
		Repository *struct {
			PullRequests struct {
				TotalCount int `json:"totalCount"`
			} `json:"pullRequests"`
		} `json:"repository"`
	}
	if err := c.graphql(ctx, r, pullRequestCountQuery, &data); err != nil {
		return hosting.Count{}, err
	}
	if data.Repository == nil {
		return hosting.Count{}, &hosting.NoAccessError{Repo: r.FullName()}
	}
	return hosting.Count{N: data.Repository.PullRequests.TotalCount}, nil
}

// ---- Pull request detail ----------------------------------------------

type restUser = wireLogin

type restPR struct {
	Number    int        `json:"number"`
	Title     string     `json:"title"`
	Draft     bool       `json:"draft"`
	Body      string     `json:"body"`
	HTMLURL   string     `json:"html_url"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	Comments  int        `json:"comments"`
	User      *restUser  `json:"user"`
	Head      restBranch `json:"head"`
	Base      restBranch `json:"base"`

	RequestedReviewers []restUser `json:"requested_reviewers"`
}

type restBranch struct {
	Ref  string `json:"ref"`
	SHA  string `json:"sha"`
	Repo *struct {
		FullName string `json:"full_name"`
	} `json:"repo"`
}

type restReview struct {
	ID          int64      `json:"id"`
	User        *restUser  `json:"user"`
	Body        string     `json:"body"`
	State       string     `json:"state"`
	SubmittedAt *time.Time `json:"submitted_at"`
}

type restIssueComment struct {
	ID        int64     `json:"id"`
	User      *restUser `json:"user"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

type restReviewComment struct {
	ID           int64     `json:"id"`
	InReplyToID  int64     `json:"in_reply_to_id"`
	User         *restUser `json:"user"`
	Body         string    `json:"body"`
	CreatedAt    time.Time `json:"created_at"`
	Path         string    `json:"path"`
	Line         *int      `json:"line"`
	OriginalLine *int      `json:"original_line"`
}

type restCommit struct {
	SHA    string `json:"sha"`
	Commit struct {
		Message string `json:"message"`
		Author  struct {
			Name string `json:"name"`
		} `json:"author"`
	} `json:"commit"`
	Author *restUser `json:"author"`
}

// verdicts reads each reviewer's standing from the submitted reviews, oldest
// first: the latest approval or change request counts, a comment-only review
// does not change it, and a dismissal clears it. Pending (unsubmitted) reviews
// are ignored.
func verdicts(reviews []restReview) []hosting.Reviewer {
	verdict := map[string]string{}
	display := map[string]string{}
	var order []string
	for _, rv := range reviews {
		if rv.User == nil || rv.User.Login == "" || rv.State == "PENDING" {
			continue
		}
		key := strings.ToLower(rv.User.Login)
		if _, ok := display[key]; !ok {
			display[key] = rv.User.Login
			order = append(order, key)
		}
		switch rv.State {
		case "APPROVED":
			verdict[key] = "approved"
		case "CHANGES_REQUESTED":
			verdict[key] = "changes"
		case "DISMISSED":
			delete(verdict, key)
		}
	}
	var out []hosting.Reviewer
	for _, k := range order {
		if v := verdict[k]; v != "" {
			out = append(out, hosting.Reviewer{Name: display[k], Verdict: v})
		}
	}
	return out
}

func reviewVerdictLabel(state string) string {
	switch state {
	case "APPROVED":
		return "approved"
	case "CHANGES_REQUESTED":
		return "requested changes"
	}
	return "commented"
}

func lineOf(w restReviewComment) int {
	switch {
	case w.Line != nil:
		return *w.Line
	case w.OriginalLine != nil:
		return *w.OriginalLine
	}
	return 0
}

func shortHash(h string) string {
	if len(h) > 7 {
		return h[:7]
	}
	return h
}

func (w restCommit) toPRCommit() hosting.PRCommit {
	subject, _, _ := strings.Cut(w.Commit.Message, "\n")
	author := w.Commit.Author.Name
	if w.Author != nil && w.Author.Login != "" {
		author = w.Author.Login
	}
	return hosting.PRCommit{Hash: w.SHA, Short: shortHash(w.SHA), Subject: strings.TrimSpace(subject), Author: author}
}

func (c *client) PullRequest(ctx context.Context, r hosting.Repo, number int) (hosting.PullRequestDetail, error) {
	base := repoPath(r)
	prPath := base + "/pulls/" + strconv.Itoa(number)
	var w restPR
	if _, err := c.d.JSON(ctx, "GET", prPath, nil, &w); err != nil {
		return hosting.PullRequestDetail{}, access(r, err)
	}

	reviews, err := getAll[restReview](ctx, c, r, prPath+"/reviews", listPages*pageSize, listPages)
	if err != nil {
		return hosting.PullRequestDetail{}, err
	}
	commits, err := getAll[restCommit](ctx, c, r, prPath+"/commits", listPages*pageSize, listPages)
	if err != nil {
		return hosting.PullRequestDetail{}, err
	}
	// One past the cap per source, so an overflow is seen.
	pages := commentCap/pageSize + 1
	issueComments, err := getAll[restIssueComment](ctx, c, r, base+"/issues/"+strconv.Itoa(number)+"/comments", commentCap+1, pages)
	if err != nil {
		return hosting.PullRequestDetail{}, err
	}
	reviewComments, err := getAll[restReviewComment](ctx, c, r, prPath+"/comments", commentCap+1, pages)
	if err != nil {
		return hosting.PullRequestDetail{}, err
	}

	d := hosting.PullRequestDetail{Description: w.Body, Created: w.CreatedAt}
	d.PullRequest = hosting.PullRequest{
		Number:   number,
		Title:    w.Title,
		Draft:    w.Draft,
		Author:   loginOf(w.User),
		AuthorID: loginOf(w.User),
		Source:   w.Head.Ref,
		Target:   w.Base.Ref,
		HeadHash: w.Head.SHA,
		URL:      w.HTMLURL,
		Fork:     w.Head.Repo == nil || w.Base.Repo == nil || !strings.EqualFold(w.Head.Repo.FullName, w.Base.Repo.FullName),
		Comments: w.Comments,
		Updated:  w.UpdatedAt,
	}
	if d.PullRequest.Number == 0 {
		d.PullRequest.Number = w.Number
	}

	// Reviewers: those with a standing verdict, then requested ones without.
	have := map[string]bool{}
	for _, rv := range verdicts(reviews) {
		have[strings.ToLower(rv.Name)] = true
		d.Reviewers = append(d.Reviewers, rv)
		if rv.Verdict == "approved" {
			d.PullRequest.Approvals++
		}
	}
	for _, rq := range w.RequestedReviewers {
		if rq.Login != "" && !have[strings.ToLower(rq.Login)] {
			have[strings.ToLower(rq.Login)] = true
			d.Reviewers = append(d.Reviewers, hosting.Reviewer{Name: rq.Login, Verdict: "pending"})
		}
	}

	for _, cm := range commits {
		d.Commits = append(d.Commits, cm.toPRCommit())
	}

	var all []hosting.Comment
	for _, ic := range issueComments {
		all = append(all, hosting.Comment{
			ID: "issue-" + strconv.FormatInt(ic.ID, 10), Author: loginOf(ic.User), Created: ic.CreatedAt, Body: ic.Body,
		})
	}
	for _, rc := range reviewComments {
		cm := hosting.Comment{
			ID: "review-comment-" + strconv.FormatInt(rc.ID, 10), Author: loginOf(rc.User), Created: rc.CreatedAt,
			Body: rc.Body, Path: rc.Path, Line: lineOf(rc),
		}
		if rc.InReplyToID != 0 {
			cm.Parent = "review-comment-" + strconv.FormatInt(rc.InReplyToID, 10)
		}
		all = append(all, cm)
	}
	for _, rv := range reviews {
		if strings.TrimSpace(rv.Body) == "" || rv.State == "PENDING" || rv.SubmittedAt == nil {
			continue
		}
		all = append(all, hosting.Comment{
			ID: "review-" + strconv.FormatInt(rv.ID, 10), Author: loginOf(rv.User), Created: *rv.SubmittedAt,
			Body: rv.Body, Verdict: reviewVerdictLabel(rv.State),
		})
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Created.Before(all[j].Created) })
	if len(all) > commentCap {
		all, d.CommentsMore = all[:commentCap], true
	}
	d.Comments = all

	// A failing checks call (no scope, rate limit, network) must not hide the
	// pull request: the section stays none. A rejected token is still an error.
	head := w.Head.SHA
	if d.Checks, err = c.Checks(ctx, r, head); err != nil {
		if errors.Is(err, hosting.ErrRejected) {
			return hosting.PullRequestDetail{}, err
		}
		d.Checks = hosting.CommitStatus{Hash: head, State: hosting.None}
	}
	return d, nil
}

// ---- Pull request files -----------------------------------------------

type restFile struct {
	Filename         string `json:"filename"`
	PreviousFilename string `json:"previous_filename"`
	Status           string `json:"status"`
	Patch            string `json:"patch"`
}

// PullRequestFiles lists the changed files with GitHub's own patch for each,
// up to 30 pages of 100. A file GitHub gave no patch for (binary, too large)
// is marked NoDiff.
func (c *client) PullRequestFiles(ctx context.Context, r hosting.Repo, number int) ([]hosting.PRFile, error) {
	ws, err := getAll[restFile](ctx, c, r, repoPath(r)+"/pulls/"+strconv.Itoa(number)+"/files", filePages*pageSize, filePages)
	if err != nil {
		return nil, err
	}
	files := make([]hosting.PRFile, 0, len(ws))
	for _, w := range ws {
		files = append(files, hosting.PRFile{
			Path: w.Filename, OldPath: w.PreviousFilename, Status: w.Status, Patch: w.Patch, NoDiff: w.Patch == "",
		})
	}
	return files, nil
}
