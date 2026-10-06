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
// Region is where one conflict region sits in ConflictFile.Text: lines
// Start (its <<<<<<< line) to End (after its >>>>>>> line), 0-based.
type Region struct {
	ID    string `json:"id"`
	Start int    `json:"start"`
	End   int    `json:"end"`
	// BaseAt is the ||||||| line (-1 without an ancestor section), Sep the
	// ======= line: the view needs no marker parsing of its own.
	BaseAt int `json:"baseAt"`
	Sep    int `json:"sep"`
}

type ConflictFile struct {
	Path     string   `json:"path"`
	Resolved bool     `json:"resolved"`
	Text     string   `json:"text"`
	Regions  []Region `json:"regions"`
	// Restartable: Restart file can put it back as git first wrote it.
	Restartable bool `json:"restartable"`
}

// RegionResult is what resolving one region left: the file's regions still
// to settle, and whether it was staged because none were.
type RegionResult struct {
	Left   int  `json:"left"`
	Staged bool `json:"staged"`
	// Settled: a decision card's region was already resolved another way,
	// so nothing was written.
	Settled bool `json:"settled"`
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
// commits with message (an empty one is refused), a rebase continues with
// its own message (and may leave the next commit's conflicts for the view
// to show), a stash conflict does nothing.
func (a *App) CommitMerge(id, message string) error {
	a.stopRun(id)
	return a.write(id, func(ctx context.Context, dir string) error { return merge.Continue(ctx, dir, message) })
}

// GetMergeMessage is the message git prepared for the merge in progress,
// without its comment lines, to pre-fill the editor before Commit merge.
func (a *App) GetMergeMessage(id string) (string, error) {
	dir, err := a.dir(id)
	if err != nil {
		return "", err
	}
	return merge.Message(a.ctx, dir)
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
	can, _ := merge.Restartable(a.ctx, dir)
	restartable := slices.Contains(can, path)
	// Check if the path is in the Conflicts list.
	for _, p := range st.Conflicts {
		if p == path {
			data, err := os.ReadFile(filepath.Join(dir, path))
			if err != nil {
				return ConflictFile{}, err
			}
			f := ConflictFile{Path: path, Text: string(data), Regions: []Region{}, Restartable: restartable}
			if hunks, err := merge.Parse(f.Text); err == nil {
				for _, h := range hunks {
					f.Regions = append(f.Regions, Region{ID: h.ID, Start: h.Start, End: h.End, BaseAt: h.BaseAt, Sep: h.Sep})
				}
			}
			return f, nil
		}
	}
	// Check if the path is in the Manual list.
	for _, p := range st.Manual {
		if p == path {
			return ConflictFile{Path: path, Text: "This file has no conflict markers to edit here. Take one side with the buttons above, or resolve it in your editor.", Regions: []Region{}, Restartable: restartable}, nil
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
	return ConflictFile{Path: path, Resolved: true, Text: out, Regions: []Region{}, Restartable: restartable}, nil
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
	if a.aiBusy(id) {
		return fmt.Errorf("%w; wait for it or stop it first", ErrChatBusy)
	}
	return a.writeMerge(id, func(ctx context.Context, dir string) error { return merge.Take(ctx, dir, path, merge.Side(side)) })
}

// ResolveMergeRegion settles one conflict region of path: one side whole
// ("ours", "theirs"), both ours first ("both"), or the user's own text
// ("text"). It stages the file once no regions are left.
func (a *App) ResolveMergeRegion(id, path, region, choice, text string) (RegionResult, error) {
	if a.aiBusy(id) {
		return RegionResult{}, fmt.Errorf("%w; wait for it or stop it first", ErrChatBusy)
	}
	return a.applyRegion(id, path, region, choice, text)
}

// applyRegion writes one region (a side, both, or given text) under the
// merge write lock, staging the file when it was the last region. Callers
// check the chat slot first.
func (a *App) applyRegion(id, path, region, choice, text string) (RegionResult, error) {
	var res RegionResult
	err := a.writeMerge(id, func(ctx context.Context, dir string) error {
		content := text
		if choice != "text" {
			h, err := merge.Region(dir, path, region)
			if err != nil {
				return err
			}
			if content, err = merge.RegionText(h, choice); err != nil {
				return err
			}
		}
		left, err := merge.ResolveRegion(ctx, dir, path, region, content)
		if err != nil {
			return err
		}
		res.Left = left
		if left == 0 {
			if err := merge.Stage(ctx, dir, path); err != nil {
				return fmt.Errorf("%w: %w", errNotStaged, err)
			}
			res.Staged = true
		}
		return nil
	})
	return res, err
}

// RestartConflictFile puts path back as the operation left it, markers
// included, discarding what was resolved in it.
func (a *App) RestartConflictFile(id, path string) error {
	if a.aiBusy(id) {
		return fmt.Errorf("%w; wait for it or stop it first", ErrChatBusy)
	}
	return a.writeMerge(id, func(ctx context.Context, dir string) error { return merge.Restart(ctx, dir, path) })
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
const MergeMaxSteps = 60

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
	// Before the merge checks: an off repository answers ErrAIOff first.
	cfg, o, err := a.aiSettingsFor(repoID)
	if err != nil {
		return err
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
	provider, err := a.chatProvider(cfg)
	if err != nil {
		return repoNote(err, o.ChatProvider != "")
	}

	a.ai.mu.Lock()
	if _, busy := a.ai.runs[repoID]; busy {
		a.ai.mu.Unlock()
		return ErrChatBusy
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.ai.runs[repoID] = cancel
	a.ai.mu.Unlock()
	a.cancelSuggestions(repoID)
	finish := func() {
		a.ai.mu.Lock()
		delete(a.ai.runs, repoID)
		a.ai.mu.Unlock()
		cancel()
	}
	if err := a.stillOn(repoID); err != nil {
		finish()
		return err
	}

	text := resolveRequest(st)
	history, err := a.ai.deps.Chats.Load(repoID)
	if err == nil {
		history = append(history, ai.Message{Role: ai.RoleUser, Content: text})
		err = a.ai.deps.Chats.Save(repoID, history)
	}
	var system string
	if err == nil {
		system, err = a.systemPrompt(repo, o, prompts.ResolveConflicts, prompts.Vars{
			Repo: repo.Name, Path: repo.Path, Branch: st.Into, Date: time.Now().Format("2006-01-02"),
		}, kindGuidance(st.Kind))
	}
	if err != nil {
		finish()
		return err
	}

	a.emit(agent.EventStart, agent.StartEvent{RepoID: repoID, RunID: runID, Text: text, Provider: cfg.ChatProvider, Model: cfg.ChatModel})

	nudge := &resolveNudge{dir: repo.Path, startedFor: startedFor}
	go func() {
		run := agent.Run{
			RepoID: repoID, RunID: runID,
			Provider: provider, Model: cfg.ChatModel, System: system,
			Tools:    append(mergetools.Specs(), tools.Specs()...),
			MaxSteps: MergeMaxSteps,
			Continue: nudge.next,
			RunTool: func(ctx context.Context, call ai.ToolCall, step int) string {
				nudge.saw(call)
				if isMergeTool(call.Name) {
					out := a.runMergeTool(ctx, repoID, startedFor, sides, call)
					nudge.carded(call, out)
					return out
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
			// A stopped answer also ends here, with context.Canceled.
			if runErr == nil {
				a.suggestReplies(repoID, runID, cfg, o)
			}
		}
	}()
	return nil
}

// MaxResolveNudges caps how often a resolve run is told to carry on after
// the model stopped with conflicts left.
const MaxResolveNudges = 2

// resolveNudge decides whether a resolve run whose model has stopped should
// be told to carry on, while the operation it was started for still has
// conflicted files and at most MaxResolveNudges times. The first time it
// always does: even a strong model leaves a region it could settle with a
// closer look. After that only when some conflicted file went unread since
// the last nudge — a model that stopped without looking. One that read
// every file left and still declined is leaving them on purpose; pressing
// it again only pushes it to guess.
type resolveNudge struct {
	dir, startedFor string
	asks            int
	read            map[string]bool // read_conflict paths since the last nudge
	cards           map[string]bool // path + "\x00" + region id left as cards
}

// carded notes a region the model left to the user as a card (a
// propose_options call the tool accepted).
func (n *resolveNudge) carded(call ai.ToolCall, result string) {
	if call.Name != "propose_options" || !strings.HasPrefix(result, shownMark) {
		return
	}
	path, _ := call.Args["path"].(string)
	region, _ := call.Args["region"].(string)
	if n.cards == nil {
		n.cards = map[string]bool{}
	}
	n.cards[path+"\x00"+region] = true
}

// allCarded reports whether every region still open in the conflicted
// files has a card: the model has nothing left to do.
func (n *resolveNudge) allCarded(conflicts []string) bool {
	for _, path := range conflicts {
		data, err := os.ReadFile(filepath.Join(n.dir, path))
		if err != nil {
			return false
		}
		hunks, err := merge.Parse(string(data))
		if err != nil || len(hunks) == 0 {
			return false
		}
		for _, h := range hunks {
			if !n.cards[path+"\x00"+h.ID] {
				return false
			}
		}
	}
	return true
}

// saw notes a tool call of the run (calls and Continue share its goroutine).
func (n *resolveNudge) saw(call ai.ToolCall) {
	if call.Name != "read_conflict" {
		return
	}
	if path, _ := call.Args["path"].(string); path != "" {
		if n.read == nil {
			n.read = map[string]bool{}
		}
		n.read[path] = true
	}
}

func (n *resolveNudge) next(ctx context.Context) string {
	if n.asks >= MaxResolveNudges || merge.Fingerprint(ctx, n.dir) != n.startedFor {
		return ""
	}
	st, err := merge.Status(ctx, n.dir)
	if err != nil || !st.Merging || len(st.Conflicts) == 0 {
		return ""
	}
	if n.allCarded(st.Conflicts) {
		return ""
	}
	if n.asks > 0 && !slices.ContainsFunc(st.Conflicts, func(p string) bool { return !n.read[p] }) {
		return ""
	}
	left, _ := mergetools.Run(ctx, n.dir, ai.ToolCall{Name: "list_conflicts"}, mergetools.Sides{})
	n.asks++
	n.read = nil
	return "CommitTree: you stopped, but git still reports these files in conflict:\n" + left +
		"\nCarry on with read_conflict, resolve_hunk and stage_file; a file whose regions are all resolved still needs stage_file. " +
		"If you are leaving a region for the user on purpose, call propose_options for it (or, if even the options are unclear, name it and say why in one line), then stop. " +
		"Regions you already left as cards are the user's: do not resolve them or propose them again."
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

// EventChatChoice tells the chat a propose_options card was answered: the
// tool's recorded result, which the card reads its state from.
const EventChatChoice = "chat:choice"

type ChatChoiceEvent struct {
	RepoID  string `json:"repoID"`
	CallID  string `json:"callID"`
	Summary string `json:"summary"`
}

// errNotStaged: the region was written, but staging the file failed.
var errNotStaged = errors.New("written, but the file could not be staged")

const (
	shownMark   = mergetools.CardShown
	choseMark   = "The user chose "
	settledMark = "Settled another way"
)

// ChooseRegionOption applies the user's pick on a propose_options card:
// option's text from the stored call, or text itself when option is -1.
// The choice is recorded on the call's tool result in the history, so the
// card shows it after a reload and the model sees it in a later chat.
// The chat slot is held throughout, so no run appends to the history
// between the load and the save.
func (a *App) ChooseRegionOption(repoID, callID string, option int, text string) (RegionResult, error) {
	if a.ai == nil {
		return RegionResult{}, ErrAIDisabled
	}
	a.ai.mu.Lock()
	if _, busy := a.ai.runs[repoID]; busy {
		a.ai.mu.Unlock()
		return RegionResult{}, fmt.Errorf("%w; wait for it or stop it first", ErrChatBusy)
	}
	a.ai.runs[repoID] = func() {}
	a.ai.mu.Unlock()
	defer func() {
		a.ai.mu.Lock()
		delete(a.ai.runs, repoID)
		a.ai.mu.Unlock()
	}()

	history, err := a.ai.deps.Chats.Load(repoID)
	if err != nil {
		return RegionResult{}, err
	}
	call, result := findCall(history, callID)
	if result < 0 || call.Name != "propose_options" {
		return RegionResult{}, fmt.Errorf("no such choice %q", callID)
	}
	switch c := history[result].Content; {
	case strings.HasPrefix(c, choseMark) || strings.HasPrefix(c, settledMark):
		return RegionResult{}, errors.New("this card was already decided")
	case !strings.HasPrefix(c, shownMark):
		// The tool refused these options; no card was shown.
		return RegionResult{}, fmt.Errorf("no such choice %q", callID)
	}
	path, _ := call.Args["path"].(string)
	region, _ := call.Args["region"].(string)
	label := "their own text"
	switch {
	case option >= 0:
		opts, ok := mergetools.ParseOptions(call.Args)
		if !ok || option >= len(opts) {
			return RegionResult{}, fmt.Errorf("the card has no option %d", option)
		}
		label, text = opts[option].Label, opts[option].Text
	case option != -1:
		return RegionResult{}, fmt.Errorf("the card has no option %d", option)
	}

	res, err := a.applyRegion(repoID, path, region, "text", text)
	var summary string
	switch {
	case errors.Is(err, merge.ErrNoSuchRegion) && a.twinOpen(repoID, path, region):
		return RegionResult{}, errors.New("that region moved when an identical one was resolved; settle it in the Merge view")
	case errors.Is(err, errNotStaged):
		// Written: record the choice, so a later Apply does not call it
		// settled some other way; still report the staging failure.
		summary = fmt.Sprintf("%s%q for %s (region %s); it was written, but the file could not be staged.", choseMark, label, path, region)
		history[result].Content = summary
		if saveErr := a.ai.deps.Chats.Save(repoID, history); saveErr == nil {
			a.emit(EventChatChoice, ChatChoiceEvent{RepoID: repoID, CallID: callID, Summary: summary})
		}
		a.emit(EventMergeChanged, MergeChangedEvent{RepoID: repoID})
		return res, err
	case errors.Is(err, merge.ErrNoSuchRegion) || (errors.Is(err, merge.ErrNotInMerge) && a.stillMerging(repoID)):
		summary = fmt.Sprintf("%s: region %s of %s is no longer in conflict.", settledMark, region, path)
		res = RegionResult{Settled: true}
	case err != nil:
		return RegionResult{}, err
	default:
		summary = fmt.Sprintf("%s%q for %s (region %s); it was written.", choseMark, label, path, region)
		if res.Staged {
			summary += " The file is resolved and staged."
		}
	}
	history[result].Content = summary
	if err := a.ai.deps.Chats.Save(repoID, history); err != nil {
		return res, err
	}
	a.emit(EventChatChoice, ChatChoiceEvent{RepoID: repoID, CallID: callID, Summary: summary})
	a.emit(EventMergeChanged, MergeChangedEvent{RepoID: repoID})
	return res, nil
}

// findCall finds the tool call callID in history and the index of its
// result message: the k-th tool message after the assistant message for
// its k-th call, the pairing the provider adapters use. result is -1 when
// the call or its result is missing.
func findCall(history []ai.Message, callID string) (call ai.ToolCall, result int) {
	result = -1
	for i, m := range history {
		if m.Role != ai.RoleAssistant {
			continue
		}
		for k, c := range m.ToolCalls {
			if c.ID != callID {
				continue
			}
			j := i + 1 + k
			if j < len(history) && history[j].Role == ai.RoleTool && history[j].ToolName == c.Name {
				call, result = c, j
			}
		}
	}
	return call, result
}

// twinOpen reports whether region was one of identical twins (its id
// carries its line, "hash@line") and a region with the same content is
// still open in path: the card's region may only have moved.
func (a *App) twinOpen(repoID, path, region string) bool {
	hash, _, twin := strings.Cut(region, "@")
	if !twin {
		return false
	}
	dir, err := a.dir(repoID)
	if err != nil {
		return false
	}
	data, err := os.ReadFile(filepath.Join(dir, path))
	if err != nil {
		return false
	}
	hunks, err := merge.Parse(string(data))
	if err != nil {
		return false
	}
	return slices.ContainsFunc(hunks, func(h merge.Hunk) bool {
		return h.ID == hash || strings.HasPrefix(h.ID, hash+"@")
	})
}

// stillMerging reports whether repoID has a merge, rebase or cherry-pick in progress.
func (a *App) stillMerging(repoID string) bool {
	dir, err := a.dir(repoID)
	if err != nil {
		return false
	}
	st, err := merge.Status(a.ctx, dir)
	return err == nil && st.Merging
}
