package guild

import "sync"

// Sink receives the lines of every registered output instead of stdout. It is
// installed by the ui and called from the goroutines of the running matchers, so
// implementations must be safe for concurrent use.
type Sink interface {
	Line(out *ANSIOut, text string)
}

// outputs is the registry of the named outputs of a build. It lets a ui list the
// services it can filter and switches all of them from stdout to a sink at once.
type outputs struct {
	lock sync.Mutex
	sink Sink
	list []*ANSIOut
}

func newOutputs() *outputs {
	return &outputs{}
}

// register adopts an output so its lines can be routed to a sink. Registering the
// same output twice is a no op, because On may be called with a matcher chain that
// was already registered.
func (o *outputs) register(a *ANSIOut) {
	o.lock.Lock()
	defer o.lock.Unlock()

	for _, e := range o.list {
		if e == a {
			return
		}
	}

	a.outs = o
	o.list = append(o.list, a)
}

func (o *outputs) all() []*ANSIOut {
	o.lock.Lock()
	defer o.lock.Unlock()

	return append([]*ANSIOut{}, o.list...)
}

func (o *outputs) setSink(s Sink) {
	o.lock.Lock()
	defer o.lock.Unlock()

	o.sink = s
}

func (o *outputs) hasSink() bool {
	o.lock.Lock()
	defer o.lock.Unlock()

	return o.sink != nil
}

func (o *outputs) line(a *ANSIOut, text string) {
	o.lock.Lock()
	sink := o.sink
	o.lock.Unlock()

	if sink == nil {
		a.print([]string{text})
		return
	}

	sink.Line(a, text)
}
