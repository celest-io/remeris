//go:build windows

package steps

import (
	"os/exec"
	"syscall"
)

// shellCommand runs line through the platform shell: cmd /C on Windows. The command line is
// passed verbatim, because Go's argument quoting is not what cmd.exe expects.
func shellCommand(line string) *exec.Cmd {
	cmd := exec.Command("cmd")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: "cmd /C " + line}
	return cmd
}
