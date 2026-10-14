package app

import (
	"context"

	"git-ui/internal/cmdlog"
	"git-ui/internal/credprompt"
)

// CredentialClosed is credential:closed: the dialog with this id is gone
// (answered, cancelled, timed out, or its command ended).
type CredentialClosed struct {
	ID string `json:"id"`
}

// CredentialNotice is credential:notice: ssh telling the user something
// that needs no answer (e.g. "touch your security key").
type CredentialNotice struct {
	Text string `json:"text"`
}

// WithCredentials starts the credential server, so the network commands the
// user starts can ask for credentials (docs/spec/05-remote-and-stash.md).
// Without it, or when it returns an error (Windows, no socket), nothing
// prompts, as before. o.Exe is the program git and ssh run as helper.
func WithCredentials(a *App, o credprompt.Options) error {
	s, err := credprompt.NewServer(o, a.credentialUI())
	if err != nil {
		return err
	}
	a.creds = s
	return nil
}

// credentialUI is how the credential server reaches the frontend. Payloads
// never carry an answer. The server calls these from its own goroutine,
// never with its lock held. It is unexported: Wails binds every exported
// App method.
func (a *App) credentialUI() credprompt.UI {
	return credprompt.UI{
		Show:   func(r credprompt.Request) { a.emitUnlessShutdown("credential:request", r) },
		Closed: func(id string) { a.emitUnlessShutdown("credential:closed", CredentialClosed{ID: id}) },
		Notice: func(text string) { a.emitUnlessShutdown("credential:notice", CredentialNotice{Text: text}) },
	}
}

// AnswerCredential is the dialog's Continue (Trust and connect for a host
// key). The answer is handed to git and never logged.
func (a *App) AnswerCredential(id string, ans credprompt.Answer) error {
	if a.creds == nil {
		return credprompt.ErrUnknownRequest
	}
	return a.creds.Answer(id, ans)
}

// CancelCredential is the dialog's Cancel.
func (a *App) CancelCredential(id string) error {
	if a.creds == nil {
		return credprompt.ErrUnknownRequest
	}
	return a.creds.Cancel(id)
}

// prompting marks ctx so the network command run with it may ask; op is the
// context line's verb, e.g. "Push" or "Fetch origin".
func (a *App) prompting(ctx context.Context, id, dir, op string) context.Context {
	if a.creds == nil {
		return ctx
	}
	name := id
	if r, ok := a.repo(id); ok {
		name = r.Name
	}
	_, fromAI := a.aiWrites.Load(cmdlog.RepoKey(dir))
	return a.creds.Allow(ctx, credprompt.Info{RepoID: id, Dir: dir, Context: op + " · " + name, FromAI: fromAI})
}
