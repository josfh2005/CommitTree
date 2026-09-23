// Package writetools defines the chat's write tools: their specs, argument
// validation and the proposal shown to the user before anything runs.
// Execution lives in internal/app, through the same methods the UI uses.
package writetools

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"git-ui/internal/ai"
	"git-ui/internal/gitcmd"
	"git-ui/internal/refs"
	"git-ui/internal/worktree"
)

// ErrChanged is returned by Recheck when the repository moved between the
// proposal and the user's approval.
var ErrChanged = errors.New("the repository changed since this was proposed; check its state and propose again")

var writeToolNames = map[string]bool{
	"stage_files":     true,
	"unstage_files":   true,
	"commit":          true,
	"create_branch":   true,
	"checkout_branch": true,
	"stash_push":      true,
	"fetch":           true,
	"push":            true,
	"pull":            true,
	"merge_branch":    true,
}

// IsWrite reports whether name is one of the write tools.
func IsWrite(name string) bool { return writeToolNames[name] }

// Env carries the settings a proposal needs beyond the repository itself.
type Env struct {
	PullStrategy string
}

// Proposal is what a write tool call would do: Title/Details are shown to
// the user, Fingerprint/Staged are the precondition Recheck verifies, and
// the rest are the normalised arguments the app executes with, so execution
// never re-parses the model's JSON.
type Proposal struct {
	Title            string
	Details          []string
	Fingerprint      string
	Staged           []string
	Branch           string
	Remote           string
	Name             string
	Start            string
	Message          string
	Paths            []string
	Checkout         bool
	IncludeUntracked bool
}

func Specs() []ai.ToolSpec {
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	boolean := func(desc string) map[string]any { return map[string]any{"type": "boolean", "description": desc} }
	strArray := func(desc string) map[string]any {
		return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": desc}
	}
	obj := func(props map[string]any, required ...string) map[string]any {
		o := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			o["required"] = required
		}
		return o
	}
	return []ai.ToolSpec{
		{
			Name:        "stage_files",
			Description: "Stage one or more files with an unstaged or untracked change.",
			Parameters:  obj(map[string]any{"paths": strArray("Paths to stage")}, "paths"),
		},
		{
			Name:        "unstage_files",
			Description: "Unstage one or more currently staged files.",
			Parameters:  obj(map[string]any{"paths": strArray("Paths to unstage")}, "paths"),
		},
		{
			Name:        "commit",
			Description: "Commit the currently staged files.",
			Parameters:  obj(map[string]any{"message": str("Commit message")}, "message"),
		},
		{
			Name:        "create_branch",
			Description: "Create a new branch, optionally switching to it.",
			Parameters: obj(map[string]any{
				"name":     str("New branch name"),
				"start":    str("Commit, branch or tag to start the new branch from (default HEAD)"),
				"checkout": boolean("Switch to the new branch after creating it"),
			}, "name"),
		},
		{
			Name:        "checkout_branch",
			Description: "Switch to a local branch, or check out a remote branch as a new local branch (use remote/name).",
			Parameters:  obj(map[string]any{"name": str("Branch to switch to, or remote/branch for a remote-tracking branch")}, "name"),
		},
		{
			Name:        "stash_push",
			Description: "Stash the staged and unstaged changes, optionally including untracked files.",
			Parameters: obj(map[string]any{
				"message":           str("Optional stash message"),
				"include_untracked": boolean("Also stash untracked files"),
			}),
		},
		{
			Name:        "fetch",
			Description: "Fetch from every configured remote.",
			Parameters:  obj(map[string]any{}),
		},
		{
			Name:        "push",
			Description: "Push the current branch to its upstream, or publish it to origin if it has none.",
			Parameters:  obj(map[string]any{}),
		},
		{
			Name:        "pull",
			Description: "Pull the current branch's upstream into it, using the configured pull strategy.",
			Parameters:  obj(map[string]any{}),
		},
		{
			Name:        "merge_branch",
			Description: "Merge another branch into the current branch, always creating a merge commit.",
			Parameters:  obj(map[string]any{"branch": str("Branch to merge into the current branch")}, "branch"),
		},
	}
}

// Prepare validates a write tool call against the repository and returns the
// proposal to show the user. Errors are user-facing text with no "error:"
// prefix; the caller adds it.
func Prepare(ctx context.Context, dir string, call ai.ToolCall, env Env) (Proposal, error) {
	var p Proposal
	var err error
	switch call.Name {
	case "stage_files":
		p, err = prepareStageFiles(ctx, dir, call.Args)
	case "unstage_files":
		p, err = prepareUnstageFiles(ctx, dir, call.Args)
	case "commit":
		p, err = prepareCommit(ctx, dir, call.Args)
	case "create_branch":
		p, err = prepareCreateBranch(ctx, dir, call.Args)
	case "checkout_branch":
		p, err = prepareCheckoutBranch(ctx, dir, call.Args)
	case "stash_push":
		p, err = prepareStashPush(ctx, dir, call.Args)
	case "fetch":
		p, err = prepareFetch(ctx, dir)
	case "push":
		p, err = preparePush(ctx, dir)
	case "pull":
		p, err = preparePull(ctx, dir, env)
	case "merge_branch":
		p, err = prepareMergeBranch(ctx, dir, call.Args)
	default:
		return Proposal{}, fmt.Errorf("unknown tool %q", call.Name)
	}
	if err != nil {
		return Proposal{}, err
	}
	fp, err := refs.Fingerprint(ctx, dir)
	if err != nil {
		return Proposal{}, err
	}
	p.Fingerprint = fp
	return p, nil
}

// Recheck compares p's precondition with the repository now.
func Recheck(ctx context.Context, dir string, p Proposal, tool string) error {
	fp, err := refs.Fingerprint(ctx, dir)
	if err != nil {
		return err
	}
	if fp != p.Fingerprint {
		return ErrChanged
	}
	if tool == "commit" {
		st, err := worktree.Status(ctx, dir)
		if err != nil {
			return err
		}
		staged := stagedPaths(st)
		if strings.Join(staged, "\x00") != strings.Join(p.Staged, "\x00") {
			return ErrChanged
		}
	}
	return nil
}

func stagedPaths(st worktree.State) []string {
	paths := make([]string, len(st.Staged))
	for i, f := range st.Staged {
		paths[i] = f.Path
	}
	sort.Strings(paths)
	return paths
}

// --- argument decoding ---

func stringArg(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("argument %q must be a string", key)
	}
	return s, nil
}

func boolArg(args map[string]any, key string) (bool, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return false, nil
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("argument %q must be a boolean", key)
	}
	return b, nil
}

func pathsArg(args map[string]any, key string) ([]string, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return nil, nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("argument %q must be an array of strings", key)
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("argument %q must be an array of strings", key)
		}
		out = append(out, s)
	}
	return out, nil
}

// --- stage_files / unstage_files ---

func prepareStageFiles(ctx context.Context, dir string, args map[string]any) (Proposal, error) {
	paths, err := pathsArg(args, "paths")
	if err != nil {
		return Proposal{}, err
	}
	if len(paths) == 0 {
		return Proposal{}, errors.New("give at least one path")
	}
	st, err := worktree.Status(ctx, dir)
	if err != nil {
		return Proposal{}, err
	}
	avail := map[string]bool{}
	for _, f := range st.Unstaged {
		avail[f.Path] = true
	}
	for _, f := range st.Untracked {
		avail[f.Path] = true
	}
	for _, p := range paths {
		if !avail[p] {
			return Proposal{}, fmt.Errorf("%q has no unstaged change", p)
		}
	}
	return Proposal{
		Title:   fmt.Sprintf("Stage %d file(s)", len(paths)),
		Details: paths,
		Paths:   paths,
	}, nil
}

func prepareUnstageFiles(ctx context.Context, dir string, args map[string]any) (Proposal, error) {
	paths, err := pathsArg(args, "paths")
	if err != nil {
		return Proposal{}, err
	}
	if len(paths) == 0 {
		return Proposal{}, errors.New("give at least one path")
	}
	st, err := worktree.Status(ctx, dir)
	if err != nil {
		return Proposal{}, err
	}
	staged := map[string]bool{}
	for _, f := range st.Staged {
		staged[f.Path] = true
	}
	for _, p := range paths {
		if !staged[p] {
			return Proposal{}, fmt.Errorf("%q is not staged", p)
		}
	}
	return Proposal{
		Title:   fmt.Sprintf("Unstage %d file(s)", len(paths)),
		Details: paths,
		Paths:   paths,
	}, nil
}

// --- commit ---

func prepareCommit(ctx context.Context, dir string, args map[string]any) (Proposal, error) {
	message, err := stringArg(args, "message")
	if err != nil {
		return Proposal{}, err
	}
	if strings.TrimSpace(message) == "" {
		return Proposal{}, errors.New("the commit message is empty")
	}
	st, err := worktree.Status(ctx, dir)
	if err != nil {
		return Proposal{}, err
	}
	if len(st.Staged) == 0 {
		return Proposal{}, errors.New("nothing is staged; stage files first")
	}
	staged := stagedPaths(st)

	details := append([]string{}, strings.Split(message, "\n")...)
	details = append(details, "Files:")
	details = append(details, staged...)

	return Proposal{
		Title:   fmt.Sprintf("Commit %d staged file(s)", len(staged)),
		Details: details,
		Message: message,
		Staged:  staged,
	}, nil
}

// --- create_branch ---

func prepareCreateBranch(ctx context.Context, dir string, args map[string]any) (Proposal, error) {
	name, err := stringArg(args, "name")
	if err != nil {
		return Proposal{}, err
	}
	start, err := stringArg(args, "start")
	if err != nil {
		return Proposal{}, err
	}
	if start == "" {
		start = "HEAD"
	}
	checkout, err := boolArg(args, "checkout")
	if err != nil {
		return Proposal{}, err
	}

	if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "check-ref-format", "--branch", name); err != nil {
		return Proposal{}, fmt.Errorf("%q is not a valid branch name", name)
	}
	if verifyRef(ctx, dir, "refs/heads/"+name) {
		return Proposal{}, fmt.Errorf("branch %q already exists", name)
	}
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "log", "-1", "--format=%h%x09%s", start, "--")
	if err != nil {
		return Proposal{}, fmt.Errorf("%q is not a commit", start)
	}
	short, subject := splitTab(strings.TrimRight(out, "\n"))

	title := fmt.Sprintf("Create branch %s at %s", name, short)
	if checkout {
		title += " and switch to it"
	}

	return Proposal{
		Title:    title,
		Details:  []string{strings.TrimSpace(short + " " + subject)},
		Name:     name,
		Start:    start,
		Checkout: checkout,
	}, nil
}

func splitTab(s string) (string, string) {
	parts := strings.SplitN(s, "\t", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return parts[0], ""
}

// --- checkout_branch ---

func prepareCheckoutBranch(ctx context.Context, dir string, args map[string]any) (Proposal, error) {
	name, err := stringArg(args, "name")
	if err != nil {
		return Proposal{}, err
	}
	current := currentBranch(ctx, dir)

	if verifyRef(ctx, dir, "refs/heads/"+name) {
		if name == current {
			return Proposal{}, fmt.Errorf("already on %s", name)
		}
		return Proposal{
			Title: fmt.Sprintf("Switch from %s to %s", current, name),
			Name:  name,
		}, nil
	}

	if !verifyRef(ctx, dir, "refs/remotes/"+name) {
		return Proposal{}, fmt.Errorf("no branch %q", name)
	}
	parts := strings.SplitN(name, "/", 2)
	remote, rest := parts[0], ""
	if len(parts) > 1 {
		rest = parts[1]
	}
	return Proposal{
		Title:   fmt.Sprintf("Switch from %s to %s", current, name),
		Details: []string{fmt.Sprintf("creates local branch %s tracking %s", rest, name)},
		Remote:  remote,
		Name:    rest,
	}, nil
}

// --- stash_push ---

func prepareStashPush(ctx context.Context, dir string, args map[string]any) (Proposal, error) {
	message, err := stringArg(args, "message")
	if err != nil {
		return Proposal{}, err
	}
	includeUntracked, err := boolArg(args, "include_untracked")
	if err != nil {
		return Proposal{}, err
	}
	st, err := worktree.Status(ctx, dir)
	if err != nil {
		return Proposal{}, err
	}

	hasChanges := len(st.Staged) > 0 || len(st.Unstaged) > 0
	if !hasChanges && (!includeUntracked || len(st.Untracked) == 0) {
		return Proposal{}, errors.New("nothing to stash")
	}

	paths := map[string]bool{}
	for _, f := range st.Staged {
		paths[f.Path] = true
	}
	for _, f := range st.Unstaged {
		paths[f.Path] = true
	}
	if includeUntracked {
		for _, f := range st.Untracked {
			paths[f.Path] = true
		}
	}

	var details []string
	if message != "" {
		details = append(details, message)
	}
	if includeUntracked {
		details = append(details, "includes untracked files")
	}

	return Proposal{
		Title:            fmt.Sprintf("Stash %d file(s)", len(paths)),
		Details:          details,
		Message:          message,
		IncludeUntracked: includeUntracked,
	}, nil
}

// --- fetch ---

func prepareFetch(ctx context.Context, dir string) (Proposal, error) {
	remotes, err := listRemotes(ctx, dir)
	if err != nil {
		return Proposal{}, err
	}
	if len(remotes) == 0 {
		return Proposal{}, errors.New("no remote is configured")
	}
	return Proposal{Title: fmt.Sprintf("Fetch from %s", strings.Join(remotes, ", "))}, nil
}

// --- push ---

func preparePush(ctx context.Context, dir string) (Proposal, error) {
	branch := currentBranch(ctx, dir)
	if branch == "" {
		return Proposal{}, errors.New("HEAD is detached; check out a branch first")
	}

	upstream, hasUpstream := upstreamOf(ctx, dir)

	var title string
	var logArgs []string
	var total int

	if hasUpstream {
		count, err := revListCount(ctx, dir, upstream+"..HEAD")
		if err != nil {
			return Proposal{}, err
		}
		if count == 0 {
			return Proposal{}, fmt.Errorf("%s has nothing to push to %s", branch, upstream)
		}
		title = fmt.Sprintf("Push %s to %s", branch, upstream)
		logArgs = []string{"log", "--oneline", "-n", "5", upstream + "..HEAD"}
		total = count
	} else {
		remotes, err := listRemotes(ctx, dir)
		if err != nil {
			return Proposal{}, err
		}
		if !contains(remotes, "origin") {
			return Proposal{}, errors.New(`no upstream and no "origin" remote`)
		}
		title = fmt.Sprintf("Publish %s to origin (sets upstream origin/%s)", branch, branch)
		count, err := revListCount(ctx, dir, "HEAD")
		if err != nil {
			return Proposal{}, err
		}
		logArgs = []string{"log", "--oneline", "-n", "5", "HEAD"}
		total = count
	}

	details, err := logDetails(ctx, dir, logArgs, total)
	if err != nil {
		return Proposal{}, err
	}
	return Proposal{Title: title, Details: details}, nil
}

// --- pull ---

func preparePull(ctx context.Context, dir string, env Env) (Proposal, error) {
	branch := currentBranch(ctx, dir)
	if branch == "" {
		return Proposal{}, errors.New("HEAD is detached; check out a branch first")
	}
	upstream, hasUpstream := upstreamOf(ctx, dir)
	if !hasUpstream {
		return Proposal{}, fmt.Errorf("%s has no upstream", branch)
	}
	count, err := revListCount(ctx, dir, "HEAD.."+upstream)
	if err != nil {
		return Proposal{}, err
	}
	if count == 0 {
		return Proposal{}, fmt.Errorf("%s is up to date with %s", branch, upstream)
	}
	title := fmt.Sprintf("Pull %s into %s (%s)", upstream, branch, env.PullStrategy)
	details, err := logDetails(ctx, dir, []string{"log", "--oneline", "-n", "5", "HEAD.." + upstream}, count)
	if err != nil {
		return Proposal{}, err
	}
	return Proposal{Title: title, Details: details}, nil
}

// --- merge_branch ---

func prepareMergeBranch(ctx context.Context, dir string, args map[string]any) (Proposal, error) {
	branch, err := stringArg(args, "branch")
	if err != nil {
		return Proposal{}, err
	}
	if !verifyRef(ctx, dir, "refs/heads/"+branch) && !verifyRef(ctx, dir, "refs/remotes/"+branch) {
		return Proposal{}, fmt.Errorf("no branch %q", branch)
	}
	current := currentBranch(ctx, dir)
	if branch == current {
		return Proposal{}, fmt.Errorf("cannot merge %s into itself", branch)
	}
	count, err := revListCount(ctx, dir, "HEAD.."+branch)
	if err != nil {
		return Proposal{}, err
	}
	if count == 0 {
		return Proposal{}, fmt.Errorf("%s is already merged into %s", branch, current)
	}
	return Proposal{
		Title:   fmt.Sprintf("Merge %s into %s", branch, current),
		Details: []string{fmt.Sprintf("%d commit(s)", count), "a merge commit is always created"},
		Branch:  branch,
	}, nil
}

// --- shared git helpers ---

func currentBranch(ctx context.Context, dir string) string {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "symbolic-ref", "-q", "--short", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func verifyRef(ctx context.Context, dir, ref string) bool {
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", ref)
	return err == nil
}

func upstreamOf(ctx context.Context, dir string) (string, bool) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(out), true
}

func listRemotes(ctx context.Context, dir string) ([]string, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "remote")
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}

func revListCount(ctx context.Context, dir, revRange string) (int, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-list", "--count", revRange)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("writetools: parse rev-list count: %w", err)
	}
	return n, nil
}

// logDetails runs a `git log --oneline -n 5 <range>` style command and adds
// a summary line when total exceeds what was shown.
func logDetails(ctx context.Context, dir string, logArgs []string, total int) ([]string, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, logArgs...)
	if err != nil {
		return nil, err
	}
	out = strings.TrimRight(out, "\n")
	var lines []string
	if out != "" {
		lines = strings.Split(out, "\n")
	}
	if total > len(lines) {
		lines = append(lines, fmt.Sprintf("… and %d more", total-len(lines)))
	}
	return lines, nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
