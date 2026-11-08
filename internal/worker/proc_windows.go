//go:build windows

package worker

import "os/exec"

// Windows has no process groups in the POSIX sense; kill the worker itself.
// (The Windows CLI runs without a sandbox in v1.)
func setProcessGroup(cmd *exec.Cmd) {}

func killGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
