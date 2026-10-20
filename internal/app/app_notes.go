package app

import (
	"encoding/base64"
	"errors"

	wr "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/thangkes/cmdex/internal/store"
	"github.com/thangkes/cmdex/internal/viewer"
)

// Notes (the chat bubble): texts the user keeps, with images, encrypted in
// the database like the user's commands.

// Notes lists the notes, oldest first ("" for all, or words to find).
func (a *App) Notes(query string) ([]store.Note, error) { return a.store.Notes(query) }

// AddNote stores a note, about path when it is not "".
func (a *App) AddNote(body, path string) (store.Note, error) { return a.store.AddNote(body, path) }

// EditNote changes a note's text.
func (a *App) EditNote(id int64, body string) error { return a.store.EditNote(id, body) }

// DeleteNote removes a note and its images.
func (a *App) DeleteNote(id int64) error { return a.store.DeleteNote(id) }

// AddNoteImage stores an image (base64) for the note being written and
// returns the ID its text uses: ![name](cmdex-img:ID).
func (a *App) AddNoteImage(b64 string) (int64, error) {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return 0, errors.New("the image could not be read")
	}
	return a.store.AddNoteImage(data)
}

// NoteImage returns an image as a data: URL for the page to show.
func (a *App) NoteImage(id int64) (string, error) {
	im, err := a.store.NoteImage(id)
	if err != nil {
		return "", err
	}
	return "data:" + im.Mime + ";base64," + im.Data, nil
}

// ---- the notes window -------------------------------------------------------
//
// Notes open in a window of their own: a second Cmdex process (package
// viewer) that is part of this session. It never opens the database: its
// page calls these methods here, and only these.

var notesCalls = map[string]bool{"Notes": true, "AddNote": true, "EditNote": true, "DeleteNote": true,
	"AddNoteImage": true, "NoteImage": true, "ShowNotePath": true}

// OpenNotes opens the notes window, or brings it to the front.
func (a *App) OpenNotes() error {
	a.notesMu.Lock()
	defer a.notesMu.Unlock()
	if a.notesWin != nil {
		if a.notesWin.ready {
			a.notesWin.send(viewer.Msg{T: "focus"})
		}
		return nil
	}
	c, err := startChild("notes", 32<<20, a.notesMsg, a.notesEnd)
	if err != nil {
		return err
	}
	a.notesWin = c
	a.emit("notes:state", true)
	return nil
}

// CloseNotes closes the notes window.
func (a *App) CloseNotes() {
	a.notesMu.Lock()
	c := a.notesWin
	a.notesMu.Unlock()
	if c != nil {
		c.close()
	}
}

func (a *App) notesMsg(c *childProc, m viewer.Msg) {
	switch m.T {
	case "ready":
		a.notesMu.Lock()
		c.ready = true
		pin := a.notePin
		a.notesMu.Unlock()
		c.send(a.helloMsg(true))
		c.send(viewer.Msg{T: "pin", Path: pin})
	case "call":
		go func() {
			res, err := a.callMethod(notesCalls, m.Method, m.Args)
			r := viewer.Msg{T: "result", ID: m.ID, Result: res}
			if err != nil {
				r.Result, r.Err = nil, err.Error()
			}
			c.send(r)
		}()
	}
}

func (a *App) notesEnd(c *childProc) {
	a.notesMu.Lock()
	if a.notesWin == c {
		a.notesWin = nil
	}
	started := c.ready
	a.notesMu.Unlock()
	a.emit("notes:state", false, !started) // ended before its page was ready: it failed
}

// notesSend passes a message to the notes window, when it is open.
func (a *App) notesSend(m viewer.Msg) {
	a.notesMu.Lock()
	c := a.notesWin
	ok := c != nil && c.ready
	a.notesMu.Unlock()
	if ok {
		c.send(m)
	}
}

// SetNotePin tells the notes window what the map shows chosen, so a note
// can be pinned to it.
func (a *App) SetNotePin(path string) {
	a.notesMu.Lock()
	a.notePin = path
	a.notesMu.Unlock()
	a.notesSend(viewer.Msg{T: "pin", Path: path})
}

// ShowNotePath shows a note's file or folder on the map, in this window.
func (a *App) ShowNotePath(path string) {
	a.emit("notes:showPath", path)
	if a.ctx != nil {
		wr.WindowUnminimise(a.ctx)
		wr.WindowShow(a.ctx)
	}
}

// closeNotes closes the notes window (shutdown).
func (a *App) closeNotes() {
	a.notesMu.Lock()
	c := a.notesWin
	a.notesMu.Unlock()
	if c != nil {
		c.in.Close()
		c.kill()
	}
}
