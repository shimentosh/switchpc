//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// detach puts the child in its own process group so it survives when the
// parent (possibly a launchd job) exits.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
