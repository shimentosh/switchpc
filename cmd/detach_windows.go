//go:build windows

package main

import "os/exec"

// detach is a no-op on Windows; child processes outlive their parent.
func detach(*exec.Cmd) {}
