//go:build !windows

package explain

import "os/exec"

func hideWindow(*exec.Cmd) {}
