package credprompt

import (
	"regexp"
	"strings"
)

// Kind is what a request asks for; the dialog has one shape per kind.
type Kind string

const (
	KindHTTPS      Kind = "https"
	KindPassphrase Kind = "ssh-passphrase"
	KindPassword   Kind = "ssh-password"
	KindHostKey    Kind = "ssh-hostkey"
	KindOther      Kind = "ssh-other"
	// KindGitPrompt is git's own "Username for …" / "Password for …"
	// askpass prompt. CommitTree's credential helper already asked (or the
	// user cancelled), so it is always refused: never a second dialog.
	KindGitPrompt Kind = "git-prompt"
)

// Prompt is an askpass prompt text, recognised.
type Prompt struct {
	Kind                          Kind
	Host, User                    string
	KeyPath, KeyType, Fingerprint string
	// Text is the prompt trimmed, shown verbatim for KindOther.
	Text string
}

var (
	rePassphrase = regexp.MustCompile(`^Enter passphrase for key '(.+)':$`)
	rePassword   = regexp.MustCompile(`^(\S+)@(\S+)'s password:$`)
	reHostKey    = regexp.MustCompile(`The authenticity of host '([^' ]+)`)
	reKeyLine    = regexp.MustCompile(`(?m)^(\S+) key fingerprint is (\S+?)\.?$`)
	reGitPrompt  = regexp.MustCompile(`^(Username|Password) for '.*':$`)
)

// ClassifyPrompt recognises the prompts OpenSSH and git pass to askpass.
// Output is in English (gitcmd sets LC_ALL=C).
func ClassifyPrompt(text string) Prompt {
	t := strings.TrimSpace(text)
	p := Prompt{Kind: KindOther, Text: t}
	switch {
	case reGitPrompt.MatchString(t):
		p.Kind = KindGitPrompt
	case rePassphrase.MatchString(t):
		p.Kind = KindPassphrase
		p.KeyPath = rePassphrase.FindStringSubmatch(t)[1]
	case rePassword.MatchString(t):
		m := rePassword.FindStringSubmatch(t)
		p.Kind, p.User, p.Host = KindPassword, m[1], m[2]
	case strings.Contains(t, "Are you sure you want to continue connecting") && reHostKey.MatchString(t):
		p.Kind = KindHostKey
		p.Host = reHostKey.FindStringSubmatch(t)[1]
		if m := reKeyLine.FindStringSubmatch(t); m != nil {
			p.KeyType, p.Fingerprint = m[1], m[2]
		}
	}
	return p
}
