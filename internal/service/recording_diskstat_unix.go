//go:build !windows

package service

import "golang.org/x/sys/unix"

func statDiskUsage(path string) (diskUsage, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return diskUsage{}, err
	}
	blockSize := uint64(stat.Bsize)
	return diskUsage{
		Total: stat.Blocks * blockSize,
		Free:  stat.Bavail * blockSize,
	}, nil
}
