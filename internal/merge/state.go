package merge

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"git-ui/internal/gitcmd"
	"git-ui/internal/refs"
)

// Kind says what the repository is in the middle of resolving. Every value
// but KindStash has a git-level marker; KindStash is the leftover case —
// unmerged entries with no marker at all, which only a stash apply/pop
// leaves behind.
type Kind string

const (
	KindMerge      Kind = "merge"
	KindRebase     Kind = "rebase"
	KindCherryPick Kind = "cherry-pick"
	KindRevert     Kind = "revert"
	KindAM         Kind = "am"
	KindStash      Kind = "stash"
)

// State is what the UI needs to show a merge in progress. Conflicts are the
// unmerged text files both sides changed — the agent's to resolve and stage,
// whether or not markers remain. Manual are the ones only a human can settle:
// delete/modify, added on one side, symlinks, submodules, binaries, and
// anything in the worktree that is not a regular file.
type State struct {
	Kind      Kind     `json:"kind"`
	Merging   bool     `json:"merging"`
	From      string   `json:"from"`
	Into      string   `json:"into"`
	Conflicts []string `json:"conflicts"`
	Manual    []string `json:"manual"`
	// Staged and Unstaged are the merge's settled files: in the index for
	// the merge commit, or changed in the worktree but not yet added.
	Staged   []string `json:"staged"`
	Unstaged []string `json:"unstaged"`
	// Step and Total are set only for KindRebase: "commit 2 of 5". Subject
	// is the commit currently being replayed.
	Step    int    `json:"step,omitempty"`
	Total   int    `json:"total,omitempty"`
	Subject string `json:"subject,omitempty"`
	// OursLabel and TheirsLabel name the two sides of a conflict for the
	// view ("main", "a1b2c3 fix login"); the descriptions add what each
	// side is, for the AI resolver. A rebase swaps git's ours and theirs
	// relative to a merge, which is why the names come from here. Set for
	// merge, rebase and cherry-pick only.
	OursLabel         string `json:"oursLabel,omitempty"`
	TheirsLabel       string `json:"theirsLabel,omitempty"`
	OursDescription   string `json:"oursDescription,omitempty"`
	TheirsDescription string `json:"theirsDescription,omitempty"`
}

// Status reports whether dir is in the middle of a merge, a rebase, or a
// conflicted stash apply/pop, and what is still unresolved. A repository
// with none of the three yields the zero State and no error.
func Status(ctx context.Context, dir string) (State, error) {
	st := State{Conflicts: []string{}, Manual: []string{}, Staged: []string{}, Unstaged: []string{}}

	entries, err := unmergedEntries(ctx, dir)
	if err != nil {
		return State{}, err
	}

	// Detection order matters, and the cheap-looking "no MERGE_HEAD, no
	// rebase directory, unmerged entries" shortcut is wrong: a conflicted
	// cherry-pick or revert matches it exactly, and `git am` writes to
	// .git/rebase-apply, so treating that directory as a rebase would make
	// Abort run `rebase --abort` on an am. Sequencer heads first, then
	// MERGE_HEAD, then the two rebase directories (am told apart by its own
	// "applying" file), and only then the markerless stash case.
	switch {
	case pickedCommit(ctx, dir, "CHERRY_PICK_HEAD") != "":
		st.Kind = KindCherryPick
		st.Into = refs.CurrentLabel(ctx, dir)
		st.From, st.Subject = sequencerFromInto(ctx, dir, "CHERRY_PICK_HEAD")
	case pickedCommit(ctx, dir, "REVERT_HEAD") != "":
		st.Kind = KindRevert
		st.Into = refs.CurrentLabel(ctx, dir)
		st.From, st.Subject = sequencerFromInto(ctx, dir, "REVERT_HEAD")
	case hasMergeHead(ctx, dir):
		st.Kind = KindMerge
		st.Into = refs.CurrentLabel(ctx, dir)
		st.From = mergeFrom(ctx, dir)
	default:
		if rebaseDir, ok := inRebase(ctx, dir); ok {
			if isApplyingMailbox(rebaseDir) {
				st.Kind = KindAM
				st.Into = refs.CurrentLabel(ctx, dir)
				st.Subject = amSubject(rebaseDir)
			} else {
				st.Kind = KindRebase
				st.From, st.Into = rebaseFromInto(ctx, dir, rebaseDir)
				st.Step, st.Total, st.Subject = rebaseStepInfo(ctx, dir, rebaseDir)
			}
		} else if len(entries) > 0 {
			st.Kind = KindStash
		}
	}
	nameSides(ctx, dir, &st)
	if st.Kind == "" {
		return st, nil
	}
	st.Merging = true

	paths := make([]string, 0, len(entries))
	for path := range entries {
		paths = append(paths, path)
	}
	manualByAttrs, err := attrsCallForManual(ctx, dir, paths, st.Kind == KindMerge)
	if err != nil {
		return State{}, err
	}
	for path, e := range entries {
		if e.needsHuman() || !isText(filepath.Join(dir, path)) || manualByAttrs[path] {
			st.Manual = append(st.Manual, path)
			continue
		}
		st.Conflicts = append(st.Conflicts, path)
	}
	sort.Strings(st.Conflicts)
	sort.Strings(st.Manual)

	if st.Kind != KindMerge {
		// Only a merge has an "incoming side" (mergeTouched) to filter an
		// unrelated dirty file by; for every other kind every settled path
		// here is the conflict's own.
		if st.Staged, err = changedPaths(ctx, dir, entries, "--cached", "HEAD"); err != nil {
			return State{}, err
		}
		if st.Unstaged, err = changedPaths(ctx, dir, entries); err != nil {
			return State{}, err
		}
		return st, nil
	}

	if st.Staged, err = changedPaths(ctx, dir, entries, "--cached", "HEAD"); err != nil {
		return State{}, err
	}
	// Unstaged is limited to what the merge brings in: a merge may start with
	// unrelated uncommitted work, which is not the merge's to stage.
	touched, err := mergeTouched(ctx, dir)
	if err != nil {
		return State{}, err
	}
	dirty, err := changedPaths(ctx, dir, entries)
	if err != nil {
		return State{}, err
	}
	for _, p := range dirty {
		if touched[p] {
			st.Unstaged = append(st.Unstaged, p)
		}
	}
	return st, nil
}

// nameSides fills OursLabel/TheirsLabel and their descriptions for the kinds
// where a conflict has two named sides: merge, cherry-pick and rebase.
func nameSides(ctx context.Context, dir string, st *State) {
	commit := func(short, subject string) string { return strings.TrimSpace(short + " " + subject) }
	switch st.Kind {
	case KindMerge:
		st.OursLabel, st.TheirsLabel = st.Into, st.From
		st.OursDescription = st.Into + " (the branch you are merging into)"
		st.TheirsDescription = st.From + " (the branch being merged)"
	case KindCherryPick:
		st.OursLabel, st.TheirsLabel = st.Into, commit(st.From, st.Subject)
		st.OursDescription = st.Into + " (the current branch)"
		st.TheirsDescription = st.From + " \"" + st.Subject + "\" (the commit being cherry-picked)"
	case KindRebase:
		st.OursLabel = st.Into
		st.OursDescription = st.Into + " (the base being rebased onto)"
		if short, subject := sequencerFromInto(ctx, dir, "REBASE_HEAD"); short != "" {
			st.TheirsLabel = commit(short, subject)
			st.TheirsDescription = short + " \"" + subject + "\" (your commit being replayed)"
		} else {
			st.TheirsLabel = st.From
			st.TheirsDescription = st.From + " (your commits being replayed)"
		}
	}
}

// pickedCommit returns the full hash CHERRY_PICK_HEAD or REVERT_HEAD points
// at, or "" when that pseudo-ref does not exist.
func pickedCommit(ctx context.Context, dir, ref string) string {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", ref)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// sequencerFromInto labels a cherry-pick or a revert by the commit it is
// replaying: From is its short hash (there is no branch to name), Subject
// its message's first line.
func sequencerFromInto(ctx context.Context, dir, ref string) (from, subject string) {
	if out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "log", "-1", "--format=%h%x00%s", ref); err == nil {
		fields := strings.SplitN(strings.TrimSpace(out), "\x00", 2)
		if len(fields) == 2 {
			return fields[0], fields[1]
		}
	}
	return "", ""
}

// isApplyingMailbox reports whether a rebase-apply directory belongs to
// `git am` rather than to an old-backend rebase: am writes an "applying"
// file there, a rebase does not.
func isApplyingMailbox(rebaseDir string) bool {
	if filepath.Base(rebaseDir) != "rebase-apply" {
		return false
	}
	_, err := os.Stat(filepath.Join(rebaseDir, "applying"))
	return err == nil
}

// amSubject is the first line of the patch being applied, when git left one
// where it can be read.
func amSubject(rebaseDir string) string {
	data, err := os.ReadFile(filepath.Join(rebaseDir, "msg-clean"))
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(data)), "\n")
	return line
}

// hasMergeHead reports whether a merge is in progress.
func hasMergeHead(ctx context.Context, dir string) bool {
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", "MERGE_HEAD")
	return err == nil
}

// inRebase reports whether a rebase is in progress and the absolute path of
// its bookkeeping directory — "rebase-merge" for the merge backend (a plain
// `git rebase` since git 2.26), or "rebase-apply" for the older one.
func inRebase(ctx context.Context, dir string) (string, bool) {
	for _, name := range []string{"rebase-merge", "rebase-apply"} {
		out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--git-path", name)
		if err != nil {
			continue
		}
		p := strings.TrimSpace(out)
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		if info, statErr := os.Stat(p); statErr == nil && info.IsDir() {
			return p, true
		}
	}
	return "", false
}

// rebaseFromInto reads the branch being rebased and the commit it is being
// replayed onto. Both backends write head-name and onto.
func rebaseFromInto(ctx context.Context, dir, rebaseDir string) (from, into string) {
	if data, err := os.ReadFile(filepath.Join(rebaseDir, "head-name")); err == nil {
		from = strings.TrimPrefix(strings.TrimSpace(string(data)), "refs/heads/")
	}
	if data, err := os.ReadFile(filepath.Join(rebaseDir, "onto")); err == nil {
		onto := strings.TrimSpace(string(data))
		// "Rebasing feature onto 5e6df51" tells the user nothing; name the
		// branch that commit is on when there is one, and fall back to the
		// short hash when there isn't (a detached onto, or a commit no
		// branch contains any more).
		if out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "name-rev", "--name-only", "--no-undefined", "--refs=refs/heads/*", onto); err == nil {
			into = strings.TrimSpace(out)
		} else if out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--short", onto); err == nil {
			into = strings.TrimSpace(out)
		}
	}
	return from, into
}

// rebaseStepInfo reads "commit N of M" and the replayed commit's subject.
// Only the merge backend (rebase-merge) writes msgnum/end in a form this
// reads; the older apply backend is left at the zero values rather than
// guessed at.
func rebaseStepInfo(ctx context.Context, dir, rebaseDir string) (step, total int, subject string) {
	if filepath.Base(rebaseDir) != "rebase-merge" {
		return 0, 0, ""
	}
	step = readIntFile(filepath.Join(rebaseDir, "msgnum"))
	total = readIntFile(filepath.Join(rebaseDir, "end"))
	if out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "log", "-1", "--format=%s", "REBASE_HEAD"); err == nil {
		subject = strings.TrimSpace(out)
	}
	return step, total, subject
}

func readIntFile(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return n
}

// mergeTouched is the set of paths the incoming side can change: what it
// changed since the merge base, plus the new name of each file our side
// renamed whose old name it changed — git puts their edit there, a path
// their own diff never mentions.
func mergeTouched(ctx context.Context, dir string) (map[string]bool, error) {
	touched := map[string]bool{}
	base, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "merge-base", "HEAD", "MERGE_HEAD")
	if err != nil {
		// Unrelated histories have no merge base; everything differing
		// between the two sides is then the merge's.
		paths, err := changedPaths(ctx, dir, nil, "HEAD", "MERGE_HEAD")
		for _, p := range paths {
			touched[p] = true
		}
		return touched, err
	}
	base = strings.TrimSpace(base)
	theirs, err := changedPaths(ctx, dir, nil, base, "MERGE_HEAD")
	if err != nil {
		return nil, err
	}
	for _, p := range theirs {
		touched[p] = true
	}
	// -z --name-status: a status field, then one path, or two (old, new)
	// for a rename or copy.
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "diff", "-z", "--name-status", "-M", base, "HEAD")
	if err != nil {
		return nil, err
	}
	fields := strings.Split(out, "\x00")
	for i := 0; i < len(fields); {
		status := fields[i]
		if status == "" {
			break
		}
		if (status[0] == 'R' || status[0] == 'C') && i+2 < len(fields) {
			if status[0] == 'R' && touched[fields[i+1]] {
				touched[fields[i+2]] = true
			}
			i += 3
			continue
		}
		i += 2
	}
	return touched, nil
}

// unmerged is what git's index says about one unmerged path: which of the
// base (1), ours (2) and theirs (3) stages exist, and every mode seen.
type unmerged struct {
	stages [4]bool
	modes  []string
}

// needsHuman reports a conflict the index alone shows the agent can't settle:
// a side is missing (delete/modify, or added on one side only), or an entry
// is a symlink or a submodule rather than a file.
func (u unmerged) needsHuman() bool {
	if !u.stages[2] || !u.stages[3] {
		return true
	}
	for _, m := range u.modes {
		if m == "120000" || m == "160000" {
			return true
		}
	}
	return false
}

// unmergedEntries groups `git ls-files -u` by path. Each NUL-terminated
// record is "<mode> <sha> <stage>\t<path>".
func unmergedEntries(ctx context.Context, dir string) (map[string]unmerged, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "ls-files", "-u", "-z")
	if err != nil {
		return nil, err
	}
	entries := map[string]unmerged{}
	for _, rec := range strings.Split(out, "\x00") {
		meta, path, ok := strings.Cut(rec, "\t")
		if !ok {
			continue
		}
		fields := strings.Fields(meta)
		if len(fields) != 3 {
			continue
		}
		e := entries[path]
		e.modes = append(e.modes, fields[0])
		if n := fields[2]; len(n) == 1 && n[0] >= '1' && n[0] <= '3' {
			e.stages[n[0]-'0'] = true
		}
		entries[path] = e
	}
	return entries, nil
}

// attrsCallForManual asks git for the merge and conflict-marker-size
// attributes of every candidate path and reports, per path, whether any of
// them mean our marker parser does not apply. It unions the answer over the
// worktree's own .gitattributes (source ""), HEAD's (source "HEAD"), and —
// only when withMergeHead is set, i.e. Kind is KindMerge — MERGE_HEAD's
// (source "MERGE_HEAD"): MERGE_HEAD is not a valid tree-ish outside a merge,
// and `git check-attr --source` on it then fails outright, so every other
// Kind must not ask. Checking HEAD and (when applicable) MERGE_HEAD, not
// just the worktree, matters because the incoming side may have changed or
// removed the very attribute rule that was in effect when git wrote the
// conflicted file, and the worktree's post-conflict .gitattributes can
// differ from both. A path is reported true (Manual) as soon as any one
// source calls for it; that is fail-safe by construction, since an
// attribute any side ever had wins. An empty paths slice makes no git calls.
func attrsCallForManual(ctx context.Context, dir string, paths []string, withMergeHead bool) (map[string]bool, error) {
	result := map[string]bool{}
	if len(paths) == 0 {
		return result, nil
	}
	sources := []string{"", "HEAD"}
	if withMergeHead {
		sources = append(sources, "MERGE_HEAD")
	}
	for _, source := range sources {
		args := []string{"check-attr"}
		if source != "" {
			args = append(args, "--source", source)
		}
		args = append(args, "-z", "merge", "conflict-marker-size", "--")
		args = append(args, paths...)
		out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, args...)
		if err != nil {
			return nil, err
		}
		for path, attrs := range parseCheckAttrZ(out) {
			if needsHumanForAttrs(attrs) {
				result[path] = true
			}
		}
	}
	return result, nil
}

// parseCheckAttrZ parses the -z output of `git check-attr`: a flat sequence
// of NUL-terminated fields in triples of path, attribute, value. A trailing
// empty field from the final terminator is ignored.
func parseCheckAttrZ(raw string) map[string]map[string]string {
	fields := strings.Split(raw, "\x00")
	if n := len(fields); n > 0 && fields[n-1] == "" {
		fields = fields[:n-1]
	}
	result := map[string]map[string]string{}
	for i := 0; i+2 < len(fields); i += 3 {
		path, attr, value := fields[i], fields[i+1], fields[i+2]
		if result[path] == nil {
			result[path] = map[string]string{}
		}
		result[path][attr] = value
	}
	return result
}

// needsHumanForAttrs reports whether a path's merge/conflict-marker-size
// attributes mean our marker parser does not apply, so an otherwise-text
// conflict there cannot be trusted as resolved just because it has no
// markers our parser recognises.
func needsHumanForAttrs(attrs map[string]string) bool {
	switch attrs["merge"] {
	case "", "unspecified", "set", "text":
	default:
		return true
	}
	switch attrs["conflict-marker-size"] {
	case "", "unspecified", "7":
	default:
		return true
	}
	return false
}

// binaryProbe is how much of a file git itself inspects for a NUL byte when
// deciding whether it is binary.
const binaryProbe = 8000

// isText reports whether the worktree path is a regular file whose first
// bytes hold no NUL. It never follows a symlink and never opens anything but
// a regular file, so a link or a FIFO planted by a merge can't make it block
// or read outside the repository, and it reads at most binaryProbe bytes.
func isText(full string) bool {
	info, err := os.Lstat(full)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	f, err := os.Open(full)
	if err != nil {
		return false
	}
	defer f.Close()
	var buf [binaryProbe]byte
	n, err := io.ReadFull(f, buf[:])
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return false
	}
	return !bytes.Contains(buf[:n], []byte{0})
}

// mergeFrom names what is being merged in: the quoted branch, tag or remote
// branch from the message git prepared, falling back to MERGE_HEAD's short
// hash when the message carries no quoted name.
func mergeFrom(ctx context.Context, dir string) string {
	gitDir, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--absolute-git-dir")
	if err == nil {
		data, err := os.ReadFile(filepath.Join(strings.TrimSpace(gitDir), "MERGE_MSG"))
		if err == nil {
			first := strings.SplitN(string(data), "\n", 2)[0]
			if a, b := strings.Index(first, "'"), strings.LastIndex(first, "'"); a >= 0 && b > a {
				return first[a+1 : b]
			}
		}
	}
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--short", "MERGE_HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}
