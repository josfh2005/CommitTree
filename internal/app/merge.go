package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"git-ui/internal/ai"
	"git-ui/internal/ai/agent"
	"git-ui/internal/ai/mergetools"
	"git-ui/internal/ai/prompts"
	"git-ui/internal/ai/tools"
	"git-ui/internal/gitcmd"
	"git-ui/internal/merge"
	"git-ui/internal/stash"
)

// ConflictFile is one file of a merge as the UI shows it: the raw content
// with markers while it is conflicted, and the staged diff once it is not.
type ConflictFile struct {
	Path     string `json:"path"`
	Resolved bool   `json:"resolved"`
	Text     string `json:"text"`
}

// MergeBranch merges branch into the repository's current branch. A
// conflicted merge is left in place for the user or the agent to resolve.
func (a *App) MergeBranch(id, branch string) (merge.Result, error) {
	var result merge.Result
	err := a.write(id, func(ctx context.Context, dir string) error {
		var err error
		result, err = merge.Start(ctx, dir, branch)
		return err
	})
	return result, err
}

func (a *App) GetMergeState(id string) (merge.State, error) {
	dir, err := a.dir(id)
	if err != nil {
		return merge.State{}, err
	}
	return merge.Status(a.ctx, dir)
}

// AbortMerge stops any agent run on the repository first, so it can't go on
// resolving a merge that no longer exists — or the next one.
func (a *App) AbortMerge(id string) error {
	a.stopRun(id)
	return a.write(id, func(ctx context.Context, dir string) error { return merge.Abort(ctx, dir) })
}

// CommitMerge stops any agent run on the repository first, as AbortMerge
// does, then advances whatever conflict resolution is in progress: a merge
// commits, a rebase continues (and may leave the next commit's conflicts
// for the view to show), a stash conflict does nothing.
func (a *App) CommitMerge(id string) error {
	a.stopRun(id)
	return a.write(id, func(ctx context.Context, dir string) error { return merge.Continue(ctx, dir) })
}

// stopRun cancels the repository's running agent, if any. StopChat's only
// error is ErrAIDisabled, and with AI off there is no run to stop.
func (a *App) stopRun(id string) {
	_ = a.StopChat(id)
}

// mergePaths lists every path this merge touches: the ones still unmerged,
// and the settled ones, staged or not. Paths come from git verbatim, so a
// caller's path is accepted only when it matches one exactly — a crafted
// pathspec such as ":(glob)*" can never equal one.
func mergePaths(st merge.State) map[string]bool {
	paths := map[string]bool{}
	for _, list := range [][]string{st.Conflicts, st.Manual, st.Staged, st.Unstaged} {
		for _, p := range list {
			paths[p] = true
		}
	}
	return paths
}

// GetConflictFile returns what the merge view shows for one file. Only files
// belonging to the merge in progress can be read, so a path from the
// renderer can't be used to read the disk.
func (a *App) GetConflictFile(id, path string) (ConflictFile, error) {
	dir, err := a.dir(id)
	if err != nil {
		return ConflictFile{}, err
	}
	st, err := merge.Status(a.ctx, dir)
	if err != nil {
		return ConflictFile{}, err
	}
	if !mergePaths(st)[path] {
		return ConflictFile{}, fmt.Errorf("%q is not part of this merge", path)
	}
	// Check if the path is in the Conflicts list.
	for _, p := range st.Conflicts {
		if p == path {
			data, err := os.ReadFile(filepath.Join(dir, path))
			if err != nil {
				return ConflictFile{}, err
			}
			return ConflictFile{Path: path, Text: string(data)}, nil
		}
	}
	// Check if the path is in the Manual list.
	for _, p := range st.Manual {
		if p == path {
			return ConflictFile{Path: path, Text: "This file has no conflict markers to edit here. Right-click it in the list to take ours or theirs, or resolve it in your editor."}, nil
		}
	}
	// Otherwise it is settled: show what the merge commit changes against
	// our side — the index if staged, the worktree if not.
	// --literal-pathspecs: the path is a filename, never a glob or magic pathspec.
	against := "--cached"
	if slices.Contains(st.Unstaged, path) {
		against = "HEAD"
	}
	out, err := gitcmd.Run(a.ctx, dir, gitcmd.ReadTimeout, "--literal-pathspecs", "diff", against, "--", path)
	if err != nil {
		return ConflictFile{}, err
	}
	return ConflictFile{Path: path, Resolved: true, Text: out}, nil
}

// StageMergeFile adds one of the merge's unstaged files to the index.
func (a *App) StageMergeFile(id, path string) error {
	return a.writeMerge(id, func(ctx context.Context, dir string) error { return merge.Stage(ctx, dir, path) })
}

// UnstageMergeFile takes one of the merge's staged files out of the index,
// keeping its content.
func (a *App) UnstageMergeFile(id, path string) error {
	return a.writeMerge(id, func(ctx context.Context, dir string) error { return merge.Unstage(ctx, dir, path) })
}

// TakeMergeSide settles one of the merge's Manual files with one side's
// version, "ours" or "theirs", and stages it.
func (a *App) TakeMergeSide(id, path, side string) error {
	return a.writeMerge(id, func(ctx context.Context, dir string) error { return merge.Take(ctx, dir, path, merge.Side(side)) })
}

// writeMerge runs fn under the repository's write lock, so it can't
// interleave with an agent tool call, drops a stash entry a conflicted Pop
// left behind once resolving it leaves nothing unmerged, then tells the
// merge view and any running agent's UI that something moved.
func (a *App) writeMerge(id string, fn func(ctx context.Context, dir string) error) error {
	if err := a.write(id, func(ctx context.Context, dir string) error {
		if err := fn(ctx, dir); err != nil {
			return err
		}
		a.finishOwedDrop(id, ctx, dir)
		return nil
	}); err != nil {
		return err
	}
	a.emit(EventMergeChanged, MergeChangedEvent{RepoID: id})
	return nil
}

// finishOwedDrop drops a stash entry a conflicted StashPop left behind, once
// resolving it leaves nothing unmerged. Git itself never records that a
// drop is still owed, so this in-memory reminder is the only place it
// lives — losing it (an app restart mid-resolution) never risks the
// changes themselves, only the tidiness of dropping the entry.
//
// The reminder is keyed by the stash's commit hash, not its index: indices
// shift whenever another stash is pushed or dropped, and StashDrop needs no
// clean tree, so that is reachable while a conflict is still open. Looking
// the hash up again here, against the stash list as it stands right now,
// means a shift never makes this drop the wrong entry — at worst the owed
// one is already gone (dropped by hand, or the reminder outlived a restart)
// and nothing here matches, so nothing is dropped.
//
// Callers must already hold the repository's write lock (they run this from
// inside their a.write closure, passing that closure's ctx and dir) so the
// List → Drop pair below can't interleave with a concurrent StashDrop, which
// would otherwise be free to shift indices between the two and make this
// drop the wrong entry.
func (a *App) finishOwedDrop(id string, ctx context.Context, dir string) {
	v, ok := a.owedDrops.Load(id)
	if !ok {
		return
	}
	st, err := merge.Status(ctx, dir)
	if err != nil || st.Merging {
		return
	}
	a.owedDrops.Delete(id)
	sha := v.(string)
	entries, err := stash.List(ctx, dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.Hash == sha {
			_ = stash.Drop(ctx, dir, e.Index)
			return
		}
	}
}

// EventMergeChanged tells the frontend the working tree moved during a merge,
// so the merge view can refresh while the agent works.
const EventMergeChanged = "merge:changed"

type MergeChangedEvent struct {
	RepoID string `json:"repoID"`
}

// MergeMaxSteps is generous because each conflicted file costs several tool
// rounds; history trimming keeps the context bounded regardless.
const MergeMaxSteps = 30

// resolvable are the kinds the conflict agent works on. Each has a
// fingerprint that changes when the operation (or, for a rebase, the step)
// does, which is what keeps a run from reaching into the next one.
var resolvable = map[merge.Kind]bool{merge.KindMerge: true, merge.KindRebase: true, merge.KindCherryPick: true}

// resolveRequest is the user message a resolve run starts with.
func resolveRequest(st merge.State) string {
	switch st.Kind {
	case merge.KindRebase:
		s := fmt.Sprintf("Resolve the conflicts from rebasing %s onto %s", st.From, st.Into)
		if st.Total > 0 {
			s += fmt.Sprintf(" (commit %d of %d: %s)", st.Step, st.Total, st.Subject)
		}
		return s
	case merge.KindCherryPick:
		return fmt.Sprintf("Resolve the conflicts from cherry-picking %s %q onto %s", st.From, st.Subject, st.Into)
	}
	return fmt.Sprintf("Resolve the conflicts from merging %s into %s", st.From, st.Into)
}

// kindGuidance is appended to the (user-editable) resolver prompt, so the
// model knows what each side is trying to do whatever the prompt says.
func kindGuidance(k merge.Kind) string {
	switch k {
	case merge.KindRebase:
		return "This is a rebase, one commit at a time. The base side is the code being rebased onto and is already final; re-apply the intent of the commit being replayed on top of it, without undoing what the base changed."
	case merge.KindCherryPick:
		return "This is a cherry-pick. The current branch's side is final; apply the intent of the commit being cherry-picked on top of it, without undoing what the current branch changed."
	}
	return "This is a merge. Keep the intent of both branches."
}

// ResolveConflicts runs the conflict agent over the merge in progress. It
// shares the repository's chat slot with SendChat and ExplainInChat, so a
// resolve run and a chat can never interleave, and it stops before
// committing: staging is as far as the agent goes.
func (a *App) ResolveConflicts(repoID, runID string) error {
	if a.ai == nil {
		return ErrAIDisabled
	}
	if runID == "" {
		return errors.New("run id is required")
	}
	repo, ok := a.repo(repoID)
	if !ok {
		return fmt.Errorf("unknown repository %q", repoID)
	}
	st, err := merge.Status(a.ctx, repo.Path)
	if err != nil {
		return err
	}
	if !st.Merging {
		return errors.New("this repository is not merging")
	}
	if !resolvable[st.Kind] {
		return fmt.Errorf("the AI resolver handles merges, rebases and cherry-picks, not a %s", st.Kind)
	}
	// The run belongs to this operation (for a rebase, this step); its tools
	// refuse to act on any other.
	startedFor := merge.Fingerprint(a.ctx, repo.Path)
	if startedFor == "" {
		return errors.New("this repository is not merging")
	}
	sides := mergetools.Sides{Ours: st.OursDescription, Theirs: st.TheirsDescription}
	cfg, err := a.aiSettings()
	if err != nil {
		return err
	}
	provider, err := a.chatProvider(cfg)
	if err != nil {
		return err
	}

	a.ai.mu.Lock()
	if _, busy := a.ai.runs[repoID]; busy {
		a.ai.mu.Unlock()
		return ErrChatBusy
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.ai.runs[repoID] = cancel
	a.ai.mu.Unlock()
	finish := func() {
		a.ai.mu.Lock()
		delete(a.ai.runs, repoID)
		a.ai.mu.Unlock()
		cancel()
	}

	text := resolveRequest(st)
	history, err := a.ai.deps.Chats.Load(repoID)
	if err == nil {
		history = append(history, ai.Message{Role: ai.RoleUser, Content: text})
		err = a.ai.deps.Chats.Save(repoID, history)
	}
	var system string
	if err == nil {
		system, err = a.ai.deps.Prompts.Get(prompts.ResolveConflicts, prompts.Vars{
			Repo: repo.Name, Path: repo.Path, Branch: st.Into, Date: time.Now().Format("2006-01-02"),
		})
		if err == nil {
			system += "\n\n" + kindGuidance(st.Kind)
		}
	}
	if err != nil {
		finish()
		return err
	}

	a.emit(agent.EventStart, agent.StartEvent{RepoID: repoID, RunID: runID, Text: text, Provider: cfg.ChatProvider, Model: cfg.ChatModel})

	go func() {
		run := agent.Run{
			RepoID: repoID, RunID: runID,
			Provider: provider, Model: cfg.ChatModel, System: system,
			Tools:    append(mergetools.Specs(), tools.Specs()...),
			MaxSteps: MergeMaxSteps,
			RunTool: func(ctx context.Context, call ai.ToolCall, step int) string {
				if isMergeTool(call.Name) {
					return a.runMergeTool(ctx, repoID, startedFor, sides, call)
				}
				return tools.Run(ctx, repo.Path, call)
			},
			Emit: a.emit,
		}
		updated, runErr := agent.Execute(ctx, run, history)
		at := answerTime()
		stampAnswer(updated[len(history):], cfg.ChatProvider, cfg.ChatModel, at)
		saveErr := a.ai.deps.Chats.Save(repoID, updated)
		// Release the repo before announcing the end so a new message can be
		// sent, or the merge acted on, as soon as the frontend sees done/error.
		finish()
		a.emit(EventMergeChanged, MergeChangedEvent{RepoID: repoID})
		// The model's closing words can claim success whatever happened; end
		// with git's own account. It goes before done/error, which close the
		// run the frontend attaches notices to.
		if st, err := merge.Status(a.ctx, repo.Path); err == nil {
			if summary := resolveSummary(st); summary != "" {
				a.emit(agent.EventNotice, agent.NoticeEvent{RepoID: repoID, RunID: runID, Text: summary})
			}
		}
		switch {
		case runErr != nil && !errors.Is(runErr, context.Canceled):
			a.emit(agent.EventError, agent.ErrorEvent{RepoID: repoID, RunID: runID, Message: runErr.Error(), Code: chatErrorCode(runErr)})
		case saveErr != nil:
			a.emit(agent.EventError, agent.ErrorEvent{RepoID: repoID, RunID: runID, Message: saveErr.Error(), Code: "other"})
		default:
			a.emit(agent.EventDone, agent.DoneEvent{RepoID: repoID, RunID: runID, At: at})
		}
	}()
	return nil
}

// resolveSummary is what git says is left once a resolve run ends, or ""
// when the repository is no longer merging (aborted or committed meanwhile).
func resolveSummary(st merge.State) string {
	if !st.Merging {
		return ""
	}
	if len(st.Conflicts) == 0 && len(st.Manual) == 0 {
		return "Checked with git: nothing left to resolve. Review the Staged files before committing."
	}
	parts := []string{"Checked with git."}
	if len(st.Conflicts) > 0 {
		parts = append(parts, "Still conflicted: "+strings.Join(st.Conflicts, ", ")+".")
	}
	if len(st.Manual) > 0 {
		parts = append(parts, "Needs you: "+strings.Join(st.Manual, ", ")+".")
	}
	return strings.Join(parts, " ")
}

// runMergeTool runs one mergetools call under the repository's write lock,
// so an agent edit and an abort or commit can never interleave, and only
// while the operation the run was started for is still the one in progress.
// It uses the run's ctx rather than the lock's, so stopping the run still
// reaches the tool.
func (a *App) runMergeTool(ctx context.Context, repoID, startedFor string, sides mergetools.Sides, call ai.ToolCall) string {
	var out string
	var changed bool
	err := a.write(repoID, func(_ context.Context, dir string) error {
		if merge.Fingerprint(ctx, dir) != startedFor {
			out = "The operation this run was started for is no longer in progress; stop."
			return nil
		}
		out, changed = mergetools.Run(ctx, dir, call, sides)
		return nil
	})
	switch {
	case errors.Is(err, ErrBusy):
		return "The repository is busy with another operation; stop and report."
	case err != nil:
		return "Could not run " + call.Name + ": " + err.Error()
	}
	if changed {
		a.emit(EventMergeChanged, MergeChangedEvent{RepoID: repoID})
	}
	return out
}

func isMergeTool(name string) bool {
	for _, spec := range mergetools.Specs() {
		if spec.Name == name {
			return true
		}
	}
	return false
}
