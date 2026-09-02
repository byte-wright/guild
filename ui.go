package guild

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/byte-wright/timui"
	"github.com/byte-wright/timui/util"
	"github.com/byte-wright/timui/vaxis"
	"gitlab.com/bytewright/gmath/mathi"
)

const (
	uiLogLines  = 20000
	uiFrameTime = time.Millisecond * 33
	uiFollowBox = 12
	uiSideMin   = 8
)

// ui is the terminal frontend of a build. It owns no build state, it only decides
// which of the buffered lines are on screen.
type ui struct {
	gb  *GBuild
	tui *timui.Timui
	log *logBuffer

	outs  []*ANSIOut
	shown map[*ANSIOut]bool

	// hovered is the button under the cursor and preview the one ctrl is held
	// over, both found while drawing and therefore one frame old when used
	hovered *ANSIOut
	preview *ANSIOut

	scroll timui.ScrollState

	// visible is the filtered view of the buffer, reused every frame
	visible []logLine

	// previewSet is the single entry filter of a preview, reused every frame
	previewSet map[*ANSIOut]bool
}

// ContinuousUI runs the build like Continuous, but with a terminal ui instead of a
// stream of prefixed lines. Every service gets a button in the column on the right
// that shows or hides its output, and the log can be scrolled back with the wheel,
// the scroll bar or the keyboard.
//
// Stdout and stderr are redirected to stdout.log and stderr.log in stateDir while
// it runs, because the ui owns the terminal.
func (g *GBuild) ContinuousUI() error {
	restore, err := util.RedirectStdToFilesIn(filepath.Join(g.root, stateDir))
	if err != nil {
		return err
	}

	backend, err := vaxis.NewBackend()
	if err != nil {
		restore.Restore()
		return err
	}

	u := &ui{
		gb:         g,
		tui:        timui.New(backend),
		log:        newLogBuffer(uiLogLines),
		shown:      map[*ANSIOut]bool{},
		previewSet: map[*ANSIOut]bool{},
		scroll:     timui.ScrollState{Follow: true},
	}

	u.theme()

	g.outs.setSink(u.log)

	defer func() {
		backend.Exit()
		restore.Restore()

		// shutdown messages belong on the terminal again, the ui is gone
		g.outs.setSink(nil)
		g.Close()
	}()

	exit := make(chan os.Signal, 1)
	signal.Notify(exit, os.Interrupt, syscall.SIGTERM)

	// unlike Continuous the containers come up in the background, so the ui is
	// already there to show what they are doing
	go g.startup()

	go g.run()

	u.loop(backend, exit)

	return nil
}

// startup brings the containers up and publishes the env file. Failures are
// reported into the log rather than ending the run, so they stay readable.
func (g *GBuild) startup() {
	ctx := g.context("", false)

	err := g.startContainers(ctx)
	if err != nil {
		ctx.Println("could not start containers:", err)
		return
	}

	err = g.writeEnv()
	if err != nil {
		ctx.Println("could not write env file:", err)
	}
}

func (u *ui) theme() {
	u.tui.Theme.Text = timui.MustRGBS("#bbb")
	u.tui.Theme.BorderLine = timui.MustRGBS("#555")
	u.tui.Theme.BorderStyle = timui.BorderSingle
}

func (u *ui) loop(backend *vaxis.VaxisBackend, exit chan os.Signal) {
	for {
		select {
		case <-exit:
			return
		default:
		}

		if backend.Events() {
			return
		}

		time.Sleep(uiFrameTime)

		u.render()

		if u.tui.KeyPressed(timui.KeyEscape) {
			return
		}
	}
}

func (u *ui) render() {
	u.sync()
	u.handleKeys()

	u.visible = u.log.filter(u.visible, u.filter())

	u.hovered = nil
	u.preview = nil

	u.tui.Grid(func(grid *timui.Grid) {
		grid.Rows(
			timui.Split().Factor(1).Fixed(1),
			func(cell *timui.GridCell) {
				cell.Columns(
					timui.Split().Factor(1).Fixed(u.sideWidth()),
					func(*timui.GridCell) {
						u.logView()
					},
					func(*timui.GridCell) {
						u.sideBar()
					},
				)
			},
			func(*timui.GridCell) {
				u.bar()
			},
		)
	})

	u.tui.Finish()
}

// filter is the set of outputs the log is shown for. A preview replaces the
// toggles for as long as ctrl is held over a button, without changing them.
func (u *ui) filter() map[*ANSIOut]bool {
	if u.preview == nil {
		return u.shown
	}

	clear(u.previewSet)
	u.previewSet[u.preview] = true

	return u.previewSet
}

// sync adopts outputs registered since the last frame, shown by default.
func (u *ui) sync() {
	outs := u.gb.outs.all()
	if len(outs) == len(u.outs) {
		return
	}

	u.outs = outs

	for _, o := range outs {
		_, known := u.shown[o]
		if !known {
			u.shown[o] = true
		}
	}
}

func (u *ui) handleKeys() {
	for _, k := range u.tui.Keys() {
		switch k.Key {
		case timui.KeyUp:
			u.scroll.Scroll(-1)
		case timui.KeyDown:
			u.scroll.Scroll(1)
		case timui.KeyPageUp:
			u.scroll.PageUp()
		case timui.KeyPageDown:
			u.scroll.PageDown()
		case timui.KeyHome:
			u.scroll.Top()
		case timui.KeyEnd:
			u.scroll.Bottom()
		case timui.KeyRune:
			if k.Rune == 'f' {
				u.scroll.Follow = !u.scroll.Follow
			}
		}
	}
}

func (u *ui) logView() {
	u.tui.ScrollListV("log", &u.scroll, len(u.visible), func(i int) {
		u.drawLine(u.visible[i])
	})
}

func (u *ui) drawLine(l logLine) {
	prefix := l.out.Prefix()

	r, g, b := l.out.RGB()
	col := timui.RGB(r, g, b)

	u.tui.Text(prefix+" |", mathi.Vec2{}, col.RGBA(0xff), timui.Transparent)
	u.tui.Text(l.text, mathi.Vec2{X: len(prefix) + 3},
		u.tui.Theme.Text.RGBA(0xff), timui.Transparent)
}

// sideWidth is the width of the button column, wide enough for the longest name.
func (u *ui) sideWidth() int {
	w := uiSideMin

	for _, o := range u.outs {
		if len(o.Name())+2 > w {
			w = len(o.Name()) + 2
		}
	}

	return w
}

func (u *ui) sideBar() {
	heights := make([]int, len(u.outs))
	cells := make([]func(), 0, len(u.outs)+1)

	for i, o := range u.outs {
		out := o

		heights[i] = 1
		cells = append(cells, func() {
			u.toggle(out)
		})
	}

	cells = append(cells, func() {})

	u.tui.Rows(timui.Split().Fixed(heights...).Factor(1), cells...)
}

func (u *ui) bar() {
	u.tui.Columns(
		timui.Split().Fixed(uiFollowBox).Factor(1),
		func() {
			u.tui.Checkbox("follow", &u.scroll.Follow)
		},
		u.status,
	)
}

// toggle is a one row button carrying the color of its output, lit while the output
// is shown and dimmed while it is hidden. Holding ctrl over it previews that output
// alone, clicking with ctrl makes it the only shown one.
func (u *ui) toggle(out *ANSIOut) {
	mouse := u.tui.MouseInput(out.Name())
	ctrl := u.tui.Mods().Has(timui.ModCtrl)

	r, g, b := out.RGB()
	col := timui.RGB(r, g, b)

	fg := col
	bg := col.MulDiv(1, 6)

	if !u.shown[out] {
		fg = col.MulDiv(1, 3)
		bg = timui.RGB(0x11, 0x11, 0x11)
	}

	if mouse.Hovered() > 0 {
		bg = bg.Add(timui.RGB(0x22, 0x22, 0x22))

		u.hovered = out

		if ctrl {
			fg = col
			u.preview = out
		}
	}

	u.tui.SetArea(' ', fg, bg)
	u.tui.Text(" "+out.Name(), mathi.Vec2{}, fg.RGBA(0xff), bg.RGBA(0xff))

	if mouse.LeftReleased() {
		if ctrl {
			u.solo(out)
		} else {
			u.shown[out] = !u.shown[out]
		}
	}
}

// solo shows out and hides everything else.
func (u *ui) solo(out *ANSIOut) {
	for _, o := range u.outs {
		u.shown[o] = o == out
	}
}

func (u *ui) status() {
	if u.hovered != nil {
		u.tui.Label("  ctrl shows only " + u.hovered.Name() + ", ctrl+click keeps it that way")
		return
	}

	u.tui.Label(fmt.Sprintf("  %v lines   pgup/pgdn/home/end scroll, f follows, esc quits",
		len(u.visible)))
}
