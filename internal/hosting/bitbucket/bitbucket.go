// Package bitbucket is the Bitbucket Cloud client of the hosting integration:
// pipelines and commit statuses, pull requests, their discussion and their
// diff (docs/superpowers/specs/2026-10-23-hosting-integration-design.md, §3
// and §5). It talks to the API only through a hosting.Doer, which keeps the
// token on the API host.
package bitbucket

import (
	"context"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"git-ui/internal/hosting"
)

const (
	pipelinePages   = 3   // pages of 100 pipelines per log refresh
	listCap         = 100 // pull requests per list
	listPageLen     = 50
	commentCap      = 500
	maxDiffBytes    = 10 << 20
	diffstatPageLen = 500
)

type client struct{ d *hosting.Doer }

// New returns the Bitbucket client over d, whose BaseURL is
// https://api.bitbucket.org/2.0.
func New(d *hosting.Doer) hosting.Client { return &client{d: d} }

var _ hosting.Client = (*client)(nil)

// ---- helpers ----------------------------------------------------------

func repoPath(r hosting.Repo) string {
	return "/repositories/" + url.PathEscape(r.Owner) + "/" + url.PathEscape(r.Name)
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

type page[T any] struct {
	Values []T    `json:"values"`
	Next   string `json:"next"`
	Size   *int   `json:"size"`
}

func getPage[T any](ctx context.Context, c *client, target string) (page[T], error) {
	var p page[T]
	_, err := c.d.JSON(ctx, "GET", target, nil, &p)
	return p, err
}

// onAPIHost reports whether a `next` link points at the API host the Doer is
// confined to. A link elsewhere ends pagination.
func (c *client) onAPIHost(link string) bool {
	u, err := url.Parse(link)
	if err != nil {
		return false
	}
	b, err := url.Parse(c.d.BaseURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Scheme, b.Scheme) && strings.EqualFold(u.Hostname(), b.Hostname()) &&
		effectivePort(u) == effectivePort(b)
}

func effectivePort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	if strings.EqualFold(u.Scheme, "https") {
		return "443"
	}
	return "80"
}

// ---- wire types -------------------------------------------------------

type wirePipeline struct {
	BuildNumber int       `json:"build_number"`
	CreatedOn   time.Time `json:"created_on"`
	Duration    int       `json:"duration_in_seconds"`
	State       struct {
		Name   string `json:"name"`
		Result *struct {
			Name string `json:"name"`
		} `json:"result"`
	} `json:"state"`
	Target struct {
		Type     string `json:"type"`
		RefName  string `json:"ref_name"`
		Selector struct {
			Type    string `json:"type"`
			Pattern string `json:"pattern"`
		} `json:"selector"`
		Commit struct {
			Hash string `json:"hash"`
		} `json:"commit"`
	} `json:"target"`
}

type wireStatus struct {
	Key       string    `json:"key"`
	Name      string    `json:"name"`
	State     string    `json:"state"`
	URL       string    `json:"url"`
	UpdatedOn time.Time `json:"updated_on"`
}

type wireUser struct {
	DisplayName string `json:"display_name"`
	UUID        string `json:"uuid"`
}

type wireParticipant struct {
	User     wireUser `json:"user"`
	Role     string   `json:"role"`
	Approved bool     `json:"approved"`
	State    string   `json:"state"`
}

type wireEnd struct {
	Branch struct {
		Name string `json:"name"`
	} `json:"branch"`
	Commit struct {
		Hash string `json:"hash"`
	} `json:"commit"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

type wirePR struct {
	ID           int       `json:"id"`
	Title        string    `json:"title"`
	Draft        bool      `json:"draft"`
	Description  string    `json:"description"`
	CommentCount int       `json:"comment_count"`
	CreatedOn    time.Time `json:"created_on"`
	UpdatedOn    time.Time `json:"updated_on"`
	Summary      struct {
		Raw string `json:"raw"`
	} `json:"summary"`
	Author       wireUser          `json:"author"`
	Source       wireEnd           `json:"source"`
	Destination  wireEnd           `json:"destination"`
	Participants []wireParticipant `json:"participants"`
	Links        struct {
		HTML struct {
			Href string `json:"href"`
		} `json:"html"`
	} `json:"links"`
}

func (w wirePR) approvals() int {
	n := 0
	for _, p := range w.Participants {
		if p.Approved {
			n++
		}
	}
	return n
}

func (w wirePR) approvedBy(uuid string) bool {
	for _, p := range w.Participants {
		if p.User.UUID == uuid && p.Approved {
			return true
		}
	}
	return false
}

func (w wirePR) toPR() hosting.PullRequest {
	return hosting.PullRequest{
		Number:    w.ID,
		Title:     w.Title,
		Draft:     w.Draft,
		Author:    w.Author.DisplayName,
		AuthorID:  w.Author.UUID,
		Source:    w.Source.Branch.Name,
		Target:    w.Destination.Branch.Name,
		HeadHash:  w.Source.Commit.Hash,
		URL:       w.Links.HTML.Href,
		Fork:      w.Source.Repository.FullName != w.Destination.Repository.FullName,
		Approvals: w.approvals(),
		Comments:  w.CommentCount,
		Updated:   w.UpdatedOn,
	}
}

// ---- User -------------------------------------------------------------

func (c *client) User(ctx context.Context) (hosting.Account, error) {
	var u wireUser
	if _, err := c.d.JSON(ctx, "GET", "/user", nil, &u); err != nil {
		return hosting.Account{}, err
	}
	return hosting.Account{ID: u.UUID, Name: u.DisplayName}, nil
}

// ---- Statuses and Checks ----------------------------------------------

// pipelineCheck turns a pipeline into a check. ok is false for a state this
// client does not know.
func pipelineCheck(r hosting.Repo, p wirePipeline) (hosting.Check, bool) {
	var st hosting.State
	switch p.State.Name {
	case "PENDING", "IN_PROGRESS", "PAUSED":
		st = hosting.Running
	case "COMPLETED":
		result := ""
		if p.State.Result != nil {
			result = p.State.Result.Name
		}
		switch result {
		case "SUCCESSFUL":
			st = hosting.Success
		case "FAILED", "ERROR":
			st = hosting.Failed
		case "STOPPED", "EXPIRED":
			st = hosting.Stopped
		default:
			return hosting.Check{}, false
		}
	default:
		return hosting.Check{}, false
	}
	t := p.Target
	n := strconv.Itoa(p.BuildNumber)
	return hosting.Check{
		Key:         t.Type + "|" + t.RefName + "|" + t.Selector.Type + ":" + t.Selector.Pattern,
		Name:        "Pipeline #" + n,
		State:       st,
		URL:         "https://bitbucket.org/" + r.FullName() + "/pipelines/results/" + n,
		DurationSec: p.Duration,
		Created:     p.CreatedOn,
	}, true
}

func (c *client) pipelinePage(ctx context.Context, r hosting.Repo, n int) (page[wirePipeline], error) {
	q := url.Values{"sort": {"-created_on"}, "pagelen": {"100"}, "page": {strconv.Itoa(n)}}
	p, err := getPage[wirePipeline](ctx, c, repoPath(r)+"/pipelines?"+q.Encode())
	return p, access(r, err)
}

func summarize(hash string, checks []hosting.Check) hosting.CommitStatus {
	if len(checks) == 0 {
		return hosting.CommitStatus{Hash: hash, State: hosting.None}
	}
	return hosting.CommitStatus{Hash: hash, State: hosting.Aggregate(checks), Checks: hosting.Latest(checks)}
}

// Statuses reads the repository's newest pipelines (up to 3 pages) and
// matches them to the requested commits. The latest run of each trigger
// counts, per commit. A commit with no pipeline is none.
func (c *client) Statuses(ctx context.Context, r hosting.Repo, hashes []string) (map[string]hosting.CommitStatus, error) {
	out := make(map[string]hosting.CommitStatus, len(hashes))
	want := map[string]bool{} // lower-case requested hash
	for _, h := range hashes {
		if h != "" {
			want[strings.ToLower(h)] = true
		}
	}
	if len(want) == 0 {
		return out, nil
	}
	byHash := map[string][]hosting.Check{}
	found := map[string]bool{}
	for n := 1; n <= pipelinePages; n++ {
		pg, err := c.pipelinePage(ctx, r, n)
		if err != nil {
			return nil, err
		}
		for _, p := range pg.Values {
			h := strings.ToLower(p.Target.Commit.Hash)
			if !want[h] {
				continue
			}
			found[h] = true
			if ck, ok := pipelineCheck(r, p); ok {
				byHash[h] = append(byHash[h], ck)
			}
		}
		if len(found) == len(want) || pg.Next == "" || len(pg.Values) == 0 {
			break
		}
	}
	for _, h := range hashes {
		if h != "" {
			out[h] = summarize(h, byHash[strings.ToLower(h)])
		}
	}
	return out, nil
}

func statusState(s string) (hosting.State, bool) {
	switch s {
	case "SUCCESSFUL":
		return hosting.Success, true
	case "FAILED":
		return hosting.Failed, true
	case "INPROGRESS":
		return hosting.Running, true
	case "STOPPED":
		return hosting.Stopped, true
	}
	return "", false
}

// urlKey identifies a check by its URL. Bitbucket reports a pipeline's commit
// status with the pipeline's own results address (".../results/<n>" in a path
// or a fragment), so those are compared by pipeline number.
func urlKey(u string) string {
	if i := strings.LastIndex(u, "/results/"); i >= 0 {
		if n := u[i+len("/results/"):]; n != "" && strings.Trim(n, "0123456789") == "" {
			return "pipeline:" + n
		}
	}
	return u
}

// Checks merges the commit's build statuses (external CI included) with its
// pipelines from the newest page of the pipeline list, drops what shows twice
// (by URL) and aggregates the latest run of each.
func (c *client) Checks(ctx context.Context, r hosting.Repo, hash string) (hosting.CommitStatus, error) {
	var checks []hosting.Check
	seen := map[string]bool{}
	pg, err := c.pipelinePage(ctx, r, 1)
	if err != nil {
		return hosting.CommitStatus{}, err
	}
	for _, p := range pg.Values {
		if !strings.EqualFold(p.Target.Commit.Hash, hash) {
			continue
		}
		if ck, ok := pipelineCheck(r, p); ok {
			seen[urlKey(ck.URL)] = true
			checks = append(checks, ck)
		}
	}
	q := url.Values{"pagelen": {"100"}}
	sp, err := getPage[wireStatus](ctx, c, repoPath(r)+"/commit/"+url.PathEscape(hash)+"/statuses?"+q.Encode())
	if err != nil {
		return hosting.CommitStatus{}, access(r, err)
	}
	for _, s := range sp.Values {
		st, ok := statusState(s.State)
		if !ok {
			continue
		}
		if s.URL != "" {
			k := urlKey(s.URL)
			if seen[k] {
				continue
			}
			seen[k] = true
		}
		checks = append(checks, hosting.Check{Key: s.Key, Name: s.Name, State: st, URL: s.URL, Created: s.UpdatedOn})
	}
	return summarize(hash, checks), nil
}

// ---- Pull request list ------------------------------------------------

func listQuery(f hosting.Filter, me hosting.Account, pagelen int) url.Values {
	q := url.Values{
		"state":   {"OPEN"},
		"sort":    {"-updated_on"},
		"pagelen": {strconv.Itoa(pagelen)},
	}
	switch f {
	case hosting.FilterMine:
		q.Set("q", `author.uuid="`+me.ID+`"`)
	case hosting.FilterReview:
		q.Set("q", `reviewers.uuid="`+me.ID+`"`)
	}
	return q
}

func (c *client) PullRequests(ctx context.Context, r hosting.Repo, f hosting.Filter, me hosting.Account) ([]hosting.PullRequest, bool, error) {
	q := listQuery(f, me, listPageLen)
	q.Set("fields", "+values.participants")
	target := repoPath(r) + "/pullrequests?" + q.Encode()
	var prs []hosting.PullRequest
	more := false
	for target != "" {
		pg, err := getPage[wirePR](ctx, c, target)
		if err != nil {
			return nil, false, access(r, err)
		}
		for _, w := range pg.Values {
			if f == hosting.FilterReview && w.approvedBy(me.ID) {
				continue
			}
			if len(prs) == listCap {
				more = true
				break
			}
			prs = append(prs, w.toPR())
		}
		if more || pg.Next == "" || !c.onAPIHost(pg.Next) {
			break
		}
		target = pg.Next
	}
	return prs, more, nil
}

// PullRequestCount asks for one row and reads `size`. When Bitbucket leaves
// it out, the first page of 100 is counted instead.
func (c *client) PullRequestCount(ctx context.Context, r hosting.Repo) (hosting.Count, error) {
	base := repoPath(r) + "/pullrequests?"
	pg, err := getPage[wirePR](ctx, c, base+listQuery(hosting.FilterOpen, hosting.Account{}, 1).Encode())
	if err != nil {
		return hosting.Count{}, access(r, err)
	}
	if pg.Size != nil {
		return hosting.Count{N: *pg.Size}, nil
	}
	pg, err = getPage[wirePR](ctx, c, base+listQuery(hosting.FilterOpen, hosting.Account{}, 100).Encode())
	if err != nil {
		return hosting.Count{}, access(r, err)
	}
	return hosting.Count{N: len(pg.Values), More: pg.Next != ""}, nil
}

// ---- Pull request detail ----------------------------------------------

type wireCommit struct {
	Hash    string `json:"hash"`
	Message string `json:"message"`
	Author  struct {
		Raw  string    `json:"raw"`
		User *wireUser `json:"user"`
	} `json:"author"`
}

type wireComment struct {
	ID        int       `json:"id"`
	Deleted   bool      `json:"deleted"`
	CreatedOn time.Time `json:"created_on"`
	User      wireUser  `json:"user"`
	Content   struct {
		Raw string `json:"raw"`
	} `json:"content"`
	Parent *struct {
		ID int `json:"id"`
	} `json:"parent"`
	Inline *struct {
		Path string `json:"path"`
		From *int   `json:"from"`
		To   *int   `json:"to"`
	} `json:"inline"`
}

func verdictOf(p wireParticipant) string {
	switch {
	case p.Approved:
		return "approved"
	case p.State == "changes_requested":
		return "changes"
	}
	return "pending"
}

func prCommit(w wireCommit) hosting.PRCommit {
	subject, _, _ := strings.Cut(strings.TrimSpace(w.Message), "\n")
	author := ""
	if w.Author.User != nil {
		author = w.Author.User.DisplayName
	}
	if author == "" {
		author, _, _ = strings.Cut(w.Author.Raw, " <")
	}
	short := w.Hash
	if len(short) > 7 {
		short = short[:7]
	}
	return hosting.PRCommit{Hash: w.Hash, Short: short, Subject: strings.TrimSpace(subject), Author: author}
}

func prComment(w wireComment) hosting.Comment {
	c := hosting.Comment{
		ID:      strconv.Itoa(w.ID),
		Author:  w.User.DisplayName,
		Created: w.CreatedOn,
		Body:    w.Content.Raw,
	}
	if w.Parent != nil {
		c.Parent = strconv.Itoa(w.Parent.ID)
	}
	if w.Inline != nil {
		c.Path = w.Inline.Path
		switch {
		case w.Inline.To != nil:
			c.Line = *w.Inline.To
		case w.Inline.From != nil:
			c.Line = *w.Inline.From
		}
	}
	return c
}

func (c *client) comments(ctx context.Context, r hosting.Repo, prPath string) (out []hosting.Comment, more bool, err error) {
	target := prPath + "/comments?" + url.Values{"pagelen": {"100"}}.Encode()
	for target != "" {
		pg, err := getPage[wireComment](ctx, c, target)
		if err != nil {
			return nil, false, access(r, err)
		}
		for _, w := range pg.Values {
			if w.Deleted {
				continue
			}
			if len(out) == commentCap {
				return out, true, nil
			}
			out = append(out, prComment(w))
		}
		if pg.Next == "" || !c.onAPIHost(pg.Next) {
			break
		}
		target = pg.Next
	}
	return out, false, nil
}

func (c *client) PullRequest(ctx context.Context, r hosting.Repo, number int) (hosting.PullRequestDetail, error) {
	prPath := repoPath(r) + "/pullrequests/" + strconv.Itoa(number)
	var w wirePR
	if _, err := c.d.JSON(ctx, "GET", prPath, nil, &w); err != nil {
		return hosting.PullRequestDetail{}, access(r, err)
	}
	d := hosting.PullRequestDetail{PullRequest: w.toPR(), Created: w.CreatedOn, Description: w.Description}
	if d.Description == "" {
		d.Description = w.Summary.Raw
	}
	for _, p := range w.Participants {
		if p.Role == "REVIEWER" {
			d.Reviewers = append(d.Reviewers, hosting.Reviewer{Name: p.User.DisplayName, Verdict: verdictOf(p)})
		}
	}

	cp, err := getPage[wireCommit](ctx, c, prPath+"/commits?"+url.Values{"pagelen": {"100"}}.Encode())
	if err != nil {
		return hosting.PullRequestDetail{}, access(r, err)
	}
	for _, wc := range cp.Values {
		d.Commits = append(d.Commits, prCommit(wc))
	}

	if d.Comments, d.CommentsMore, err = c.comments(ctx, r, prPath); err != nil {
		return hosting.PullRequestDetail{}, err
	}
	sort.SliceStable(d.Comments, func(i, j int) bool { return d.Comments[i].Created.Before(d.Comments[j].Created) })

	// A failing checks call (no pipeline scope, rate limit, network) must not
	// hide the pull request: the section is left empty (none). A rejected token
	// is still an error.
	head := w.Source.Commit.Hash
	if d.Checks, err = c.Checks(ctx, r, head); err != nil {
		if errors.Is(err, hosting.ErrRejected) {
			return hosting.PullRequestDetail{}, err
		}
		d.Checks = hosting.CommitStatus{Hash: head, State: hosting.None}
	}
	return d, nil
}

// ---- Pull request files -----------------------------------------------

type wireDiffstat struct {
	Status string `json:"status"`
	New    *struct {
		Path string `json:"path"`
	} `json:"new"`
	Old *struct {
		Path string `json:"path"`
	} `json:"old"`
}

// PullRequestFiles lists the changed files and attaches each one's patch,
// cut from the pull request's unified diff. A file the diff has no section
// for, or every file when the diff is over 10 MB, is marked NoDiff.
func (c *client) PullRequestFiles(ctx context.Context, r hosting.Repo, number int) ([]hosting.PRFile, error) {
	prPath := repoPath(r) + "/pullrequests/" + strconv.Itoa(number)
	pg, err := getPage[wireDiffstat](ctx, c, prPath+"/diffstat?"+url.Values{"pagelen": {strconv.Itoa(diffstatPageLen)}}.Encode())
	if err != nil {
		return nil, access(r, err)
	}
	var patches map[string]string
	raw, err := c.d.Raw(ctx, prPath+"/diff", maxDiffBytes)
	switch {
	case err == nil:
		patches = hosting.SplitUnifiedDiff(string(raw))
	case errors.Is(err, hosting.ErrTooLarge):
		// no patches: every file is NoDiff
	default:
		return nil, access(r, err)
	}
	files := make([]hosting.PRFile, 0, len(pg.Values))
	for _, s := range pg.Values {
		f := hosting.PRFile{Status: s.Status}
		if s.Old != nil {
			f.OldPath = s.Old.Path
		}
		if s.New != nil {
			f.Path = s.New.Path
		}
		if f.Path == "" {
			f.Path = f.OldPath
		}
		if p, ok := patches[f.Path]; ok {
			f.Patch = p
		} else {
			f.NoDiff = true
		}
		files = append(files, f)
	}
	return files, nil
}
