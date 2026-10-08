package gitflow

import (
	"context"
	"fmt"
	"strings"
)

// Settings is what the Git-flow tab of Repository settings shows: the
// repository's branch roles, or — when git-flow is not set up — the
// defaults the app assumes (and Set up would write).
type Settings struct {
	// Initialized: both gitflow.branch.master and gitflow.branch.develop are set.
	Initialized bool `json:"initialized"`
	// Problem is set when the configured master or develop does not exist.
	Problem  string   `json:"problem"`
	Master   string   `json:"master"`
	Develop  string   `json:"develop"`
	Prefixes Prefixes `json:"prefixes"`
}

// ReadSettings reads the gitflow.* keys; whatever is missing is filled with
// the defaults: the existing main or master, develop and the usual prefixes.
func ReadSettings(ctx context.Context, dir string) (Settings, error) {
	cfg, err := readConfig(ctx, dir)
	if err != nil {
		return Settings{}, err
	}
	s := Settings{Master: cfg["gitflow.branch.master"], Develop: cfg["gitflow.branch.develop"], Prefixes: defaultPrefixes}
	s.Initialized = s.Master != "" && s.Develop != ""
	for _, t := range types {
		if v := cfg["gitflow.prefix."+t]; v != "" {
			s.Prefixes.set(t, v)
		}
	}
	if s.Master == "" {
		s.Master = defaultMaster(ctx, dir)
	}
	if s.Develop == "" {
		s.Develop = "develop"
	}
	if s.Initialized {
		for _, b := range []string{s.Master, s.Develop} {
			if !branchExists(ctx, dir, b) {
				s.Problem = b + " does not exist"
				break
			}
		}
	}
	return s, nil
}

// defaultMaster is the local main or master, else the checked-out branch,
// else "main".
func defaultMaster(ctx context.Context, dir string) string {
	for _, b := range []string{"main", "master"} {
		if branchExists(ctx, dir, b) {
			return b
		}
	}
	if cur := currentBranch(ctx, dir); cur != "" {
		return cur
	}
	return "main"
}

// SaveSettings rewrites the gitflow.* keys of a repository that is already
// set up. It creates, renames and deletes no branch, and does not require
// the named branches to exist.
func SaveSettings(ctx context.Context, dir string, cfg Config) error {
	cur, err := readConfig(ctx, dir)
	if err != nil {
		return err
	}
	if cur["gitflow.branch.master"] == "" || cur["gitflow.branch.develop"] == "" {
		return ErrNotInitialized
	}
	cfg = trimmed(cfg)
	if err := validate(ctx, dir, cfg); err != nil {
		return err
	}
	return writeConfig(ctx, dir, cfg)
}

func trimmed(c Config) Config {
	c.Master, c.Develop = strings.TrimSpace(c.Master), strings.TrimSpace(c.Develop)
	c.Prefixes.Feature = strings.TrimSpace(c.Prefixes.Feature)
	c.Prefixes.Release = strings.TrimSpace(c.Prefixes.Release)
	c.Prefixes.Hotfix = strings.TrimSpace(c.Prefixes.Hotfix)
	c.Prefixes.Warmfix = strings.TrimSpace(c.Prefixes.Warmfix)
	return c
}

// validate checks the names and prefixes alone, not whether the branches exist.
func validate(ctx context.Context, dir string, cfg Config) error {
	for _, n := range []string{cfg.Master, cfg.Develop} {
		if err := checkBranchName(ctx, dir, n); err != nil {
			return err
		}
	}
	if cfg.Develop == cfg.Master {
		return fmt.Errorf("%w: production and development are both %q", ErrInvalidName, cfg.Master)
	}
	for _, t := range types {
		p, _ := cfg.Prefixes.of(t)
		if p == "" {
			return fmt.Errorf("the %s prefix is empty", t)
		}
		if strings.ContainsAny(p, " \t\r\n") {
			return fmt.Errorf("the %s prefix has spaces: %q", t, p)
		}
		if err := checkBranchName(ctx, dir, p+"x"); err != nil {
			return fmt.Errorf("the %s prefix is invalid: %q", t, p)
		}
	}
	return nil
}

func checkBranchName(ctx context.Context, dir, n string) error {
	if n == "" || strings.HasPrefix(n, "-") || strings.ContainsAny(n, " \t\r\n") {
		return fmt.Errorf("%w: %q", ErrInvalidName, n)
	}
	if _, err := git(ctx, dir, "check-ref-format", "--branch", n); err != nil {
		return fmt.Errorf("%w: %q", ErrInvalidName, n)
	}
	return nil
}

func writeConfig(ctx context.Context, dir string, cfg Config) error {
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
