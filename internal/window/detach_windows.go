//go:build windows

package window

import "os/exec"

func detach(*exec.Cmd) {}
