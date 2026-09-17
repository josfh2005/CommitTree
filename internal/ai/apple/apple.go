// Package apple runs Apple Intelligence through the bundled git-ui-apple
// helper, which wraps the FoundationModels framework.
package apple

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"git-ui/internal/ai"
)

const DefaultTimeout = 60 * time.Second

var ErrHelperNotFound = errors.New("apple intelligence helper not found")

type Availability struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

type Client struct {
	Path    string
	Timeout time.Duration
}

func New(path string) *Client { return &Client{Path: path, Timeout: DefaultTimeout} }

// Locate finds the helper next to the running executable (inside the .app),
// or in the Swift build folder when running from the repository.
func Locate() string {
	if exe, err := os.Executable(); err == nil {
		if p := filepath.Join(filepath.Dir(exe), "git-ui-apple"); isExecutable(p) {
			return p
		}
	}
	if wd, err := os.Getwd(); err == nil {
		if p := filepath.Join(wd, "helpers", "apple", ".build", "release", "git-ui-apple"); isExecutable(p) {
			return p
		}
	}
	return ""
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

func (c *Client) Status(ctx context.Context) Availability {
	if c.Path == "" {
		return Availability{Reason: "helperNotFound"}
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, c.Path, "status").Output()
	if err != nil {
		return Availability{Reason: "helperFailed"}
	}
	var a Availability
	if err := json.Unmarshal(bytes.TrimSpace(out), &a); err != nil {
		return Availability{Reason: "helperFailed"}
	}
	return a
}

func (c *Client) Respond(ctx context.Context, instructions, prompt string) (<-chan ai.Chunk, error) {
	if c.Path == "" {
		return nil, ErrHelperNotFound
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)

	input, err := json.Marshal(map[string]string{"instructions": instructions, "prompt": prompt})
	if err != nil {
		cancel()
		return nil, err
	}
	cmd := exec.CommandContext(ctx, c.Path, "respond")
	cmd.Stdin = bytes.NewReader(input)
	cmd.WaitDelay = 2 * time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}

	ch := make(chan ai.Chunk)
	go func() {
		defer cancel()
		defer close(ch)
		finished := false
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scanner.Scan() && !finished {
			var line struct {
				Delta string `json:"delta"`
				Done  bool   `json:"done"`
				Error string `json:"error"`
			}
			if json.Unmarshal(scanner.Bytes(), &line) != nil {
				continue
			}
			switch {
			case line.Error != "":
				ch <- ai.Chunk{Err: errors.New(line.Error)}
				finished = true
			case line.Done:
				ch <- ai.Chunk{Done: true}
				finished = true
			case line.Delta != "":
				ch <- ai.Chunk{Delta: line.Delta}
			}
		}
		waitErr := cmd.Wait()
		if finished {
			return
		}
		err := waitErr
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		if err == nil {
			err = errors.New("apple intelligence helper exited without an answer")
		}
		ch <- ai.Chunk{Err: err}
	}()
	return ch, nil
}
