package guild

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

type notifyRootTest struct {
	t      *testing.T
	dir    string
	n      *notifyRoot
	target chan string
}

func TestNotifyRoot(t *testing.T) {
	runNotifyRootTest("create one file", t, testCreateOneFile)
	runNotifyRootTest("create and delete file", t, testDeleteFile)
	runNotifyRootTest("append one file", t, testAppendFile)
}

func (nrt *notifyRootTest) fetch() []string {
	events := []string{}
	for len(nrt.target) > 0 {
		events = append(events, <-nrt.target)
	}
	return events
}

func (nrt *notifyRootTest) wait() {
	time.Sleep(time.Millisecond * 5)
}

func runNotifyRootTest(name string, t *testing.T, f func(*notifyRootTest)) {
	t.Run(name, func(t *testing.T) {
		dir, err := os.MkdirTemp("", "notify")
		if err != nil {
			t.Fatal(err)
		}

		defer os.RemoveAll(dir)

		nrt := &notifyRootTest{
			t:      t,
			dir:    dir,
			target: make(chan string, 1000),
		}

		n, err := newNotifyRoot(dir, nrt.target, []string{})
		if err != nil {
			t.Fatal(err)
		}
		nrt.n = n

		f(nrt)

		n.Stop()
	})
}

func testCreateOneFile(t *notifyRootTest) {
	tmpFolder := filepath.Join(t.dir, "file.txt")
	if err := os.WriteFile(tmpFolder, []byte("foobar"), 0o666); err != nil {
		t.t.Fatal(err)
	}
	t.wait()
	events := t.fetch()
	if len(events) != 1 {
		t.t.Errorf("should have written 1 event but is %v", len(events))
	}
	if events[0] != "file.txt" {
		t.t.Error("event should be file.txt but was ", events[0])
	}
}

func testDeleteFile(t *notifyRootTest) {
	tmpFile := filepath.Join(t.dir, "file.txt")
	if err := os.WriteFile(tmpFile, []byte("foobar"), 0o666); err != nil {
		t.t.Fatal(err)
	}
	t.wait()
	_ = t.fetch()
	err := os.Remove(tmpFile)
	if err != nil {
		t.t.Error(err)
	}
	t.wait()
	events := t.fetch()
	if len(events) != 0 {
		t.t.Error("delete should not write an event")
	}
}

func testAppendFile(t *notifyRootTest) {
	tmpFile := filepath.Join(t.dir, "file.txt")
	if err := os.WriteFile(tmpFile, []byte("foobar"), 0o666); err != nil {
		t.t.Fatal(err)
	}
	t.wait()
	_ = t.fetch()
	f, err := os.OpenFile(tmpFile, os.O_APPEND|os.O_WRONLY, 0o666)
	if err != nil {
		t.t.Error(err)
	}
	if _, err = f.WriteString("bla"); err != nil {
		t.t.Error(err)
	}
	f.Close()

	t.wait()
	events := t.fetch()
	if len(events) != 1 {
		t.t.Error("should have written 1 event but was ", len(events))
	}

	if events[0] != "file.txt" {
		t.t.Error("event should be file.txt but was ", events[0])
	}
}
