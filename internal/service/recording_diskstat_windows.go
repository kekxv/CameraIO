//go:build windows

package service

import "golang.org/x/sys/windows"

func statDiskUsage(path string) (diskUsage, error) {
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return diskUsage{}, err
	}
	var freeAvailable, total uint64
	if err := windows.GetDiskFreeSpaceEx(pathPointer, &freeAvailable, &total, nil); err != nil {
		return diskUsage{}, err
	}
	return diskUsage{Total: total, Free: freeAvailable}, nil
}
