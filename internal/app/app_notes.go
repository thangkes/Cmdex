package app

import (
	"encoding/base64"
	"errors"

	"github.com/thangkes/cmdex/internal/store"
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
