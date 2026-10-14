//go:build !windows

package credprompt

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"git-ui/internal/gitcmd"
)

// fakeUI records what the server sent. Read it only through the accessor
// methods: the server calls the UI from its own goroutine.
type fakeUI struct {
	mu       sync.Mutex
	shownL   []Request
	closedL  []string
	noticesL []string
	showCh   chan Request
	closedCh chan string
}

func newFakeUI() *fakeUI {
	return &fakeUI{showCh: make(chan Request, 20), closedCh: make(chan string, 20)}
}

func (f *fakeUI) ui() UI {
	return UI{
		Show: func(r Request) {
			f.mu.Lock()
			f.shownL = append(f.shownL, r)
			f.mu.Unlock()
			f.showCh <- r
		},
		Closed: func(id string) {
			f.mu.Lock()
			f.closedL = append(f.closedL, id)
			f.mu.Unlock()
			f.closedCh <- id
		},
		Notice: func(t string) { f.mu.Lock(); f.noticesL = append(f.noticesL, t); f.mu.Unlock() },
	}
}

func (f *fakeUI) shown() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Request(nil), f.shownL...)
}

func (f *fakeUI) notices() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.noticesL...)
}

func (f *fakeUI) next(t *testing.T) Request {
	t.Helper()
	select {
	case r := <-f.showCh:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("no request shown")
	}
	return Request{}
}

// nextClosed waits for the next credential:closed id.
func (f *fakeUI) nextClosed(t *testing.T) string {
	t.Helper()
	select {
	case id := <-f.closedCh:
		return id
	case <-time.After(5 * time.Second):
		t.Fatal("nothing closed")
	}
	return ""
}

func newTestServer(t *testing.T, timeout time.Duration) (*Server, *fakeUI) {
	t.Helper()
	ui := newFakeUI()
	s, err := NewServer(Options{Exe: "/unused", Timeout: timeout}, ui.ui())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Shutdown)
	return s, ui
}

// helperRun runs RunHelper in-process with the session's environment.
func helperRun(sess gitcmd.PromptSession, args []string, stdin string) (int, string) {
	env := map[string]string{}
	for _, kv := range sess.Env() {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	var out bytes.Buffer
	code := RunHelper(args, func(k string) string { return env[k] }, strings.NewReader(stdin), &out)
	return code, out.String()
}

type result struct {
	code int
	out  string
}

func goHelper(sess gitcmd.PromptSession, args []string, stdin string) chan result {
	ch := make(chan result, 1)
	go func() { c, o := helperRun(sess, args, stdin); ch <- result{c, o} }()
	return ch
}

const getHTTPS = "protocol=https\nhost=h.test\n\n"

func TestGetIsAnswered(t *testing.T) {
	s, ui := newTestServer(t, time.Minute)
	sess := s.begin(Info{RepoID: "r1", Context: "Push · repo"})
	done := goHelper(sess, []string{"credential-helper", "get"}, getHTTPS)
	req := ui.next(t)
	if req.Kind != KindHTTPS || req.Host != "h.test" || req.Context != "Push · repo" || req.RepoID != "r1" {
		t.Fatalf("request %+v", req)
	}
	if err := s.Answer(req.ID, Answer{Username: "me", Secret: "tok"}); err != nil {
		t.Fatal(err)
	}
	r := <-done
	if r.code != 0 || r.out != "password=tok\nusername=me\n" {
		t.Fatalf("helper %d %q", r.code, r.out)
	}
	if err := sess.End(false); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownNonceIsRefused(t *testing.T) {
	s, ui := newTestServer(t, time.Minute)
	sess := s.begin(Info{})
	_ = sess.End(false) // nonce no longer valid
	code, out := helperRun(sess, []string{"credential-helper", "get"}, getHTTPS)
	if code != 0 || out != "" {
		t.Fatalf("get: %d %q", code, out)
	}
	code, _ = helperRun(sess, []string{"Enter passphrase for key '/k': "}, "")
	if code == 0 {
		t.Fatal("askpass with a dead nonce succeeded")
	}
	if len(ui.shown()) != 0 {
		t.Fatal("something was shown")
	}
}

func TestQueueShowsOneAtATime(t *testing.T) {
	s, ui := newTestServer(t, time.Minute)
	a, b := s.begin(Info{RepoID: "a"}), s.begin(Info{RepoID: "b"})
	doneA := goHelper(a, []string{"credential-helper", "get"}, getHTTPS)
	first := ui.next(t)
	doneB := goHelper(b, []string{"credential-helper", "get"}, getHTTPS)
	again := ui.next(t) // head re-sent with waiting=1
	if again.ID != first.ID || again.Waiting != 1 || again.Deadline != first.Deadline {
		t.Fatalf("re-show %+v (first %+v)", again, first)
	}
	_ = s.Answer(first.ID, Answer{Username: "u", Secret: "p"})
	<-doneA
	second := ui.next(t)
	if second.RepoID != "b" || second.Waiting != 0 {
		t.Fatalf("second %+v", second)
	}
	_ = s.Answer(second.ID, Answer{Username: "u", Secret: "p"})
	<-doneB
}

func TestTimeoutOutcome(t *testing.T) {
	s, ui := newTestServer(t, 50*time.Millisecond)
	sess := s.begin(Info{})
	done := goHelper(sess, []string{"credential-helper", "get"}, getHTTPS)
	req := ui.next(t)
	r := <-done
	if r.out != "" {
		t.Fatalf("answered %q", r.out)
	}
	err := sess.End(true)
	if err == nil || err.Error() != "No credentials entered (timed out)." {
		t.Fatalf("outcome %v", err)
	}
	if id := ui.nextClosed(t); id != req.ID {
		t.Fatalf("closed %q want %q", id, req.ID)
	}
}

func TestCancelOutcomes(t *testing.T) {
	s, ui := newTestServer(t, time.Minute)
	sess := s.begin(Info{})
	done := goHelper(sess, []string{"credential-helper", "get"}, getHTTPS)
	_ = s.Cancel(ui.next(t).ID)
	<-done
	if err := sess.End(true); err == nil || err.Error() != "No credentials entered." {
		t.Fatalf("https cancel %v", err)
	}

	ssh := s.begin(Info{})
	hk := "The authenticity of host 'h (1.2.3.4)' can't be established.\nED25519 key fingerprint is SHA256:x.\nAre you sure you want to continue connecting (yes/no/[fingerprint])? "
	done = goHelper(ssh, []string{hk}, "")
	req := ui.next(t)
	if req.Kind != KindHostKey || req.Fingerprint != "SHA256:x" || req.Host != "h" {
		t.Fatalf("hostkey %+v", req)
	}
	_ = s.Cancel(req.ID)
	if r := <-done; r.code == 0 {
		t.Fatal("cancelled host key exited 0")
	}
	if err := ssh.End(true); err == nil || err.Error() != "The SSH host was not trusted." {
		t.Fatalf("hostkey cancel %v", err)
	}
}

func TestHostKeyTrustAnswersYes(t *testing.T) {
	s, ui := newTestServer(t, time.Minute)
	sess := s.begin(Info{})
	done := goHelper(sess, []string{"The authenticity of host 'h (1.2.3.4)' can't be established.\nRSA key fingerprint is SHA256:y.\nAre you sure you want to continue connecting (yes/no/[fingerprint])? "}, "")
	_ = s.Answer(ui.next(t).ID, Answer{Trust: true})
	if r := <-done; r.code != 0 || r.out != "yes\n" {
		t.Fatalf("got %d %q", r.code, r.out)
	}
}

func TestEndClosesPending(t *testing.T) {
	s, ui := newTestServer(t, time.Minute)
	sess := s.begin(Info{})
	done := goHelper(sess, []string{"credential-helper", "get"}, getHTTPS)
	req := ui.next(t)
	if err := sess.End(true); err != nil {
		t.Fatalf("ending a session is not a credential outcome: %v", err)
	}
	if r := <-done; r.out != "" {
		t.Fatalf("answered %q", r.out)
	}
	if id := ui.nextClosed(t); id != req.ID {
		t.Fatalf("closed %q want %q", id, req.ID)
	}
}

func TestEraseOutcomes(t *testing.T) {
	s, ui := newTestServer(t, time.Minute)
	sess := s.begin(Info{})
	done := goHelper(sess, []string{"credential-helper", "get"}, getHTTPS)
	_ = s.Answer(ui.next(t).ID, Answer{Username: "me", Secret: "bad"})
	<-done
	helperRun(sess, []string{"credential-helper", "erase"}, getHTTPS+"username=me\npassword=bad\n")
	if err := sess.End(true); err == nil || err.Error() != "The username or token was not accepted." {
		t.Fatalf("typed then erased: %v", err)
	}

	stale := s.begin(Info{})
	helperRun(stale, []string{"credential-helper", "erase"}, getHTTPS)
	if err := stale.End(true); err == nil || !strings.HasPrefix(err.Error(), "The saved credential was not accepted") {
		t.Fatalf("stale: %v", err)
	}
}

func TestPassphraseRetry(t *testing.T) {
	s, ui := newTestServer(t, time.Minute)
	sess := s.begin(Info{})
	p := []string{"Enter passphrase for key '/k': "}
	done := goHelper(sess, p, "")
	r1 := ui.next(t)
	_ = s.Answer(r1.ID, Answer{Secret: "wrong"})
	<-done
	done = goHelper(sess, p, "")
	r2 := ui.next(t)
	if r1.Retry || !r2.Retry || r2.KeyPath != "/k" {
		t.Fatalf("retry flags %v %v", r1.Retry, r2)
	}
	_ = s.Answer(r2.ID, Answer{Secret: "right"})
	if r := <-done; r.out != "right\n" {
		t.Fatalf("got %q", r.out)
	}
}

func TestGitOwnPromptIsRefused(t *testing.T) {
	s, ui := newTestServer(t, time.Minute)
	sess := s.begin(Info{})
	code, _ := helperRun(sess, []string{"Username for 'https://h.test': "}, "")
	if code == 0 || len(ui.shown()) != 0 {
		t.Fatalf("code %d shown %d", code, len(ui.shown()))
	}
}

func TestAnswerRejectsLineBreaks(t *testing.T) {
	s, ui := newTestServer(t, time.Minute)
	sess := s.begin(Info{})
	done := goHelper(sess, []string{"credential-helper", "get"}, getHTTPS)
	req := ui.next(t)
	if err := s.Answer(req.ID, Answer{Username: "me\nhost=evil", Secret: "x"}); !errors.Is(err, ErrLineBreak) {
		t.Fatalf("got %v", err)
	}
	_ = s.Answer(req.ID, Answer{Username: "me", Secret: "x"})
	<-done
	if err := s.Answer("nope", Answer{}); !errors.Is(err, ErrUnknownRequest) {
		t.Fatalf("unknown id: %v", err)
	}
}

func TestNoticeIsNotADialog(t *testing.T) {
	s, ui := newTestServer(t, time.Minute)
	sess := s.begin(Info{})
	env := map[string]string{"SSH_ASKPASS_PROMPT": "none"}
	for _, kv := range sess.Env() {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	code := RunHelper([]string{"Confirm user presence for key ED25519-SK"}, func(k string) string { return env[k] }, strings.NewReader(""), &bytes.Buffer{})
	if code != 0 || len(ui.shown()) != 0 || len(ui.notices()) != 1 {
		t.Fatalf("code %d shown %d notices %v", code, len(ui.shown()), ui.notices())
	}
}

func TestSocketIsPrivate(t *testing.T) {
	s, _ := newTestServer(t, time.Minute)
	di, err := os.Stat(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	si, err := os.Stat(s.sock)
	if err != nil {
		t.Fatal(err)
	}
	if m := di.Mode().Perm(); m != 0o700 {
		t.Errorf("socket directory mode %v, want 0700", m)
	}
	if m := si.Mode().Perm(); m != 0o600 {
		t.Errorf("socket mode %v, want 0600", m)
	}
}

func TestShutdownRefusesAndRemovesSocket(t *testing.T) {
	s, ui := newTestServer(t, time.Minute)
	sess := s.begin(Info{})
	done := goHelper(sess, []string{"credential-helper", "get"}, getHTTPS)
	ui.next(t)
	dir := s.dir
	s.Shutdown()
	if r := <-done; r.out != "" {
		t.Fatalf("answered %q", r.out)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("socket dir still there: %v", err)
	}
	if s.begin(Info{}) != nil {
		t.Fatal("a session began after Shutdown")
	}
	// A helper still holding an old nonce is refused too.
	if code, out := helperRun(sess, []string{"credential-helper", "get"}, getHTTPS); code != 0 || out != "" {
		t.Fatalf("get after Shutdown: %d %q", code, out)
	}
	if n := len(ui.shown()); n != 1 {
		t.Fatalf("shown %d dialogs, want 1", n)
	}
}

func TestNilServerAllowIsIdentity(t *testing.T) {
	var s *Server
	ctx := context.Background()
	if s.Allow(ctx, Info{}) != ctx {
		t.Fatal("nil server changed ctx")
	}
}

func TestAnswerRejectsNul(t *testing.T) {
	s, ui := newTestServer(t, time.Minute)
	sess := s.begin(Info{})
	done := goHelper(sess, []string{"credential-helper", "get"}, getHTTPS)
	req := ui.next(t)
	for _, a := range []Answer{{Username: "me\x00", Secret: "x"}, {Username: "me", Secret: "x\x00y"}, {Username: "me", Secret: "x\ry"}} {
		if err := s.Answer(req.ID, a); !errors.Is(err, ErrLineBreak) {
			t.Fatalf("%+v: got %v", a, err)
		}
	}
	_ = s.Cancel(req.ID)
	<-done
}

func TestRemovingAWaitingRequestRefreshesTheHead(t *testing.T) {
	s, ui := newTestServer(t, time.Minute)
	a, b, c := s.begin(Info{RepoID: "a"}), s.begin(Info{RepoID: "b"}), s.begin(Info{RepoID: "c"})
	doneA := goHelper(a, []string{"credential-helper", "get"}, getHTTPS)
	first := ui.next(t)
	doneB := goHelper(b, []string{"credential-helper", "get"}, getHTTPS)
	if r := ui.next(t); r.Waiting != 1 {
		t.Fatalf("after b: %+v", r)
	}
	doneC := goHelper(c, []string{"credential-helper", "get"}, getHTTPS)
	if r := ui.next(t); r.Waiting != 2 {
		t.Fatalf("after c: %+v", r)
	}
	_ = c.End(true) // c's git command died while its request waited
	if r := <-doneC; r.out != "" {
		t.Fatalf("answered %q", r.out)
	}
	again := ui.next(t)
	if again.ID != first.ID || again.Waiting != 1 || again.Deadline != first.Deadline {
		t.Fatalf("head after c left: %+v (first %+v)", again, first)
	}
	select {
	case id := <-ui.closedCh:
		t.Fatalf("closed %q: the shown head did not leave", id)
	default:
	}
	_ = s.Answer(first.ID, Answer{Username: "u", Secret: "p"})
	<-doneA
	second := ui.next(t)
	if second.RepoID != "b" || second.Waiting != 0 {
		t.Fatalf("second %+v", second)
	}
	_ = s.Answer(second.ID, Answer{Username: "u", Secret: "p"})
	<-doneB
}

func TestShutdownDoesNotShowTheNextRequest(t *testing.T) {
	s, ui := newTestServer(t, time.Minute)
	a, b := s.begin(Info{RepoID: "a"}), s.begin(Info{RepoID: "b"})
	doneA := goHelper(a, []string{"credential-helper", "get"}, getHTTPS)
	first := ui.next(t)
	doneB := goHelper(b, []string{"credential-helper", "get"}, getHTTPS)
	ui.next(t) // head re-sent with waiting=1
	s.Shutdown()
	if r := <-doneA; r.out != "" {
		t.Fatalf("a answered %q", r.out)
	}
	if r := <-doneB; r.out != "" {
		t.Fatalf("b answered %q", r.out)
	}
	if id := ui.nextClosed(t); id != first.ID {
		t.Fatalf("closed %q want %q", id, first.ID)
	}
	flushed := make(chan struct{})
	s.out <- func() { close(flushed) } // everything queued before has been delivered
	<-flushed
	if n := len(ui.shown()); n != 2 {
		t.Fatalf("shown %d requests, want 2 (head, head re-sent)", n)
	}
	select {
	case id := <-ui.closedCh:
		t.Fatalf("second close %q: only the shown request closes", id)
	default:
	}
}
