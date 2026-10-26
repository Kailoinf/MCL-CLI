//go:build darwin

package launch

import (
	"encoding/binary"
	"syscall"
)

// systemMemory asks sysctl for physical memory. macOS does not hand out an
// "available" figure that means the same thing as Linux's MemAvailable (inactive
// and cached pages are most of an idle machine's memory), so the recommendation
// uses half of the physical total rather than a number that would look alarming.
func systemMemory() (total, available int64, ok bool) {
	raw, err := syscall.Sysctl("hw.memsize")
	if err != nil || len(raw) < 8 {
		return 0, 0, false
	}
	total = int64(binary.LittleEndian.Uint64([]byte(raw[:8])))
	if total <= 0 {
		return 0, 0, false
	}
	return total, total / 2, true
}
