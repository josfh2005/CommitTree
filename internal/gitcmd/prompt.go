package gitcmd

import "context"

// PromptSession is what a context marked by WithPrompts gives one git
// command: environment that lets git and ssh ask the user through
// CommitTree, and an End called once git has exited.
type PromptSession interface {
	// Env is appended to git's environment before the caller's own env, so
	// a variable the caller sets with the same name beats the session's.
	// That is entry by entry: NoPromptEnv, say, overrides the session's
	// GIT_TERMINAL_PROMPT-style variables, but not its GIT_CONFIG_* helper
	// entries, which NoPromptEnv does not set.
	Env() []string
	// End closes the session; failed says whether git exited unsuccessfully.
	// A non-nil result is why it failed — the user cancelled or let the
	// dialog time out, or the credential was refused — and replaces git's
	// own message.
	End(failed bool) error
}

type promptKey struct{}

// WithPrompts marks ctx: every git command run with it calls begin just
// before starting and, when begin returns a session, runs with its Env.
// Unmarked contexts never prompt.
func WithPrompts(ctx context.Context, begin func() PromptSession) context.Context {
	return context.WithValue(ctx, promptKey{}, begin)
}

func promptsFrom(ctx context.Context) func() PromptSession {
	if ctx == nil {
		return nil
	}
	b, _ := ctx.Value(promptKey{}).(func() PromptSession)
	return b
}

// Prompts reports whether ctx was marked by WithPrompts, that is, whether
// commands run with it may ask the user for credentials.
func Prompts(ctx context.Context) bool { return promptsFrom(ctx) != nil }

// PromptError is a prompt session's reason a command failed. Its message is
// the whole error the user sees: it says what happened in their words, where
// git's own stderr ("could not read Username…") would not.
type PromptError struct{ Msg string }

func (e *PromptError) Error() string { return e.Msg }
