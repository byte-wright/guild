package guild

import "log"

type stdoutContext struct {
	files []string
	once  bool
}

func (s *stdoutContext) Files() []string {
	return s.files
}

func (s *stdoutContext) File() string {
	if len(s.files) == 0 {
		return ""
	}

	return s.files[len(s.files)-1]
}

func (s *stdoutContext) Println(out ...any) {
	log.Println(out...)
}

func (s *stdoutContext) Once() bool {
	return s.once
}

// filesContext replaces the files of the context it wraps, for decorators that merge
// several calls into one. File stays the one of the wrapped context, which is the most
// recent change.
type filesContext struct {
	Context
	files []string
}

func (f *filesContext) Files() []string {
	return f.files
}
