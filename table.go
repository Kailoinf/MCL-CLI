package main

import "strings"

// Terminal-cell accounting for the tables the CLI prints.
//
// Go's %-20s pads by bytes/runes, but a Chinese character occupies two terminal
// cells: a Chinese interface (or a path with CJK in it) would otherwise shift
// every following column. These helpers pad by display width instead.

// runeWidth is the number of terminal cells a rune occupies: 2 for East Asian
// wide/fullwidth characters, 0 for combining marks, 1 otherwise.
func runeWidth(r rune) int {
	switch {
	case r < 0x20 || (r >= 0x7f && r < 0xa0):
		return 0
	case r < 0x7f:
		return 1
	case r == 0x0300: // combining marks (roughly; enough for our tables)
		return 0
	case r >= 0x1100 && (r <= 0x115f || // Hangul Jamo
		r == 0x2329 || r == 0x232a ||
		(r >= 0x2e80 && r <= 0xa4cf && r != 0x303f) || // CJK radicals … Yi
		(r >= 0xac00 && r <= 0xd7a3) || // Hangul syllables
		(r >= 0xf900 && r <= 0xfaff) || // CJK compatibility ideographs
		(r >= 0xfe30 && r <= 0xfe6f) || // CJK compatibility forms
		(r >= 0xff00 && r <= 0xff60) || // fullwidth forms
		(r >= 0xffe0 && r <= 0xffe6) ||
		(r >= 0x20000 && r <= 0x3fffd)): // CJK extension B and beyond
		return 2
	}
	return 1
}

// displayWidth is the number of terminal cells a string occupies.
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

// padRight pads s with spaces up to n cells. Longer values are returned as they
// are: a table that shifts is better than one that eats a path.
func padRight(s string, n int) string {
	if w := displayWidth(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}
