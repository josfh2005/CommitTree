package reposettings

import "strings"

const (
	projectHeading = "## Project instructions"
	privateHeading = "## Your instructions for this repository"
)

// Assemble appends to base the repository's approved instructions for
// action and then the user's private ones, each under its heading. Private
// instructions come last so they win over the repository's.
func Assemble(base, action string, repo RepoInstructions, approved bool, private map[string]string) string {
	parts := []string{base}
	if approved {
		if texts := repo.Text(action); len(texts) > 0 {
			parts = append(parts, projectHeading)
			parts = append(parts, texts...)
		}
	}
	var mine []string
	for _, k := range []string{AllActions, action} {
		if v := strings.TrimSpace(private[k]); v != "" {
			mine = append(mine, v)
		}
	}
	if len(mine) > 0 {
		parts = append(parts, privateHeading)
		parts = append(parts, mine...)
	}
	return strings.Join(parts, "\n\n")
}
