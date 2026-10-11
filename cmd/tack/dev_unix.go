//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

// setProcessGroup puts the app in its own process group, so killTree can
// reach what it starts in turn.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killTree kills p and every process in its group.
func killTree(p *os.Process) error {
	if err := syscall.Kill(-p.Pid, syscall.SIGKILL); err != nil {
		return p.Kill()
	}
	return nil
}
