package guild

// stdCapture routes what the process writes to stdout and stderr into the ui log.
// Everything that does not print through a Context ends up there: the standard
// logger, libraries, and the build program itself. Without it those lines would
// only be in the log files, which cannot be read while the ui owns the terminal.
type stdCapture struct {
	out *ANSIOut
	err *ANSIOut
	log *logBuffer
}

// newStdCapture registers the two outputs, so they get a button next to the
// services and can be hidden like any other.
func (g *GBuild) newStdCapture(log *logBuffer) *stdCapture {
	c := &stdCapture{
		out: NewANSIOut("stdout", 12, 160, 160, 160, nil),
		err: NewANSIOut("stderr", 12, 255, 80, 80, nil),
		log: log,
	}

	g.outs.register(c.out)
	g.outs.register(c.err)

	return c
}

// stdout and stderr are the taps of the redirect. They append to the buffer
// directly instead of going through outputs, because an output with no sink falls
// back to printing to stdout, which is what feeds these taps.
func (c *stdCapture) stdout(line string) {
	c.log.Lines(c.out, []string{line})
}

func (c *stdCapture) stderr(line string) {
	c.log.Lines(c.err, []string{line})
}
