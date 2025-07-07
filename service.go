package guild

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
)

type ServiceCmd struct {
	cmd    string
	params []string
	env    map[string]string

	currentCmd *exec.Cmd
}

func Service(cmd string, params ...string) *ServiceCmd {
	return &ServiceCmd{
		cmd:    cmd,
		params: params,
		env:    map[string]string{},
	}
}

func (e *ServiceCmd) Env(name, value string) *ServiceCmd {
	ns := &ServiceCmd{
		cmd:    e.cmd,
		params: e.params,
		env:    e.env,
	}

	for k, v := range e.env {
		ns.env[k] = v
	}

	ns.env[name] = value

	return ns
}

func (e *ServiceCmd) ForwardEnv(name string) *ServiceCmd {
	ns := &ServiceCmd{
		cmd:    e.cmd,
		params: e.params,
		env:    e.env,
	}

	for k, v := range e.env {
		ns.env[k] = v
	}

	ns.env[name] = os.Getenv(name)

	return ns
}

func (e *ServiceCmd) Match(c Context) {
	if c.Once() {
		return
	}

	if e.isRunning() {
		err := e.currentCmd.Process.Signal(syscall.SIGINT)
		if err != nil {
			c.Println(err)
		}

		for i := 0; i < 100; i++ {
			time.Sleep(time.Microsecond * 10)
			if !e.isRunning() {
				break
			}
		}

		if e.isRunning() {
			err := e.currentCmd.Process.Kill()
			if err != nil {
				c.Println(err)
			}
		}

		err = e.currentCmd.Wait()
		if err != nil {
			c.Println(err)
		}
	}

	c.Println("start service")

	e.currentCmd = exec.Command(e.cmd, e.params...)
	e.currentCmd.Env = []string{}

	for k, v := range e.env {
		e.currentCmd.Env = append(e.currentCmd.Env, fmt.Sprintf("%v=%v", k, v))
	}

	stdoutPipe, err := e.currentCmd.StdoutPipe()
	if err != nil {
		c.Println("Error getting StdoutPipe:", err)
		return
	}

	stderrPipe, err := e.currentCmd.StderrPipe()
	if err != nil {
		c.Println("Error getting StderrPipe:", err)
		return
	}

	err = e.currentCmd.Start()
	if err != nil {
		c.Println("Error starting command:", err)
		return
	}

	scan := func(r io.Reader) {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			line := scanner.Text()
			c.Println(line)
		}
		err := scanner.Err()
		if err != nil {
			c.Println("Error reading:", err)
		}
	}

	go scan(stdoutPipe)
	go scan(stderrPipe)
}

func (s *ServiceCmd) isRunning() bool {
	if s.currentCmd == nil {
		return false
	}

	err := s.currentCmd.Process.Signal(syscall.Signal(0))
	return err == nil
}
