//go:build windows

package credprompt

import (
	"context"
	"errors"
	"io"
	"time"
)

// On Windows CommitTree does not prompt: commands behave as before. This
// file declares every name the rest of the program uses from the real
// (non-Windows) implementation so that it compiles.

// DefaultTimeout is how long a dialog waits for the user.
const DefaultTimeout = time.Minute

var (
	ErrUnknownRequest = errors.New("credprompt: no such request")
	ErrLineBreak      = errors.New("Line breaks are not allowed.")
)

type Info struct {
	RepoID, Dir, Context string
	FromAI               bool
}

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

type Answer struct {
	Username string `json:"username"`
	Secret   string `json:"secret"`
	Remember bool   `json:"remember"`
	Trust    bool   `json:"trust"`
}

type UI struct {
	Show   func(Request)
	Closed func(id string)
	Notice func(text string)
}

type Options struct {
	Exe      string
	Timeout  time.Duration
	Platform string
}

type Server struct{}

func NewServer(Options, UI) (*Server, error) {
	return nil, errors.New("credential prompts: not on Windows")
}
func (s *Server) Allow(ctx context.Context, _ Info) context.Context { return ctx }
func (s *Server) Answer(string, Answer) error                       { return ErrUnknownRequest }
func (s *Server) Cancel(string) error                               { return ErrUnknownRequest }
func (s *Server) Shutdown()                                         {}

func RunHelper([]string, func(string) string, io.Reader, io.Writer) int { return 1 }
