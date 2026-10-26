//go:build windows

package main

// freeBytes is not available on Windows through the standard library: reporting
// the instance's own size is better than pulling in a dependency for a
// diagnostic line, so the free-space line is simply omitted.
func freeBytes(path string) (uint64, bool) { return 0, false }
