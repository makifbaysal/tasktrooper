//go:build !windows

package hostshell

import "os/exec"

func applyCommandLine(*exec.Cmd, Shell, string) {}
