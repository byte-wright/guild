package guild

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func writeFiles(t *testing.T, root string, paths ...string) {
	t.Helper()

	for _, p := range paths {
		full := filepath.Join(root, p)

		err := os.MkdirAll(filepath.Dir(full), 0o755)
		if err != nil {
			t.Fatal(err)
		}

		err = os.WriteFile(full, []byte("x"), 0o644)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func receive(t *testing.T, c chan Context) Context {
	t.Helper()

	select {
	case ctx := <-c:
		return ctx
	case <-time.After(2 * time.Second):
		t.Fatal("matcher was not called")
		return nil
	}
}

func TestDebounceMergesFiles(t *testing.T) {
	got := make(chan Context, 1)

	chains := map[string]func(Matcher) Matcher{
		"debounce": func(m Matcher) Matcher {
			return Debounce(20*time.Millisecond, m)
		},
		"debounce around output": func(m Matcher) Matcher {
			return Debounce(20*time.Millisecond, NewANSIOut("out", 8, 0, 0, 0, m))
		},
		"output around debounce": func(m Matcher) Matcher {
			return NewANSIOut("out", 8, 0, 0, 0, Debounce(20*time.Millisecond, m))
		},
	}

	for name, chain := range chains {
		t.Run(name, func(t *testing.T) {
			m := chain(Func(func(c Context) { got <- c }))

			for _, f := range []string{"b.go", "a.go", "b.go"} {
				m.Match(&stdoutContext{files: []string{f}})
			}

			c := receive(t, got)
			if !slices.Equal(c.Files(), []string{"a.go", "b.go"}) {
				t.Errorf("files should be merged and sorted but were %v", c.Files())
			}

			if c.File() != "b.go" {
				t.Errorf("file should be the last change but was %v", c.File())
			}

			m.Match(&stdoutContext{files: []string{"c.go"}})

			c = receive(t, got)
			if !slices.Equal(c.Files(), []string{"c.go"}) {
				t.Errorf("the next batch should start empty but was %v", c.Files())
			}
		})
	}
}

func TestRunPassesChangedAndRemovedFiles(t *testing.T) {
	dir := t.TempDir()

	b, err := New(dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	defer b.Close()

	got := make(chan Context, 10)

	b.On(`\.txt$`, Func(func(c Context) { got <- c }))

	go b.run()

	writeFiles(t, dir, "a.txt")

	c := receive(t, got)
	if !slices.Equal(c.Files(), []string{"a.txt"}) || c.File() != "a.txt" || c.Once() {
		t.Errorf("a write should report a.txt, was %v %q once=%v", c.Files(), c.File(), c.Once())
	}

	// let the write settle, its events must not be mistaken for the removal
	time.Sleep(50 * time.Millisecond)

	for len(got) > 0 {
		<-got
	}

	err = os.Remove(filepath.Join(dir, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}

	c = receive(t, got)
	if !slices.Equal(c.Files(), []string{"a.txt"}) {
		t.Errorf("a removal should report a.txt, was %v", c.Files())
	}
}

func TestOncePassesMatchingFiles(t *testing.T) {
	dir := t.TempDir()

	writeFiles(t, dir,
		"a.png",
		"img/b.png",
		"img/c.jpeg",
		"node_modules/x.png",
		"notes.txt",
	)

	b, err := New(dir, []string{"node_modules"})
	if err != nil {
		t.Fatal(err)
	}

	defer b.Close()

	var images Context

	svgCalled := false

	b.On(`\.(png|jpeg)$`, Func(func(c Context) { images = c }))
	b.On(`\.svg$`, Func(func(Context) { svgCalled = true }))

	err = b.Once()
	if err != nil {
		t.Fatal(err)
	}

	if images == nil {
		t.Fatal("the image listener should have been called")
	}

	if !slices.Equal(images.Files(), []string{"a.png", "img/b.png", "img/c.jpeg"}) {
		t.Errorf("files should be the sorted matches outside of excluded folders but were %v",
			images.Files())
	}

	if !images.Once() {
		t.Error("context should be in once mode")
	}

	if svgCalled {
		t.Error("a listener without matching files must not be called")
	}
}

func TestOnceSeesFilesOfEarlierListeners(t *testing.T) {
	dir := t.TempDir()

	writeFiles(t, dir, "locales/de.yaml")

	b, err := New(dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	defer b.Close()

	var goFiles []string

	b.On(`\.yaml$`, Func(func(Context) {
		writeFiles(t, dir, "gen/locales.go")
	}))
	b.On(`\.go$`, Func(func(c Context) { goFiles = c.Files() }))

	err = b.Once()
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(goFiles, []string{"gen/locales.go"}) {
		t.Errorf("the generated file should be passed to the later listener but was %v", goFiles)
	}
}
