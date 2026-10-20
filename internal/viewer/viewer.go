// Package viewer runs Cmdex's other windows: the shared log window, which
// shows only masked terminal output so it can be shared on screen, and the
// notes window. Each is a second Cmdex process (Wails gives a process one
// window) that belongs to the Cmdex that started it: one session, two
// windows.
//
// Cmdex starts it with "cmdex --viewer" or "cmdex --notes" and talks to it
// over the child's standard input and output, one JSON message per line.
// Those are anonymous pipes: no other program can connect, there is no port
// and no socket. Started by hand, a window finds no Cmdex at the other end
// of its input and closes at once. A window never opens the database: the
// notes window asks Cmdex, which answers only the calls that window may make.
package viewer

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Msg is one message.
//
// From Cmdex: "hello" (Lang, Theme, OS, Protect), "tabs" (Tabs), "reset"
// (Tab, Lines, Stats, Total: the whole masked log of a tab), "add" (Tab,
// Lines, Stats, Total: new lines), "pin" (Path: what the map shows chosen,
// for pinning a note), "protect" (Protect), "focus" (come to the front),
// "result" (ID, Result or Err: the answer to a call).
//
// From a window: "ready" (the page can receive), "mask" (Text: mask this
// too), "call" (ID, Method, Args: a call into Cmdex).
type Msg struct {
	T       string            `json:"t"`
	Lang    string            `json:"lang,omitempty"`
	Theme   string            `json:"theme,omitempty"`
	OS      string            `json:"os,omitempty"`
	Protect bool              `json:"protect,omitempty"`
	Tabs    []Tab             `json:"tabs,omitempty"`
	Tab     string            `json:"tab,omitempty"`
	Lines   []string          `json:"lines,omitempty"`
	Stats   map[string]int    `json:"stats,omitempty"`
	Total   int               `json:"total,omitempty"`
	Text    string            `json:"text,omitempty"`
	Path    string            `json:"path,omitempty"`
	ID      int64             `json:"id,omitempty"`
	Method  string            `json:"method,omitempty"`
	Args    []json.RawMessage `json:"args,omitempty"`
	Result  json.RawMessage   `json:"result,omitempty"`
	Err     string            `json:"err,omitempty"`
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

// Hooks connect a window's bridge to Wails.
type Hooks struct {
	Emit    func(name string, data ...any) // to the page
	Quit    func()                         // close the window
	Focus   func()                         // bring the window to the front (nil: ignored)
	Protect func(on bool)                  // screen-capture protection (nil: never protected)
}

// Bridge is bound to a window's page.
type Bridge struct {
	h   Hooks
	out *Writer

	mu      sync.Mutex
	next    int64
	waiting map[int64]chan Msg
}

// NewBridge connects a window's page to Cmdex: requests go to out.
func NewBridge(out io.Writer, h Hooks) *Bridge {
	return &Bridge{h: h, out: NewWriter(out), waiting: map[int64]chan Msg{}}
}

// Serve reads Cmdex's messages until it closes the pipe, then quits.
func (b *Bridge) Serve(in io.Reader) {
	Read(in, 64<<20, func(m Msg) {
		switch m.T {
		case "result":
			b.mu.Lock()
			c := b.waiting[m.ID]
			delete(b.waiting, m.ID)
			b.mu.Unlock()
			if c != nil {
				c <- m
			}
		case "focus":
			if b.h.Focus != nil {
				b.h.Focus()
			}
		case "protect":
			if b.h.Protect != nil {
				b.h.Protect(m.Protect)
			}
		case "hello", "tabs", "reset", "add", "pin":
			if m.T == "hello" && b.h.Protect != nil {
				b.h.Protect(m.Protect)
			}
			b.h.Emit("view", m)
		}
	})
	b.mu.Lock()
	for id, c := range b.waiting {
		c <- Msg{T: "result", ID: id, Err: "Cmdex has closed"}
		delete(b.waiting, id)
	}
	b.mu.Unlock()
	b.h.Quit()
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

// Call runs method in Cmdex with args (a JSON array) and returns its result
// as JSON. Cmdex decides which methods a window may call.
func (b *Bridge) Call(method, args string) (string, error) {
	var list []json.RawMessage
	if err := json.Unmarshal([]byte(args), &list); err != nil {
		return "", errors.New("bad arguments")
	}
	c := make(chan Msg, 1)
	b.mu.Lock()
	b.next++
	id := b.next
	b.waiting[id] = c
	b.mu.Unlock()
	if err := b.out.Send(Msg{T: "call", ID: id, Method: method, Args: list}); err != nil {
		b.mu.Lock()
		delete(b.waiting, id)
		b.mu.Unlock()
		return "", err
	}
	select {
	case m := <-c:
		if m.Err != "" {
			return "", errors.New(m.Err)
		}
		return string(m.Result), nil
	case <-time.After(2 * time.Minute):
		b.mu.Lock()
		delete(b.waiting, id)
		b.mu.Unlock()
		return "", errors.New("Cmdex did not answer")
	}
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
