package guild

import "testing"

func TestSanitize(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain text is untouched", "SELECT 1", "SELECT 1"},
		{"tab moves to the next tab stop", "a\tb", "a       b"},
		{"a full tab stop is not swallowed", "12345678\tb", "12345678        b"},
		{"tabs of indented sql are expanded", "\tFROM guidelines", "        FROM guidelines"},
		{"carriage return restarts the line", "10%\r99%", "99%"},
		{"trailing carriage return is dropped", "line\r", "line"},
		{"a shorter overwrite keeps the rest", "abcdef\rxy", "xycdef"},
		{"color escapes are removed", "\x1b[31mred\x1b[0m text", "red text"},
		{"window titles are removed", "\x1b]0;title\x07done", "done"},
		{"unterminated escapes swallow the rest", "keep\x1b[31", "keep"},
		{"other control characters are dropped", "a\x00b\x07c\x7f", "abc"},
	}

	for _, test := range tests {
		got := sanitize(test.in)
		if got != test.want {
			t.Errorf("%v: sanitize(%q) = %q, want %q", test.name, test.in, got, test.want)
		}
	}
}

// TestSanitizeLeavesNoControlRunes is the property the ui depends on, whatever a
// service prints: every rune of a sanitized line takes exactly one cell.
func TestSanitizeLeavesNoControlRunes(t *testing.T) {
	lines := []string{
		"time=2026-09-04T15:19:13.271+02:00 level=INFO msg=\"pq: invalid byte\"",
		"\tSELECT d.id,\r\n\tsc.matchscore\x1b[0m",
		"\x1b[1;32m ok \x1b[0m\tguild\t0.201s",
		"\r\r\r",
	}

	for _, l := range lines {
		for _, r := range sanitize(l) {
			if r < 0x20 || r == 0x7f {
				t.Errorf("sanitize(%q) still contains control rune %q", l, r)
			}
		}
	}
}
