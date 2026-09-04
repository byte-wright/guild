package guild

import "sync"

// logLine is one line of output together with the output it came from, so the ui
// can filter and color it without having to parse anything back out.
type logLine struct {
	out  *ANSIOut
	text string
}

// logBuffer is the ring of recent output lines feeding the ui. Lines of hidden
// outputs are kept as well, so showing one again reveals its history instead of
// only what arrives from then on.
type logBuffer struct {
	lock  sync.Mutex
	lines []logLine
	max   int
}

func newLogBuffer(max int) *logBuffer {
	return &logBuffer{max: max}
}

// Lines implements Sink. It is called from the goroutines of the running matchers
// and appends all lines of a message under one lock, so a multi line message is
// never split up by another output. Lines are sanitized on the way in, once per
// line instead of once per line and frame.
func (b *logBuffer) Lines(out *ANSIOut, texts []string) {
	b.lock.Lock()
	defer b.lock.Unlock()

	for _, t := range texts {
		if len(b.lines) >= b.max {
			// dropping a chunk instead of a single line keeps this from copying the
			// whole buffer on every line once it is full
			drop := b.max / 10
			if drop < 1 {
				drop = 1
			}

			b.lines = append(b.lines[:0], b.lines[drop:]...)
		}

		b.lines = append(b.lines, logLine{out: out, text: sanitize(t)})
	}
}

// filter collects the lines of the shown outputs into dst. dst is reused across
// frames so rendering does not allocate.
func (b *logBuffer) filter(dst []logLine, shown map[*ANSIOut]bool) []logLine {
	b.lock.Lock()
	defer b.lock.Unlock()

	dst = dst[:0]

	for _, l := range b.lines {
		if shown[l.out] {
			dst = append(dst, l)
		}
	}

	return dst
}
