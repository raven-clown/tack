package main

import (
	"context"
	"os"
	"os/exec"
	"strconv"
)

func setProcessGroup(*exec.Cmd) {}

// killTree kills p and its descendants; taskkill walks the tree.
func killTree(p *os.Process) error {
	if err := exec.CommandContext(context.Background(), "taskkill", "/T", "/F", "/PID", strconv.Itoa(p.Pid)).Run(); err != nil {
		return p.Kill()
	}
	return nil
}
