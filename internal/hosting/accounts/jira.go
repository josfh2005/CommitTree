package accounts

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Jira is the site and project keys that turn ticket keys in commit and pull
// request text into links. Both are needed for a link to appear.
type Jira struct {
	Site string   `json:"site"` // https://<host>[/path], no trailing slash; "" when unset
	Keys []string `json:"keys"` // upper-case, deduplicated, in the order typed
}

var jiraKey = regexp.MustCompile(`^[A-Z][A-Z0-9]+$`)

// NormalizeJira checks what the user typed. The site is empty (unset) or an
// https address with a host (no credentials, query or fragment), with the
// trailing slash trimmed. The keys are separated by commas or spaces,
// upper-cased and deduplicated; each is a letter, then letters or digits.
func NormalizeJira(site, keys string) (Jira, error) {
	out := Jira{Keys: []string{}}
	site = strings.TrimSpace(site)
	if site != "" {
		u, err := url.Parse(site)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.Contains(site, "?") || strings.Contains(site, "#") {
			return Jira{}, fmt.Errorf("the Jira site must be an https address, like https://example.atlassian.net")
		}
		out.Site = strings.TrimRight(site, "/")
	}
	seen := map[string]bool{}
	for _, k := range strings.FieldsFunc(keys, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == ';' }) {
		k = strings.ToUpper(k)
		if !jiraKey.MatchString(k) {
			return Jira{}, fmt.Errorf("%q is not a Jira project key (letters and digits, starting with a letter)", k)
		}
		if !seen[k] {
			seen[k] = true
			out.Keys = append(out.Keys, k)
		}
	}
	return out, nil
}

// Jira returns the saved Jira settings.
func (s *Store) Jira() (Jira, error) {
	cfg, err := s.Config()
	if err != nil {
		return Jira{}, err
	}
	keys := []string{}
	if cfg.JiraKeys != "" {
		keys = strings.Split(cfg.JiraKeys, ",")
	}
	return Jira{Site: cfg.JiraSite, Keys: keys}, nil
}

// SaveJira validates and stores the Jira settings in hosting.json (they are
// not secret). Nothing is stored when they are invalid.
func (s *Store) SaveJira(site, keys string) (Jira, error) {
	j, err := NormalizeJira(site, keys)
	if err != nil {
		return Jira{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, err := Load(s.path)
	if err != nil {
		return Jira{}, err
	}
	cfg.JiraSite, cfg.JiraKeys = j.Site, strings.Join(j.Keys, ",")
	if err := Save(s.path, cfg); err != nil {
		return Jira{}, err
	}
	return j, nil
}
