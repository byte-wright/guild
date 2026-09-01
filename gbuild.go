package guild

import (
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

type GBuild struct {
	root       string
	changes    chan string
	notifyRoot *notifyRoot

	lock       sync.RWMutex
	listeners  []*listener
	containers []*Container
	published  map[string]string
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

// Stopper is an optional interface for matchers that hold resources beyond a single
// match, like a running service. Decorating matchers must forward Stop to their child.
type Stopper interface {
	Stop()
}

// New build a watcher at given path.
// Folders can be excluded from beeing watched by adding the path,
// relative from root withouth / prefix.
func New(root string, exclude []string) (*GBuild, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	gb := &GBuild{
		root:      abs,
		changes:   make(chan string, 1000),
		published: map[string]string{},
	}

	// guild writes its own state below stateDir, watching it would feed changes back into
	// the matchers that caused them.
	exclude = append(append([]string{}, exclude...), stateDir)

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

	g.lock.RLock()
	defer g.lock.RUnlock()

	for _, l := range g.listeners {
		stopper, ok := l.matcher.(Stopper)
		if ok {
			stopper.Stop()
		}
	}

	for _, c := range g.containers {
		c.stop()
	}

	g.removeEnv()
}

func (g *GBuild) Continuous() {
	exit := make(chan os.Signal, 1)
	signal.Notify(exit, os.Interrupt, syscall.SIGTERM)

	sc := &stdoutContext{}

	err := g.startContainers(sc)
	if err != nil {
		sc.Println("could not start containers:", err)
		g.Close()
		return
	}

	err = g.writeEnv()
	if err != nil {
		sc.Println("could not write env file:", err)
	}

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

func (d *debounce) Stop() {
	stopper, ok := d.matcher.(Stopper)
	if ok {
		stopper.Stop()
	}
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

// Publish makes a value available to tools running outside of guild by writing it
// to the env file in stateDir. The file is written once all containers are up and
// removed again on shutdown.
func (g *GBuild) Publish(name, value string) {
	g.lock.Lock()
	defer g.lock.Unlock()

	g.published[name] = value
}

func (g *GBuild) envPath() string {
	return filepath.Join(g.root, stateDir, "env")
}

func (g *GBuild) writeEnv() error {
	g.lock.RLock()
	defer g.lock.RUnlock()

	if len(g.published) == 0 {
		return nil
	}

	names := make([]string, 0, len(g.published))
	for k := range g.published {
		names = append(names, k)
	}

	sort.Strings(names)

	sb := strings.Builder{}
	for _, n := range names {
		sb.WriteString(n + "=" + g.published[n] + "\n")
	}

	err := os.MkdirAll(filepath.Join(g.root, stateDir), 0o755)
	if err != nil {
		return err
	}

	return os.WriteFile(g.envPath(), []byte(sb.String()), 0o644)
}

func (g *GBuild) removeEnv() {
	err := os.Remove(g.envPath())
	if err != nil && !os.IsNotExist(err) {
		log.Println("could not remove env file:", err)
	}
}
