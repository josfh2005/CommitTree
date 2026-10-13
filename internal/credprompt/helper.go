//go:build !windows

package credprompt

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"
)

// wireReq / wireResp are the helper↔app messages: one JSON line each way.
type wireReq struct {
	Nonce  string            `json:"nonce"`
	Op     string            `json:"op"` // get, store, erase, askpass, notice
	Fields map[string]string `json:"fields,omitempty"`
	Prompt string            `json:"prompt,omitempty"`
}

type wireResp struct {
	OK     bool              `json:"ok"`
	Fields map[string]string `json:"fields,omitempty"`
	Answer string            `json:"answer,omitempty"`
}

// RunHelper is the whole program when CommitTree runs as git's credential
// helper (`credential-helper get|store|erase`) or as git's/ssh's askpass
// (one argument: the prompt). It returns the exit code. It never writes
// anything it got from the app to stderr.
func RunHelper(args []string, getenv func(string) string, stdin io.Reader, stdout io.Writer) int {
	sock, nonce := getenv(EnvSock), getenv(EnvNonce)
	if len(args) >= 2 && args[0] == "credential-helper" {
		fields, err := ParseCredential(stdin)
		if err != nil {
			return 0
		}
		switch args[1] {
		case "get":
			resp, err := call(sock, wireReq{Nonce: nonce, Op: "get", Fields: fields})
			if err == nil && resp.OK {
				_, _ = io.WriteString(stdout, FormatCredential(resp.Fields))
			}
		case "store", "erase":
			_, _ = call(sock, wireReq{Nonce: nonce, Op: args[1], Fields: fields})
		}
		// git ignores a helper's exit code; "nothing printed" means no credential.
		return 0
	}
	prompt := ""
	if len(args) > 0 {
		prompt = args[0]
	}
	if getenv("SSH_ASKPASS_PROMPT") == "none" {
		_, _ = call(sock, wireReq{Nonce: nonce, Op: "notice", Prompt: prompt})
		return 0
	}
	resp, err := call(sock, wireReq{Nonce: nonce, Op: "askpass", Prompt: prompt})
	if err != nil || !resp.OK {
		return 1
	}
	_, _ = fmt.Fprintln(stdout, resp.Answer)
	return 0
}

func call(sock string, req wireReq) (wireResp, error) {
	var resp wireResp
	conn, err := net.DialTimeout("unix", sock, 5*time.Second)
	if err != nil {
		return resp, err
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return resp, err
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return resp, err
	}
	err = json.Unmarshal(line, &resp)
	return resp, err
}
