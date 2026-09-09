package guild

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byte-wright/timui/util"
)

func TestStdCaptureLandsInTheLogAndGetsButtons(t *testing.T) {
	u, be, gb := newTestUI(t, 60, 10)

	capture := gb.newStdCapture(u.log)

	capture.stdout("printed by a library")
	capture.stderr("and something going wrong")

	u.render()

	screen := be.screen()

	if !strings.Contains(screen, "printed by a library") ||
		!strings.Contains(screen, "and something going wrong") {
		t.Errorf("captured lines are not in the log:\n%v", screen)
	}

	if !strings.Contains(screen, "stdout") || !strings.Contains(screen, "stderr") {
		t.Errorf("captured outputs have no button:\n%v", screen)
	}

	// the outputs are ordinary ones, so they can be hidden like the services
	u.shown[capture.err] = false
	u.render()

	if strings.Contains(be.screen(), "and something going wrong") {
		t.Errorf("hiding stderr did not remove its lines:\n%v", be.screen())
	}
}

// the redirect and the capture are only ever used together, so this walks the whole
// path a stray print takes, through the real pipes into the buffer
func TestStdCaptureCollectsRealPrints(t *testing.T) {
	u, _, gb := newTestUI(t, 60, 10)

	capture := gb.newStdCapture(u.log)

	restore, err := util.RedirectStdToFilesIn(filepath.Join(gb.root, stateDir),
		util.TapStdout(capture.stdout), util.TapStderr(capture.stderr))
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println("a stray print")
	fmt.Fprintln(os.Stderr, "a stray complaint")

	restore.Restore()

	u.render()

	lines := u.log.filter(nil, map[*ANSIOut]bool{capture.out: true, capture.err: true})

	texts := make([]string, len(lines))
	for i, l := range lines {
		texts[i] = l.text
	}

	joined := strings.Join(texts, "\n")

	if !strings.Contains(joined, "a stray print") || !strings.Contains(joined, "a stray complaint") {
		t.Errorf("captured %q, want the prints in it", joined)
	}
}
