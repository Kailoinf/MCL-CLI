//go:build !linux && !darwin && !windows

package launch

// systemMemory reports nothing on platforms whose memory figures mcli does not
// know how to read. The recommendation then uses its conservative default rather
// than pretending to know.
func systemMemory() (total, available int64, ok bool) { return 0, 0, false }
