package guild

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/fatih/color"
)

type ANSIOut struct {
	prefix  string
	color   *color.Color
	matcher Matcher
}

type ansiOutContext struct {
	parent *ANSIOut
	ctx    Context
}

func NewANSIOut(prefix string, n int, r, g, b int, matcher Matcher) Matcher {
	if len(prefix) > n {
		prefix = prefix[:n]
	}

	for len(prefix) < n {
		prefix = " " + prefix
	}

	return &ANSIOut{
		prefix:  prefix,
		color:   color.RGB(r, g, b),
		matcher: matcher,
	}
}

func (a *ANSIOut) Match(ctx Context) {
	a.matcher.Match(&ansiOutContext{
		parent: a,
		ctx:    ctx,
	})
}

func (a *ansiOutContext) File() string {
	return a.ctx.File()
}

func (a *ansiOutContext) Once() bool {
	return a.ctx.Once()
}

func (a *ansiOutContext) Println(out ...any) {
	a.parent.color.Set()

	sb := bytes.NewBufferString("")

	for i, o := range out {
		if i > 0 {
			sb.WriteString(" ")
		}

		sb.WriteString(fmt.Sprint(o))
	}

	lines := strings.Split(sb.String(), "\n")

	for _, l := range lines {
		fmt.Println(a.parent.prefix+" |", l)
	}

	defer color.Unset()
}
