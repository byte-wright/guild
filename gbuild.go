package guild

import (
	"os"
	"os/signal"
	"regexp"
	"sync"
	"time"
)

type GBuild struct {
	changes    chan string
	notifyRoot *notifyRoot

	lock      sync.RWMutex
	listeners []*listener
}

type Context interface {
	// File can be the file that triggered the match action but its only intended for debugging.
	// It might have been discarded during debounce or when running in one shot mode.
	File() string

	// Print sends a line of text to the output
	Println(out ...any)

	Once() bool
}

type listener struct {
	pattern *regexp.Regexp
	matcher Matcher
}

type Matcher interface {
	Match(ctx Context)
}

// New build a watcher at given path.
// Folders can be excluded from beeing watched by adding the path,
// relative from root withouth / prefix.
func New(root string, exclude []string) (*GBuild, error) {
	gb := &GBuild{
		changes: make(chan string, 1000),
	}

	var err error
	gb.notifyRoot, err = newNotifyRoot(root, gb.changes, exclude)
	if err != nil {
		return nil, err
	}

	return gb, nil
}

func (g *GBuild) On(pattern string, matcher Matcher) {
	g.lock.Lock()
	defer g.lock.Unlock()

	g.listeners = append(g.listeners, &listener{
		pattern: regexp.MustCompile(pattern),
		matcher: matcher,
	})
}

func (g *GBuild) run() {
	for c := range g.changes {
		g.lock.RLock()
		for _, l := range g.listeners {
			sm := l.pattern.FindStringSubmatch(c)
			if len(sm) > 0 {
				l.matcher.Match(&stdoutContext{file: c})
			}
		}

		g.lock.RUnlock()
	}
}

func (g *GBuild) Close() {
	g.notifyRoot.Stop()
	time.Sleep(time.Millisecond * 10)
}

func (g *GBuild) Continuous() {
	exit := make(chan os.Signal, 1)
	signal.Notify(exit, os.Interrupt)

	go g.run()

	<-exit

	g.Close()
}

func (g *GBuild) Once() {
	sc := &stdoutContext{
		once: true,
	}

	for _, listener := range g.listeners {
		listener.matcher.Match(sc)
	}
}

type printFile struct {
	prefix string
}

func PrintFile(prefix string) Matcher {
	return &printFile{
		prefix: prefix,
	}
}

func (p *printFile) Match(ctx Context) {
	ctx.Println(p.prefix, ctx.File())
}

type debounce struct {
	delay     time.Duration
	matcher   Matcher
	ctx       Context
	count     int
	debounced chan int

	lock sync.Mutex
}

// Debounce collects all file changes and triggers at maximum once for the given duration.
// Changes to different files are merged and the file paramter reflects the last change.
func Debounce(delay time.Duration, m Matcher) Matcher {
	deb := &debounce{
		delay:     delay,
		matcher:   m,
		debounced: make(chan int),
	}

	go deb.run()

	return deb
}

func (d *debounce) Match(c Context) {
	if c.Once() {
		d.matcher.Match(c)
		return
	}

	d.lock.Lock()
	defer d.lock.Unlock()

	d.ctx = c

	if d.count == 0 {
		go func() {
			time.Sleep(d.delay)
			d.debounced <- 0
		}()
	}

	d.count++
}

func (d *debounce) run() {
	for range d.debounced {
		d.lock.Lock()

		d.matcher.Match(d.ctx)
		d.count = 0
		d.ctx = nil

		d.lock.Unlock()
	}
}

type funcCall struct {
	f func(c Context)
}

func Func(f func(c Context)) Matcher {
	return &funcCall{f: f}
}

func (f *funcCall) Match(c Context) {
	f.f(c)
}
