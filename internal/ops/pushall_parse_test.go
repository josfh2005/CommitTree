package ops

import "testing"

func TestParsePorcelainAndClassify(t *testing.T) {
	out := "To ../r.git\n" +
		" \trefs/heads/ff:refs/heads/other\t86a06b8..8a98883\n" +
		"!\trefs/heads/main:refs/heads/main\t[rejected] (fetch first)\n" +
		"!\trefs/heads/nff:refs/heads/nff\t[rejected] (non-fast-forward)\n" +
		"=\trefs/heads/same:refs/heads/same\t[up to date]\n" +
		"*\trefs/heads/new:refs/heads/new\t[new branch]\n" +
		"+\trefs/heads/forced:refs/heads/forced\t1111111...2222222 (forced update)\n" +
		"!\trefs/heads/hook:refs/heads/hook\t[remote rejected] (pre-receive hook declined)\n" +
		"Done\n"
	lines := parsePorcelain(out)
	want := map[string]struct {
		status PushStatus
		reason string
	}{
		"ff":     {PushPushed, ""},
		"main":   {PushRejected, "The remote has commits you don't have — pull main first"},
		"nff":    {PushRejected, "The remote has commits you don't have — pull nff first"},
		"same":   {PushUpToDate, ""},
		"new":    {PushPushed, ""},
		"forced": {PushPushed, ""},
		"hook":   {PushRejected, "[remote rejected] (pre-receive hook declined)"},
	}
	if len(lines) != len(want) {
		t.Fatalf("parsed %d lines, want %d: %+v", len(lines), len(want), lines)
	}
	for branch, w := range want {
		l, ok := lines["refs/heads/"+branch]
		if !ok {
			t.Fatalf("no line for %s", branch)
		}
		status, reason := classifyPushLine(l, branch)
		if status != w.status || reason != w.reason {
			t.Errorf("%s = %s %q, want %s %q", branch, status, reason, w.status, w.reason)
		}
	}
}

func TestParsePorcelainToleratesATrimmedFlagAndNoise(t *testing.T) {
	lines := parsePorcelain("\trefs/heads/a:refs/heads/a\t1..2\n")
	if s, _ := classifyPushLine(lines["refs/heads/a"], "a"); s != PushPushed {
		t.Errorf("trimmed space flag = %s, want pushed", s)
	}
	if got := parsePorcelain(""); len(got) != 0 {
		t.Errorf("empty output parsed to %+v", got)
	}
	if got := parsePorcelain("fatal: 'x' does not appear to be a git repository\n"); len(got) != 0 {
		t.Errorf("error text parsed to %+v", got)
	}
}
