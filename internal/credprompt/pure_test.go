package credprompt

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestParseCredential(t *testing.T) {
	f, err := ParseCredential(strings.NewReader("protocol=https\nhost=github.com\nusername=me\n\nignored=1\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"protocol": "https", "host": "github.com", "username": "me"}
	if !reflect.DeepEqual(f, want) {
		t.Fatalf("got %v", f)
	}
}

func TestParseCredentialEOFWithoutBlankLine(t *testing.T) {
	f, _ := ParseCredential(strings.NewReader("host=a"))
	if f["host"] != "a" {
		t.Fatalf("got %v", f)
	}
}

func TestFormatCredentialSortedAndRoundTrips(t *testing.T) {
	in := map[string]string{"username": "me", "password": "p=w", "host": "h", "protocol": "https"}
	s := FormatCredential(in)
	if s != "host=h\npassword=p=w\nprotocol=https\nusername=me\n" {
		t.Fatalf("got %q", s)
	}
	back, _ := ParseCredential(strings.NewReader(s))
	if !reflect.DeepEqual(back, in) {
		t.Fatalf("round trip %v", back)
	}
}

func TestCredentialURL(t *testing.T) {
	cases := map[string]map[string]string{
		"https://github.com":           {"protocol": "https", "host": "github.com"},
		"https://github.com/org/r.git": {"protocol": "https", "host": "github.com", "path": "org/r.git"},
		"http://127.0.0.1:8080/x":      {"protocol": "http", "host": "127.0.0.1:8080", "path": "x"},
	}
	for want, f := range cases {
		if got := CredentialURL(f); got != want {
			t.Errorf("%v → %q, want %q", f, got, want)
		}
	}
}

func TestClassifyPrompt(t *testing.T) {
	hostKey := "The authenticity of host 'github.com (140.82.121.4)' can't be established.\n" +
		"ED25519 key fingerprint is SHA256:+DiY3wvvV6TuJJhbpZisF/zLDA0zPMSvHdkr4UvCOqU.\n" +
		"This key is not known by any other names.\n" +
		"Are you sure you want to continue connecting (yes/no/[fingerprint])? "
	rsaPort := "The authenticity of host '[git.example.com]:2222 ([10.0.0.1]:2222)' can't be established.\n" +
		"RSA key fingerprint is SHA256:abcDEF123.\n" +
		"Are you sure you want to continue connecting (yes/no/[fingerprint])? "
	cases := []struct {
		in   string
		want Prompt
	}{
		{"Enter passphrase for key '/Users/me/.ssh/id_ed25519': ", Prompt{Kind: KindPassphrase, KeyPath: "/Users/me/.ssh/id_ed25519"}},
		{"git@example.com's password: ", Prompt{Kind: KindPassword, User: "git", Host: "example.com"}},
		{hostKey, Prompt{Kind: KindHostKey, Host: "github.com", KeyType: "ED25519", Fingerprint: "SHA256:+DiY3wvvV6TuJJhbpZisF/zLDA0zPMSvHdkr4UvCOqU"}},
		{rsaPort, Prompt{Kind: KindHostKey, Host: "[git.example.com]:2222", KeyType: "RSA", Fingerprint: "SHA256:abcDEF123"}},
		{"Username for 'https://github.com': ", Prompt{Kind: KindGitPrompt}},
		{"Password for 'https://me@github.com': ", Prompt{Kind: KindGitPrompt}},
		{"Verification code: ", Prompt{Kind: KindOther, Text: "Verification code:"}},
	}
	for _, c := range cases {
		got := ClassifyPrompt(c.in)
		if c.want.Text == "" && c.want.Kind != KindOther {
			got.Text = ""
		}
		if got != c.want {
			t.Errorf("%q\n got  %+v\n want %+v", c.in, got, c.want)
		}
	}
}

func TestShellQuoteSurvivesSh(t *testing.T) {
	for _, s := range []string{"/Applications/CommitTree.app/Contents/MacOS/CommitTree", "/Apps/My Apps/Commit'Tree", `/a/"b"/$c`} {
		out, err := exec.Command("sh", "-c", "printf %s "+shellQuote(s)).Output()
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != s {
			t.Errorf("%q came back as %q", s, out)
		}
	}
}

func TestSessionEnv(t *testing.T) {
	got := sessionEnv("/x/Commit Tree", "/tmp/ct-1/s", "abc", "")
	want := []string{
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=credential.helper",
		"GIT_CONFIG_VALUE_0=!'/x/Commit Tree' credential-helper",
		"GIT_ASKPASS=/x/Commit Tree",
		"SSH_ASKPASS=/x/Commit Tree",
		"SSH_ASKPASS_REQUIRE=force",
		"COMMITREE_PROMPT_SOCK=/tmp/ct-1/s",
		"COMMITREE_PROMPT_NONCE=abc",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q", got)
	}
}

func TestSessionEnvAfterInheritedCount(t *testing.T) {
	got := sessionEnv("/x", "/s", "n", "2")
	if got[0] != "GIT_CONFIG_COUNT=3" || got[1] != "GIT_CONFIG_KEY_2=credential.helper" || !strings.HasPrefix(got[2], "GIT_CONFIG_VALUE_2=") {
		t.Fatalf("got %q", got[:3])
	}
	if bad := sessionEnv("/x", "/s", "n", "junk"); bad[0] != "GIT_CONFIG_COUNT=1" {
		t.Fatalf("junk count: %q", bad[0])
	}
}

func TestHelperLabel(t *testing.T) {
	cases := map[string]string{
		"osxkeychain":           "osxkeychain",
		"store --file ~/.creds": "store",
		"/usr/local/share/gcm-core/git-credential-manager": "manager",
		"!f() { echo; }; f":     "your credential helper",
		"  cache --timeout=60 ": "cache",
	}
	for in, want := range cases {
		if got := helperLabel(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}
