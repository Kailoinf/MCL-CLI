//go:build linux

package launch

import (
	"os"
)

// systemMemory reads total and available memory from /proc/meminfo.
func systemMemory() (total, available int64, ok bool) {
	raw, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, false
	}
	return parseMeminfo(string(raw))
}
