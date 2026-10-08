//go:build !windows

package steps

import "os/exec"

// shellCommand runs line through the platform shell: sh -c on Unix.
func shellCommand(line string) *exec.Cmd {
	return exec.Command("sh", "-c", line)
}
