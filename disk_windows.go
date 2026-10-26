//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

// freeBytes reports the bytes available to the caller on the volume holding path,
// which is what the doctor line means on Windows too.
func freeBytes(path string) (uint64, bool) {
	ptr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, false
	}
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")
	var freeToCaller, total, totalFree uint64
	ok, _, _ := proc.Call(
		uintptr(unsafe.Pointer(ptr)),
		uintptr(unsafe.Pointer(&freeToCaller)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if ok == 0 {
		return 0, false
	}
	return freeToCaller, true
}
