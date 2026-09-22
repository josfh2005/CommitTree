// Package terminal runs interactive shells on pseudo-terminals for the
// embedded terminal panel. It knows nothing about Wails: output, settle and
// exit are reported through Callbacks, and the app layer turns them into
// events.
package terminal

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
	"unsafe"

	"github.com/creack/pty"
)

const (
	flushEvery  = 16 * time.Millisecond
	settleAfter = 400 * time.Millisecond
	killAfter   = 2 * time.Second
)

var (
	ErrUnknownTab = errors.New("unknown terminal tab")
	ErrExited     = errors.New("terminal has exited")
)

// Callbacks receive a session's output, the moment its output settles after
// the user pressed Enter, and its exit code. They are called from the
// session's own goroutines and must not block for long.
type Callbacks struct {
	OnData    func(tab, data string)
	OnSettled func(tab, repoID string)
	OnExit    func(tab string, code int)
}

type session struct {
	id, repoID string
	cmd        *exec.Cmd
	pty        *os.File
	done       chan struct{} // closed once the shell has been reaped

	// emitMu serializes calls to OnData: it is held across taking the
	// buffer and calling the callback, so the timer-driven flush and the
	// final EOF flush (both of which can fire around the same time) never
	// call OnData concurrently and reorder chunks, which would corrupt
	// escape sequences split across them.
	emitMu sync.Mutex

	mu       sync.Mutex
	buf      []byte
	flushing bool
	armed    bool
	exited   bool
	settle   *time.Timer
}

type Manager struct {
	cb    Callbacks
	shell string

	mu   sync.Mutex
	tabs map[string]*session
	next int
}

// DefaultShell is $SHELL, or the platform's default when it is unset.
func DefaultShell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	if runtime.GOOS == "darwin" {
		return "/bin/zsh"
	}
	return "/bin/sh"
}

func NewManager(cb Callbacks) *Manager {
	return &Manager{cb: cb, shell: DefaultShell(), tabs: map[string]*session{}}
}

// Shell is the basename of the shell new tabs run, for tab labels.
func (m *Manager) Shell() string { return filepath.Base(m.shell) }

// Open starts a login shell in dir on a new pty and returns its tab ID.
func (m *Manager) Open(repoID, dir string, cols, rows int) (string, error) {
	cmd := exec.Command(m.shell, "-l")
	cmd.Dir = dir
	cmd.Env = shellEnv(os.Environ())
	// StartWithSize makes the shell a session leader with the pty as its
	// controlling terminal, so it is also its own process group.
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return "", fmt.Errorf("start %s: %w", m.shell, err)
	}
	m.mu.Lock()
	m.next++
	id := fmt.Sprintf("t%d", m.next)
	m.mu.Unlock()

	// Build the whole session, including its settle timer, before it is
	// visible to other goroutines: a concurrent CloseAll/CloseRepo must
	// never observe a session with a nil settle timer or race its
	// assignment.
	s := &session{id: id, repoID: repoID, cmd: cmd, pty: f, done: make(chan struct{})}
	s.settle = time.AfterFunc(time.Hour, func() { m.fireSettle(s) })
	s.settle.Stop()

	m.mu.Lock()
	m.tabs[s.id] = s
	m.mu.Unlock()

	go m.read(s)
	return s.id, nil
}

// shellEnv builds the shell's environment from base (the app's own inherited
// environment): TERM and COLORTERM are always added, and LANG is added as a
// UTF-8 default only when none of LC_ALL, LC_CTYPE or LANG is already set —
// launching the app from Finder rather than a shell profile often leaves all
// three unset, which makes line-drawing and other non-ASCII shell output
// render as replacement characters.
func shellEnv(base []string) []string {
	hasLocale := false
	for _, e := range base {
		for _, prefix := range []string{"LC_ALL=", "LC_CTYPE=", "LANG="} {
			if strings.HasPrefix(e, prefix) && e != prefix {
				hasLocale = true
			}
		}
	}
	env := append([]string(nil), base...)
	if !hasLocale {
		env = append(env, "LANG=en_US.UTF-8")
	}
	return append(env, "TERM=xterm-256color", "COLORTERM=truecolor")
}

func (m *Manager) get(tab string) (*session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.tabs[tab]
	if !ok {
		return nil, ErrUnknownTab
	}
	return s, nil
}

// Write sends keystrokes to the shell. A carriage return arms the settle
// detector: once output then stays quiet for settleAfter, OnSettled fires.
func (m *Manager) Write(tab, data string) error {
	s, err := m.get(tab)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.exited {
		s.mu.Unlock()
		return ErrExited
	}
	if strings.ContainsRune(data, '\r') {
		s.armed = true
		s.settle.Reset(settleAfter)
	}
	s.mu.Unlock()
	_, err = s.pty.Write([]byte(data))
	return err
}

func (m *Manager) Resize(tab string, cols, rows int) error {
	s, err := m.get(tab)
	if err != nil {
		return err
	}
	return pty.Setsize(s.pty, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

// Close ends a tab: SIGHUP to the shell's group and the terminal's
// foreground group, close the pty, and SIGKILL both if the shell outlives
// killAfter. Closing an exited tab just forgets it.
func (m *Manager) Close(tab string) error {
	m.mu.Lock()
	s, ok := m.tabs[tab]
	delete(m.tabs, tab)
	m.mu.Unlock()
	if !ok {
		return ErrUnknownTab
	}
	// If the shell has already been reaped, its pid/pgid may since have
	// been recycled onto an unrelated process. Never signal in that case.
	select {
	case <-s.done:
		s.pty.Close()
		return nil
	default:
	}
	s.settle.Stop()
	groups := []int{s.cmd.Process.Pid}
	if fg := foregroundGroup(s.pty); fg > 0 && fg != groups[0] {
		groups = append(groups, fg)
	}
	for _, g := range groups {
		syscall.Kill(-g, syscall.SIGHUP)
	}
	s.pty.Close()
	select {
	case <-s.done:
	case <-time.After(killAfter):
		for _, g := range groups {
			syscall.Kill(-g, syscall.SIGKILL)
		}
		select {
		case <-s.done:
		case <-time.After(killAfter):
			// The reader goroutine is leaked in this pathological case;
			// Close must not hang the caller waiting for it.
		}
	}
	return nil
}

func (m *Manager) CloseRepo(repoID string) {
	for _, id := range m.ids(func(s *session) bool { return s.repoID == repoID }) {
		m.Close(id)
	}
}

func (m *Manager) CloseAll() {
	for _, id := range m.ids(func(*session) bool { return true }) {
		m.Close(id)
	}
}

func (m *Manager) ids(keep func(*session) bool) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for id, s := range m.tabs {
		if keep(s) {
			out = append(out, id)
		}
	}
	return out
}

// foregroundGroup reads the pty's foreground process group, reaching the
// fd via SyscallConn.
func foregroundGroup(f *os.File) int {
	rc, err := f.SyscallConn()
	if err != nil {
		return 0
	}
	var pgrp int32
	rc.Control(func(fd uintptr) {
		syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(syscall.TIOCGPGRP), uintptr(unsafe.Pointer(&pgrp)))
	})
	return int(pgrp)
}

func (m *Manager) read(s *session) {
	chunk := make([]byte, 32*1024)
	for {
		n, err := s.pty.Read(chunk)
		if n > 0 {
			s.mu.Lock()
			s.buf = append(s.buf, chunk[:n]...)
			if !s.flushing {
				s.flushing = true
				time.AfterFunc(flushEvery, func() { m.flush(s) })
			}
			if s.armed {
				s.settle.Reset(settleAfter)
			}
			s.mu.Unlock()
		}
		if err != nil {
			break
		}
	}
	m.flush(s)
	s.cmd.Wait()
	code := -1
	if s.cmd.ProcessState != nil {
		code = s.cmd.ProcessState.ExitCode()
	}
	s.mu.Lock()
	s.exited = true
	s.mu.Unlock()
	close(s.done)
	m.cb.OnExit(s.id, code)
}

func (m *Manager) flush(s *session) {
	s.emitMu.Lock()
	defer s.emitMu.Unlock()
	s.mu.Lock()
	head, rest := splitUTF8(s.buf)
	s.buf = append([]byte(nil), rest...)
	s.flushing = false
	s.mu.Unlock()
	if len(head) > 0 {
		m.cb.OnData(s.id, strings.ToValidUTF8(string(head), "�"))
	}
}

func (m *Manager) fireSettle(s *session) {
	s.mu.Lock()
	armed := s.armed
	s.armed = false
	s.mu.Unlock()
	if armed {
		m.cb.OnSettled(s.id, s.repoID)
	}
}

// splitUTF8 splits b into a prefix ending on a rune boundary and the bytes
// of a trailing rune that is not complete yet.
func splitUTF8(b []byte) (head, rest []byte) {
	for i := len(b) - 1; i >= 0 && i >= len(b)-utf8.UTFMax; i-- {
		if utf8.RuneStart(b[i]) {
			if !utf8.FullRune(b[i:]) {
				return b[:i], b[i:]
			}
			break
		}
	}
	return b, nil
}

func utf8Valid(b []byte) bool { return utf8.Valid(b) }
