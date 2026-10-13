package credprompt

import (
	"path/filepath"
	"strconv"
	"strings"
)

// The helper finds the app through these; their presence is also what makes
// main() run as the helper.
const (
	EnvSock  = "COMMITREE_PROMPT_SOCK"
	EnvNonce = "COMMITREE_PROMPT_NONCE"
)

// shellQuote quotes s for sh: git runs a "!" helper through the shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// sessionEnv is the environment of one prompting git command. CommitTree is
// appended as the last credential helper through GIT_CONFIG_COUNT, after any
// count the user's environment already has (inheritedCount; junk counts as
// 0), so the user's helpers are asked first and args stay as they are.
func sessionEnv(exe, sock, nonce, inheritedCount string) []string {
	n, err := strconv.Atoi(strings.TrimSpace(inheritedCount))
	if err != nil || n < 0 {
		n = 0
	}
	i := strconv.Itoa(n)
	return []string{
		"GIT_CONFIG_COUNT=" + strconv.Itoa(n+1),
		"GIT_CONFIG_KEY_" + i + "=credential.helper",
		"GIT_CONFIG_VALUE_" + i + "=!" + shellQuote(exe) + " credential-helper",
		"GIT_ASKPASS=" + exe,
		"SSH_ASKPASS=" + exe,
		"SSH_ASKPASS_REQUIRE=force",
		EnvSock + "=" + sock,
		EnvNonce + "=" + nonce,
	}
}

// helperLabel names a credential.helper value for "Remember in <name>".
func helperLabel(value string) string {
	v := strings.TrimSpace(value)
	if v == "" || strings.HasPrefix(v, "!") {
		return "your credential helper"
	}
	name := filepath.Base(strings.Fields(v)[0])
	return strings.TrimPrefix(name, "git-credential-")
}
