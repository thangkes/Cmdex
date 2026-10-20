package viewer

import (
	"io"
	"testing"
	"time"
)

// A window's call goes to Cmdex and its answer comes back; when Cmdex
// closes the pipe, waiting calls fail and the window quits.
func TestBridgeCall(t *testing.T) {
	toCmdex, fromWin := io.Pipe()
	toWin, fromCmdex := io.Pipe()
	quit := make(chan bool, 1)
	var events []Msg
	protected := make(chan bool, 4)
	b := NewBridge(fromWin, Hooks{
		Emit:    func(name string, data ...any) { events = append(events, data[0].(Msg)) },
		Quit:    func() { quit <- true },
		Protect: func(on bool) { protected <- on },
	})
	go b.Serve(toWin)
	cmdex := NewWriter(fromCmdex)
	go Read(toCmdex, 1<<20, func(m Msg) {
		if m.T == "call" && m.Method == "Notes" {
			cmdex.Send(Msg{T: "result", ID: m.ID, Result: []byte(`[{"id":1}]`)})
		}
		if m.T == "call" && m.Method == "Bad" {
			cmdex.Send(Msg{T: "result", ID: m.ID, Err: "Bad is not available here"})
		}
	})
	if r, err := b.Call("Notes", `[""]`); err != nil || r != `[{"id":1}]` {
		t.Fatalf("got %q %v", r, err)
	}
	if _, err := b.Call("Bad", `[]`); err == nil {
		t.Error("an error from Cmdex was lost")
	}
	if _, err := b.Call("Notes", `not json`); err == nil {
		t.Error("bad arguments sent")
	}
	cmdex.Send(Msg{T: "hello", Protect: true})
	if on := <-protected; !on {
		t.Error("hello did not protect")
	}
	fromCmdex.Close()
	select {
	case <-quit:
	case <-time.After(3 * time.Second):
		t.Fatal("the window did not quit when Cmdex went away")
	}
}
