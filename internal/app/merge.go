package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"git-ui/internal/ai"
	"git-ui/internal/ai/agent"
	"git-ui/internal/ai/mergetools"
	"git-ui/internal/ai/ollama"
	"git-ui/internal/ai/prompts"
	"git-ui/internal/ai/tools"
	"git-ui/internal/gitcmd"
	"git-ui/internal/merge"
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

func (a *App) AbortMerge(id string) error {
	return a.write(id, func(ctx context.Context, dir string) error { return merge.Abort(ctx, dir) })
}

func (a *App) CommitMerge(id string) error {
	return a.write(id, func(ctx context.Context, dir string) error { return merge.Commit(ctx, dir) })
}

// mergePaths lists every path this merge touches: the ones still unmerged,
// and the ones already staged into it. Paths come from git verbatim, so a
// caller's path is accepted only when it matches one exactly — a crafted
// pathspec such as ":(glob)*" can never equal one.
func mergePaths(ctx context.Context, dir string, st merge.State) (map[string]bool, error) {
	paths := map[string]bool{}
	for _, p := range st.Conflicts {
		paths[p] = true
	}
	for _, p := range st.Manual {
		paths[p] = true
	}
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "diff", "--cached", "--name-only", "-z", "HEAD")
	if err != nil {
		return nil, err
	}
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			paths[p] = true
		}
	}
	return paths, nil
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
	// Build the set of paths this merge touches; accept only exact matches.
	paths, err := mergePaths(a.ctx, dir, st)
	if err != nil {
		return ConflictFile{}, err
	}
	if !paths[path] {
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
			return ConflictFile{Path: path, Text: "This file has no conflict markers to edit here. Resolve it in your editor."}, nil
		}
	}
	// Otherwise it is staged into the merge: return the diff.
	out, err := gitcmd.Run(a.ctx, dir, gitcmd.ReadTimeout, "diff", "--cached", "--", path)
	if err != nil {
		return ConflictFile{}, err
	}
	return ConflictFile{Path: path, Resolved: true, Text: out}, nil
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
	repo, ok := a.store.Get(repoID)
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
	cfg, err := a.aiSettings()
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

	text := fmt.Sprintf("Resolve the conflicts from merging %s into %s", st.From, st.Into)
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
	}
	if err != nil {
		finish()
		return err
	}

	a.emit(agent.EventStart, agent.StartEvent{RepoID: repoID, RunID: runID, Text: text})

	go func() {
		run := agent.Run{
			RepoID: repoID, RunID: runID,
			Provider: ollama.New(cfg.OllamaURL), Model: cfg.ChatModel, System: system,
			Tools:    append(mergetools.Specs(), tools.Specs()...),
			MaxSteps: MergeMaxSteps,
			RunTool: func(ctx context.Context, call ai.ToolCall) string {
				if isMergeTool(call.Name) {
					out, changed := mergetools.Run(ctx, repo.Path, call)
					if changed {
						a.emit(EventMergeChanged, MergeChangedEvent{RepoID: repoID})
					}
					return out
				}
				return tools.Run(ctx, repo.Path, call)
			},
			Emit: a.emit,
		}
		updated, runErr := agent.Execute(ctx, run, history)
		saveErr := a.ai.deps.Chats.Save(repoID, updated)
		// Release the repo before announcing the end so a new message can be
		// sent, or the merge acted on, as soon as the frontend sees done/error.
		finish()
		a.emit(EventMergeChanged, MergeChangedEvent{RepoID: repoID})
		switch {
		case runErr != nil && !errors.Is(runErr, context.Canceled):
			a.emit(agent.EventError, agent.ErrorEvent{RepoID: repoID, RunID: runID, Message: runErr.Error(), Code: chatErrorCode(runErr)})
		case saveErr != nil:
			a.emit(agent.EventError, agent.ErrorEvent{RepoID: repoID, RunID: runID, Message: saveErr.Error(), Code: "other"})
		default:
			a.emit(agent.EventDone, agent.DoneEvent{RepoID: repoID, RunID: runID})
		}
	}()
	return nil
}

func isMergeTool(name string) bool {
	for _, spec := range mergetools.Specs() {
		if spec.Name == name {
			return true
		}
	}
	return false
}
