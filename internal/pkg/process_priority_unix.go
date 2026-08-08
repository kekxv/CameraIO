//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package pkg

import "golang.org/x/sys/unix"

func lowerRecordingProcessPriority(pid int) error {
	current, err := unix.Getpriority(unix.PRIO_PROCESS, pid)
	if err != nil {
		return err
	}
	return unix.Setpriority(unix.PRIO_PROCESS, pid, loweredNiceValue(current))
}
