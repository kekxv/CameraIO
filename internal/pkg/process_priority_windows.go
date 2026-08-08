//go:build windows

package pkg

import "golang.org/x/sys/windows"

func lowerRecordingProcessPriority(pid int) error {
	process, err := windows.OpenProcess(windows.PROCESS_SET_INFORMATION, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(process)
	return windows.SetPriorityClass(process, windows.BELOW_NORMAL_PRIORITY_CLASS)
}
