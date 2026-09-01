package guild

import (
	"fmt"
	"io"
	"net"
	"strconv"
	"testing"
	"time"
)

// echoServer stands in for a container, listening on a port guild discovers later.
func echoServer(t *testing.T) (int, func()) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}

			go func() {
				_, _ = io.Copy(c, c)
				c.Close()
			}()
		}
	}()

	return l.Addr().(*net.TCPAddr).Port, func() { l.Close() }
}

func dialHost(t *testing.T, p *portMap) net.Conn {
	conn, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(p.hostPort)))
	if err != nil {
		t.Fatal(err)
	}

	return conn
}

func TestProxyForwards(t *testing.T) {
	target, closeTarget := echoServer(t)
	defer closeTarget()

	ready := make(chan struct{})

	p, err := newPortMap(5432, ready)
	if err != nil {
		t.Fatal(err)
	}

	defer p.close()

	p.setTarget(target)
	close(ready)

	conn := dialHost(t, p)
	defer conn.Close()

	if _, err := fmt.Fprint(conn, "hello"); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 5)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}

	if string(buf) != "hello" {
		t.Errorf("expected hello but was %q", string(buf))
	}
}

// A client connecting before the container exists must be held, not refused, so
// services do not need connect retry logic.
func TestProxyParksUntilReady(t *testing.T) {
	target, closeTarget := echoServer(t)
	defer closeTarget()

	ready := make(chan struct{})

	p, err := newPortMap(5432, ready)
	if err != nil {
		t.Fatal(err)
	}

	defer p.close()

	conn := dialHost(t, p)
	defer conn.Close()

	if _, err := fmt.Fprint(conn, "early"); err != nil {
		t.Fatal(err)
	}

	err = conn.SetReadDeadline(time.Now().Add(time.Millisecond * 50))
	if err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 5)
	_, err = io.ReadFull(conn, buf)

	netErr, ok := err.(net.Error)
	if !ok || !netErr.Timeout() {
		t.Fatalf("connection should have been parked but read returned %v", err)
	}

	p.setTarget(target)
	close(ready)

	err = conn.SetReadDeadline(time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}

	if string(buf) != "early" {
		t.Errorf("expected early but was %q", string(buf))
	}
}

func TestProxyCloseDropsConnections(t *testing.T) {
	target, closeTarget := echoServer(t)
	defer closeTarget()

	ready := make(chan struct{})
	close(ready)

	p, err := newPortMap(5432, ready)
	if err != nil {
		t.Fatal(err)
	}

	p.setTarget(target)

	conn := dialHost(t, p)
	defer conn.Close()

	if _, err := fmt.Fprint(conn, "x"); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 1)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}

	p.close()

	err = conn.SetReadDeadline(time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}

	_, err = io.ReadFull(conn, buf)
	if err == nil {
		t.Error("connection should have been closed")
	}
}
