package store

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Notes are texts the user keeps in Cmdex (the chat bubble), each
// optionally about a file or folder. The text is plain Markdown-like
// markup, never HTML: the page draws it with its own renderer. Images are
// stored apart (note_images) and placed in the text as
// ![name](cmdex-img:ID). Text, path and images are encrypted like the
// user's commands.

// Note is one note.
type Note struct {
	ID      int64       `json:"id"`
	Body    string      `json:"body"`
	Path    string      `json:"path,omitempty"` // a file or folder the note is about
	Created int64       `json:"created"`
	Updated int64       `json:"updated"`
	Images  []NoteImage `json:"images,omitempty"` // in backups only
}

// NoteImage is an image of a note; Data is base64.
type NoteImage struct {
	ID   int64  `json:"id"`
	Mime string `json:"mime"`
	Data string `json:"data"`
}

const (
	maxNoteBody  = 50000
	maxNotePath  = 4096
	maxNoteImage = 8 << 20 // bytes, after the page has scaled the image down
)

var (
	ErrNoteEmpty  = errors.New("the note is empty")
	ErrNotAnImage = errors.New("only PNG, JPEG, GIF and WebP images can be added")
	reImageRef    = regexp.MustCompile(`\(cmdex-img:(\d+)\)`)
)

func checkNote(body, path string) error {
	if strings.TrimSpace(body) == "" {
		return ErrNoteEmpty
	}
	if utf8.RuneCountInString(body) > maxNoteBody || len(path) > maxNotePath {
		return errors.New("the note is too long")
	}
	if !utf8.ValidString(body) || !utf8.ValidString(path) {
		return errors.New("the note is not valid text")
	}
	return nil
}

// noteKey identifies a note across computers (image IDs differ there).
func noteKey(n Note) string {
	return strconv.FormatInt(n.Created, 10) + "\x00" + reImageRef.ReplaceAllString(n.Body, "(cmdex-img)")
}

// imageRefs lists the image IDs a note's text shows.
func imageRefs(body string) []int64 {
	var out []int64
	for _, m := range reImageRef.FindAllStringSubmatch(body, -1) {
		if id, err := strconv.ParseInt(m[1], 10, 64); err == nil {
			out = append(out, id)
		}
	}
	return out
}

// Notes returns the notes, oldest first; with query, only those whose text
// or path holds every word of it (case-insensitive). Notes are decrypted
// to be searched: there are few of them.
func (s *Store) Notes(query string) ([]Note, error) {
	rows, err := s.db.Query("SELECT id, body, path, created, updated FROM notes ORDER BY created, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	words := strings.Fields(strings.ToLower(query))
	out := []Note{}
	for rows.Next() {
		var n Note
		if err := rows.Scan(&n.ID, &n.Body, &n.Path, &n.Created, &n.Updated); err != nil {
			return nil, err
		}
		if n.Body, err = s.open("notes.body", n.Body); err != nil {
			return nil, err
		}
		if n.Path, err = s.open("notes.path", n.Path); err != nil {
			return nil, err
		}
		hay := strings.ToLower(n.Body + "\n" + n.Path)
		keep := true
		for _, w := range words {
			keep = keep && strings.Contains(hay, w)
		}
		if keep {
			out = append(out, n)
		}
	}
	return out, rows.Err()
}

// AddNote stores a new note; the images its text shows become its own.
func (s *Store) AddNote(body, path string) (Note, error) {
	body = strings.TrimRight(body, " \t\r\n")
	if err := checkNote(body, path); err != nil {
		return Note{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Note{}, err
	}
	defer tx.Rollback()
	t := now()
	res, err := tx.Exec("INSERT INTO notes(body, path, created, updated) VALUES(?, ?, ?, ?)", s.seal("notes.body", body), s.seal("notes.path", path), t, t)
	if err != nil {
		return Note{}, err
	}
	id, _ := res.LastInsertId()
	if err := claimImages(tx, id, body); err != nil {
		return Note{}, err
	}
	return Note{ID: id, Body: body, Path: path, Created: t, Updated: t}, tx.Commit()
}

// EditNote changes a note's text; images it no longer shows are deleted.
func (s *Store) EditNote(id int64, body string) error {
	body = strings.TrimRight(body, " \t\r\n")
	if err := checkNote(body, ""); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec("UPDATE notes SET body = ?, updated = ? WHERE id = ?", s.seal("notes.body", body), now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("no such note")
	}
	if err := claimImages(tx, id, body); err != nil {
		return err
	}
	return tx.Commit()
}

// claimImages gives note id the images its text shows that are its own or
// not yet any note's, and deletes its images the text no longer shows.
func claimImages(tx *sql.Tx, id int64, body string) error {
	refs := imageRefs(body)
	keep := map[int64]bool{}
	for _, r := range refs {
		keep[r] = true
		if _, err := tx.Exec("UPDATE note_images SET note_id = ? WHERE id = ? AND note_id IN (0, ?)", id, r, id); err != nil {
			return err
		}
	}
	rows, err := tx.Query("SELECT id FROM note_images WHERE note_id = ?", id)
	if err != nil {
		return err
	}
	var drop []int64
	for rows.Next() {
		var x int64
		rows.Scan(&x)
		if !keep[x] {
			drop = append(drop, x)
		}
	}
	rows.Close()
	for _, x := range drop {
		if _, err := tx.Exec("DELETE FROM note_images WHERE id = ?", x); err != nil {
			return err
		}
	}
	return nil
}

// DeleteNote removes a note and its images.
func (s *Store) DeleteNote(id int64) error {
	_, err := s.db.Exec("DELETE FROM notes WHERE id = ?", id)
	return err
}

// imageType checks that data really is an image Cmdex shows (never SVG,
// which can hold scripts) and returns its type.
func imageType(data []byte) (string, error) {
	if len(data) == 0 || len(data) > maxNoteImage {
		return "", errors.New("the image is empty or larger than 8 MB")
	}
	switch m := http.DetectContentType(data); m {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return m, nil
	}
	return "", ErrNotAnImage
}

// AddNoteImage stores an image for a note being written (it belongs to no
// note until one shows it) and returns its ID.
func (s *Store) AddNoteImage(data []byte) (int64, error) {
	mime, err := imageType(data)
	if err != nil {
		return 0, err
	}
	res, err := s.db.Exec("INSERT INTO note_images(note_id, mime, data, created) VALUES(0, ?, ?, ?)",
		mime, s.seal("notes.image", base64.StdEncoding.EncodeToString(data)), now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// NoteImage returns an image as base64 with its type.
func (s *Store) NoteImage(id int64) (NoteImage, error) {
	im := NoteImage{ID: id}
	if err := s.db.QueryRow("SELECT mime, data FROM note_images WHERE id = ?", id).Scan(&im.Mime, &im.Data); err != nil {
		return im, err
	}
	var err error
	im.Data, err = s.open("notes.image", im.Data)
	return im, err
}

// DropLooseNoteImages deletes images added for a note that was never saved
// (older than a day).
func (s *Store) DropLooseNoteImages() error {
	_, err := s.db.Exec("DELETE FROM note_images WHERE note_id = 0 AND created < ?", now()-86400)
	return err
}

// noteImages returns a note's images (for a backup).
func (s *Store) noteImages(id int64) ([]NoteImage, error) {
	rows, err := s.db.Query("SELECT id FROM note_images WHERE note_id = ? ORDER BY id", id)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var x int64
		rows.Scan(&x)
		ids = append(ids, x)
	}
	rows.Close()
	var out []NoteImage
	for _, x := range ids {
		im, err := s.NoteImage(x)
		if err != nil {
			return nil, err
		}
		out = append(out, im)
	}
	return out, nil
}

// importNote adds a note from a backup: its images get new IDs here and the
// text is changed to show them.
func (s *Store) importNote(tx *sql.Tx, n Note) error {
	newID := map[int64]int64{}
	for _, im := range n.Images {
		data, err := base64.StdEncoding.DecodeString(im.Data)
		if err != nil {
			continue
		}
		mime, err := imageType(data)
		if err != nil {
			continue
		}
		res, err := tx.Exec("INSERT INTO note_images(note_id, mime, data, created) VALUES(0, ?, ?, ?)", mime, s.seal("notes.image", im.Data), now())
		if err != nil {
			return err
		}
		newID[im.ID], _ = res.LastInsertId()
	}
	body := reImageRef.ReplaceAllStringFunc(n.Body, func(m string) string {
		old, _ := strconv.ParseInt(reImageRef.FindStringSubmatch(m)[1], 10, 64)
		if id, ok := newID[old]; ok {
			return "(cmdex-img:" + strconv.FormatInt(id, 10) + ")"
		}
		return "(cmdex-img:0)" // an image the backup did not hold
	})
	res, err := tx.Exec("INSERT INTO notes(body, path, created, updated) VALUES(?, ?, ?, ?)",
		s.seal("notes.body", body), s.seal("notes.path", n.Path), n.Created, n.Updated)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	return claimImages(tx, id, body)
}
