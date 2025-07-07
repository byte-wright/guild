package guild

import (
	"bufio"
	"io"
	"os/exec"
	"strings"
	"sync"
)

type ExecCmd struct {
	cmd    string
	params []string
}

func Exec(cmd string, params ...string) *ExecCmd {
	return &ExecCmd{
		cmd:    cmd,
		params: params,
	}
}

func (e *ExecCmd) Match(c Context) {
	c.Println(" >", e.cmd, strings.Join(e.params, " "))

	cmd := exec.Command(e.cmd, e.params...)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		c.Println("Error getting StdoutPipe:", err)
		return
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		c.Println("Error getting StderrPipe:", err)
		return
	}

	err = cmd.Start()
	if err != nil {
		c.Println("Error starting command:", err)
		return
	}

	scan := func(r io.Reader, wg *sync.WaitGroup) {
		defer wg.Done()
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			line := scanner.Text()
			c.Println(line)
		}
		err := scanner.Err()
		if err != nil {
			c.Println(err)
		}
	}

	wg := sync.WaitGroup{}
	wg.Add(2)

	go scan(stdoutPipe, &wg)
	go scan(stderrPipe, &wg)

	wg.Wait()
	err = cmd.Wait()
	if err != nil {
		c.Println("Command finished with error:", err)
	}
}
