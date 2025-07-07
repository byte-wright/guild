package guild

import "log"

type stdoutContext struct {
	file string
	once bool
}

func (s *stdoutContext) File() string {
	return ""
}

func (s *stdoutContext) Println(out ...any) {
	log.Println(out...)
}

func (s *stdoutContext) Once() bool {
	return s.once
}
