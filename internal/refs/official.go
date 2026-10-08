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

// RuleFromConfig builds the rule from the gitflow.branch.master,
// gitflow.branch.develop and gitflow.prefix.release config values. It is
// the one place that decides whether git-flow counts as configured (both
// branch names set); otherwise the zero rule, the defaults, comes back.
func RuleFromConfig(master, develop, releasePrefix string) OfficialRule {
	if master == "" || develop == "" {
		return OfficialRule{}
	}
	if releasePrefix == "" {
		releasePrefix = defaultReleasePrefix
	}
	return OfficialRule{Master: master, Develop: develop, ReleasePrefix: releasePrefix}
}

// Configured reports whether the rule comes from a git-flow config rather
// than being the defaults.
func (r OfficialRule) Configured() bool { return r.Master != "" && r.Develop != "" }

// IsOfficial reports whether the local branch name is official.
func (r OfficialRule) IsOfficial(name string) bool {
	if !r.Configured() {
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

// configList is a seam for tests: they replace it to simulate a config that
// cannot be read (real git fails every command then, not just this one).
var configList = func(ctx context.Context, dir string) (string, error) {
	return gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "config", "--list", "-z")
}

// ReadOfficialRule reads the git-flow names from the repository's config.
// It lives here, not in gitflow, because gitflow depends on ops and ops on
// refs.
func ReadOfficialRule(ctx context.Context, dir string) (OfficialRule, error) {
	out, err := configList(ctx, dir)
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
	return RuleFromConfig(master, develop, release), nil
}

// OfficialRuleOrDefault is ReadOfficialRule for callers that must not fail
// because the config could not be read: they get the default rule then.
func OfficialRuleOrDefault(ctx context.Context, dir string) OfficialRule {
	rule, err := ReadOfficialRule(ctx, dir)
	if err != nil {
		return OfficialRule{}
	}
	return rule
}
