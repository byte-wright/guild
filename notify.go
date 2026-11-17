package guild

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gitlab.com/akabio/rnotify"
)

// batchDelay is how long changes are collected before they are forwarded. Tools often
// save a file in several steps, collecting them reports it once.
const batchDelay = time.Millisecond

// notifyRoot will listen for file changes for given root
type notifyRoot struct {
	stopSignal chan struct{}
	stopOnce   sync.Once
	target     chan string
	onError    func(error)
	watcher    *rnotify.Watcher

	// known are the files reported so far. rnotify reports a folder that was moved out
	// of the tree as that single path, known is what it gets expanded into.
	known map[string]bool
}

// newNotifyRoot will create a notifier rooted in the given root path. It sends the
// paths of changed and removed files to target, relative to root and deduplicated
// within batchDelay. Errors that do not stop watching, like an unreadable folder or a
// kernel queue overflow, are passed to onError.
// The root path must exist and be a folder.
func newNotifyRoot(root string, target chan string, exclude []string, onError func(error)) (*notifyRoot, error) {
	if target == nil {
		panic("target must not be nil")
	}

	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	// we have to check if source is a directory because notify will also allow files
	src, err := os.Stat(root)
	if err != nil {
		return nil, err
	}

	if !src.IsDir() {
		return nil, fmt.Errorf("given root path '%v' is not a directory", root)
	}

	nr := &notifyRoot{
		stopSignal: make(chan struct{}),
		target:     target,
		onError:    onError,
		known:      map[string]bool{},
	}

	nr.watcher, err = rnotify.New(root, exclude)
	if err != nil {
		return nil, err
	}

	go nr.process()

	return nr, nil
}

// Stop will release watch and stop processing of events, channel might not get emptied before.
// It is safe to call multiple times and never blocks, even if the target channel is full.
func (nr *notifyRoot) Stop() {
	nr.stopOnce.Do(func() {
		nr.watcher.Close()
		close(nr.stopSignal)
	})
}

func (nr *notifyRoot) process() {
	batch := map[string]bool{}
	errs := nr.watcher.Errors

	var flush <-chan time.Time

	for {
		select {
		case event, ok := <-nr.watcher.Events:
			if !ok {
				return
			}

			for _, f := range nr.files(event) {
				batch[f] = true
			}

			if flush == nil && len(batch) > 0 {
				flush = time.After(batchDelay)
			}

		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}

			nr.onError(err)

		case <-flush:
			flush = nil

			for f := range batch {
				// a full target channel must not keep us from stopping
				select {
				case nr.target <- f:
				case <-nr.stopSignal:
					return
				}
			}

			batch = map[string]bool{}

		case <-nr.stopSignal:
			return
		}
	}
}

// files are the files an event is about. A removed path that is not a known file is a
// folder and stands for the known files below it.
func (nr *notifyRoot) files(e rnotify.Event) []string {
	if e.Op != rnotify.Removed {
		nr.known[e.Path] = true
		return []string{e.Path}
	}

	if nr.known[e.Path] {
		delete(nr.known, e.Path)
		return []string{e.Path}
	}

	prefix := e.Path + "/"
	files := []string{}

	for f := range nr.known {
		if strings.HasPrefix(f, prefix) {
			delete(nr.known, f)
			files = append(files, f)
		}
	}

	return files
}
