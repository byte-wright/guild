package guild

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// stateDir holds guild's own runtime state below the watched root. It is excluded
// from watching and should be added to .gitignore.
const stateDir = ".guild"

// rootLabel marks every container started by a guild instance so leftovers from a
// previous run can be reaped, even if that run was killed hard.
const rootLabel = "guild.root"

const readyTimeout = time.Minute

// Container is a docker container started when guild starts and removed when it
// stops. Containers are always created fresh, never reused, and carry no named
// volumes, so every run starts from clean state.
type Container struct {
	image string
	name  string
	env   map[string]string
	ports []*portMap
	out   *ANSIOut

	root  string
	ready chan struct{}
}

// Container declares a container. It is started by Continuous and ignored by Once.
func (g *GBuild) Container(image string) *Container {
	g.lock.Lock()
	defer g.lock.Unlock()

	c := &Container{
		image: image,
		name:  fmt.Sprintf("guild-%v-%v", shortHash(g.root), len(g.containers)),
		env:   map[string]string{},
		root:  g.root,
		ready: make(chan struct{}),
	}

	g.containers = append(g.containers, c)

	return c
}

func (c *Container) Env(name, value string) *Container {
	c.env[name] = value
	return c
}

// Out routes the container logs through a colored prefix, like NewANSIOut does for
// matchers.
func (c *Container) Out(prefix string, n int, r, g, b int) *Container {
	c.out = NewANSIOut(prefix, n, r, g, b, nil)
	return c
}

// Port exposes a container port. Guild binds a loopback port immediately, so
// HostPort is known while wiring up, before the container exists. Connections to it
// are held until the container accepts them.
func (c *Container) Port(containerPort int) *Container {
	pm, err := newPortMap(containerPort, c.ready)
	if err != nil {
		panic(fmt.Sprintf("cannot listen for container port %v: %v", containerPort, err))
	}

	c.ports = append(c.ports, pm)

	return c
}

// HostPort is the loopback port that forwards to the given container port.
func (c *Container) HostPort(containerPort int) int {
	for _, p := range c.ports {
		if p.containerPort == containerPort {
			return p.hostPort
		}
	}

	panic(fmt.Sprintf("port %v was not exposed on container %v", containerPort, c.image))
}

func (c *Container) context() Context {
	if c.out == nil {
		return &stdoutContext{}
	}

	return c.out.Context()
}

func (c *Container) start() error {
	ctx := c.context()

	args := []string{
		"run", "-d", "--rm",
		"--name", c.name,
		"--label", rootLabel + "=" + c.root,
	}

	for k, v := range c.env {
		args = append(args, "-e", fmt.Sprintf("%v=%v", k, v))
	}

	for _, p := range c.ports {
		// port 0 lets docker pick, so concurrent guild instances never collide
		args = append(args, "-p", fmt.Sprintf("127.0.0.1:0:%v", p.containerPort))
	}

	args = append(args, c.image)

	ctx.Println("starting", c.image)

	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker run %v: %v: %v", c.image, err, strings.TrimSpace(string(out)))
	}

	for _, p := range c.ports {
		published, err := c.publishedPort(p.containerPort)
		if err != nil {
			return err
		}

		p.setTarget(published)
	}

	go c.streamLogs()

	err = c.waitReady()
	if err != nil {
		return err
	}

	close(c.ready)
	ctx.Println("ready")

	return nil
}

// publishedPort asks docker which loopback port it picked for a container port.
func (c *Container) publishedPort(containerPort int) (int, error) {
	out, err := exec.Command("docker", "port", c.name, fmt.Sprintf("%v/tcp", containerPort)).Output()
	if err != nil {
		return 0, fmt.Errorf("docker port %v %v: %v", c.name, containerPort, err)
	}

	line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])

	idx := strings.LastIndex(line, ":")
	if idx < 0 {
		return 0, fmt.Errorf("cannot parse published port from %q", line)
	}

	return strconv.Atoi(line[idx+1:])
}

func (c *Container) waitReady() error {
	deadline := time.Now().Add(readyTimeout)

	for _, p := range c.ports {
		for {
			if p.dialTarget() {
				break
			}

			if time.Now().After(deadline) {
				return fmt.Errorf("container %v did not accept connections on port %v within %v",
					c.image, p.containerPort, readyTimeout)
			}

			time.Sleep(time.Millisecond * 100)
		}
	}

	return nil
}

func (c *Container) streamLogs() {
	ctx := c.context()

	cmd := exec.Command("docker", "logs", "-f", c.name)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return
	}

	err = cmd.Start()
	if err != nil {
		return
	}

	go scanLines(stdoutPipe, ctx)
	go scanLines(stderrPipe, ctx)

	_ = cmd.Wait()
}

func (c *Container) stop() {
	for _, p := range c.ports {
		p.close()
	}

	err := exec.Command("docker", "rm", "-fv", c.name).Run()
	if err != nil {
		c.context().Println("could not remove container:", err)
	}
}

func (g *GBuild) startContainers(ctx Context) error {
	g.lock.RLock()
	containers := append([]*Container{}, g.containers...)
	g.lock.RUnlock()

	if len(containers) == 0 {
		return nil
	}

	err := sweep(g.root)
	if err != nil {
		ctx.Println("could not reap old containers:", err)
	}

	// containers do not depend on each other, so there is no reason to serialize them
	errs := make(chan error, len(containers))

	for _, c := range containers {
		go func(c *Container) {
			errs <- c.start()
		}(c)
	}

	var first error

	for range containers {
		err := <-errs
		if err != nil && first == nil {
			first = err
		}
	}

	return first
}

// sweep removes containers left behind by an earlier run of this root. Without it a
// hard kill would leak both the containers and their anonymous volumes.
func sweep(root string) error {
	out, err := exec.Command("docker", "ps", "-aq", "--filter", "label="+rootLabel+"="+root).Output()
	if err != nil {
		return err
	}

	ids := strings.Fields(string(out))
	if len(ids) == 0 {
		return nil
	}

	return exec.Command("docker", append([]string{"rm", "-fv"}, ids...)...).Run()
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:8]
}
