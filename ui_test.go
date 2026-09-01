package guild

import (
	"strings"
	"testing"

	"github.com/byte-wright/timui"
	"gitlab.com/bytewright/gmath/mathi"
)

// uiBackend is a timui backend recording the painted characters, so the ui can be
// rendered and read back without a terminal.
type uiBackend struct {
	size    mathi.Vec2
	chars   [][]rune
	mouse   mathi.Vec2
	pressed bool
	input   timui.Input
}

func newUIBackend(w, h int) *uiBackend {
	chars := make([][]rune, h)
	for i := range chars {
		chars[i] = make([]rune, w)
		for j := range chars[i] {
			chars[i][j] = ' '
		}
	}

	return &uiBackend{
		size:  mathi.Vec2{X: w, Y: h},
		chars: chars,
		mouse: mathi.Vec2{X: -1, Y: -1},
	}
}

func (b *uiBackend) Size() mathi.Vec2                { return b.size }
func (b *uiBackend) MousePosition() mathi.Vec2       { return b.mouse }
func (b *uiBackend) MousePressed(key timui.Key) bool { return b.pressed }
func (b *uiBackend) Render()                         {}

func (b *uiBackend) TakeInput() timui.Input {
	input := b.input
	b.input = timui.Input{}

	return input
}

func (b *uiBackend) Set(pos mathi.Vec2, char rune, fg, bg uint32) {
	if char != 0 {
		b.chars[pos.Y][pos.X] = char
	}
}

func (b *uiBackend) screen() string {
	rows := make([]string, len(b.chars))
	for i, r := range b.chars {
		rows[i] = string(r)
	}

	return strings.Join(rows, "\n")
}

func newTestUI(t *testing.T, w, h int) (*ui, *uiBackend, *GBuild) {
	t.Helper()

	gb, err := New(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(gb.Close)

	be := newUIBackend(w, h)

	u := &ui{
		gb:     gb,
		tui:    timui.New(be),
		log:    newLogBuffer(uiLogLines),
		shown:  map[*ANSIOut]bool{},
		scroll: timui.ScrollState{Follow: true},
	}

	u.theme()

	gb.outs.setSink(u.log)

	return u, be, gb
}

func TestUIShowsAndHidesOutputs(t *testing.T) {
	u, be, gb := newTestUI(t, 60, 10)

	compile := NewANSIOut("compile", 8, 0, 255, 0, Func(func(Context) {}))
	backend := NewANSIOut("backend", 8, 255, 0, 0, Func(func(Context) {}))

	gb.On("a", compile)
	gb.On("b", backend)

	compile.Context().Println("compiling the thing")
	backend.Context().Println("listening on 8000")

	u.render()

	screen := be.screen()
	if !strings.Contains(screen, "compiling the thing") {
		t.Fatalf("compile line missing:\n%v", screen)
	}

	if !strings.Contains(screen, "listening on 8000") {
		t.Fatalf("backend line missing:\n%v", screen)
	}

	u.shown[backend] = false
	u.render()

	screen = be.screen()
	if !strings.Contains(screen, "compiling the thing") {
		t.Fatalf("compile line missing after hiding backend:\n%v", screen)
	}

	if strings.Contains(screen, "listening on 8000") {
		t.Fatalf("backend line still shown:\n%v", screen)
	}

	// hidden lines are kept, so showing an output again reveals its history
	u.shown[backend] = true
	u.render()

	if !strings.Contains(be.screen(), "listening on 8000") {
		t.Fatalf("backend history lost:\n%v", be.screen())
	}
}

func TestUIToggleButtonClick(t *testing.T) {
	u, be, gb := newTestUI(t, 60, 10)

	compile := NewANSIOut("compile", 8, 0, 255, 0, Func(func(Context) {}))
	gb.On("a", compile)

	compile.Context().Println("compiling the thing")

	u.render()

	if !u.shown[compile] {
		t.Fatal("output should start out shown")
	}

	// the bar is the last row inside the grid border, guild's own output takes the
	// first button so compile follows it
	x := 1 + len(gb.sysOut.Name()) + 2 + 1
	be.mouse = mathi.Vec2{X: x, Y: 8}

	u.render()
	be.pressed = true
	u.render()
	be.pressed = false
	u.render()

	// the release is seen during a frame, the filtered view follows on the next
	u.render()
	u.render()

	if u.shown[compile] {
		t.Fatalf("clicking the button should have hidden the output:\n%v", be.screen())
	}

	if strings.Contains(be.screen(), "compiling the thing") {
		t.Fatalf("line still shown after hiding:\n%v", be.screen())
	}
}

func TestUIFollowsTailAndScrollsBack(t *testing.T) {
	u, be, gb := newTestUI(t, 60, 10)

	out := NewANSIOut("svc", 8, 0, 0, 255, Func(func(Context) {}))
	gb.On("a", out)

	ctx := out.Context()
	for i := 0; i < 200; i++ {
		ctx.Println("line", i)
	}

	u.render()

	if !strings.Contains(be.screen(), "line 199") {
		t.Fatalf("tail not shown:\n%v", be.screen())
	}

	u.scroll.Top()
	u.render()

	screen := be.screen()
	if !strings.Contains(screen, "line 0") {
		t.Fatalf("top not shown after scrolling back:\n%v", screen)
	}

	if strings.Contains(screen, "line 199") {
		t.Fatalf("tail still shown after scrolling back:\n%v", screen)
	}

	// new lines must not drag the view along while the user is reading back
	ctx.Println("line 200")
	u.render()

	if strings.Contains(be.screen(), "line 200") {
		t.Fatalf("view jumped to the tail while scrolled back:\n%v", be.screen())
	}
}

func TestUISystemMessagesAreCaptured(t *testing.T) {
	u, be, gb := newTestUI(t, 60, 10)

	gb.context("", false).Println("could not start containers: boom")

	u.render()

	if !strings.Contains(be.screen(), "could not start containers: boom") {
		t.Fatalf("system message missing:\n%v", be.screen())
	}
}

func TestUISurvivesATinyTerminal(t *testing.T) {
	u, _, gb := newTestUI(t, 3, 3)

	out := NewANSIOut("svc", 8, 0, 0, 255, Func(func(Context) {}))
	gb.On("a", out)

	out.Context().Println("a line")

	// a terminal can be resized to anything while the ui runs, the bar of buttons
	// does not fit long before that becomes unreasonable
	u.render()
	u.render()
}
