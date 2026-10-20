package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/thangkes/cmdex/internal/viewer"
)

// The notes window is part of this session: its calls reach this App, and
// only the notes' own methods answer.
func TestNotesWindowCalls(t *testing.T) {
	a := maskApp(t)
	a.SetNotePin(`C:\work\site.txt`)
	c, got, send := fakeChild(t, "notes", a.notesMsg, a.notesEnd)
	a.notesMu.Lock()
	a.notesWin = c
	a.notesMu.Unlock()

	send.Send(viewer.Msg{T: "ready"})
	if h := next(t, got, "hello"); !h.Protect {
		t.Errorf("the notes window should be protected by default: %+v", h)
	}
	if p := next(t, got, "pin"); p.Path != `C:\work\site.txt` {
		t.Errorf("pin %q", p.Path)
	}

	call := func(id int64, method string, args ...any) viewer.Msg {
		t.Helper()
		var raw []json.RawMessage
		for _, x := range args {
			b, _ := json.Marshal(x)
			raw = append(raw, b)
		}
		send.Send(viewer.Msg{T: "call", ID: id, Method: method, Args: raw})
		r := next(t, got, "result")
		if r.ID != id {
			t.Fatalf("result %d for call %d", r.ID, id)
		}
		return r
	}
	if r := call(1, "AddNote", "from the notes window", ""); r.Err != "" {
		t.Fatal(r.Err)
	}
	r := call(2, "Notes", "")
	if r.Err != "" || !strings.Contains(string(r.Result), "from the notes window") {
		t.Fatalf("notes %s %s", r.Result, r.Err)
	}
	notes, _ := a.store.Notes("")
	if len(notes) != 1 {
		t.Fatalf("the note is not in this session's database: %+v", notes)
	}
	// anything else is refused
	for _, m := range []string{"SetSetting", "OpenLocal", "Run", "DeleteConnection", "ExportBackup", "MaskInfo", "notesMsg"} {
		if r := call(3, m, "x", "y"); r.Err == "" {
			t.Errorf("%s answered the notes window", m)
		}
	}
	if r := call(4, "AddNote", 5, true); r.Err == "" {
		t.Error("bad arguments accepted")
	}

	// protection and map choices follow the main window
	a.SetProtection(false)
	if p := next(t, got, "protect"); p.Protect {
		t.Error("protection change not passed on")
	}
	a.SetNotePin("/home/u/x")
	if p := next(t, got, "pin"); p.Path != "/home/u/x" {
		t.Errorf("pin %q", p.Path)
	}
}
