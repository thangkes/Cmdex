// Package conn opens the interactive sessions shown in Cmdex's terminal:
// a shell on this computer, SSH, Telnet and serial (COM / tty) ports.
// Every session moves raw bytes; the terminal emulator lives in the UI.
package conn

import (
	"errors"
	"io"
	"sync"
)

// Session is an open connection.
type Session interface {
	// Write sends what the user typed (or a command Cmdex runs).
	Write(p []byte) (int, error)
	// Resize tells the other side the terminal size, where that means anything.
	Resize(cols, rows int) error
	// Close ends the session.
	Close() error
}

// Handlers receive a session's output and its end.
type Handlers struct {
	Data   func([]byte)
	Closed func(err error)
}

// pump copies r to h.Data until r ends, then reports the end once.
func pump(r io.Reader, h Handlers, done func()) {
	buf := make([]byte, 32<<10)
	for {
		n, err := r.Read(buf)
		if n > 0 && h.Data != nil {
			b := make([]byte, n)
			copy(b, buf[:n])
			h.Data(b)
		}
		if err != nil {
			if done != nil {
				done()
			}
			if h.Closed != nil {
				if errors.Is(err, io.EOF) {
					err = nil
				}
				h.Closed(err)
			}
			return
		}
	}
}

// once makes Close idempotent.
type once struct {
	o   sync.Once
	err error
}

func (c *once) do(f func() error) error {
	c.o.Do(func() { c.err = f() })
	return c.err
}

// Enter converts the Enter key (the terminal sends CR) to the line ending a
// serial device expects.
func Enter(p []byte, lineEnd string) []byte {
	switch lineEnd {
	case "lf":
		return replaceCR(p, []byte{'\n'})
	case "crlf":
		return replaceCR(p, []byte{'\r', '\n'})
	}
	return p
}

func replaceCR(p, with []byte) []byte {
	out := make([]byte, 0, len(p)+4)
	for _, b := range p {
		if b == '\r' {
			out = append(out, with...)
			continue
		}
		out = append(out, b)
	}
	return out
}
