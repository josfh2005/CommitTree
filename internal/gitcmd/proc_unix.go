//go:build !windows

package gitcmd

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// startInGroup starts git in a process group of its own, so interrupt
// reaches the hooks and helpers it runs too, as Ctrl+C in a terminal does.
func startInGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// interrupt sends SIGINT to git's process group.
func interrupt(cmd *exec.Cmd) error {
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
