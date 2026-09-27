package cmdlog

import (
	"reflect"
	"strings"
	"testing"
)

func TestRedactArgs(t *testing.T) {
	cases := []struct{ in, want string }{
		{"push https://bob:hunter22@github.com/o/r.git main", "push https://bob:***@github.com/o/r.git main"},
		{"fetch https://ghp_abcdef123@github.com/o/r.git", "fetch https://***@github.com/o/r.git"},
		{"-c http.https://github.com/.extraheader=AUTHORIZATION:_basic_c2VjcmV0 fetch", "-c http.https://github.com/.extraheader=*** fetch"},
		{"-c credential.token=abcd1234 push", "-c credential.token=*** push"},
		{"-c my.Password=letmein push", "-c my.Password=*** push"},
		{"-c core.quotepath=false status", "-c core.quotepath=false status"},
		{"remote add origin git@github.com:o/r.git", "remote add origin git@github.com:o/r.git"},
		{"push ssh://git@host/r.git", "push ssh://***@host/r.git"},
	}
	for _, c := range cases {
		got, _ := RedactArgs(strings.Fields(c.in))
		if strings.Join(got, " ") != c.want {
			t.Errorf("%q:\n got %q\nwant %q", c.in, strings.Join(got, " "), c.want)
		}
	}
}

func TestRedactArgsDoesNotModifyInput(t *testing.T) {
	in := []string{"push", "https://bob:hunter22@h/r"}
	RedactArgs(in)
	if !reflect.DeepEqual(in, []string{"push", "https://bob:hunter22@h/r"}) {
		t.Fatalf("input changed: %v", in)
	}
}

func TestMaskOutputHidesSecretsAndURLCreds(t *testing.T) {
	_, secrets := RedactArgs([]string{"push", "https://bob:hunter22@github.com/o/r.git"})
	out := MaskOutput("fatal: unable to access 'https://bob:hunter22@github.com/o/r.git/': denied; hunter22 again", secrets)
	if strings.Contains(out, "hunter22") {
		t.Fatalf("secret leaked: %q", out)
	}
	if !strings.Contains(out, "https://bob:***@github.com") {
		t.Fatalf("URL not redacted: %q", out)
	}
}

func TestMaskOutputIgnoresTinySecrets(t *testing.T) {
	// A 1-3 character "secret" would mask ordinary letters all over the output.
	if got := MaskOutput("a b c", []string{"a"}); got != "a b c" {
		t.Fatalf("got %q", got)
	}
}
