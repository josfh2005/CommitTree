// Package gitflow starts and finishes git-flow branches with plain git,
// reading and writing the gitflow.* config SourceTree and git-flow use.
package gitflow

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"git-ui/internal/gitcmd"
)

const (
	Feature = "feature"
	Release = "release"
	Hotfix  = "hotfix"
	Warmfix = "warmfix"
)

// types is the order branches are classified and listed in.
var types = []string{Feature, Release, Hotfix, Warmfix}

var (
	ErrNotInitialized = errors.New("git-flow is not initialised in this repository")
	ErrInvalidName    = errors.New("invalid branch name")
	ErrNoRelease      = errors.New("no local release branch")
	ErrNotFlowBranch  = errors.New("not a git-flow branch")
)

type Prefixes struct {
	Feature string `json:"feature"`
	Release string `json:"release"`
	Hotfix  string `json:"hotfix"`
	Warmfix string `json:"warmfix"`
}

var defaultPrefixes = Prefixes{Feature: "feature/", Release: "release/", Hotfix: "hotfix/", Warmfix: "warmfix/"}

func (p Prefixes) of(typ string) (string, bool) {
	switch typ {
	case Feature:
		return p.Feature, true
	case Release:
		return p.Release, true
	case Hotfix:
		return p.Hotfix, true
	case Warmfix:
		return p.Warmfix, true
	}
	return "", false
}

// Config is what Init writes.
type Config struct {
	Master   string   `json:"master"`
	Develop  string   `json:"develop"`
	Prefixes Prefixes `json:"prefixes"`
}

type FlowBranch struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Short string `json:"short"`
	Base  string `json:"base"`
	// InProgress: a finish already merged the branch into at least one of
	// its targets (a merge commit whose second parent is the branch tip).
	InProgress bool `json:"inProgress"`
	// baseKey is the config key Base came from, as spelled there: its case
	// may differ from Name's (SourceTree's Warmfix/ vs warmfix/).
	baseKey string
}

type Flow struct {
	Initialized bool         `json:"initialized"`
	Problem     string       `json:"problem"`
	Master      string       `json:"master"`
	Develop     string       `json:"develop"`
	Prefixes    Prefixes     `json:"prefixes"`
	Current     *FlowBranch  `json:"current"`
	Branches    []FlowBranch `json:"branches"`
	Releases    []string     `json:"releases"`
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, args...)
	return strings.TrimSpace(out), err
}

// readConfig returns every gitflow.* key. -z keeps values with odd
// characters intact: records are NUL-separated, "key\nvalue".
func readConfig(ctx context.Context, dir string) (map[string]string, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "config", "--list", "-z")
	if err != nil {
		return nil, err
	}
	cfg := map[string]string{}
	for _, rec := range strings.Split(out, "\x00") {
		key, value, _ := strings.Cut(rec, "\n")
		if strings.HasPrefix(key, "gitflow.") {
			cfg[key] = value
		}
	}
	return cfg, nil
}

func branchExists(ctx context.Context, dir, name string) bool {
	_, err := git(ctx, dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+name)
	return err == nil
}

func currentBranch(ctx context.Context, dir string) string {
	name, err := git(ctx, dir, "symbolic-ref", "-q", "--short", "HEAD")
	if err != nil {
		return ""
	}
	return name
}

// isAncestor reports whether a is contained in b.
func isAncestor(ctx context.Context, dir, a, b string) (bool, error) {
	_, err := git(ctx, dir, "merge-base", "--is-ancestor", a, b)
	if err == nil {
		return true, nil
	}
	var gerr *gitcmd.Error
	if errors.As(err, &gerr) && gerr.ExitCode == 1 {
		return false, nil
	}
	return false, err
}

// mergedVia reports whether target has a merge commit bringing in tip — a
// merge whose second parent is tip. Plain containment is not enough: a
// branch just started from develop is contained in develop too.
func mergedVia(ctx context.Context, dir, tip, target string) bool {
	out, err := git(ctx, dir, "rev-list", "--merges", "--parents", "--ancestry-path", tip+".."+target)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) >= 3 && slices.Contains(f[2:], tip) {
			return true
		}
	}
	return false
}

func classify(name string, p Prefixes) (typ, short string, ok bool) {
	lower := strings.ToLower(name)
	for _, t := range types {
		prefix, _ := p.of(t)
		if prefix != "" && strings.HasPrefix(lower, strings.ToLower(prefix)) && len(name) > len(prefix) {
			return t, name[len(prefix):], true
		}
	}
	return "", "", false
}

func Read(ctx context.Context, dir string) (Flow, error) {
	f := Flow{Branches: []FlowBranch{}, Releases: []string{}}
	cfg, err := readConfig(ctx, dir)
	if err != nil {
		return f, err
	}
	f.Master, f.Develop = cfg["gitflow.branch.master"], cfg["gitflow.branch.develop"]
	f.Initialized = f.Master != "" && f.Develop != ""
	f.Prefixes = defaultPrefixes
	for _, t := range types {
		if v, ok := cfg["gitflow.prefix."+t]; ok && v != "" {
			switch t {
			case Feature:
				f.Prefixes.Feature = v
			case Release:
				f.Prefixes.Release = v
			case Hotfix:
				f.Prefixes.Hotfix = v
			case Warmfix:
				f.Prefixes.Warmfix = v
			}
		}
	}
	if !f.Initialized {
		return f, nil
	}
	for _, b := range []string{f.Master, f.Develop} {
		if !branchExists(ctx, dir, b) {
			f.Problem = b + " does not exist"
			return f, nil
		}
	}
	out, err := git(ctx, dir, "for-each-ref", "--format=%(refname:short)%09%(objectname)", "refs/heads")
	if err != nil {
		return f, err
	}
	// Branch names are compared ignoring case: on a case-insensitive
	// filesystem a branch created as warmfix/X inside an existing Warmfix/
	// directory is listed as Warmfix/X while HEAD and its base key say
	// warmfix/X.
	bases := map[string][2]string{}
	for key, value := range cfg {
		if name, ok := strings.CutPrefix(key, "gitflow.branch."); ok {
			if name, ok := strings.CutSuffix(name, ".base"); ok {
				bases[strings.ToLower(name)] = [2]string{key, value}
			}
		}
	}
	current := currentBranch(ctx, dir)
	for _, line := range strings.Split(out, "\n") {
		name, tip, _ := strings.Cut(line, "\t")
		typ, short, ok := classify(name, f.Prefixes)
		if !ok {
			continue
		}
		base := bases[strings.ToLower(name)]
		b := FlowBranch{Name: name, Type: typ, Short: short, Base: base[1], baseKey: base[0]}
		if typ == Release {
			f.Releases = append(f.Releases, name)
		}
		for _, target := range required(f, b) {
			if mergedVia(ctx, dir, tip, target) {
				b.InProgress = true
				break
			}
		}
		f.Branches = append(f.Branches, b)
		if current != "" && strings.EqualFold(name, current) {
			cur := b
			f.Current = &cur
		}
	}
	return f, nil
}

// required are the targets every finish of b merges into; a hotfix's
// releases are optional and left out.
func required(f Flow, b FlowBranch) []string {
	switch b.Type {
	case Feature:
		return []string{f.Develop}
	case Release, Hotfix:
		return []string{f.Master, f.Develop}
	case Warmfix:
		if b.Base != "" {
			return []string{b.Base}
		}
	}
	return nil
}

// targets are the branches a finish of b merges into, in order. releases
// are the hotfix's ticked releases, or a warmfix's chosen release when it
// has no recorded base.
func targets(f Flow, b FlowBranch, releases []string) ([]string, error) {
	for _, r := range releases {
		if !slices.Contains(f.Releases, r) {
			return nil, fmt.Errorf("%s is not a local release branch", r)
		}
	}
	switch b.Type {
	case Feature:
		return []string{f.Develop}, nil
	case Release:
		return []string{f.Master, f.Develop}, nil
	case Hotfix:
		return append(append([]string{f.Master}, releases...), f.Develop), nil
	case Warmfix:
		if b.Base != "" {
			return []string{b.Base}, nil
		}
		if len(releases) > 0 {
			return releases[:1], nil
		}
		return nil, fmt.Errorf("%w: no release to finish %s into", ErrNoRelease, b.Name)
	}
	return nil, ErrNotFlowBranch
}

// ending is the branch left checked out after a finish.
func ending(f Flow, b FlowBranch, targets []string) string {
	if b.Type == Warmfix {
		return targets[0]
	}
	return f.Develop
}

func Init(ctx context.Context, dir string, cfg Config) error {
	if !branchExists(ctx, dir, cfg.Master) {
		return fmt.Errorf("%s does not exist", cfg.Master)
	}
	if cfg.Develop == "" || cfg.Develop == cfg.Master || strings.HasPrefix(cfg.Develop, "-") {
		return fmt.Errorf("%w: %q", ErrInvalidName, cfg.Develop)
	}
	if _, err := git(ctx, dir, "check-ref-format", "--branch", cfg.Develop); err != nil {
		return fmt.Errorf("%w: %q", ErrInvalidName, cfg.Develop)
	}
	for _, t := range types {
		if p, _ := cfg.Prefixes.of(t); p == "" {
			return fmt.Errorf("the %s prefix is empty", t)
		}
	}
	if !branchExists(ctx, dir, cfg.Develop) {
		if _, err := git(ctx, dir, "branch", cfg.Develop, cfg.Master); err != nil {
			return err
		}
	}
	set := [][2]string{
		{"gitflow.branch.master", cfg.Master},
		{"gitflow.branch.develop", cfg.Develop},
		{"gitflow.prefix.feature", cfg.Prefixes.Feature},
		{"gitflow.prefix.release", cfg.Prefixes.Release},
		{"gitflow.prefix.hotfix", cfg.Prefixes.Hotfix},
		{"gitflow.prefix.warmfix", cfg.Prefixes.Warmfix},
	}
	for _, kv := range set {
		if _, err := git(ctx, dir, "config", kv[0], kv[1]); err != nil {
			return err
		}
	}
	return nil
}
