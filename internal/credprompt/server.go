//go:build !windows

package credprompt

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"git-ui/internal/gitcmd"
)

// DefaultTimeout is how long a dialog waits for the user.
const DefaultTimeout = time.Minute

var (
	ErrUnknownRequest = errors.New("credprompt: no such request")
	ErrLineBreak      = errors.New("Line breaks are not allowed.")

	errTimedOut = errors.New("timed out")
	errCancel   = errors.New("cancelled")
	errEnded    = errors.New("session ended")
)

// Info says which command a session belongs to.
type Info struct {
	RepoID string
	// Dir is where "Remember" discovery runs; "" for a clone.
	Dir string
	// Context is the dialog's context line, e.g. "Push · my-repo".
	Context string
	FromAI  bool
}

// Request is one dialog, sent to the frontend as credential:request. It
// never carries an answer.
type Request struct {
	ID          string          `json:"id"`
	Kind        Kind            `json:"kind"`
	RepoID      string          `json:"repoId"`
	Context     string          `json:"context"`
	FromAI      bool            `json:"fromAI"`
	Host        string          `json:"host"`
	Path        string          `json:"path,omitempty"`
	Username    string          `json:"username,omitempty"`
	Prompt      string          `json:"prompt,omitempty"`
	KeyPath     string          `json:"keyPath,omitempty"`
	KeyType     string          `json:"keyType,omitempty"`
	Fingerprint string          `json:"fingerprint,omitempty"`
	Remember    *RememberTarget `json:"remember"`
	Retry       bool            `json:"retry"`
	Waiting     int             `json:"waiting"`
	Deadline    int64           `json:"deadline"`
}

// Answer is what the dialog sends back.
type Answer struct {
	Username string `json:"username"`
	Secret   string `json:"secret"`
	Remember bool   `json:"remember"`
	Trust    bool   `json:"trust"`
}

// UI is how the server reaches the frontend.
type UI struct {
	Show   func(Request)
	Closed func(id string)
	Notice func(text string)
}

type Options struct {
	// Exe is the program git and ssh run: CommitTree's own binary.
	Exe string
	// Timeout per shown dialog; 0 means DefaultTimeout.
	Timeout time.Duration
	// Platform is DetectPlatformHelper's answer ("" for none).
	Platform string
}

type reply struct {
	ok  bool
	a   Answer
	why error
}

type pending struct {
	req   Request
	s     *session
	reply chan reply
	timer *time.Timer
}

// Server is the app side: sessions, the queue of requests, the socket.
type Server struct {
	opts Options
	ui   UI
	dir  string
	sock string
	ln   net.Listener

	mu       sync.Mutex
	sessions map[string]*session
	queue    []*pending
	nextID   int
	closed   bool

	// out carries UI calls, in order, to the one goroutine (pump) that
	// makes them — never with mu held.
	out chan func()
}

func NewServer(o Options, ui UI) (*Server, error) {
	if o.Timeout == 0 {
		o.Timeout = DefaultTimeout
	}
	dir, err := os.MkdirTemp("", "ct-")
	if err != nil {
		return nil, err
	}
	sock := filepath.Join(dir, "s")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	if err := os.Chmod(sock, 0o600); err != nil {
		ln.Close()
		os.RemoveAll(dir)
		return nil, err
	}
	s := &Server{opts: o, ui: ui, dir: dir, sock: sock, ln: ln, sessions: map[string]*session{}, out: make(chan func(), 256)}
	go s.pump()
	go s.accept()
	return s, nil
}

// pump makes the UI calls in the order they were queued. out is never
// closed: a late timer may still queue a call after Shutdown.
func (s *Server) pump() {
	for f := range s.out {
		f()
	}
}

// Allow marks ctx so the git commands run with it may ask the user.
func (s *Server) Allow(ctx context.Context, info Info) context.Context {
	if s == nil {
		return ctx
	}
	return gitcmd.WithPrompts(ctx, func() gitcmd.PromptSession {
		// Not "return s.begin(info)": a nil *session would become a non-nil
		// interface.
		if ss := s.begin(info); ss != nil {
			return ss
		}
		return nil
	})
}

func (s *Server) begin(info Info) *session {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil
	}
	ss := &session{srv: s, nonce: hex.EncodeToString(b), info: info, asked: map[string]int{}}
	s.sessions[ss.nonce] = ss
	return ss
}

func (s *Server) lookup(nonce string) *session {
	for n, ss := range s.sessions {
		if subtle.ConstantTimeCompare([]byte(n), []byte(nonce)) == 1 {
			return ss
		}
	}
	return nil
}

func (s *Server) accept() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(c)
	}
}

func (s *Server) handle(c net.Conn) {
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	line, err := bufio.NewReader(&limitReader{c, 64 * 1024}).ReadBytes('\n')
	if err != nil {
		return
	}
	_ = c.SetReadDeadline(time.Time{})
	var req wireReq
	if json.Unmarshal(line, &req) != nil {
		return
	}
	resp := s.serve(req)
	_ = json.NewEncoder(c).Encode(resp)
}

// serve answers one helper message.
func (s *Server) serve(req wireReq) wireResp {
	s.mu.Lock()
	ss := s.lookup(req.Nonce)
	s.mu.Unlock()
	if ss == nil {
		return wireResp{}
	}
	switch req.Op {
	case "get":
		return ss.get(req.Fields)
	case "store":
		ss.store(req.Fields)
		return wireResp{OK: true}
	case "erase":
		ss.mu.Lock()
		ss.erased = true
		ss.mu.Unlock()
		return wireResp{OK: true}
	case "notice":
		if s.ui.Notice != nil {
			s.ui.Notice(strings.TrimSpace(req.Prompt))
		}
		return wireResp{OK: true}
	case "askpass":
		return ss.askpass(req.Prompt)
	}
	return wireResp{}
}

// ask queues r for ss and waits for its reply.
func (s *Server) ask(ss *session, r Request) reply {
	p := &pending{req: r, s: ss, reply: make(chan reply, 1)}
	s.mu.Lock()
	if s.closed || ss.isEnded() {
		s.mu.Unlock()
		return reply{why: errEnded}
	}
	s.nextID++
	p.req.ID = strconv.Itoa(s.nextID)
	s.queue = append(s.queue, p)
	if len(s.queue) == 1 {
		s.showHeadLocked()
	} else {
		s.resendHeadLocked()
	}
	s.mu.Unlock()
	return <-p.reply
}

// showHeadLocked shows the head of the queue for the first time and starts
// its timer.
func (s *Server) showHeadLocked() {
	if len(s.queue) == 0 {
		return
	}
	p := s.queue[0]
	p.req.Waiting = len(s.queue) - 1
	p.req.Deadline = time.Now().Add(s.opts.Timeout).UnixMilli()
	id := p.req.ID
	p.timer = time.AfterFunc(s.opts.Timeout, func() { s.finish(id, reply{why: errTimedOut}) })
	s.showLocked(p.req)
}

// resendHeadLocked sends the shown head again with the current number of
// requests waiting behind it; its deadline stays.
func (s *Server) resendHeadLocked() {
	if len(s.queue) == 0 {
		return
	}
	head := s.queue[0]
	head.req.Waiting = len(s.queue) - 1
	s.showLocked(head.req)
}

func (s *Server) showLocked(r Request) {
	if s.ui.Show != nil {
		s.out <- func() { s.ui.Show(r) }
	}
}

// finish removes request id from the queue with r; it reports whether id
// was there.
func (s *Server) finish(id string, r reply) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.removeLocked(func(p *pending) bool { return p.req.ID == id }, r)
}

// removeLocked takes every request for which match is true out of the queue
// and answers each with r; it reports whether there was any.
//
// If the shown head left, the UI is told it closed and the next request is
// shown (unless the server is shutting down). If only requests behind the
// head left, the head is sent again so its waiting count is current.
func (s *Server) removeLocked(match func(*pending) bool, r reply) bool {
	var kept []*pending
	headID, headGone, removed := "", false, false
	for i, p := range s.queue {
		if !match(p) {
			kept = append(kept, p)
			continue
		}
		if p.timer != nil {
			p.timer.Stop()
		}
		p.reply <- r
		removed = true
		if i == 0 {
			headGone, headID = true, p.req.ID
		}
	}
	if !removed {
		return false
	}
	s.queue = kept
	if headGone && s.ui.Closed != nil {
		s.out <- func() { s.ui.Closed(headID) }
	}
	switch {
	case s.closed:
	case headGone:
		s.showHeadLocked()
	default:
		s.resendHeadLocked()
	}
	return true
}

// refuseText reports whether an answer field could smuggle extra lines into
// git's credential protocol (or ssh's askpass output).
func refuseText(v string) bool { return strings.ContainsAny(v, "\r\n\x00") }

// Answer is the dialog's Continue.
func (s *Server) Answer(id string, a Answer) error {
	if refuseText(a.Username) || refuseText(a.Secret) {
		return ErrLineBreak
	}
	if !s.finish(id, reply{ok: true, a: a}) {
		return ErrUnknownRequest
	}
	return nil
}

// Cancel is the dialog's Cancel (or Esc).
func (s *Server) Cancel(id string) error {
	if !s.finish(id, reply{why: errCancel}) {
		return ErrUnknownRequest
	}
	return nil
}

// Shutdown answers "no credential" to everything pending, stops the socket
// and removes its directory. Later sessions are not created.
func (s *Server) Shutdown() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.removeLocked(func(*pending) bool { return true }, reply{why: errEnded})
	s.mu.Unlock()
	s.ln.Close()
	os.RemoveAll(s.dir)
}

// session is one prompting git command.
type session struct {
	srv   *Server
	nonce string
	info  Info

	mu       sync.Mutex
	ended    bool
	answered bool // CommitTree supplied the HTTPS credential
	erased   bool
	outcome  error
	target   *RememberTarget
	remember bool
	stored   map[string]string
	asked    map[string]int
}

func (ss *session) isEnded() bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.ended
}

func (ss *session) Env() []string {
	return sessionEnv(ss.srv.opts.Exe, ss.srv.sock, ss.nonce, os.Getenv("GIT_CONFIG_COUNT"))
}

func (ss *session) hasOutcome() bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.outcome != nil
}

func (ss *session) setOutcome(kind Kind, why error) {
	var msg string
	switch {
	case errors.Is(why, errTimedOut):
		msg = "No credentials entered (timed out)."
	case errors.Is(why, errCancel) && kind == KindHostKey:
		msg = "The SSH host was not trusted."
	case errors.Is(why, errCancel):
		msg = "No credentials entered."
	default:
		return // session ended: the command's own error stands
	}
	ss.mu.Lock()
	ss.outcome = &gitcmd.PromptError{Msg: msg}
	ss.mu.Unlock()
}

func (ss *session) base(kind Kind) Request {
	return Request{Kind: kind, RepoID: ss.info.RepoID, Context: ss.info.Context, FromAI: ss.info.FromAI}
}

func (ss *session) get(f map[string]string) wireResp {
	if ss.hasOutcome() {
		return wireResp{}
	}
	r := ss.base(KindHTTPS)
	r.Host, r.Path, r.Username = f["host"], f["path"], f["username"]
	r.Remember = findRemember(context.Background(), ss.info.Dir, CredentialURL(f), ss.srv.opts.Platform)
	rep := ss.srv.ask(ss, r)
	if !rep.ok {
		ss.setOutcome(KindHTTPS, rep.why)
		return wireResp{}
	}
	ss.mu.Lock()
	ss.answered, ss.target, ss.remember = true, r.Remember, rep.a.Remember
	ss.mu.Unlock()
	return wireResp{OK: true, Fields: map[string]string{"username": rep.a.Username, "password": rep.a.Secret}}
}

func (ss *session) store(f map[string]string) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if ss.answered {
		ss.stored = f
	}
}

func (ss *session) askpass(text string) wireResp {
	p := ClassifyPrompt(text)
	if p.Kind == KindGitPrompt || ss.hasOutcome() {
		return wireResp{}
	}
	r := ss.base(p.Kind)
	r.Host, r.Username, r.KeyPath, r.KeyType, r.Fingerprint = p.Host, p.User, p.KeyPath, p.KeyType, p.Fingerprint
	if p.Kind == KindOther {
		r.Prompt = p.Text
	}
	key := string(p.Kind) + "\x00" + p.KeyPath + "\x00" + p.User + "@" + p.Host
	ss.mu.Lock()
	ss.asked[key]++
	r.Retry = ss.asked[key] > 1 && (p.Kind == KindPassphrase || p.Kind == KindPassword)
	ss.mu.Unlock()
	rep := ss.srv.ask(ss, r)
	if !rep.ok || (p.Kind == KindHostKey && !rep.a.Trust) {
		why := rep.why
		if rep.ok { // answered without Trust: same as Cancel
			why = errCancel
		}
		ss.setOutcome(p.Kind, why)
		return wireResp{}
	}
	if p.Kind == KindHostKey {
		return wireResp{OK: true, Answer: "yes"}
	}
	return wireResp{OK: true, Answer: rep.a.Secret}
}

// End closes the session (gitcmd calls it once git exited): pending
// requests close, Remember runs after a success, and a failure's credential
// outcome comes back.
func (ss *session) End(failed bool) error {
	s := ss.srv
	s.mu.Lock()
	ss.mu.Lock()
	ss.ended = true
	ss.mu.Unlock()
	delete(s.sessions, ss.nonce)
	s.removeLocked(func(p *pending) bool { return p.s == ss }, reply{why: errEnded})
	s.mu.Unlock()

	ss.mu.Lock()
	defer ss.mu.Unlock()
	defer func() {
		for k := range ss.stored {
			ss.stored[k] = ""
		}
		ss.stored = nil
	}()
	if !failed {
		if ss.stored != nil && ss.target != nil {
			ctx := context.Background()
			switch {
			case ss.remember && !ss.target.User:
				_ = approve(ctx, ss.info.Dir, s.opts.Platform, ss.stored)
			case !ss.remember && ss.target.User:
				_ = reject(ctx, ss.info.Dir, ss.stored)
			}
		}
		return nil
	}
	if ss.outcome != nil {
		return ss.outcome
	}
	if ss.erased {
		if ss.answered {
			return &gitcmd.PromptError{Msg: "The username or token was not accepted."}
		}
		return &gitcmd.PromptError{Msg: "The saved credential was not accepted and was removed. Try again to enter a new one."}
	}
	return nil
}

// limitReader stops a request line at n bytes.
type limitReader struct {
	c net.Conn
	n int
}

func (l *limitReader) Read(p []byte) (int, error) {
	if l.n <= 0 {
		return 0, errors.New("request too long")
	}
	if len(p) > l.n {
		p = p[:l.n]
	}
	n, err := l.c.Read(p)
	l.n -= n
	return n, err
}
