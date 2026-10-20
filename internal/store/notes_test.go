package store

import (
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// a tiny PNG (header and a witness string after it: DetectContentType reads
// the header only)
func pngWith(text string) []byte {
	return append([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), []byte(text)...)
}

func keyedStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cmdex.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.SetUpKey(KeyKeychain, "test", nil, NewDatabaseKey()); err != nil {
		t.Fatal(err)
	}
	return s, path
}

// Notes, their paths and their images are encrypted on disk.
func TestNotesEncrypted(t *testing.T) {
	s, path := keyedStore(t)
	img, err := s.AddNoteImage(pngWith("WITNESS-IMAGE-BYTES"))
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf("**Mật khẩu wifi** quán: witness-note-text 日本語 ✨\n![photo](cmdex-img:%d)", img)
	n, err := s.AddNote(body, `D:\Work\witness-folder`)
	if err != nil {
		t.Fatal(err)
	}
	s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	for _, w := range []string{"witness-note-text", "Mật khẩu", "witness-folder", "WITNESS-IMAGE-BYTES", base64.StdEncoding.EncodeToString([]byte("WITNESS-IMAGE-BYTES"))[:12]} {
		if fileHas(t, path, w) || fileHas(t, path+"-wal", w) {
			t.Errorf("%q readable in the database file", w)
		}
	}
	got, err := s.Notes("")
	if err != nil || len(got) != 1 || got[0].Body != body || got[0].Path != `D:\Work\witness-folder` || got[0].ID != n.ID {
		t.Fatalf("read back: %v %+v", err, got)
	}
	im, err := s.NoteImage(img)
	if err != nil || im.Mime != "image/png" {
		t.Fatalf("image: %v %+v", err, im)
	}
	if raw, _ := base64.StdEncoding.DecodeString(im.Data); !strings.Contains(string(raw), "WITNESS-IMAGE-BYTES") {
		t.Error("image data changed")
	}
}

func TestNotesEditSearchDelete(t *testing.T) {
	s, _ := keyedStore(t)
	a, _ := s.NoteImage(0) // none: an error, nothing else
	_ = a
	i1, _ := s.AddNoteImage(pngWith("one"))
	i2, _ := s.AddNoteImage(pngWith("two"))
	n, err := s.AddNote(fmt.Sprintf("deploy notes ![a](cmdex-img:%d) ![b](cmdex-img:%d)", i1, i2), "")
	if err != nil {
		t.Fatal(err)
	}
	s.AddNote("shopping list", "")
	if got, _ := s.Notes("DEPLOY"); len(got) != 1 || got[0].ID != n.ID {
		t.Errorf("search: %+v", got)
	}
	if got, _ := s.Notes("deploy shopping"); len(got) != 0 {
		t.Error("every word must match")
	}
	// the edit drops image 2 from the text: it is deleted
	if err := s.EditNote(n.ID, fmt.Sprintf("deploy notes ![a](cmdex-img:%d)", i1)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.NoteImage(i2); err == nil {
		t.Error("an image the note no longer shows was kept")
	}
	if _, err := s.NoteImage(i1); err != nil {
		t.Error("the image the note shows was deleted")
	}
	// another note cannot take an image that is not loose
	other, _ := s.AddNote(fmt.Sprintf("steal ![x](cmdex-img:%d)", i1), "")
	if err := s.DeleteNote(other.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.NoteImage(i1); err != nil {
		t.Error("deleting another note took the image along")
	}
	if err := s.DeleteNote(n.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.NoteImage(i1); err == nil {
		t.Error("the note's image outlived it")
	}
	for _, bad := range []string{"", "   \n", strings.Repeat("x", maxNoteBody+1)} {
		if _, err := s.AddNote(bad, ""); err == nil {
			t.Errorf("accepted a note of %d characters", len(bad))
		}
	}
	if err := s.EditNote(99999, "x"); err == nil {
		t.Error("edited a missing note")
	}
}

func TestNoteImageTypes(t *testing.T) {
	s, _ := keyedStore(t)
	for _, ok := range [][]byte{pngWith("x"), append([]byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00"), 1, 2, 3), []byte("GIF89a......"), []byte("RIFF\x00\x00\x00\x00WEBPVP8 ")} {
		if _, err := s.AddNoteImage(ok); err != nil {
			t.Errorf("refused %q: %v", ok[:4], err)
		}
	}
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	for _, bad := range [][]byte{svg, []byte("<html><img src=x onerror=alert(1)>"), []byte("MZ\x90\x00 an exe"), {}, make([]byte, maxNoteImage+1)} {
		if _, err := s.AddNoteImage(bad); err == nil {
			t.Errorf("accepted %q", string(bad[:min(len(bad), 12)]))
		}
	}
	if !errors.Is(func() error { _, err := s.AddNoteImage(svg); return err }(), ErrNotAnImage) {
		t.Error("an SVG should be refused as not an image")
	}
}

// Notes and their images travel in backups; image IDs are renumbered and
// the text follows. A merge does not duplicate notes.
func TestNotesBackup(t *testing.T) {
	a, _ := keyedStore(t)
	img, _ := a.AddNoteImage(pngWith("pic"))
	a.AddNote(fmt.Sprintf("with image ![p](cmdex-img:%d)", img), `C:\x`)
	a.AddNote("plain", "")
	a.SetSetting("map.root", "sealed-folder")
	b, err := a.ExportBackup("test")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Notes) != 2 || len(b.Notes[0].Images) != 1 || b.Notes[0].ID != 0 {
		t.Fatalf("export: %+v", b.Notes)
	}
	if _, ok := b.Settings["map.root"]; ok {
		t.Error("the map folder left in a backup")
	}

	c, _ := keyedStore(t)
	c.AddNoteImage(pngWith("taken id")) // IDs differ on the other computer
	r, err := c.ImportBackup(b, false)
	if err != nil || r.Notes != 2 {
		t.Fatalf("import: %v %+v", err, r)
	}
	got, _ := c.Notes("")
	ref := imageRefs(got[0].Body)
	if len(ref) != 1 {
		t.Fatalf("body %q", got[0].Body)
	}
	im, err := c.NoteImage(ref[0])
	if raw, _ := base64.StdEncoding.DecodeString(im.Data); err != nil || !strings.Contains(string(raw), "pic") {
		t.Errorf("image after import: %v", err)
	}
	if r, _ := c.ImportBackup(b, false); r.Notes != 0 {
		t.Errorf("merging again added %d notes", r.Notes)
	}
	if r, _ := c.ImportBackup(b, true); r.Notes != 2 {
		t.Errorf("replace: %d notes", r.Notes)
	}
	if got, _ := c.Notes(""); len(got) != 2 {
		t.Errorf("after replace: %d notes", len(got))
	}
}

// Masking rules are sealed in the database (a rule may be the secret
// itself), travel in the clear inside a backup like notes, and are sealed
// again with the other computer's key on import.
func TestMaskRulesBackup(t *testing.T) {
	a, _ := keyedStore(t)
	rules := `[{"kind":"text","value":"Acme Corp"}]`
	a.SetSealedSetting("mask.rules", rules)
	if raw := a.Setting("mask.rules", ""); strings.Contains(raw, "Acme") {
		t.Fatalf("stored in the clear: %q", raw)
	}
	b, err := a.ExportBackup("test")
	if err != nil {
		t.Fatal(err)
	}
	if b.Settings["mask.rules"] != rules {
		t.Fatalf("backup has %q", b.Settings["mask.rules"])
	}
	c, _ := keyedStore(t)
	if _, err := c.ImportBackup(b, false); err != nil {
		t.Fatal(err)
	}
	if raw := c.Setting("mask.rules", ""); strings.Contains(raw, "Acme") {
		t.Errorf("imported in the clear: %q", raw)
	}
	if got := c.SealedSetting("mask.rules"); got != rules {
		t.Errorf("imported %q", got)
	}
}
