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
	"git-ui/internal/gitcmd"
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
func (a *App) runWriteTool(ctx context.Context, repoID, runID, dir string, call ai.ToolCall) string {
	// Read fresh, not once for the whole SendChat run: a pull proposed early
	// in a long-running chat should use the strategy configured by the time
	// the user actually approves it, not the one in effect when the answer
	// started.
	gs, err := a.gitSettings()
	if err != nil {
		return "error: " + err.Error()
	}
	p, err := writetools.Prepare(ctx, dir, call, writetools.Env{PullStrategy: gs.PullStrategy})
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
	// Recheck runs just before the App method below takes the per-repo
	// write lock: the repository could still move in that gap (a millisecond
	// window), but it's the same window every other write path has between
	// its own precondition check and taking the lock, and is accepted here
	// too.
	if err := writetools.Recheck(ctx, dir, p, call.Name); err != nil {
		return "error: " + err.Error()
	}
	unmark := a.markAIWrite(repoID)
	done, err := a.executeWrite(repoID, call.Name, p)
	unmark()
	if errors.Is(err, ErrBusy) {
		return "error: another operation is running in this repository; try again when it finishes"
	}
	// Emitted whenever execution was attempted, even on error: a partial
	// effect (branch created but checkout failed, some paths staged before
	// a failure, fetch succeeded before a pull error) can still have
	// changed the repository, and the UI needs to refresh to show it.
	a.emit(EventRepoChanged, RepoChangedEvent{RepoID: repoID})
	if err != nil {
		return "error: " + err.Error()
	}
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
			// CreateBranch creates the branch, then (with Checkout) switches
			// to it: a failure can be the switch alone, after the branch
			// already exists. Say so, or "nothing happened" would be wrong.
			if p.Checkout && a.branchExists(repoID, p.Name) {
				return "", fmt.Errorf("created branch %s but could not switch to it: %w", p.Name, err)
			}
			return "", err
		}
		return p.Title, nil
	case "checkout_branch":
		if p.Remote == "" {
			if err := a.Checkout(repoID, p.Name); err != nil {
				return "", err
			}
			return p.Title, nil
		}
		outcome, err := a.CheckoutRemote(repoID, p.Remote, p.Name)
		if err != nil {
			return "", err
		}
		switch ops.CheckoutOutcome(outcome) {
		case ops.CheckoutFastForwarded:
			return fmt.Sprintf("%s (fast-forwarded %s to %s/%s)", p.Title, p.Name, p.Remote, p.Name), nil
		case ops.CheckoutDiverged:
			return fmt.Sprintf("%s (local %s has commits not on %s/%s; it was left as it was)", p.Title, p.Name, p.Remote, p.Name), nil
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
	case "cherry_pick":
		result, err := a.CherryPick(repoID, p.Commit)
		if err != nil {
			return "", err
		}
		switch result.Outcome {
		case merge.Conflicted:
			return fmt.Sprintf("cherry-pick stopped with conflicts in %d file(s); the conflict view is open — resolve them there", len(result.Conflicts)), nil
		case merge.NothingToApply:
			return "nothing to apply: those changes are already on the branch", nil
		}
		return p.Title, nil
	default:
		return "", fmt.Errorf("unknown tool %q", tool)
	}
}

// branchExists reports whether repoID now has a local branch called name.
func (a *App) branchExists(repoID, name string) bool {
	dir, err := a.dir(repoID)
	if err != nil {
		return false
	}
	_, err = gitcmd.Run(a.ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", "refs/heads/"+name)
	return err == nil
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
