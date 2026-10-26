//go:build !windows

package main

import "syscall"

// freeBytes returns the bytes available to an unprivileged user on the
// filesystem holding path. The second result is false when it cannot be told
// (the caller then simply omits the line).
func freeBytes(path string) (uint64, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, false
	}
	return uint64(st.Bavail) * uint64(st.Bsize), true
}
