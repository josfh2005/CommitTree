package reposettings

import "testing"

func TestAssembleOrderAndHeadings(t *testing.T) {
	repo := RepoInstructions{Files: []RepoFile{
		{Name: "commit-message.md", Text: "Conventional Commits."},
		{Name: "instructions.md", Text: "We use Go."},
	}, Hash: "sha256:x"}
	private := map[string]string{"all": "Answer in Spanish.", "commit-message": "Mention the ticket.", "chat": "unused here"}
	got := Assemble("BASE", "commit-message", repo, true, private)
	want := "BASE\n\n## Project instructions\n\nWe use Go.\n\nConventional Commits.\n\n## Your instructions for this repository\n\nAnswer in Spanish.\n\nMention the ticket."
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestAssembleSkipsUnapprovedAndEmpty(t *testing.T) {
	repo := RepoInstructions{Files: []RepoFile{{Name: "instructions.md", Text: "We use Go."}}, Hash: "sha256:x"}
	if got := Assemble("BASE", "chat", repo, false, nil); got != "BASE" {
		t.Fatalf("unapproved repo text used: %q", got)
	}
	if got := Assemble("BASE", "chat", RepoInstructions{}, true, map[string]string{"all": "  "}); got != "BASE" {
		t.Fatalf("blank private text used: %q", got)
	}
}
