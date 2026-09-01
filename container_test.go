package guild

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// stubDocker puts a fake docker on PATH that reports the given port as published and
// records the arguments it was called with.
func stubDocker(t *testing.T, publishedPort int) string {
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")

	script := "#!/bin/sh\n" +
		"echo \"$@\" >> " + log + "\n" +
		"case \"$1\" in\n" +
		"  run) echo deadbeef ;;\n" +
		"  port) echo 127.0.0.1:" + strconv.Itoa(publishedPort) + " ;;\n" +
		"  logs) echo stub log line ;;\n" +
		"esac\n"

	err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755)
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	return log
}

func TestContainerStartsAndForwards(t *testing.T) {
	target, closeTarget := echoServer(t)
	defer closeTarget()

	calls := stubDocker(t, target)

	b, err := New(t.TempDir(), []string{})
	if err != nil {
		t.Fatal(err)
	}

	defer b.Close()

	db := b.Container("postgres:17").Port(5432).Env("POSTGRES_PASSWORD", "dev")

	if db.HostPort(5432) == 0 {
		t.Fatal("host port should be bound while wiring up")
	}

	err = b.startContainers(&stdoutContext{})
	if err != nil {
		t.Fatal(err)
	}

	conn, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(db.HostPort(5432))))
	if err != nil {
		t.Fatal(err)
	}

	defer conn.Close()

	if _, err := fmt.Fprint(conn, "ping"); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 4)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}

	if string(buf) != "ping" {
		t.Errorf("expected ping but was %q", string(buf))
	}

	out, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"--rm", "--label guild.root=", "-p 127.0.0.1:0:5432", "-e POSTGRES_PASSWORD=dev"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("docker run should have contained %q, calls were:\n%v", want, string(out))
		}
	}
}

func TestPublishWritesEnvFile(t *testing.T) {
	dir := t.TempDir()

	b, err := New(dir, []string{})
	if err != nil {
		t.Fatal(err)
	}

	b.Publish("DATABASE_URL", "postgres://localhost:123/x")
	b.Publish("REDIS_URL", "redis://localhost:456")

	err = b.writeEnv()
	if err != nil {
		t.Fatal(err)
	}

	out, err := os.ReadFile(filepath.Join(dir, stateDir, "env.sh"))
	if err != nil {
		t.Fatal(err)
	}

	want := "export DATABASE_URL='postgres://localhost:123/x'\nexport REDIS_URL='redis://localhost:456'\n"
	if string(out) != want {
		t.Errorf("expected %q but was %q", want, string(out))
	}

	b.Close()

	_, err = os.Stat(filepath.Join(dir, stateDir, "env.sh"))
	if !os.IsNotExist(err) {
		t.Error("env file should be removed on shutdown, a stale one points at dead containers")
	}
}

func TestPublishQuotesValues(t *testing.T) {
	dir := t.TempDir()

	b, err := New(dir, []string{})
	if err != nil {
		t.Fatal(err)
	}

	defer b.Close()

	b.Publish("PASSWORD", "it's $HOME; rm -rf /")

	err = b.writeEnv()
	if err != nil {
		t.Fatal(err)
	}

	out, err := exec.Command("sh", "-c",
		". "+filepath.Join(dir, stateDir, "env.sh")+" && printf %s \"$PASSWORD\"").Output()
	if err != nil {
		t.Fatal(err)
	}

	if string(out) != "it's $HOME; rm -rf /" {
		t.Errorf("value should survive sourcing unchanged but was %q", string(out))
	}
}

func TestStateDirIsNotWatched(t *testing.T) {
	dir := t.TempDir()

	b, err := New(dir, []string{"node_modules"})
	if err != nil {
		t.Fatal(err)
	}

	defer b.Close()

	b.Publish("A", "b")

	err = b.writeEnv()
	if err != nil {
		t.Fatal(err)
	}

	waitEvents()

	for len(b.changes) > 0 {
		c := <-b.changes
		if strings.HasPrefix(c, stateDir) {
			t.Errorf("writing %v must not trigger a change event", c)
		}
	}
}
