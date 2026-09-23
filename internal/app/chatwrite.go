package app

// Write tools in the chat: the model proposes, the user approves in the chat
// panel, and the change runs through the same App methods the UI uses —
// under the same per-repo write lock. The lock is NOT held while waiting for
// the user; the fingerprint re-check on approval is what keeps that safe.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"git-ui/internal/ai"
	"git-ui/internal/ai/writetools"
	"git-ui/internal/merge"
	"git-ui/internal/ops"
)

const (
	EventChatConfirm = "chat:confirm"
	EventRepoChanged = "repo:changed"
)

type ConfirmEvent struct {
	RepoID    string   `json:"repoID"`
	RunID     string   `json:"runID"`
	ConfirmID string   `json:"confirmID"`
	Tool      string   `json:"tool"`
	Title     string   `json:"title"`
	Details   []string `json:"details"`
}

type RepoChangedEvent struct {
	RepoID string `json:"repoID"`
}

type pendingConfirm struct {
	event  ConfirmEvent
	answer chan bool // buffered, capacity 1
}

func newConfirmID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// runWriteTool prepares a proposal for a write tool call, asks the user to
// approve it, and — on approval — re-checks and executes it. It is called
// synchronously from within the agent's tool loop, so it blocks that run
// (and only that run) until the user answers or the run is stopped.
func (a *App) runWriteTool(ctx context.Context, repoID, runID, dir string, call ai.ToolCall, env writetools.Env) string {
	p, err := writetools.Prepare(ctx, dir, call, env)
	if err != nil {
		return "error: " + err.Error()
	}

	id := newConfirmID()
	pc := &pendingConfirm{
		event: ConfirmEvent{
			RepoID: repoID, RunID: runID, ConfirmID: id,
			Tool: call.Name, Title: p.Title, Details: p.Details,
		},
		answer: make(chan bool, 1),
	}
	a.ai.mu.Lock()
	a.ai.confirms[id] = pc
	a.ai.mu.Unlock()
	a.emit(EventChatConfirm, pc.event)

	var approved bool
	select {
	case approved = <-pc.answer:
	case <-ctx.Done():
	}
	a.ai.mu.Lock()
	delete(a.ai.confirms, id)
	a.ai.mu.Unlock()

	if !approved {
		return "rejected by the user"
	}
	if err := writetools.Recheck(ctx, dir, p, call.Name); err != nil {
		return "error: " + err.Error()
	}
	done, err := a.executeWrite(repoID, call.Name, p)
	if errors.Is(err, ErrBusy) {
		return "error: another operation is running in this repository; try again when it finishes"
	}
	if err != nil {
		return "error: " + err.Error()
	}
	a.emit(EventRepoChanged, RepoChangedEvent{RepoID: repoID})
	return "done: " + done
}

// executeWrite runs the approved proposal through the same App method the UI
// uses, and returns a short past-tense description of what happened.
func (a *App) executeWrite(repoID, tool string, p writetools.Proposal) (string, error) {
	switch tool {
	case "stage_files":
		for i, path := range p.Paths {
			if err := a.StageFile(repoID, path); err != nil {
				return "", stagedSoFar(p.Paths[:i], "staged", err)
			}
		}
		return p.Title, nil
	case "unstage_files":
		for i, path := range p.Paths {
			if err := a.UnstageFile(repoID, path); err != nil {
				return "", stagedSoFar(p.Paths[:i], "unstaged", err)
			}
		}
		return p.Title, nil
	case "commit":
		if err := a.CommitChanges(repoID, p.Message, false); err != nil {
			return "", err
		}
		return p.Title, nil
	case "create_branch":
		if err := a.CreateBranch(repoID, p.Name, p.Start, p.Checkout); err != nil {
			return "", err
		}
		return p.Title, nil
	case "checkout_branch":
		var err error
		if p.Remote != "" {
			err = a.CheckoutRemote(repoID, p.Remote, p.Name)
		} else {
			err = a.Checkout(repoID, p.Name)
		}
		if err != nil {
			return "", err
		}
		return p.Title, nil
	case "stash_push":
		if err := a.StashPush(repoID, p.Message, p.IncludeUntracked); err != nil {
			return "", err
		}
		return p.Title, nil
	case "fetch":
		if err := a.Fetch(repoID); err != nil {
			return "", err
		}
		return p.Title, nil
	case "push":
		if err := a.Push(repoID); err != nil {
			return "", err
		}
		return p.Title, nil
	case "pull":
		result, err := a.Pull(repoID)
		if err != nil {
			return "", err
		}
		if result.Outcome == ops.Conflicted {
			return fmt.Sprintf("pull stopped with conflicts in %d file(s); the conflict view is open — resolve them there", len(result.Conflicts)), nil
		}
		return p.Title, nil
	case "merge_branch":
		result, err := a.MergeBranch(repoID, p.Branch)
		if err != nil {
			return "", err
		}
		if result.Outcome == merge.Conflicted {
			return fmt.Sprintf("merge stopped with conflicts in %d file(s); the conflict view is open — resolve them there", len(result.Conflicts)), nil
		}
		return p.Title, nil
	default:
		return "", fmt.Errorf("unknown tool %q", tool)
	}
}

// stagedSoFar reports a partial stage/unstage failure, naming the paths that
// already succeeded before the error.
func stagedSoFar(done []string, verb string, err error) error {
	if len(done) == 0 {
		return err
	}
	joined := ""
	for i, p := range done {
		if i > 0 {
			joined += ", "
		}
		joined += p
	}
	return fmt.Errorf("%s %s; then %w", verb, joined, err)
}

// ConfirmChatAction approves or rejects a pending write proposal.
func (a *App) ConfirmChatAction(repoID, confirmID string, approve bool) error {
	if a.ai == nil {
		return ErrAIDisabled
	}
	a.ai.mu.Lock()
	pc, ok := a.ai.confirms[confirmID]
	if ok && pc.event.RepoID != repoID {
		ok = false
	}
	if ok {
		delete(a.ai.confirms, confirmID)
	}
	a.ai.mu.Unlock()
	if !ok {
		return fmt.Errorf("no pending confirmation %q", confirmID)
	}
	pc.answer <- approve
	return nil
}

// GetChatConfirm returns the pending confirmation for repoID, or nil.
func (a *App) GetChatConfirm(repoID string) *ConfirmEvent {
	if a.ai == nil {
		return nil
	}
	a.ai.mu.Lock()
	defer a.ai.mu.Unlock()
	for _, pc := range a.ai.confirms {
		if pc.event.RepoID == repoID {
			ev := pc.event
			return &ev
		}
	}
	return nil
}
