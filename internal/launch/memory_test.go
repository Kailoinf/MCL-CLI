package launch

import "testing"

const meminfoSample = `MemTotal:       16333412 kB
MemFree:         1234567 kB
MemAvailable:    9876543 kB
Buffers:          123456 kB
`

func TestParseMeminfo(t *testing.T) {
	total, available, ok := parseMeminfo(meminfoSample)
	if !ok {
		t.Fatal("a normal meminfo must parse")
	}
	if total != 16333412*1024 {
		t.Errorf("total = %d, want %d", total, int64(16333412*1024))
	}
	if available != 9876543*1024 {
		t.Errorf("available = %d, want %d", available, int64(9876543*1024))
	}
}

func TestParseMeminfoWithoutAvailable(t *testing.T) {
	// an old kernel has no MemAvailable: half the physical memory is the fallback,
	// and a zero would silently shrink the heap to the floor
	total, available, ok := parseMeminfo("MemTotal: 8000000 kB\nMemFree: 100000 kB\n")
	if !ok || total != 8000000*1024 || available != 4000000*1024 {
		t.Errorf("total = %d available = %d ok = %v", total, available, ok)
	}
}

func TestParseMeminfoRejectsJunk(t *testing.T) {
	for _, body := range []string{"", "MemTotal: not-a-number kB", "no colons here", "MemAvailable: 100 kB"} {
		if _, _, ok := parseMeminfo(body); ok {
			t.Errorf("junk must not be read as memory: %q", body)
		}
	}
}
