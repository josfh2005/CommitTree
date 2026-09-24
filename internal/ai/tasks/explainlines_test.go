package tasks_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"git-ui/internal/ai/tasks"
	"git-ui/internal/testrepo"
)

func TestExplainLinesContext(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile("a.go", "package a\n\nfunc F() {}\n")
	r.Git("add", "a.go")
	r.Git("commit", "-q", "-m", "add F", "-m", "Needed for NEXO-9.")
	r.WriteFile("a.go", "package a\n\nfunc F() { g() }\n")
	r.Git("commit", "-q", "-am", "call g")
	short := r.Git("rev-parse", "--short=7", "HEAD")

	got, err := tasks.ExplainLinesContext(context.Background(), r.Dir, "HEAD", "a.go", 3, 3, tasks.OllamaDiffBudget)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"File: a.go", "lines 3-3", "3: func F() { g() }", "L3-3  " + short, "Commit " + short, "Subject: call g", "+func F() { g() }"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Subject: add F") {
		t.Fatalf("only commits blamed for the range belong in the context:\n%s", got)
	}
}

func TestExplainLinesContextCapsCommitsAndBudget(t *testing.T) {
	r := testrepo.New(t)
	lines := make([]string, 8)
	for i := range lines {
		lines[i] = fmt.Sprintf("l%d", i)
	}
	r.WriteFile("a.txt", strings.Join(lines, "\n")+"\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-m", "c0", fmt.Sprintf("--date=@%d", 1700000000-60))
	for i := range lines {
		lines[i] = fmt.Sprintf("L%d %s", i, strings.Repeat("x", 400))
		r.WriteFile("a.txt", strings.Join(lines, "\n")+"\n")
		r.Git("commit", "-q", "-am", fmt.Sprintf("c%d", i+1), fmt.Sprintf("--date=@%d", 1700000000+(i+1)*60))
	}
	got, err := tasks.ExplainLinesContext(context.Background(), r.Dir, "HEAD", "a.txt", 1, 8, 3000)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(got, "\nCommit "); n != tasks.MaxExplainCommits {
		t.Fatalf("commits = %d, want %d:\n%s", n, tasks.MaxExplainCommits, got)
	}
	if !strings.Contains(got, "Subject: c8") || strings.Contains(got, "Subject: c3\n") {
		t.Fatalf("want the newest commits first:\n%s", got)
	}
	if len(got) > 3000+600 {
		t.Fatalf("context %d bytes, over budget", len(got))
	}
}

func TestExplainLinesContextMixedCommittedAndUncommitted(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile("a.txt", "one\ntwo\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-m", "add")
	r.WriteFile("a.txt", "one\ntwo\nthree\n")
	got, err := tasks.ExplainLinesContext(context.Background(), r.Dir, "", "a.txt", 1, 3, tasks.OllamaDiffBudget)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Uncommitted changes:") {
		t.Fatalf("missing uncommitted section:\n%s", got)
	}
	if !strings.Contains(got, "Commit ") {
		t.Fatalf("missing committed section:\n%s", got)
	}
}

func TestExplainLinesContextBlameStaysWithinBudget(t *testing.T) {
	r := testrepo.New(t)
	n := 40
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("l%d", i)
	}
	r.WriteFile("a.txt", strings.Join(lines, "\n")+"\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-m", "c0", fmt.Sprintf("--date=@%d", 1700000000-60))
	// One commit per line, each with a long summary, so a whole-file blame
	// produces many blocks and a large formatted blame section.
	for i := range lines {
		lines[i] = fmt.Sprintf("l%d changed", i)
		r.WriteFile("a.txt", strings.Join(lines, "\n")+"\n")
		msg := fmt.Sprintf("change line %d: %s", i, strings.Repeat("word ", 20))
		r.Git("commit", "-q", "-am", msg, fmt.Sprintf("--date=@%d", 1700000000+(i+1)*60))
	}
	budget := 2000
	got, err := tasks.ExplainLinesContext(context.Background(), r.Dir, "HEAD", "a.txt", 1, n, budget)
	if err != nil {
		t.Fatal(err)
	}
	blameStart := strings.Index(got, "\nBlame:\n")
	if blameStart < 0 {
		t.Fatalf("no Blame section:\n%s", got)
	}
	blameEnd := strings.Index(got, "\n\nUncommitted changes:")
	if blameEnd < 0 {
		blameEnd = strings.Index(got, "\n\nCommit ")
	}
	if blameEnd < 0 {
		t.Fatalf("cannot find end of Blame section:\n%s", got)
	}
	blameSection := got[blameStart:blameEnd]
	if max := budget/3 + 100; len(blameSection) > max {
		t.Fatalf("blame section %d bytes, want <= %d (budget/3 plus truncation slack):\n%s", len(blameSection), max, blameSection)
	}
}

func TestExplainLinesContextWorkingTree(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile("a.txt", "one\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-m", "add")
	r.WriteFile("a.txt", "one\ntwo\n")
	got, err := tasks.ExplainLinesContext(context.Background(), r.Dir, "", "a.txt", 2, 2, tasks.OllamaDiffBudget)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"(working tree)", "(not committed yet)", "Uncommitted changes:", "+two"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}
