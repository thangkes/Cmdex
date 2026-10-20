// Package viewer is the shared-log window: a second Cmdex process that
// shows only masked terminal output, so it can be shared on screen while
// the main window (with the real values) stays protected.
//
// Cmdex starts it with "cmdex --viewer" and talks to it over the child's
// standard input and output, one JSON message per line. Those are anonymous
// pipes: no other program can connect, there is no port and no socket.
// Started by hand, it finds no Cmdex at the other end of its input and
// closes at once. It never opens the database.
package viewer

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"unicode/utf8"
)

// Msg is one message. From Cmdex: "hello" (Lang, Theme), "tabs" (Tabs),
// "reset" (Tab, Lines, Stats, Total: the whole masked log of a tab), "add"
// (Tab, Lines, Stats, Total: new lines). From the viewer: "ready" (the page
// can receive), "mask" (Text: mask this too).
type Msg struct {
	T     string         `json:"t"`
	Lang  string         `json:"lang,omitempty"`
	Theme string         `json:"theme,omitempty"`
	Tabs  []Tab          `json:"tabs,omitempty"`
	Tab   string         `json:"tab,omitempty"`
	Lines []string       `json:"lines,omitempty"`
	Stats map[string]int `json:"stats,omitempty"`
	Total int            `json:"total,omitempty"`
	Text  string         `json:"text,omitempty"`
}

// Tab names a terminal tab without its connection's name.
type Tab struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Active bool   `json:"active"`
	Ended  bool   `json:"ended"`
}

// MaxText is the longest selection the viewer asks to mask.
const MaxText = 300

// Bridge is bound to the viewer page.
type Bridge struct {
	emit func(name string, data ...any)
	quit func()
	out  *Writer
}

// NewBridge connects the page to Cmdex: messages read from in go to the
// page as "view" events (emit); requests go to out. quit closes the window
// when Cmdex goes away.
func NewBridge(out io.Writer, emit func(string, ...any), quit func()) *Bridge {
	return &Bridge{emit: emit, quit: quit, out: NewWriter(out)}
}

// Serve reads Cmdex's messages until it closes the pipe, then quits.
func (b *Bridge) Serve(in io.Reader) {
	Read(in, 64<<20, func(m Msg) {
		switch m.T {
		case "hello", "tabs", "reset", "add":
			b.emit("view", m)
		}
	})
	b.quit()
}

// Ready tells Cmdex the page can receive (it then sends everything).
func (b *Bridge) Ready() error { return b.out.Send(Msg{T: "ready"}) }

// MaskThis asks Cmdex to mask the selected text from now on, in every tab.
func (b *Bridge) MaskThis(text string) error {
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > MaxText || strings.ContainsAny(text, "\r\n") {
		return nil // one line, as selected in one log line
	}
	return b.out.Send(Msg{T: "mask", Text: text})
}

// Writer sends messages, one per line, from any goroutine.
type Writer struct {
	mu sync.Mutex
	w  io.Writer
}

// NewWriter wraps w.
func NewWriter(w io.Writer) *Writer { return &Writer{w: w} }

// Send writes one message.
func (w *Writer) Send(m Msg) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	_, err = w.w.Write(append(b, '\n'))
	return err
}

// Read calls f with each message read from r until it ends. A line longer
// than max or not a message ends the reading: the other side is not Cmdex.
func Read(r io.Reader, max int, f func(Msg)) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), max)
	for sc.Scan() {
		var m Msg
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil || m.T == "" {
			return
		}
		f(m)
	}
}
