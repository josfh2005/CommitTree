package gitcmd

import (
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type fakeSession struct {
	env     []string
	outcome error
	ended   []bool
}

func (f *fakeSession) Env() []string { return f.env }
func (f *fakeSession) End(failed bool) error {
	f.ended = append(f.ended, failed)
	return f.outcome
}

func marked(f *fakeSession) context.Context {
	return WithPrompts(context.Background(), func() PromptSession { return f })
}

func TestPromptSessionEnvReachesGit(t *testing.T) {
	f := &fakeSession{env: []string{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=test.x", "GIT_CONFIG_VALUE_0=hello"}}
	out, err := Run(marked(f), t.TempDir(), ReadTimeout, "config", "--get", "test.x")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "hello" {
		t.Fatalf("got %q", out)
	}
	if len(f.ended) != 1 || f.ended[0] {
		t.Fatalf("End calls = %v, want [false]", f.ended)
	}
}

func TestCallerEnvBeatsPromptEnv(t *testing.T) {
	f := &fakeSession{env: []string{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=test.x", "GIT_CONFIG_VALUE_0=session"}}
	out, err := RunEnv(marked(f), t.TempDir(), ReadTimeout, []string{"GIT_CONFIG_VALUE_0=caller"}, "config", "--get", "test.x")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "caller" {
		t.Fatalf("got %q", out)
	}
}

func TestPromptOutcomeReplacesGitMessage(t *testing.T) {
	f := &fakeSession{outcome: &PromptError{Msg: "No credentials entered."}}
	_, err := Run(marked(f), t.TempDir(), ReadTimeout, "config", "--get", "nope.missing")
	if err == nil {
		t.Fatal("want an error")
	}
	if err.Error() != "No credentials entered." {
		t.Fatalf("message %q", err.Error())
	}
	var pe *PromptError
	if !errors.As(err, &pe) {
		t.Fatal("want a *PromptError in the chain")
	}
	var gerr *Error
	if !errors.As(err, &gerr) {
		t.Fatalf("want *Error kept, got %#v", err)
	}
	if len(f.ended) != 1 || !f.ended[0] {
		t.Fatalf("End calls = %v, want [true]", f.ended)
	}
}

func TestCancelBeatsPromptOutcome(t *testing.T) {
	f := &fakeSession{outcome: &PromptError{Msg: "No credentials entered."}}
	ctx := marked(f)
	rec := &Recorder{Begin: func(s Start) int64 { s.Cancel(); return 1 }}
	SetRecorder(rec)
	defer SetRecorder(nil)
	_, err := Run(ctx, t.TempDir(), ReadTimeout, "hash-object", "--stdin")
	if err == nil || !errors.Is(err, ErrCancelled) {
		t.Fatalf("want ErrCancelled, got %v", err)
	}
}

func TestUnmarkedContextHasNoSession(t *testing.T) {
	if promptsFrom(context.Background()) != nil {
		t.Fatal("unmarked ctx returned a begin func")
	}
}

func TestPromptsReportsMarkedContext(t *testing.T) {
	if Prompts(context.Background()) {
		t.Fatal("unmarked ctx reports prompts")
	}
	if Prompts(nil) { //nolint:staticcheck // a nil ctx must not panic
		t.Fatal("nil ctx reports prompts")
	}
	if !Prompts(marked(&fakeSession{})) {
		t.Fatal("marked ctx does not report prompts")
	}
}

func TestNilSessionIsIgnored(t *testing.T) {
	ctx := WithPrompts(context.Background(), func() PromptSession { return nil })
	if _, err := Run(ctx, t.TempDir(), ReadTimeout, "--version"); err != nil {
		t.Fatal(err)
	}
}

func TestRunInputFeedsStdin(t *testing.T) {
	out, err := RunInput(context.Background(), t.TempDir(), 5*time.Second, nil, "x", "hash-object", "--stdin")
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("%x", sha1.Sum([]byte("blob 1\x00x")))
	if strings.TrimSpace(out) != want {
		t.Fatalf("got %q want %q", out, want)
	}
}
