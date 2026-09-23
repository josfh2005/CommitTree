// Package submodules reads a repository's git submodules as one flat,
// recursive list. It does not use `git submodule status`, which aborts on a
// gitlink that has no .gitmodules entry; each piece is read directly.
package submodules

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"git-ui/internal/gitcmd"
)

// Submodule is one gitlink, whether or not it is checked out or still
// configured in .gitmodules.
type Submodule struct {
	Name        string `json:"name"` // .gitmodules name; "" when not configured
	Path        string `json:"path"` // relative to the top repository, slash-separated
	URL         string `json:"url"`
	Recorded    string `json:"recorded"`   // commit the direct parent's index records; "" when no gitlink
	CheckedOut  string `json:"checkedOut"` // "" when not initialised
	Branch      string `json:"branch"`     // "" when detached or not initialised
	Initialised bool   `json:"initialised"`
	Configured  bool   `json:"configured"` // has both a gitlink and a .gitmodules entry
	Moved       bool   `json:"moved"`
	Modified    bool   `json:"modified"`
	Untracked   bool   `json:"untracked"`
	Conflict    bool   `json:"conflict"`
}

// List reads dir's submodules and, recursively, those of every initialised
// one, with paths relative to dir, sorted by path.
func List(ctx context.Context, dir string) ([]Submodule, error) {
	var out []Submodule
	if err := walk(ctx, dir, "", &out); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// HasAny is a cheap check for whether dir's repository has any submodules
// configured at all, without running git.
func HasAny(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".gitmodules"))
	return err == nil
}

// walk lists the direct submodules of the repository at root/prefix and
// recurses into every one of them that is initialised, appending flattened
// results (with paths relative to root) to out.
func walk(ctx context.Context, root, prefix string, out *[]Submodule) error {
	here := filepath.Join(root, filepath.FromSlash(prefix))

	links, conflicts, err := gitlinks(ctx, here)
	if err != nil {
		return err
	}
	mods := gitmodules(ctx, here)
	flags := contentFlags(ctx, here)

	for _, p := range union(links, mods, conflicts) {
		abs := filepath.Join(here, filepath.FromSlash(p))
		m, inMods := mods[p]
		s := Submodule{
			Name:     m.name,
			URL:      m.url,
			Path:     join(prefix, p),
			Recorded: links[p],
			Conflict: conflicts[p],
		}
		s.Configured = inMods && (links[p] != "" || conflicts[p])
		if head, ok := checkedOut(ctx, abs); ok {
			s.Initialised, s.CheckedOut = true, head
			s.Branch = branch(ctx, abs)
			s.Moved = s.Recorded != "" && head != s.Recorded
			f := flags[p]
			s.Modified, s.Untracked = f.modified, f.untracked
		}
		*out = append(*out, s)
		if s.Initialised {
			if err := walk(ctx, root, s.Path, out); err != nil {
				return err
			}
		}
	}
	return nil
}

// gitlinks reads dir's index for gitlink entries (mode 160000): links maps a
// path to the commit its parent's index records, and conflicts marks paths
// that are unmerged, which have no single recorded commit.
func gitlinks(ctx context.Context, dir string) (links map[string]string, conflicts map[string]bool, err error) {
	links = map[string]string{}
	conflicts = map[string]bool{}
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "ls-files", "-s", "-z")
	if err != nil {
		return nil, nil, err
	}
	for _, rec := range strings.Split(out, "\x00") {
		if rec == "" {
			continue
		}
		// "<mode> <sha> <stage>\t<path>"
		tab := strings.IndexByte(rec, '\t')
		if tab < 0 {
			continue
		}
		meta, path := rec[:tab], rec[tab+1:]
		fields := strings.SplitN(meta, " ", 3)
		if len(fields) != 3 || fields[0] != "160000" {
			continue
		}
		switch fields[2] {
		case "0":
			links[path] = fields[1]
		case "1", "2", "3":
			conflicts[path] = true
		}
	}
	return links, conflicts, nil
}

type modEntry struct{ name, url string }

// gitmodules reads dir's .gitmodules for every submodule.<name>.path and
// submodule.<name>.url entry and returns them indexed by path. A missing
// file, or any other read error (exit 1 covers both no file and no match),
// is treated as no entries rather than a failure — List must still report an
// unconfigured gitlink.
func gitmodules(ctx context.Context, dir string) map[string]modEntry {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout,
		"config", "-f", ".gitmodules", "-z", "--get-regexp", `^submodule\..*\.(path|url)$`)
	if err != nil {
		return map[string]modEntry{}
	}

	type raw struct{ path, url string }
	byName := map[string]*raw{}
	fields := strings.Split(out, "\x00")
	for _, rec := range fields {
		if rec == "" {
			continue
		}
		nl := strings.IndexByte(rec, '\n')
		if nl < 0 {
			continue
		}
		key, value := rec[:nl], rec[nl+1:]
		key = strings.TrimPrefix(key, "submodule.")
		// Names may themselves contain dots, so only the final .path/.url
		// suffix is stripped, not everything after the first dot.
		var name, field string
		switch {
		case strings.HasSuffix(key, ".path"):
			name, field = strings.TrimSuffix(key, ".path"), "path"
		case strings.HasSuffix(key, ".url"):
			name, field = strings.TrimSuffix(key, ".url"), "url"
		default:
			continue
		}
		e, ok := byName[name]
		if !ok {
			e = &raw{}
			byName[name] = e
		}
		if field == "path" {
			e.path = value
		} else {
			e.url = value
		}
	}

	byPath := map[string]modEntry{}
	for name, e := range byName {
		if e.path == "" {
			continue
		}
		byPath[e.path] = modEntry{name: name, url: e.url}
	}
	return byPath
}

type contentFlag struct{ modified, untracked bool }

// contentFlags reads dir's working-tree status for every immediate
// submodule path (porcelain v2 reports a submodule's own untracked content
// on its parent's record, so a plain status covers it) and returns the
// commit-changed/modified/untracked flags keyed by path. Any error is
// treated as no flags, matching the tolerance of the other helpers here —
// content state is best-effort, never fatal to the list.
func contentFlags(ctx context.Context, dir string) map[string]contentFlag {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "status", "--porcelain=v2", "-z")
	if err != nil {
		return map[string]contentFlag{}
	}
	flags := map[string]contentFlag{}
	fields := strings.Split(out, "\x00")
	for i := 0; i < len(fields); i++ {
		rec := fields[i]
		if rec == "" {
			continue
		}
		switch rec[0] {
		case '1', '2':
			path, sub, ok := parseSub(rec)
			if rec[0] == '2' && i+1 < len(fields) {
				// Skip the rename/copy source field that follows this record.
				i++
			}
			if !ok || !strings.HasPrefix(sub, "S") || len(sub) != 4 {
				continue
			}
			flags[path] = contentFlag{
				modified:  sub[2] == 'M',
				untracked: sub[3] == 'U',
			}
		}
	}
	return flags
}

// parseSub pulls the submodule state field (the third space-separated field)
// and the path out of a porcelain v2 "1"/"2" record, mirroring how
// internal/worktree.parseChange locates the path: everything after the
// known field count, with a rename's similarity-score field stripped first.
func parseSub(rec string) (path, sub string, ok bool) {
	parts := strings.SplitN(rec, " ", 9)
	if len(parts) < 9 {
		return "", "", false
	}
	sub = parts[2]
	path = parts[8]
	if rec[0] == '2' {
		if s := strings.SplitN(path, " ", 2); len(s) == 2 {
			path = s[1]
		}
	}
	return path, sub, true
}

// checkedOut reports whether abs holds an initialised submodule checkout: it
// must have its own .git and resolve as its own repository toplevel, not
// just an empty directory that git treats as inside the parent.
func checkedOut(ctx context.Context, abs string) (head string, ok bool) {
	if _, err := os.Stat(filepath.Join(abs, ".git")); err != nil {
		return "", false
	}
	out, err := gitcmd.Run(ctx, abs, gitcmd.ReadTimeout, "rev-parse", "--show-toplevel", "HEAD")
	if err != nil {
		return "", false
	}
	lines := strings.SplitN(out, "\n", 2)
	if len(lines) < 2 {
		return "", false
	}
	top := strings.TrimSpace(lines[0])
	headHash := strings.TrimSpace(lines[1])

	wantAbs, err1 := filepath.EvalSymlinks(abs)
	gotTop, err2 := filepath.EvalSymlinks(top)
	if err1 != nil || err2 != nil || wantAbs != gotTop {
		return "", false
	}
	return headHash, true
}

// branch returns abs's current branch name, or "" when detached.
func branch(ctx context.Context, abs string) string {
	out, err := gitcmd.Run(ctx, abs, gitcmd.ReadTimeout, "symbolic-ref", "--short", "-q", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// union returns the sorted set of paths appearing in any of the three maps,
// so a gitlink missing its .gitmodules entry (or vice versa) still gets one
// row.
func union(links map[string]string, mods map[string]modEntry, conflicts map[string]bool) []string {
	seen := map[string]bool{}
	var paths []string
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	for p := range links {
		add(p)
	}
	for p := range mods {
		add(p)
	}
	for p := range conflicts {
		add(p)
	}
	sort.Strings(paths)
	return paths
}

// join builds a slash-separated relative path from a parent prefix ("" at
// the root) and a submodule's own path.
func join(prefix, p string) string {
	if prefix == "" {
		return p
	}
	return prefix + "/" + p
}
