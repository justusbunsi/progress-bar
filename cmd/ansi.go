package cmd

import "strings"

// clamp bounds n in [lo..hi].
func clamp(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

// visibleLen returns visible columns in s, ignoring ANSI escape sequences.
func visibleLen(s string) int {
	n := 0
	inEsc := false
	for i := 0; i < len(s); i++ {
		if !inEsc {
			if s[i] == 0x1b { // ESC
				inEsc = true
				continue
			}
			n++
			continue
		}
		// End escape on final byte (good enough for CSI).
		if (s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= 'a' && s[i] <= 'z') {
			inEsc = false
		}
	}
	return n
}

// truncateVisible truncates s to at most max visible columns,
// preserving ANSI sequences and resetting attributes if we cut.
func truncateVisible(s string, max int) string {
	if max <= 0 {
		return ""
	}

	var b strings.Builder
	b.Grow(len(s))

	vis := 0
	inEsc := false

	for i := 0; i < len(s); i++ {
		c := s[i]

		if !inEsc {
			if c == 0x1b {
				inEsc = true
				b.WriteByte(c)
				continue
			}
			if vis >= max {
				b.WriteString("\x1B[0m")
				return b.String()
			}
			b.WriteByte(c)
			vis++
			continue
		}

		b.WriteByte(c)

		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
			inEsc = false
		}
	}

	if inEsc {
		b.WriteString("\x1B[0m")
	}
	return b.String()
}
