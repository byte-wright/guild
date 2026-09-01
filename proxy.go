package guild

import (
	"bufio"
	"io"
	"net"
	"strconv"
	"sync"
	"time"
)

// parkTimeout bounds how long a client connection waits for its container. Without
// it a container that never comes up would hang clients instead of refusing them.
const parkTimeout = readyTimeout

// portMap forwards a stable loopback port owned by guild to the port docker picked
// for the container. Because guild binds while the build is wired up, the port is
// known before the container exists, and connections arriving during startup are
// held open until the container is ready rather than refused.
type portMap struct {
	containerPort int
	hostPort      int

	listener *net.TCPListener
	ready    <-chan struct{}

	lock sync.Mutex
	// target is discovered once the container runs, but read from every connection
	// goroutine, so it is guarded rather than relying on the ready channel.
	targetPort int
	conns      map[net.Conn]bool
	closed     bool
}

func (p *portMap) setTarget(port int) {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.targetPort = port
}

func (p *portMap) target() int {
	p.lock.Lock()
	defer p.lock.Unlock()

	return p.targetPort
}

func newPortMap(containerPort int, ready <-chan struct{}) (*portMap, error) {
	l, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return nil, err
	}

	pm := &portMap{
		containerPort: containerPort,
		hostPort:      l.Addr().(*net.TCPAddr).Port,
		listener:      l,
		ready:         ready,
		conns:         map[net.Conn]bool{},
	}

	go pm.accept()

	return pm, nil
}

func (p *portMap) accept() {
	for {
		conn, err := p.listener.AcceptTCP()
		if err != nil {
			return
		}

		go p.handle(conn)
	}
}

func (p *portMap) handle(client *net.TCPConn) {
	if !p.track(client) {
		client.Close()
		return
	}

	defer p.untrack(client)

	select {
	case <-p.ready:
	case <-time.After(parkTimeout):
		return
	}

	target, err := net.DialTCP("tcp", nil, &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: p.target()})
	if err != nil {
		return
	}

	if !p.track(target) {
		target.Close()
		return
	}

	defer p.untrack(target)

	wg := sync.WaitGroup{}
	wg.Add(2)

	go pipe(target, client, &wg)
	go pipe(client, target, &wg)

	wg.Wait()
}

// pipe copies one direction and half closes the destination, so a peer that is done
// sending does not leave the other side waiting for an EOF that never arrives.
func pipe(dst, src *net.TCPConn, wg *sync.WaitGroup) {
	defer wg.Done()

	_, _ = io.Copy(dst, src)
	_ = dst.CloseWrite()
}

// track registers a connection for shutdown. It reports false once the port map is
// closed, so late connections are not left dangling.
func (p *portMap) track(c net.Conn) bool {
	p.lock.Lock()
	defer p.lock.Unlock()

	if p.closed {
		return false
	}

	p.conns[c] = true

	return true
}

func (p *portMap) untrack(c net.Conn) {
	p.lock.Lock()
	defer p.lock.Unlock()

	delete(p.conns, c)
	c.Close()
}

func (p *portMap) dialTarget() bool {
	conn, err := net.DialTimeout("tcp",
		net.JoinHostPort("127.0.0.1", strconv.Itoa(p.target())), time.Second)
	if err != nil {
		return false
	}

	conn.Close()

	return true
}

func (p *portMap) close() {
	p.lock.Lock()
	p.closed = true
	conns := make([]net.Conn, 0, len(p.conns))

	for c := range p.conns {
		conns = append(conns, c)
	}

	p.conns = map[net.Conn]bool{}
	p.lock.Unlock()

	p.listener.Close()

	for _, c := range conns {
		c.Close()
	}
}

func scanLines(r io.Reader, c Context) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		c.Println(scanner.Text())
	}
}
