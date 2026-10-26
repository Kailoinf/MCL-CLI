//go:build windows

package launch

import (
	"syscall"
	"unsafe"
)

// memoryStatusEx mirrors MEMORYSTATUSEX, the one call that reports both numbers
// Windows knows about. It is declared here rather than pulled in: mcli has no
// dependencies, and this is a single documented structure.
type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

// systemMemory asks kernel32 for physical and available memory, so
// `auto_memory_percent` sizes the heap on Windows the way it does on Linux.
func systemMemory() (total, available int64, ok bool) {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")
	var status memoryStatusEx
	status.Length = uint32(unsafe.Sizeof(status))
	ok0, _, _ := proc.Call(uintptr(unsafe.Pointer(&status)))
	if ok0 == 0 {
		return 0, 0, false
	}
	total = int64(status.TotalPhys)
	available = int64(status.AvailPhys)
	if total == 0 {
		return 0, 0, false
	}
	if available == 0 {
		available = total / 2
	}
	return total, available, true
}
