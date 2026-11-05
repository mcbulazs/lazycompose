package ui

import "strings"

// cleanLine makes a line of container or command output safe to print.
// Colours (SGR sequences) are kept. Every other escape sequence and control
// character is dropped, so a container cannot move the cursor, set the
// window title or write to the clipboard through its logs.
func cleanLine(s string) string {
	if isPlain(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == 0x1b:
			i = skipEscape(s, i, &b)
		case c == '\t':
			b.WriteByte(c)
			i++
		case c < 0x20 || c == 0x7f:
			i++
		case c == 0xc2 && i+1 < len(s) && s[i+1] >= 0x80 && s[i+1] <= 0x9f:
			// C1 control characters, such as the single byte CSI
			i += 2
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

func isPlain(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 0x20 && c != '\t') || c == 0x7f || c == 0xc2 {
			return false
		}
	}
	return true
}

// skipEscape handles the escape sequence starting at s[i]. It writes it to b
// only if it is a colour sequence, and returns the index after it.
func skipEscape(s string, i int, b *strings.Builder) int {
	if i+1 >= len(s) {
		return len(s)
	}
	switch s[i+1] {
	case '[': // CSI: parameters, then a final byte in 0x40-0x7e
		j := i + 2
		for j < len(s) && s[j] >= 0x20 && s[j] <= 0x3f {
			j++
		}
		if j >= len(s) || s[j] < 0x40 || s[j] > 0x7e {
			return j
		}
		if s[j] == 'm' {
			b.WriteString(s[i : j+1])
		}
		return j + 1
	case ']', 'P', 'X', '^', '_': // strings ended by BEL or ESC \
		for j := i + 2; j < len(s); j++ {
			if s[j] == 0x07 {
				return j + 1
			}
			if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
				return j + 2
			}
		}
		return len(s)
	default:
		return i + 2
	}
}
