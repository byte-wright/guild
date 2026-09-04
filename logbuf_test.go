package guild

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

// TestLogBufferKeepsMultilineTogether writes multi line messages from concurrent
// outputs and checks that no message got split up by another one.
func TestLogBufferKeepsMultilineTogether(t *testing.T) {
	outs := newOutputs()
	buf := newLogBuffer(10000)
	outs.setSink(buf)

	const writers = 8
	const messages = 50
	const height = 5

	wg := sync.WaitGroup{}

	for w := 0; w < writers; w++ {
		out := NewANSIOut(fmt.Sprintf("out%v", w), 6, 0, 0, 0, nil)
		outs.register(out)

		wg.Add(1)

		go func(w int, out *ANSIOut) {
			defer wg.Done()

			ctx := out.Context()

			for m := 0; m < messages; m++ {
				lines := make([]string, height)
				for l := range lines {
					lines[l] = fmt.Sprintf("w%v m%v l%v", w, m, l)
				}

				ctx.Println(strings.Join(lines, "\n"))
			}
		}(w, out)
	}

	wg.Wait()

	lines := buf.filter(nil, shownAll(outs))

	if len(lines) != writers*messages*height {
		t.Fatalf("expected %v lines, got %v", writers*messages*height, len(lines))
	}

	for i := 0; i < len(lines); i += height {
		for l := 0; l < height; l++ {
			want := fmt.Sprintf(" l%v", l)
			if !strings.HasSuffix(lines[i+l].text, want) {
				t.Fatalf("message starting at line %v is interleaved: %q", i,
					lines[i+l].text)
			}
		}

		if lines[i].out != lines[i+height-1].out {
			t.Fatalf("message starting at line %v mixes outputs", i)
		}
	}
}

func shownAll(o *outputs) map[*ANSIOut]bool {
	shown := map[*ANSIOut]bool{}
	for _, out := range o.all() {
		shown[out] = true
	}

	return shown
}
