//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package pkg

import "golang.org/x/sys/unix"

func lowerRecordingProcessPriority(pid int) error {
	return unix.Setpriority(unix.PRIO_PROCESS, pid, 10)
}
