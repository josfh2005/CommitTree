# AI Decision Cards Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The AI conflict resolver can leave a genuinely contradictory region as a chat card of concrete options; the user picks one (or types their own) and the app writes it through part A's region apply, without committing.

**Architecture:** A new non-blocking resolver tool `propose_options` validates the options and returns at once. The card is rebuilt from that call's stored args. `App.ChooseRegionOption(repoID, callID, option, text)` reads the option text from the chat history, applies it with the shared region-apply helper (staging on the last region), rewrites the paired tool message to record the choice, and emits `chat:choice`.

**Tech Stack:** Go 1.2x (Wails v2 app), Svelte 4 + TypeScript, vitest, Python 3 (lab grader).

**Spec:** `docs/superpowers/specs/2026-09-30-ai-decision-cards-design.md`

## Global Constraints

- Only the conflict resolver gets `propose_options` (it lives in `mergetools.Specs()`); the chat's tools are unchanged.
- Regions with markers only; no cards for files without markers.
- Non-blocking: the tool never waits for the user.
- 2–4 options per card; "Other…" is added by the UI, never by the model.
- Recorded tool-message prefixes (exact): `The user chose ` and `Settled another way`.
- Nothing is committed by any of this.
- Behaviour changes update `docs/spec/*` in the same commit.
- Merges into main use `git merge --no-ff`.
- After an applied change: rebuild and reopen the app (`make dev`, node 22).
- Commit messages carry no `Co-Authored-By` line.

## Review Focus

- A chat history holding two runs whose provider gave no ids: ids must still be unique across runs, or a click on the second card applies the first card's options (Task 1 test: ids carry the run id).
- The user clicks Apply on a card whose region the Merge view already resolved: the card turns "Settled another way", no error toast (Task 3 test).
- The user clicks Apply twice fast / on two windows: second call is refused, file written once (Task 3 test: second choice refused).
- A run starts (Resolve with AI / chat) while ChooseRegionOption is saving the history: the choice must not be lost nor the run's messages clobbered — ChooseRegionOption reserves the chat slot in `a.ai.runs` for its whole duration, so SendChat/ResolveConflicts get ErrChatBusy meanwhile (Task 3 tests: refused while a run holds the slot; slot released afterwards, also on error paths via `defer`).
- A card whose option text is empty (delete side of case 9): accepted by the tool, shown as "(removes the region)", written as nothing (Task 2 and Task 4 tests).

---

### Task 1: Unique tool call ids, carried on `chat:tool`

**Files:**
- Modify: `internal/ai/agent/agent.go` (ToolEvent; id assignment in `Execute`, including the recovered-calls branch)
- Test: `internal/ai/agent/agent_test.go`

**Interfaces:**
- Produces: every stored/emitted tool call has a non-empty `ID` unique within a chat history; `agent.ToolEvent` gains `ID string \`json:"id"\``.

Today `Execute` fills a missing id with `call_<step>_<i>` and recovered calls get `recovered_<step>_<i>`: both repeat across runs of the same history. Prefix them with the run id.

- [ ] **Step 1: Write the failing test** — read `agent_test.go` first and reuse its fake provider helper (the one that returns scripted chunks). Add:

```go
func TestExecuteGivesToolCallsIDsUniqueAcrossRuns(t *testing.T) {
	var ids []string
	var emitted []string
	for _, runID := range []string{"run-a", "run-b"} {
		// Provider: first response one tool call with no ID, then a plain answer.
		p := scripted( /* use this file's helper: step 0 → ToolCalls: []ai.ToolCall{{Name: "list_refs"}}, Done; step 1 → Delta "ok", Done */ )
		r := agent.Run{
			RepoID: "r", RunID: runID, Provider: p, Model: "m",
			RunTool: func(ctx context.Context, c ai.ToolCall, step int) string { return "x" },
			Emit: func(name string, data any) {
				if ev, ok := data.(agent.ToolEvent); ok {
					emitted = append(emitted, ev.ID)
				}
			},
		}
		msgs, err := agent.Execute(context.Background(), r, []ai.Message{{Role: ai.RoleUser, Content: "hi"}})
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range msgs {
			for _, c := range m.ToolCalls {
				ids = append(ids, c.ID)
			}
		}
	}
	if len(ids) != 2 || ids[0] == "" || ids[0] == ids[1] {
		t.Fatalf("ids = %v, want two distinct non-empty ids", ids)
	}
	if !slices.Equal(emitted, ids) {
		t.Fatalf("emitted ids %v, stored %v", emitted, ids)
	}
}

func TestExecuteKeepsAProviderToolCallID(t *testing.T) {
	// Same shape, provider sends ToolCalls: []ai.ToolCall{{ID: "toolu_1", Name: "list_refs"}}.
	// Assert the stored call's ID is exactly "toolu_1" and the ToolEvent carries "toolu_1".
}
```

Write the second test out fully in the same style (it is the first test with one run and a provider-set id).

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/ai/agent/ -run 'TestExecuteGivesToolCallsIDsUniqueAcrossRuns|TestExecuteKeepsAProviderToolCallID' -v`
Expected: FAIL — `ToolEvent` has no field `ID` (compile error).

- [ ] **Step 3: Implement**

In `ToolEvent` add after `Name`:

```go
	// ID is the call's id, unique within the chat's history: a card built
	// from the call (propose_options) is answered by it.
	ID   string         `json:"id"`
```

In `Execute`, change the two id formats:

```go
					recovered[i].ID = fmt.Sprintf("recovered_%s_%d_%d", r.RunID, step, i)
```
```go
			if calls[i].ID == "" {
				calls[i].ID = fmt.Sprintf("call_%s_%d_%d", r.RunID, step, i)
			}
```

and the emit:

```go
			r.Emit(EventTool, ToolEvent{RepoID: r.RepoID, RunID: r.RunID, ID: call.ID, Name: call.Name, Args: call.Args})
```

Check with `grep -rn "recovered_\|call_%d" internal` that no test or adapter depends on the old formats; update any test expectation that does.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ai/... ./internal/app/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ai/agent
git commit -m "feat(agent): tool call ids unique across runs, sent on chat:tool"
```

---

### Task 2: `propose_options` tool and the resolver prompt

**Files:**
- Modify: `internal/ai/mergetools/mergetools.go` (spec, `Run` case, `proposeOptions`)
- Modify: `internal/ai/prompts/defaults/resolve-conflicts.md`
- Modify: `internal/app/merge.go` (`resolveNudge.next` message)
- Modify: `docs/spec/06-ai.md` (section "The AI conflict resolver": tool list + when it proposes)
- Test: `internal/ai/mergetools/mergetools_test.go`, `internal/ai/prompts/prompts_test.go` only if a test pins the prompt text

**Interfaces:**
- Consumes: `open()`, `checkResolution()`, `merge.HasMarkers` (already in package).
- Produces: tool name `"propose_options"`; args `path string`, `region string`, `question string`, `options []{label string, text string}` (arrives as `[]any` of `map[string]any`); result prefix `"Shown to the user as a card"`; `changed` always false. Exported helper for Task 3:

```go
// Option is one choice of a propose_options card.
type Option struct{ Label, Text string }

// ParseOptions reads a propose_options call's options argument. ok is false
// when it is not a list of {label, text} objects with string fields.
func ParseOptions(args map[string]any) (opts []Option, ok bool)
```

- [ ] **Step 1: Write the failing tests** (use the file's `conflicted(t)` fixture: greeting.txt with ours `hi\n`, theirs `hola\n`; get the region id from `read_conflict`'s output line `region id <id>` or from `merge.Parse` of the file):

```go
func regionID(t *testing.T, dir, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, path))
	if err != nil {
		t.Fatal(err)
	}
	hunks, err := merge.Parse(string(data))
	if err != nil || len(hunks) == 0 {
		t.Fatalf("hunks = %v, %v", hunks, err)
	}
	return hunks[0].ID
}

func opts(pairs ...string) []any {
	var out []any
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, map[string]any{"label": pairs[i], "text": pairs[i+1]})
	}
	return out
}

func propose(t *testing.T, r *testrepo.Repo, args map[string]any) (string, bool) {
	t.Helper()
	return mergetools.Run(context.Background(), r.Dir, call("propose_options", args), mergetools.Sides{})
}

func TestProposeOptionsAcceptsValidOptionsAndWritesNothing(t *testing.T) {
	r := conflicted(t)
	before, _ := os.ReadFile(filepath.Join(r.Dir, "greeting.txt"))
	out, changed := propose(t, r, map[string]any{
		"path": "greeting.txt", "region": regionID(t, r.Dir, "greeting.txt"),
		"question": "Which greeting?",
		"options":  opts("hi (main)", "hi\n", "hola (feature)", "hola\n", "neither", ""),
	})
	if changed || !strings.HasPrefix(out, "Shown to the user as a card with 3 options") {
		t.Fatalf("out = %q, changed = %v", out, changed)
	}
	after, _ := os.ReadFile(filepath.Join(r.Dir, "greeting.txt"))
	if string(after) != string(before) {
		t.Fatal("the file changed")
	}
}

func TestProposeOptionsRefusals(t *testing.T) {
	r := conflicted(t)
	id := regionID(t, r.Dir, "greeting.txt")
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"unknown region", map[string]any{"path": "greeting.txt", "region": "deadbeef", "question": "q", "options": opts("a", "hi\n", "b", "hola\n")}, "has no region deadbeef"},
		{"not conflicted", map[string]any{"path": "nope.txt", "region": id, "question": "q", "options": opts("a", "hi\n", "b", "hola\n")}, "is not a conflicted file"},
		{"one option", map[string]any{"path": "greeting.txt", "region": id, "question": "q", "options": opts("a", "hi\n")}, "2 to 4 options"},
		{"five options", map[string]any{"path": "greeting.txt", "region": id, "question": "q", "options": opts("a", "1\n", "b", "2\n", "c", "3\n", "d", "4\n", "e", "5\n")}, "2 to 4 options"},
		{"empty label", map[string]any{"path": "greeting.txt", "region": id, "question": "q", "options": opts(" ", "hi\n", "b", "hola\n")}, "needs a label"},
		{"repeated label", map[string]any{"path": "greeting.txt", "region": id, "question": "q", "options": opts("a", "hi\n", "a", "hola\n")}, "same label"},
		{"repeated text", map[string]any{"path": "greeting.txt", "region": id, "question": "q", "options": opts("a", "hi\n", "b", "hi\n")}, "same text"},
		{"markers", map[string]any{"path": "greeting.txt", "region": id, "question": "q", "options": opts("a", "<<<<<<< x\nhi\n", "b", "hola\n")}, "conflict markers"},
		{"malformed", map[string]any{"path": "greeting.txt", "region": id, "question": "q", "options": "hi or hola"}, "list of {label, text}"},
		{"no question", map[string]any{"path": "greeting.txt", "region": id, "options": opts("a", "hi\n", "b", "hola\n")}, "needs a question"},
	}
	for _, c := range cases {
		out, changed := propose(t, r, c.args)
		if changed || !strings.Contains(out, c.want) {
			t.Errorf("%s: out = %q, want it to contain %q", c.name, out, c.want)
		}
	}
}
```

Add one more test for `checkResolution` reuse, using the file's `twoRegions(t)` or `conflictedFile(t, name, base, ours, theirs)` fixture with a line before the region: an option whose text starts with the "lines before" line is refused with `Not applied: your resolution starts with` — the message is reused, prefixed by `Option "<label>": `.

Also extend `TestSpecsCoverTheFourTools` to include `"propose_options"` (rename it `TestSpecsCoverTheTools`).

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/ai/mergetools/ -run 'Propose|Specs' -v`
Expected: FAIL — `Unknown tool "propose_options"`.

- [ ] **Step 3: Implement**

Spec entry, after `resolve_hunk`:

```go
		{
			Name:        "propose_options",
			Description: "Leave one region for the user to decide, as a card of 2 to 4 concrete options. Use it only when the two sides genuinely contradict each other. Each option's text is exactly what would replace the region, like resolve_hunk's resolved. It returns at once; do not resolve that region yourself, carry on with the rest.",
			Parameters: object(map[string]any{
				"path":     str("File path."),
				"region":   str("The region's id, as read_conflict showed it."),
				"question": str("One line: what the user has to decide and why it is their call."),
				"options": map[string]any{
					"type":        "array",
					"description": "2 to 4 choices, usually each side and, when one makes sense, a combination.",
					"items": object(map[string]any{
						"label": str("Short name of the choice, e.g. \"45000 (develop)\"."),
						"text":  str("The region's replacement, exactly as it would be written. Empty removes the region."),
					}, "label", "text"),
				},
			}, "path", "region", "question", "options"),
		},
```

`Run`: `case "propose_options": return proposeOptions(ctx, dir, call.Args), false`

```go
// Option is one choice of a propose_options card.
type Option struct{ Label, Text string }

// ParseOptions reads a propose_options call's options argument. ok is false
// when it is not a list of {label, text} objects with string fields.
func ParseOptions(args map[string]any) ([]Option, bool) {
	list, ok := args["options"].([]any)
	if !ok {
		return nil, false
	}
	out := make([]Option, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, false
		}
		label, ok1 := m["label"].(string)
		text, ok2 := m["text"].(string)
		if !ok1 || !ok2 {
			return nil, false
		}
		out = append(out, Option{Label: label, Text: text})
	}
	return out, true
}

// proposeOptions validates a card for the user; it writes nothing. The app
// shows it from the stored call and applies the chosen text later.
func proposeOptions(ctx context.Context, dir string, args map[string]any) string {
	path, hunks, msg := open(ctx, dir, args)
	if msg != "" {
		return msg
	}
	id, _ := args["region"].(string)
	i := slices.IndexFunc(hunks, func(h merge.Hunk) bool { return h.ID == id })
	if i < 0 {
		return fmt.Sprintf("Not shown: %s has no region %s (already resolved, or never there). Call read_conflict for the current regions.", path, id)
	}
	if q, _ := args["question"].(string); strings.TrimSpace(q) == "" {
		return "Not shown: the card needs a question — one line saying what the user has to decide."
	}
	opts, ok := ParseOptions(args)
	if !ok {
		return "Not shown: options must be a list of {label, text} objects."
	}
	if len(opts) < 2 || len(opts) > 4 {
		return fmt.Sprintf("Not shown: a card has 2 to 4 options, not %d.", len(opts))
	}
	labels, texts := map[string]bool{}, map[string]bool{}
	for _, o := range opts {
		label := strings.TrimSpace(o.Label)
		switch {
		case label == "":
			return "Not shown: every option needs a label."
		case labels[label]:
			return fmt.Sprintf("Not shown: two options have the same label %q.", label)
		case texts[o.Text]:
			return fmt.Sprintf("Not shown: option %q has the same text as another option.", label)
		case merge.HasMarkers(o.Text):
			return fmt.Sprintf("Not shown: option %q contains conflict markers.", label)
		}
		if m := checkResolution(hunks[i], o.Text); m != "" {
			return fmt.Sprintf("Option %q: %s", label, m)
		}
		labels[label], texts[o.Text] = true, true
	}
	return fmt.Sprintf("Shown to the user as a card with %d options; they will choose after you finish. Do not resolve this region yourself; carry on with the rest.", len(opts))
}
```

Adjust the test's expected substrings to these messages exactly ("2 to 4 options", "needs a label", "same label", "same text", "conflict markers", "list of {label, text}", "needs a question", "has no region deadbeef") — they already match the code above. Note `merge.HasMarkers` must flag a line starting `<<<<<<< `; confirm by reading `internal/merge` and, if it only checks whole files the same way, it is fine for one text.

- [ ] **Step 4: Prompt and nudge.** In `resolve-conflicts.md` replace the second paragraph:

```
Work on your own until every conflict you can settle is settled. The user
is watching and will review your work before anything is committed.
```

and replace the bullet "Only pick one side when the two changes genuinely contradict each other." with:

```
- When the two changes genuinely contradict each other (two different
  values for the same setting, one side deleting what the other edited),
  do not pick a side: call `propose_options` for that region with the
  exact text of each real alternative — usually each side, and a
  combination when one makes sense — then carry on with the rest. The
  user chooses in the chat.
```

In the final paragraph change "then what you left alone and what the user needs to decide" to "then which regions you left for the user to choose in the chat, and anything else you left alone". Keep the "If you cannot tell…" bullet.

Because the stored prompt copy shadows the default only when the user edited it (see `prompts.go` "stale copies"), no migration is needed; run `go test ./internal/ai/prompts/` in case a test pins the text and update the pinned text if so.

In `internal/app/merge.go` `resolveNudge.next`, change the last sentence to:

```go
		"If you are leaving a region for the user on purpose, call propose_options for it (or, if even the options are unclear, name it and say why in one line), then stop."
```

- [ ] **Step 5: Docs.** In `docs/spec/06-ai.md` section "The AI conflict resolver", add `propose_options` to the tool list with one paragraph: what it takes, that it writes nothing and returns at once, that it is the resolver's way to leave a real contradiction to the user, and a pointer "the card is described in 04-conflicts.md". Update any sentence there saying the resolver never asks the user.

- [ ] **Step 6: Run tests**

Run: `go test ./internal/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/ai/mergetools internal/ai/prompts internal/app/merge.go docs/spec/06-ai.md
git commit -m "feat(ai): propose_options lets the resolver leave a contradiction as a card of options"
```

---

### Task 3: `ChooseRegionOption` in the app

**Files:**
- Modify: `internal/app/merge.go` (extract `applyRegion`; add `ChooseRegionOption`, `EventChatChoice`, `ChatChoiceEvent`)
- Modify: `frontend/wailsjs/go/app/App.js`, `frontend/wailsjs/go/app/App.d.ts` (regenerated bindings)
- Test: `internal/app/merge_test.go`

**Interfaces:**
- Consumes: `mergetools.ParseOptions`, `mergetools.Option` (Task 2); tool call ids (Task 1); `merge.ErrNoSuchRegion`; `a.ai.deps.Chats.Load/Save`; `a.ai.runs`.
- Produces:

```go
const EventChatChoice = "chat:choice"

type ChatChoiceEvent struct {
	RepoID  string `json:"repoID"`
	CallID  string `json:"callID"`
	Summary string `json:"summary"`
}

// ChooseRegionOption applies option (or, with option -1, text) of the
// propose_options card callID.
func (a *App) ChooseRegionOption(repoID, callID string, option int, text string) (RegionResult, error)
```

Recorded tool-message texts (exact):
- chosen: `The user chose "<label>" for <path> (region <id>); it was written.` plus ` The file is resolved and staged.` when staged. For option -1 the label is `their own text`.
- settled: `Settled another way: region <id> of <path> is no longer in conflict.`

- [ ] **Step 1: Write the failing tests** in `merge_test.go` (fixture `newAIMergeApp`: greeting.txt, ours `hi\n`, theirs `hola\n`). A helper stores a history with a card:

```go
// withCard stores a chat history whose resolver answer proposed options for
// greeting.txt's region, and returns the call id.
func withCard(t *testing.T, a *App, id string) string {
	t.Helper()
	f, err := a.GetConflictFile(id, "greeting.txt")
	if err != nil || len(f.Regions) != 1 {
		t.Fatalf("file = %+v, %v", f, err)
	}
	callID := "call_run1_0_0"
	history := []ai.Message{
		{Role: ai.RoleUser, Content: "resolve"},
		{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{
			{ID: "call_run1_0_1", Name: "list_conflicts"}, // an earlier call in the same response
			{ID: callID, Name: "propose_options", Args: map[string]any{
				"path": "greeting.txt", "region": f.Regions[0].ID, "question": "Which?",
				"options": []any{
					map[string]any{"label": "hi (main)", "text": "hi\n"},
					map[string]any{"label": "hola (feature)", "text": "hola\n"},
				},
			}},
		}},
		{Role: ai.RoleTool, ToolName: "list_conflicts", Content: "greeting.txt — 1 conflict(s)"},
		{Role: ai.RoleTool, ToolName: "propose_options", Content: "Shown to the user as a card with 2 options; …"},
		{Role: ai.RoleAssistant, Content: "Left greeting.txt for you."},
	}
	if err := a.ai.deps.Chats.Save(id, history); err != nil {
		t.Fatal(err)
	}
	return callID
}

func toolContent(t *testing.T, a *App, id string, idx int) string {
	t.Helper()
	h, err := a.ai.deps.Chats.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	return h[idx].Content
}

func TestChooseRegionOptionWritesStagesAndRecords(t *testing.T) {
	a, r, id, ev := newAIMergeApp(t, "http://127.0.0.1:0")
	if _, err := a.MergeBranch(id, "feature"); err != nil {
		t.Fatal(err)
	}
	callID := withCard(t, a, id)
	res, err := a.ChooseRegionOption(id, callID, 1, "")
	if err != nil || !res.Staged {
		t.Fatalf("res = %+v, %v", res, err)
	}
	if data, _ := os.ReadFile(filepath.Join(r.Dir, "greeting.txt")); string(data) != "hola\n" {
		t.Fatalf("file = %q", data)
	}
	want := `The user chose "hola (feature)" for greeting.txt (region `
	if got := toolContent(t, a, id, 3); !strings.HasPrefix(got, want) || !strings.HasSuffix(got, "The file is resolved and staged.") {
		t.Fatalf("tool message = %q", got)
	}
	if got := toolContent(t, a, id, 2); got != "greeting.txt — 1 conflict(s)" {
		t.Fatalf("the other call's result changed: %q", got)
	}
	e := ev.wait(t, EventChatChoice)
	if c := e.data.(ChatChoiceEvent); c.CallID != callID || !strings.HasPrefix(c.Summary, "The user chose") {
		t.Fatalf("event = %+v", c)
	}
	ev.wait(t, EventMergeChanged)
	// The slot was only reserved while choosing.
	if a.aiBusy(id) {
		t.Fatal("chat slot still held")
	}
}

func TestChooseRegionOptionOwnText(t *testing.T) {
	a, r, id, _ := newAIMergeApp(t, "http://127.0.0.1:0")
	if _, err := a.MergeBranch(id, "feature"); err != nil {
		t.Fatal(err)
	}
	callID := withCard(t, a, id)
	if _, err := a.ChooseRegionOption(id, callID, -1, "hey\n"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(r.Dir, "greeting.txt")); string(data) != "hey\n" {
		t.Fatalf("file = %q", data)
	}
	if got := toolContent(t, a, id, 3); !strings.HasPrefix(got, `The user chose "their own text"`) {
		t.Fatalf("tool message = %q", got)
	}
}

func TestChooseRegionOptionOnASettledRegion(t *testing.T) {
	a, _, id, ev := newAIMergeApp(t, "http://127.0.0.1:0")
	if _, err := a.MergeBranch(id, "feature"); err != nil {
		t.Fatal(err)
	}
	callID := withCard(t, a, id)
	f, _ := a.GetConflictFile(id, "greeting.txt")
	if _, err := a.ResolveMergeRegion(id, "greeting.txt", f.Regions[0].ID, "ours", ""); err != nil {
		t.Fatal(err)
	}
	// Resolving staged it; the path is no longer a conflict, so the region
	// is gone either way.
	if _, err := a.ChooseRegionOption(id, callID, 0, ""); err != nil {
		t.Fatalf("err = %v, want nil (settled another way)", err)
	}
	if got := toolContent(t, a, id, 3); !strings.HasPrefix(got, "Settled another way") {
		t.Fatalf("tool message = %q", got)
	}
	ev.wait(t, EventChatChoice)
}

func TestChooseRegionOptionRefusals(t *testing.T) {
	a, _, id, _ := newAIMergeApp(t, "http://127.0.0.1:0")
	if _, err := a.MergeBranch(id, "feature"); err != nil {
		t.Fatal(err)
	}
	callID := withCard(t, a, id)
	if _, err := a.ChooseRegionOption(id, "nope", 0, ""); err == nil || !strings.Contains(err.Error(), "no such choice") {
		t.Errorf("unknown id: %v", err)
	}
	if _, err := a.ChooseRegionOption(id, callID, 2, ""); err == nil || !strings.Contains(err.Error(), "no option 2") {
		t.Errorf("out of range: %v", err)
	}
	if _, err := a.ChooseRegionOption(id, "call_run1_0_1", 0, ""); err == nil || !strings.Contains(err.Error(), "no such choice") {
		t.Errorf("not a propose_options call: %v", err)
	}
	a.ai.mu.Lock()
	a.ai.runs[id] = func() {}
	a.ai.mu.Unlock()
	if _, err := a.ChooseRegionOption(id, callID, 0, ""); !errors.Is(err, ErrChatBusy) {
		t.Errorf("busy: %v", err)
	}
	a.ai.mu.Lock()
	delete(a.ai.runs, id)
	a.ai.mu.Unlock()
	if _, err := a.ChooseRegionOption(id, callID, 0, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ChooseRegionOption(id, callID, 1, ""); err == nil || !strings.Contains(err.Error(), "already decided") {
		t.Errorf("second choice: %v", err)
	}
}
```

Check the `events` type's field names in `ai_test.go` (`event{name, data}`) and adapt `e.data` accordingly.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/app/ -run ChooseRegionOption -v`
Expected: FAIL — `a.ChooseRegionOption undefined`.

- [ ] **Step 3: Implement.** In `merge.go`, split `ResolveMergeRegion`:

```go
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
	// the former body of ResolveMergeRegion, from `var res RegionResult` on
}
```

Then:

```go
// EventChatChoice tells the chat a propose_options card was answered: the
// tool's recorded result, which the card reads its state from.
const EventChatChoice = "chat:choice"

type ChatChoiceEvent struct {
	RepoID  string `json:"repoID"`
	CallID  string `json:"callID"`
	Summary string `json:"summary"`
}

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
	if c := history[result].Content; strings.HasPrefix(c, choseMark) || strings.HasPrefix(c, settledMark) {
		return RegionResult{}, errors.New("this card was already decided")
	}
	path, _ := call.Args["path"].(string)
	region, _ := call.Args["region"].(string)
	label := "their own text"
	if option >= 0 {
		opts, ok := mergetools.ParseOptions(call.Args)
		if !ok || option >= len(opts) {
			return RegionResult{}, fmt.Errorf("the card has no option %d", option)
		}
		label, text = opts[option].Label, opts[option].Text
	} else if option != -1 {
		return RegionResult{}, fmt.Errorf("the card has no option %d", option)
	}

	res, err := a.applyRegion(repoID, path, region, "text", text)
	var summary string
	switch {
	case errors.Is(err, merge.ErrNoSuchRegion) || errors.Is(err, merge.ErrNotInMerge) && a.stillMerging(repoID):
		summary = fmt.Sprintf("%s: region %s of %s is no longer in conflict.", settledMark, region, path)
		res, err = RegionResult{}, nil
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

const (
	choseMark   = "The user chose "
	settledMark = "Settled another way"
)

// findCall finds the tool call callID in history and the index of its
// result message: the k-th tool message after the assistant message for
// its k-th call, the pairing the provider adapters use. result is -1 when
// the call or its result is missing. The last match wins.
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
```

About the settled case: after `ResolveMergeRegion` staged greeting.txt, `merge.ResolveRegion` refuses with `ErrNotInMerge` (the path is no longer a text conflict), not `ErrNoSuchRegion`. Read `internal/merge/region.go` for the exact sentinel names. Treat "path no longer conflicted while the operation is still in progress" as settled; when the operation itself is over (committed/aborted) return the error. Implement `stillMerging` as:

```go
// stillMerging reports whether repoID has a merge, rebase or cherry-pick in progress.
func (a *App) stillMerging(repoID string) bool {
	dir, err := a.dir(repoID)
	if err != nil {
		return false
	}
	st, err := merge.Status(a.ctx, dir)
	return err == nil && st.Merging
}
```

and write the switch condition with explicit parentheses: `errors.Is(err, merge.ErrNoSuchRegion) || (errors.Is(err, merge.ErrNotInMerge) && a.stillMerging(repoID))`. Add imports `strings`, `git-ui/internal/ai/mergetools` as needed (mergetools is already imported in merge.go).

- [ ] **Step 4: Run tests**

Run: `go test ./internal/app/ -run 'ChooseRegionOption|ResolveMergeRegion' -v && go test ./internal/...`
Expected: PASS.

- [ ] **Step 5: Bindings.** Regenerate: `source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && ~/go/bin/wails generate module`. Confirm `frontend/wailsjs/go/app/App.d.ts` now declares `ChooseRegionOption(arg1:string,arg2:string,arg3:number,arg4:string):Promise<app.RegionResult>`. Restore any unrelated churn under `frontend/wailsjs/runtime` with `git checkout -- frontend/wailsjs/runtime`.

- [ ] **Step 6: Commit**

```bash
git add internal/app/merge.go internal/app/merge_test.go frontend/wailsjs/go
git commit -m "feat(merge): ChooseRegionOption applies a card's option and records the choice"
```

---

### Task 4: The card in the chat

**Files:**
- Modify: `frontend/src/lib/types.ts` (`ChatToolEvent.id`, `ChatChoiceEvent`)
- Modify: `frontend/src/lib/chat.ts` (`ChatToolUse.id`, `regionChoice`, `choiceState`, `withChoice`, reducer, `CHAT_EVENTS`, `fromMessages`, `WRITE_TOOLS`)
- Modify: `frontend/src/lib/api.ts`, `frontend/src/lib/actions.ts`
- Modify: `frontend/src/components/ChatPanel.svelte`
- Modify: `docs/spec/04-conflicts.md` (section "The AI conflict resolver")
- Test: `frontend/src/lib/chat.test.ts`, `frontend/src/lib/actions.test.ts`

**Interfaces:**
- Consumes: `chat:tool` payload `id` (Task 1); `chat:choice` `{repoID, callID, summary}` and `Go.ChooseRegionOption` (Task 3); message prefixes `The user chose ` / `Settled another way`.
- Produces:

```ts
export interface RegionChoice { path: string; region: string; question: string; options: { label: string; text: string }[] }
export function regionChoice(tool: ChatToolUse): RegionChoice | null
export type ChoiceState = 'pending' | 'chosen' | 'settled'
export function choiceState(tool: ChatToolUse): ChoiceState
export function withChoice(state: ChatState, callID: string, summary: string): ChatState
// actions.ts
export async function chooseRegionOption(repoID: string, callID: string, option: number, text: string): Promise<boolean>
```

Note: `lib/types.ts` already has a `RegionChoice` type (`'ours' | 'theirs' | 'both' | 'text'`) used by `resolveMergeRegion` — name the new interface `DecisionCard` instead to avoid the clash, and `regionChoice()` → `decisionCard()`. Use those names everywhere below.

- [ ] **Step 1: Write the failing tests** in `chat.test.ts`:

```ts
import { decisionCard, choiceState, withChoice, fromMessages, applyEvent, startRun, emptyChat } from './chat'

const cardArgs = {
  path: 'config/settings.json', region: '3f2a9c1b', question: 'Which timeout?',
  options: [{ label: '45000 (develop)', text: '  "apiTimeoutMs": 45000,\n' }, { label: 'remove', text: '' }],
}

describe('decisionCard', () => {
  it('reads a propose_options call', () => {
    expect(decisionCard({ name: 'propose_options', args: cardArgs })).toEqual(cardArgs)
  })
  it('is null for other tools and malformed args', () => {
    expect(decisionCard({ name: 'resolve_hunk', args: cardArgs })).toBeNull()
    expect(decisionCard({ name: 'propose_options', args: { ...cardArgs, options: 'x' } })).toBeNull()
    expect(decisionCard({ name: 'propose_options', args: { ...cardArgs, options: [{ label: 1, text: '' }] } })).toBeNull()
    expect(decisionCard({ name: 'propose_options', args: null })).toBeNull()
  })
})

describe('choiceState', () => {
  it('follows the recorded result', () => {
    expect(choiceState({ name: 'propose_options', args: cardArgs })).toBe('pending')
    expect(choiceState({ name: 'propose_options', args: cardArgs, summary: 'Shown to the user as a card with 2 options; …' })).toBe('pending')
    expect(choiceState({ name: 'propose_options', args: cardArgs, summary: 'The user chose "remove" for x (region y); it was written.' })).toBe('chosen')
    expect(choiceState({ name: 'propose_options', args: cardArgs, summary: 'Settled another way: region y of x is no longer in conflict.' })).toBe('settled')
  })
})

describe('chat:choice', () => {
  it('updates the card after the run ended', () => {
    const s = fromMessages('r', [
      { role: 'user', content: 'resolve' },
      { role: 'assistant', content: '', toolCalls: [{ id: 'c1', name: 'propose_options', args: cardArgs }] },
      { role: 'tool', toolName: 'propose_options', content: 'Shown to the user as a card with 2 options' },
    ])
    expect(s.items[1].tools[0].id).toBe('c1')
    const next = applyEvent(s, 'chat:choice', { repoID: 'r', callID: 'c1', summary: 'The user chose "remove" for x (region y); it was written.' } as never)
    expect(choiceState(next.items[1].tools[0])).toBe('chosen')
  })
  it('ignores another repository and unknown ids', () => {
    const s = fromMessages('r', [
      { role: 'user', content: 'resolve' },
      { role: 'assistant', content: '', toolCalls: [{ id: 'c1', name: 'propose_options', args: cardArgs }] },
    ])
    expect(applyEvent(s, 'chat:choice', { repoID: 'other', callID: 'c1', summary: 'The user chose' } as never)).toBe(s)
    expect(withChoice(s, 'nope', 'The user chose')).toBe(s)
  })
  it('chat:tool carries the id', () => {
    const s = startRun(emptyChat('r'), 'resolve', 'run1')
    const next = applyEvent(s, 'chat:tool', { repoID: 'r', runID: 'run1', id: 'c9', name: 'propose_options', args: cardArgs })
    expect(next.items[1].tools[0].id).toBe('c9')
  })
})
```

Check the exact `AIMessage` shape in `types.ts` (field names `role`, `content`, `toolCalls`, `toolName`) and adjust the literals if needed; keep assertions.

In `actions.test.ts`, following how `resolveMergeRegion` is tested there (mocked `api`, `toast`), add:

```ts
it('chooseRegionOption toasts the staged file and returns true', async () => {
  api.chooseRegionOption.mockResolvedValue({ left: 0, staged: true })
  expect(await chooseRegionOption('r', 'c1', 0, '')).toBe(true)
  expect(api.chooseRegionOption).toHaveBeenCalledWith('r', 'c1', 0, '')
  // toast called with a text containing 'resolved and staged'
})
it('chooseRegionOption shows the error and returns false', async () => {
  api.chooseRegionOption.mockRejectedValue(new Error('the AI is busy'))
  expect(await chooseRegionOption('r', 'c1', 0, '')).toBe(false)
  // toast called with 'the AI is busy', 'error'
})
```

Write the toast assertions in the file's own style.

- [ ] **Step 2: Run to verify failure**

Run: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run src/lib/chat.test.ts src/lib/actions.test.ts`
Expected: FAIL — `decisionCard` is not exported.

- [ ] **Step 3: Implement `lib`.**

`types.ts`:
```ts
export interface ChatToolEvent { repoID: string; runID: string; id?: string; name: string; args: Record<string, unknown> | null }
export interface ChatChoiceEvent { repoID: string; callID: string; summary: string }
```

`chat.ts`:
- `ChatToolUse` gets `id?: string` (comment: "the call's id; a decision card is answered by it").
- `fromMessages`: `last.tools.push({ id: call.id, name: call.name, args: call.args, at: …, seq: … })`.
- reducer `chat:tool`: `last.tools.push({ id: p.id, name: p.name, args: p.args, … })`.
- `CHAT_EVENTS` gains `'chat:choice'`; `Payload` gains `ChatChoiceEvent`.
- Before the runID guard (next to `chat:confirm`):

```ts
  // chat:choice answers a card, usually long after its run ended.
  if (name === 'chat:choice') {
    const p = payload as ChatChoiceEvent
    return withChoice(state, p.callID, p.summary)
  }
```

(`payload.repoID !== state.repoID` is already checked at the top of `applyEvent`.)

```ts
export interface DecisionCard { path: string; region: string; question: string; options: { label: string; text: string }[] }

/** The card of a propose_options call, or null when it is not one or its
 *  args are malformed (then the ordinary tool row shows). */
export function decisionCard(tool: Pick<ChatToolUse, 'name' | 'args'>): DecisionCard | null {
  if (tool.name !== 'propose_options' || !tool.args) return null
  const { path, region, question, options } = tool.args as Record<string, unknown>
  if (typeof path !== 'string' || typeof region !== 'string' || typeof question !== 'string' || !Array.isArray(options)) return null
  const opts: DecisionCard['options'] = []
  for (const o of options) {
    if (!o || typeof o !== 'object') return null
    const { label, text } = o as Record<string, unknown>
    if (typeof label !== 'string' || typeof text !== 'string') return null
    opts.push({ label, text })
  }
  return { path, region, question, options: opts }
}

export type ChoiceState = 'pending' | 'chosen' | 'settled'

/** What the card shows, read from the recorded tool result (the backend
 *  rewrites it when the user chooses). */
export function choiceState(tool: Pick<ChatToolUse, 'summary'>): ChoiceState {
  const s = tool.summary ?? ''
  if (s.startsWith('The user chose ')) return 'chosen'
  if (s.startsWith('Settled another way')) return 'settled'
  return 'pending'
}

/** Records a card's answer on its tool, found by call id in any item. */
export function withChoice(state: ChatState, callID: string, summary: string): ChatState {
  for (let i = state.items.length - 1; i >= 0; i--) {
    const idx = state.items[i].tools.findIndex((t) => t.id === callID)
    if (idx < 0) continue
    const items = state.items.slice()
    const tools = items[i].tools.slice()
    tools[idx] = { ...tools[idx], summary }
    items[i] = { ...items[i], tools }
    return { ...state, items }
  }
  return state
}
```

Leave `propose_options` out of `WRITE_TOOLS` (it writes nothing).

`api.ts`, next to `resolveMergeRegion`:
```ts
  chooseRegionOption: (repoID: string, callID: string, option: number, text: string) => call<RegionResult>(Go.ChooseRegionOption(repoID, callID, option, text)),
```

`actions.ts`, after `resolveMergeRegion`:
```ts
/** chooseRegionOption applies a decision card's pick; the card itself
 *  updates from chat:choice. Returns whether it was applied. */
export async function chooseRegionOption(repoID: string, callID: string, option: number, text: string): Promise<boolean> {
  busy.set('Resolving…')
  try {
    const res = await api.chooseRegionOption(repoID, callID, option, text)
    if (res.staged) toast('Resolved and staged')
    return true
  } catch (e) {
    toast(errorMessage(e), 'error')
    return false
  } finally {
    busy.set('')
    await refreshRepo()
  }
}
```

(The staged toast cannot name the path without it; pass `path` as a 5th parameter used only for the toast — `toast(\`${path} resolved and staged\`)` — and update the test and signature: `chooseRegionOption(repoID, callID, option, text, path)`. A non-staged success shows `toast('Region resolved')`.)

- [ ] **Step 4: Run lib tests**

Run: `cd frontend && npx vitest run`
Expected: PASS.

- [ ] **Step 5: The card in `ChatPanel.svelte`.** In the tool branch, before `{#if tool.confirm}`, add a `{@const card = decisionCard(tool)}` and render when `card && tool.id`:

```svelte
{#if card && tool.id}
  {@const st = choiceState(tool)}
  <div class="confirm decision">
    <div class="confirm-title">{card.path} · region {card.region}</div>
    <div class="decision-question">{card.question}</div>
    {#if st === 'pending'}
      {#each card.options as opt, k}
        <label class="decision-option">
          <input type="radio" name={tool.id} checked={pick[tool.id] === k} on:change={() => (pick = { ...pick, [tool.id]: k })} />
          <span>{opt.label}</span>
        </label>
        <pre class="decision-text">{opt.text === '' ? '(removes the region)' : opt.text}</pre>
      {/each}
      <label class="decision-option">
        <input type="radio" name={tool.id} checked={pick[tool.id] === -1} on:change={() => startOwn(tool.id, card)} />
        <span>Other…</span>
      </label>
      {#if pick[tool.id] === -1}
        <textarea class="decision-edit" bind:value={own[tool.id]}
          on:keydown={(e) => { if (e.key === 'Enter' && e.metaKey) apply(tool.id, card); if (e.key === 'Escape') pick = { ...pick, [tool.id]: 0 } }}></textarea>
      {/if}
      <div class="confirm-actions">
        {#if state.runID !== null}<span class="decision-wait">Available when the AI finishes</span>{/if}
        <button class="btn primary" disabled={state.runID !== null || applying[tool.id]} on:click={() => apply(tool.id, card)}>Apply</button>
      </div>
    {:else}
      <div class="confirm-result">{st === 'chosen' ? tool.summary : 'Settled another way'}</div>
    {/if}
  </div>
{:else if tool.confirm}
```

(keep the existing confirm branch and the ordinary tool row after it). Script additions:

```ts
  import { decisionCard, choiceState, type DecisionCard } from '../lib/chat'
  import { chooseRegionOption } from '../lib/actions'
  // Per card (call id): the selected option (-1 = Other…), the Other… text,
  // and whether Apply is in flight.
  let pick: Record<string, number> = {}
  let own: Record<string, string> = {}
  let applying: Record<string, boolean> = {}

  function startOwn(id: string, card: DecisionCard) {
    const from = pick[id] ?? 0
    own = { ...own, [id]: own[id] ?? card.options[from >= 0 ? from : 0]?.text ?? '' }
    pick = { ...pick, [id]: -1 }
  }

  async function apply(id: string, card: DecisionCard) {
    if (!state.repoID || state.runID !== null || applying[id]) return
    const k = pick[id] ?? 0
    applying = { ...applying, [id]: true }
    await chooseRegionOption(state.repoID, id, k, k === -1 ? own[id] ?? '' : '', card.path)
    applying = { ...applying, [id]: false }
  }
```

Styles, next to `.confirm`: `.decision-question { margin: 4px 0 6px; }`, `.decision-option { display: flex; gap: 6px; align-items: center; margin-top: 6px; }`, `.decision-text { margin: 2px 0 0 22px; font-family: var(--mono); font-size: 12px; white-space: pre-wrap; color: var(--text-muted); }`, `.decision-edit { width: 100%; min-height: 60px; font-family: var(--mono); font-size: 12px; margin-top: 4px; }`, `.decision-wait { color: var(--text-muted); font-size: 12px; margin-right: auto; }`. Check the variable names used in the file's existing styles (`--text-muted` or similar) and use those.

- [ ] **Step 6: Check.** Run: `cd frontend && npm run check && npx vitest run`. Expected: 0 errors, PASS.

- [ ] **Step 7: Docs.** In `docs/spec/04-conflicts.md` section "The AI conflict resolver" add a subsection "Decisions left to you" describing: the card (file · region, question, options with their exact text, "(removes the region)", Other… pre-filled with the selected option, ⌘↵/Esc), Apply disabled while an AI run holds the repository ("Available when the AI finishes"), what Apply writes and stages and the toasts, the chosen/settled one-line states and that they survive a reload, that nothing is committed, and that files without markers never get a card.

- [ ] **Step 8: Commit**

```bash
git add frontend/src docs/spec/04-conflicts.md
git commit -m "feat(chat): decision cards — pick an option and the app writes the region"
```

---

### Task 5: Lab grader `--after-cards`, then the manual run

**Files:**
- Modify: `scripts/conflict-lab/check.py`
- Modify: `scripts/conflict-lab/README.md`

**Interfaces:**
- Consumes: the chat history JSON written by `internal/ai/chatstore` (read `chatstore.go` for its file layout: one JSON file per repo id under `<UserConfigDir>/git-ui/chats`; on macOS UserConfigDir is `~/Library/Application Support`). Messages have `role`, `toolCalls[].name`, `toolCalls[].args.path`.

- [ ] **Step 1: Implement.** Add arguments:

```python
    ap.add_argument("--after-cards", action="store_true",
                    help="cases 7 and 9 were left as cards and chosen by you: any side counts as right, "
                         "provided the chat shows a propose_options call for them")
    ap.add_argument("--chat", help="chat history file (default: the newest in the app's chats directory)")
```

Helpers:

```python
def chats_dir():
    if sys.platform == "darwin":
        base = os.path.expanduser("~/Library/Application Support")
    else:
        base = os.environ.get("XDG_CONFIG_HOME") or os.path.expanduser("~/.config")
    return os.path.join(base, "git-ui", "chats")


def proposed_paths(chat_file):
    """Paths the resolver left as cards (propose_options calls) in a chat history."""
    with open(chat_file, encoding="utf-8") as f:
        data = json.load(f)
    messages = data if isinstance(data, list) else data.get("messages", [])
    out = set()
    for m in messages:
        for c in m.get("toolCalls") or []:
            if c.get("name") == "propose_options":
                out.add((c.get("args") or {}).get("path"))
    return out
```

(Adjust `messages` extraction to the real file shape after reading `chatstore.go`.) In `main`, when `--after-cards`: resolve the chat file (`--chat` or newest `*.json` in `chats_dir()` by mtime; exit with a clear message if none), compute `proposed`, and for scenarios 7 and 9 map the verdict: if the file is not in `proposed` → `("FAIL", "no card was proposed for it")`; else `PARTIAL` → `PASS` with why `"chosen from a card: " + why`, and `PASS` (still open) → `("UNRESOLVED", "card not answered yet")`. Leave every other scenario and the default mode unchanged. `import json` at the top.

In `README.md` add a paragraph: "Decision cards: resolve with AI, answer the cards for 7 and 9 in the chat, then `python3 scripts/conflict-lab/check.py --model <m> --after-cards`." and a note in the table's 7 and 9 rows: "(with cards: a card, then your pick)".

- [ ] **Step 2: Verify the script parses and the default mode is unchanged.** Run: `python3 -m py_compile scripts/conflict-lab/check.py && python3 scripts/conflict-lab/check.py --help`. Expected: help lists `--after-cards` and `--chat`.

- [ ] **Step 3: Commit**

```bash
git add scripts/conflict-lab
git commit -m "chore(lab): check.py --after-cards grades 7 and 9 as chosen from a card"
```

- [ ] **Step 4: Build and reopen the app.** Run: `go test ./... && (cd frontend && npx vitest run) && make dev` (node 22). Leave it running.

- [ ] **Step 5: Manual lab run.** `scripts/conflict-lab/setup.sh`; in CommitTree with an Anthropic model: merge `feature/checkout` into `develop`, Resolve with AI. Expect cards for `config/settings.json` (timeout) and `src/billing/discount.py`. While the run is going, Apply is disabled. After it ends: pick an option in each; the Merge view reloads; reload the chat panel (switch repo and back) and the cards stay decided. Then `python3 scripts/conflict-lab/check.py --model claude-sonnet-5-5 --after-cards` → 11/11 expected. Record the result.

---

## After the plan

Final whole-branch review (most capable model), fix findings, owner's manual check, then `git merge --no-ff` into main with a message `Merge claude/ai-region-actions-cards-e0e3be: …`.
