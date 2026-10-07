//go:build windows

package hostshell

import (
	"os/exec"
	"syscall"
)

// cmd.exe does not parse its command line with the MSVCRT rules Go quotes for,
// so the script has to reach it verbatim: `/s /c "<script>"` strips exactly the
// outer quotes and runs the rest.
func applyCommandLine(cmd *exec.Cmd, s Shell, script string) {
	if s.Kind != Cmd {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CmdLine = syscall.EscapeArg(s.Path) + ` /d /s /c "` + script + `"`
}
