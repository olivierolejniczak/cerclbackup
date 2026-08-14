//go:build !windows

package api

import "syscall"

// diskFreeBytes returns bytes available to the current user on the
// filesystem containing dir.
func diskFreeBytes(dir string) (uint64, bool) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return 0, false
	}
	return stat.Bavail * uint64(stat.Bsize), true
}
