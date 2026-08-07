//go:build !windows

package service

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// processExited returns true only when the operating system can confirm the
// child process has exited without taking ownership of exec.Cmd.Wait.
func processExited(cmd *exec.Cmd) bool {
	if cmd == nil || cmd.Process == nil || cmd.ProcessState != nil {
		return true
	}
	err := cmd.Process.Signal(syscall.Signal(0))
	return errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH)
}
