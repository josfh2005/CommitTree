// Package credprompt lets git and ssh ask the user for credentials through
// CommitTree: a socket server in the app, and the same binary relaunched as
// git's credential helper and as git's/ssh's askpass program.
package credprompt

import (
	"bufio"
	"io"
	"sort"
	"strings"
)

// ParseCredential reads git's credential protocol: key=value lines up to a
// blank line or EOF. A line without "=" is skipped; a repeated key keeps the
// last value.
func ParseCredential(r io.Reader) (map[string]string, error) {
	f := map[string]string{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 64*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			break
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		f[k] = v
	}
	return f, sc.Err()
}

// FormatCredential writes f in git's credential protocol, keys sorted.
func FormatCredential(f map[string]string) string {
	keys := make([]string, 0, len(f))
	for k := range f {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k + "=" + f[k] + "\n")
	}
	return b.String()
}

// CredentialURL is the URL a credential request is about:
// protocol://host[/path].
func CredentialURL(f map[string]string) string {
	u := f["protocol"] + "://" + f["host"]
	if p := f["path"]; p != "" {
		u += "/" + strings.TrimPrefix(p, "/")
	}
	return u
}
