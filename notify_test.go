package guild

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
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
	runNotifyRootTest("rename file", t, testRenameFile)
	runNotifyRootTest("move folder out of the tree", t, testMoveFolderOut)
}

func (nrt *notifyRootTest) fetch() []string {
	events := []string{}
	for len(nrt.target) > 0 {
		events = append(events, <-nrt.target)
	}

	sort.Strings(events)

	return events
}

func (nrt *notifyRootTest) write(path string) {
	full := filepath.Join(nrt.dir, path)

	err := os.MkdirAll(filepath.Dir(full), 0o755)
	if err != nil {
		nrt.t.Fatal(err)
	}

	err = os.WriteFile(full, []byte("foobar"), 0o666)
	if err != nil {
		nrt.t.Fatal(err)
	}
}

func (nrt *notifyRootTest) wait() {
	waitEvents()
}

func waitEvents() {
	time.Sleep(time.Millisecond * 20)
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

		n, err := newNotifyRoot(dir, nrt.target, []string{}, func(err error) {
			t.Error("unexpected watch error:", err)
		})
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
	if !slices.Equal(events, []string{"file.txt"}) {
		t.t.Errorf("delete should report file.txt but was %v", events)
	}
}

func testRenameFile(t *notifyRootTest) {
	t.write("a.txt")
	t.wait()
	_ = t.fetch()

	err := os.Rename(filepath.Join(t.dir, "a.txt"), filepath.Join(t.dir, "b.txt"))
	if err != nil {
		t.t.Fatal(err)
	}

	t.wait()
	events := t.fetch()
	if !slices.Equal(events, []string{"a.txt", "b.txt"}) {
		t.t.Errorf("rename should report both names but was %v", events)
	}
}

// rnotify reports a folder moved out of the tree as the folder alone, the files that
// were below it have to be reported anyway.
func testMoveFolderOut(t *notifyRootTest) {
	t.write("pkg/a.txt")
	t.write("pkg/sub/b.txt")
	t.write("other.txt")
	t.wait()
	_ = t.fetch()

	err := os.Rename(filepath.Join(t.dir, "pkg"), filepath.Join(t.t.TempDir(), "pkg"))
	if err != nil {
		t.t.Fatal(err)
	}

	t.wait()
	events := t.fetch()
	if !slices.Equal(events, []string{"pkg/a.txt", "pkg/sub/b.txt"}) {
		t.t.Errorf("moving a folder out should report the files below it but was %v", events)
	}
}

func TestNotifyRootPassesErrorsOn(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read every folder")
	}

	dir := t.TempDir()

	locked := filepath.Join(dir, "pgdata")

	err := os.Mkdir(locked, 0)
	if err != nil {
		t.Fatal(err)
	}

	defer os.Chmod(locked, 0o755)

	errs := make(chan error, 10)

	n, err := newNotifyRoot(dir, make(chan string, 10), nil, func(err error) {
		errs <- err
	})
	if err != nil {
		t.Fatal("an unreadable folder below the root must not fail watching:", err)
	}

	defer n.Stop()

	select {
	case err := <-errs:
		if !strings.Contains(err.Error(), "pgdata") {
			t.Errorf("error should name the folder but was %v", err)
		}
	case <-time.After(time.Second):
		t.Error("the unreadable folder should have been reported")
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
