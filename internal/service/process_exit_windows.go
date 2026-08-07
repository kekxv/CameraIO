//go:build windows

package service

import (
	"errors"
	"os/exec"

	"golang.org/x/sys/windows"
)

// processExited checks the Windows process handle without calling Wait. Unlike
// Process.Signal(0), this works on Windows before exec.Cmd.Wait has returned.
func processExited(cmd *exec.Cmd) bool {
	if cmd == nil || cmd.Process == nil || cmd.ProcessState != nil {
		return true
	}

	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(cmd.Process.Pid))
	if err != nil {
		return errors.Is(err, windows.ERROR_INVALID_PARAMETER)
	}
	defer windows.CloseHandle(handle)

	state, err := windows.WaitForSingleObject(handle, 0)
	if err != nil {
		return false
	}
	return state == windows.WAIT_OBJECT_0
}
