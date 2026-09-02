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
	b.input.Mods = input.Mods

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
		gb:         gb,
		tui:        timui.New(be),
		log:        newLogBuffer(uiLogLines),
		shown:      map[*ANSIOut]bool{},
		previewSet: map[*ANSIOut]bool{},
		scroll:     timui.ScrollState{Follow: true},
	}

	u.theme()

	gb.outs.setSink(u.log)

	return u, be, gb
}

// button is the position of the nth button of the column on the right. The
// buttons start below the border and guild's own output always takes the first.
func button(be *uiBackend, n int) mathi.Vec2 {
	return mathi.Vec2{X: be.size.X - 4, Y: 1 + n}
}

// hover parks the cursor on pos with mods held. Hovering is found while drawing,
// so what depends on it is only on screen one frame later.
func hover(u *ui, be *uiBackend, pos mathi.Vec2, mods timui.ModMask) {
	be.mouse = pos
	be.input.Mods = mods

	u.render()
	u.render()
	u.render()
}

func click(u *ui, be *uiBackend, pos mathi.Vec2, mods timui.ModMask) {
	hover(u, be, pos, mods)

	be.pressed = true
	u.render()

	be.pressed = false
	u.render()

	// the release is only seen a frame after the button went up, and the filtered
	// view follows the frame after that
	u.render()
	u.render()
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

	click(u, be, button(be, 1), 0)

	if u.shown[compile] {
		t.Fatalf("clicking the button should have hidden the output:\n%v", be.screen())
	}

	if strings.Contains(be.screen(), "compiling the thing") {
		t.Fatalf("line still shown after hiding:\n%v", be.screen())
	}
}

func TestUICtrlHoverPreviewsWithoutToggling(t *testing.T) {
	u, be, gb := newTestUI(t, 60, 10)

	compile := NewANSIOut("compile", 8, 0, 255, 0, Func(func(Context) {}))
	backend := NewANSIOut("backend", 8, 255, 0, 0, Func(func(Context) {}))

	gb.On("a", compile)
	gb.On("b", backend)

	compile.Context().Println("compiling the thing")
	backend.Context().Println("listening on 8000")

	hover(u, be, button(be, 1), timui.ModCtrl)

	screen := be.screen()
	if !strings.Contains(screen, "compiling the thing") {
		t.Fatalf("previewed line missing:\n%v", screen)
	}

	if strings.Contains(screen, "listening on 8000") {
		t.Fatalf("preview should hide the other outputs:\n%v", screen)
	}

	if !u.shown[backend] {
		t.Fatal("a preview must not change the toggles")
	}

	if !strings.Contains(screen, "ctrl shows only compile") {
		t.Fatalf("help missing while hovering:\n%v", screen)
	}

	// letting ctrl go is enough, the toggles were never touched
	hover(u, be, button(be, 1), 0)

	if !strings.Contains(be.screen(), "listening on 8000") {
		t.Fatalf("preview outlived ctrl:\n%v", be.screen())
	}
}

func TestUICtrlClickShowsOnlyOne(t *testing.T) {
	u, be, gb := newTestUI(t, 60, 10)

	compile := NewANSIOut("compile", 8, 0, 255, 0, Func(func(Context) {}))
	backend := NewANSIOut("backend", 8, 255, 0, 0, Func(func(Context) {}))

	gb.On("a", compile)
	gb.On("b", backend)

	compile.Context().Println("compiling the thing")
	backend.Context().Println("listening on 8000")

	click(u, be, button(be, 2), timui.ModCtrl)

	if u.shown[compile] || !u.shown[backend] || u.shown[gb.sysOut] {
		t.Fatalf("ctrl click should have left only backend shown: %v %v %v",
			u.shown[compile], u.shown[backend], u.shown[gb.sysOut])
	}

	// moving off the button ends the preview, the toggles keep the selection
	hover(u, be, mathi.Vec2{X: 5, Y: 5}, 0)

	screen := be.screen()
	if !strings.Contains(screen, "listening on 8000") {
		t.Fatalf("soloed output missing:\n%v", screen)
	}

	if strings.Contains(screen, "compiling the thing") {
		t.Fatalf("ctrl click did not stick:\n%v", screen)
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

	// a terminal can be resized to anything while the ui runs, the button column
	// does not fit long before that becomes unreasonable
	u.render()
	u.render()
}
