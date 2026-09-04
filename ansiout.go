package guild

import (
	"bytes"
	"fmt"
	"strings"
	"sync"

	"github.com/fatih/color"
)

// printLock serializes the stdout path. Outputs are printed from the goroutines of
// the running matchers, so without it the lines of two of them end up mixed into
// each other, along with the color escapes around them.
var printLock sync.Mutex

type ANSIOut struct {
	name    string
	prefix  string
	r, g, b int
	color   *color.Color
	matcher Matcher

	// outs is set when the output is registered on a build. While it is nil, or
	// while no sink is installed, lines are printed to stdout.
	outs *outputs
}

type ansiOutContext struct {
	parent *ANSIOut
	ctx    Context
}

func NewANSIOut(prefix string, n int, r, g, b int, matcher Matcher) *ANSIOut {
	name := prefix

	if len(prefix) > n {
		prefix = prefix[:n]
	}

	for len(prefix) < n {
		prefix = " " + prefix
	}

	return &ANSIOut{
		name:    name,
		prefix:  prefix,
		r:       r,
		g:       g,
		b:       b,
		color:   color.RGB(r, g, b),
		matcher: matcher,
	}
}

// Name is the output name as given, without the padding of the printed prefix.
func (a *ANSIOut) Name() string {
	return a.name
}

// Prefix is the name padded to the requested width, as it appears in front of
// every line.
func (a *ANSIOut) Prefix() string {
	return a.prefix
}

// RGB is the color the output is printed in.
func (a *ANSIOut) RGB() (int, int, int) {
	return a.r, a.g, a.b
}

// Context returns a Context printing through this prefix and color, for output that
// does not originate from a match, like container logs.
func (a *ANSIOut) Context() Context {
	return &ansiOutContext{
		parent: a,
		ctx:    &stdoutContext{},
	}
}

func (a *ANSIOut) Stop() {
	stopper, ok := a.matcher.(Stopper)
	if ok {
		stopper.Stop()
	}
}

func (a *ANSIOut) Match(ctx Context) {
	a.matcher.Match(&ansiOutContext{
		parent: a,
		ctx:    ctx,
	})
}

func (a *ANSIOut) wrapped() Matcher {
	return a.matcher
}

// print writes the lines to stdout under the prefix, in one colored block so a
// terminal only has to switch color once per batch.
func (a *ANSIOut) print(lines []string) {
	printLock.Lock()
	defer printLock.Unlock()

	a.color.Set()

	for _, l := range lines {
		fmt.Println(a.prefix+" |", l)
	}

	color.Unset()
}

func (a *ansiOutContext) File() string {
	return a.ctx.File()
}

func (a *ansiOutContext) Once() bool {
	return a.ctx.Once()
}

func (a *ansiOutContext) Println(out ...any) {
	sb := bytes.NewBufferString("")

	for i, o := range out {
		if i > 0 {
			sb.WriteString(" ")
		}

		sb.WriteString(fmt.Sprint(o))
	}

	lines := strings.Split(sb.String(), "\n")

	if a.parent.outs == nil {
		a.parent.print(lines)
		return
	}

	a.parent.outs.lines(a.parent, lines)
}
