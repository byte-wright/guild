package guild

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gitlab.com/akabio/rnotify"
)

// notifyRoot will listen for file changes for given root
type notifyRoot struct {
	stopSignal chan int
	target     chan string
	watcher    *rnotify.Watcher
}

// newNotifyRoot will create a notifier rooted in the given root
// path. It will start listening and send file changes to the provided
// channel.
// The sent events are strings containing the path of the changed file.
// they are relative to root path.
// The events will be collected for a millisecond and duplicates will be removed.
// Events are often in batches for the same file because tools tend to do multiple
// different write operations when saving. This should get rid of most duplicates
// but if there are duplicates it's not really bad.
// Linked files/folders might not be returned correctly... needs to be tested.
// The root path must exist and be a folder.
func newNotifyRoot(root string, target chan string, exclude []string) (*notifyRoot, error) {
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
		stopSignal: make(chan int),
		target:     target,
	}

	nr.watcher, err = rnotify.New(root, exclude)
	if err != nil {
		return nil, err
	}

	go nr.process()

	return nr, nil
}

// Stop will release watch and stop processing of events, channel might not get emptied before.
func (nr *notifyRoot) Stop() {
	nr.watcher.Close()
	nr.stopSignal <- 1
}

// process will read notify events and forward them to the target channels
// will be run from a goroutine created by NewNotifyRoot
// it will create batches of 1 millisecond where duplicates are removed
func (nr *notifyRoot) process() {
	changes := map[string]bool{}
	delayed := make(chan int)

	for {
		select {
		case event := <-nr.watcher.Events:
			if len(changes) == 0 {
				// if we add the first event initiate a flush event
				go func() {
					time.Sleep(time.Millisecond)
					delayed <- 0
				}()
			}
			changes[event.Path] = true
		case <-nr.stopSignal:
			return
		case <-delayed:
			// now we flush all notifications to the target channel
			for k := range changes {
				nr.target <- k
			}
			// instead of deleting all by iterating, create a new empty map
			changes = map[string]bool{}
		}
	}
}
