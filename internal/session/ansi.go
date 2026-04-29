package session

import "strings"

// stripANSI removes ANSI escape sequences from a line. The conductor displays
// PTY output in a plain <pre>; raw escapes render as garbage like "[38;2;…m".
//
// Handles the three common categories:
//   - CSI sequences:   ESC '[' ... <final-byte 0x40-0x7E>      (colors, cursor moves, etc.)
//   - OSC sequences:   ESC ']' ... ( BEL | ESC '\' )           (window title, hyperlinks)
//   - Other 2-byte:    ESC <single byte>                       (e.g. ESC '=', ESC '>')
//
// Plus the bare BEL (0x07) and the standalone DEL (0x7F).
func stripANSI(s string) string {
	if !strings.ContainsAny(s, "\x1b\x07\x7f") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		c := s[i]
		switch c {
		case 0x07, 0x7f:
			i++
			continue
		case 0x1b:
			if i+1 >= len(s) {
				return b.String()
			}
			next := s[i+1]
			switch next {
			case '[':
				// CSI: scan until a final byte in 0x40..0x7E.
				j := i + 2
				for j < len(s) {
					ch := s[j]
					if ch >= 0x40 && ch <= 0x7e {
						j++
						break
					}
					j++
				}
				i = j
			case ']':
				// OSC: scan until BEL or ESC \.
				j := i + 2
				for j < len(s) {
					if s[j] == 0x07 {
						j++
						break
					}
					if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
						j += 2
						break
					}
					j++
				}
				i = j
			default:
				// Two-byte ESC sequence; skip both.
				i += 2
			}
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}
