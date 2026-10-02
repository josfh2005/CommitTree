//go:build !windows

package gitcmd

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// startInGroup starts git in a session of its own: a new process group, so
// interrupt reaches the hooks and helpers it runs too, as Ctrl+C in a
// terminal does, and no controlling terminal, so neither git nor ssh can
// ask for a passphrase or a host key on the terminal the app was started
// from (make dev); they fail instead, as in the bundled app.
func startInGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// interrupt sends SIGINT to git's process group.
func interrupt(cmd *exec.Cmd) error {
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
