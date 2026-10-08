package refs

import (
	"context"
	"strings"

	"git-ui/internal/gitcmd"
)

// OfficialRule says which branch names are the repository's official ones:
// the branches a hotfix is merged into, the ones "Push main branches"
// pushes. The zero value is the default rule — main, master, develop and
// release/*. With git-flow configured (both gitflow.branch.master and
// gitflow.branch.develop set) its names replace the defaults entirely.
type OfficialRule struct {
	Master, Develop, ReleasePrefix string
}

const defaultReleasePrefix = "release/"

// IsOfficial reports whether the local branch name is official.
func (r OfficialRule) IsOfficial(name string) bool {
	if r.Master == "" || r.Develop == "" {
		return name == "main" || name == "master" || name == "develop" || isRelease(name, defaultReleasePrefix)
	}
	prefix := r.ReleasePrefix
	if prefix == "" {
		prefix = defaultReleasePrefix
	}
	return name == r.Master || name == r.Develop || isRelease(name, prefix)
}

func isRelease(name, prefix string) bool {
	return strings.HasPrefix(name, prefix) && len(name) > len(prefix)
}

// ReadOfficialRule reads the git-flow names from the repository's config.
// It lives here, not in gitflow, because gitflow depends on ops and ops on
// refs.
func ReadOfficialRule(ctx context.Context, dir string) (OfficialRule, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "config", "--list", "-z")
	if err != nil {
		return OfficialRule{}, err
	}
	var master, develop, release string
	for _, rec := range strings.Split(out, "\x00") {
		key, value, _ := strings.Cut(rec, "\n")
		switch key {
		case "gitflow.branch.master":
			master = value
		case "gitflow.branch.develop":
			develop = value
		case "gitflow.prefix.release":
			release = value
		}
	}
	if master == "" || develop == "" {
		return OfficialRule{}, nil
	}
	if release == "" {
		release = defaultReleasePrefix
	}
	return OfficialRule{Master: master, Develop: develop, ReleasePrefix: release}, nil
}
