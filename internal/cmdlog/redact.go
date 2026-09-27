package cmdlog

import (
	"regexp"
	"strings"
)

// urlCreds matches the userinfo of a URL: scheme://user[:password]@.
var urlCreds = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)([^/@\s:]+)(:[^/@\s]*)?@`)

// minSecret is the shortest secret masked in output; shorter ones would
// mask ordinary text.
const minSecret = 4

// RedactArgs returns a copy of args with credentials replaced by ***, and
// the secrets it removed so MaskOutput can hide them in git's output too.
func RedactArgs(args []string) ([]string, []string) {
	out := make([]string, len(args))
	var secrets []string
	for i, a := range args {
		if i > 0 && args[i-1] == "-c" {
			if key, value, ok := strings.Cut(a, "="); ok && secretKey(key) {
				out[i] = key + "=***"
				secrets = append(secrets, value)
				continue
			}
		}
		out[i] = redactURLs(a, &secrets)
	}
	return out, secrets
}

// MaskOutput hides every secret, and any URL credentials, in s.
func MaskOutput(s string, secrets []string) string {
	for _, sec := range secrets {
		if len(sec) >= minSecret {
			s = strings.ReplaceAll(s, sec, "***")
		}
	}
	return redactURLs(s, nil)
}

func secretKey(key string) bool {
	k := strings.ToLower(key)
	if strings.HasPrefix(k, "http.") && strings.HasSuffix(k, ".extraheader") {
		return true
	}
	return strings.Contains(k, "token") || strings.Contains(k, "password") || strings.Contains(k, "secret")
}

// redactURLs replaces user:password@ with user:***@ and a bare user@ (often
// a token) with ***@, appending what it removed to secrets when non-nil.
func redactURLs(s string, secrets *[]string) string {
	return urlCreds.ReplaceAllStringFunc(s, func(m string) string {
		p := urlCreds.FindStringSubmatch(m)
		scheme, user, pass := p[1], p[2], p[3]
		if pass != "" {
			if secrets != nil {
				*secrets = append(*secrets, pass[1:])
			}
			return scheme + user + ":***@"
		}
		if secrets != nil {
			*secrets = append(*secrets, user)
		}
		return scheme + "***@"
	})
}
