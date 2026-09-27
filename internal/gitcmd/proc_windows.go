//go:build windows

package gitcmd

import "os/exec"

// Windows has no SIGINT to send to a process: git is killed instead.
func startInGroup(*exec.Cmd) {}

func interrupt(cmd *exec.Cmd) error { return cmd.Process.Kill() }
