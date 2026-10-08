package refs

import (
	"context"
	"errors"
	"testing"

	"git-ui/internal/testrepo"
)

// An unreadable git config must not fail the refs: the default rule applies.
func TestListFallsBackToDefaultRuleWhenConfigUnreadable(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("branch", "feature/x")
	r.Git("config", "gitflow.branch.master", "prod")
	r.Git("config", "gitflow.branch.develop", "dev")

	orig := configList
	t.Cleanup(func() { configList = orig })
	configList = func(context.Context, string) (string, error) { return "", errors.New("boom") }

	if _, err := ReadOfficialRule(context.Background(), r.Dir); err == nil {
		t.Fatal("ReadOfficialRule: want the error")
	}
	if rule := OfficialRuleOrDefault(context.Background(), r.Dir); rule != (OfficialRule{}) {
		t.Fatalf("OfficialRuleOrDefault = %+v, want the default rule", rule)
	}
	got, err := List(context.Background(), r.Dir)
	if err != nil {
		t.Fatalf("List failed on an unreadable config: %v", err)
	}
	official := map[string]bool{}
	for _, b := range got.Local {
		official[b.Name] = b.Official
	}
	if !official["main"] || official["feature/x"] {
		t.Fatalf("default rule not applied: %v", official)
	}
}
