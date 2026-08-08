package pkg

import "fmt"

// LowerRecordingProcessPriority lowers the priority of a recording process.
func LowerRecordingProcessPriority(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("invalid process ID: %d", pid)
	}
	return lowerRecordingProcessPriority(pid)
}
