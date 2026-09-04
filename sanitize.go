package guild

// tabWidth is the tab stop used when expanding the tabs of a log line.
const tabWidth = 8

// sanitize turns a line of arbitrary program output into plain text that can be
// painted cell by cell. Without it the control characters reach the terminal as
// they are: a tab jumps the cursor to the next tab stop and an escape sequence is
// interpreted, both of which shift the rest of the row and leave the cells that
// were skipped showing whatever the previous frame painted there.
//
// Tabs are expanded so indented output keeps its shape, a carriage return moves
// back to the start of the line and lets what follows overwrite it, like a
// terminal would, and everything else that cannot be painted is dropped.
func sanitize(text string) string {
	if !hasControl(text) {
		return text
	}

	runes := []rune(text)
	out := make([]rune, 0, len(runes))
	col := 0

	for i := 0; i < len(runes); i++ {
		r := runes[i]

		switch {
		case r == '\t':
			stop := (col/tabWidth + 1) * tabWidth
			for col < stop {
				out = writeAt(out, col, ' ')
				col++
			}

		case r == '\r':
			col = 0

		case r == 0x1b:
			i += escapeLen(runes[i:]) - 1

		case r < 0x20 || r == 0x7f:

		default:
			out = writeAt(out, col, r)
			col++
		}
	}

	return string(out)
}

func hasControl(text string) bool {
	for i := 0; i < len(text); i++ {
		if text[i] < 0x20 || text[i] == 0x7f {
			return true
		}
	}

	return false
}

// writeAt puts r at col, padding with spaces when a carriage return left the
// column ahead of what was written so far.
func writeAt(out []rune, col int, r rune) []rune {
	for len(out) < col {
		out = append(out, ' ')
	}

	if col < len(out) {
		out[col] = r
		return out
	}

	return append(out, r)
}

// escapeLen is the length of the escape sequence starting at runes[0], which is
// ESC. Unterminated sequences swallow the rest of the line, the same way a
// terminal would wait for their end.
func escapeLen(runes []rune) int {
	if len(runes) < 2 {
		return 1
	}

	switch runes[1] {
	case '[':
		// CSI, ends at the first byte in 0x40 to 0x7e, like the m of a color
		for i := 2; i < len(runes); i++ {
			if runes[i] >= 0x40 && runes[i] <= 0x7e {
				return i + 1
			}
		}

		return len(runes)

	case ']':
		// OSC, ends at BEL or at ST
		for i := 2; i < len(runes); i++ {
			if runes[i] == 0x07 {
				return i + 1
			}

			if runes[i] == 0x1b && i+1 < len(runes) && runes[i+1] == '\\' {
				return i + 2
			}
		}

		return len(runes)
	}

	return 2
}
