package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	wr "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/thangkes/cmdex/internal/store"
)

// ImportPreview describes a file before it is imported, so the user can
// choose how (Kind: "backup", "db" or "pack").
type ImportPreview struct {
	Path        string `json:"path"`
	Kind        string `json:"kind"`
	Commands    int    `json:"commands"`
	Explains    int    `json:"explains"`
	Connections int    `json:"connections"`
	Collected   int    `json:"collected"`
	Exported    string `json:"exported"`
	App         string `json:"app"`
}

// loaded is a file read for import: a full backup, or a pack.
type loaded struct {
	kind   string
	backup store.Backup
	pack   store.Pack
}

var sqliteMagic = []byte("SQLite format 3\x00")

func loadImport(path string) (loaded, error) {
	f, err := os.Open(path)
	if err != nil {
		return loaded{}, err
	}
	head := make([]byte, len(sqliteMagic))
	n, _ := f.Read(head)
	f.Close()
	if n == len(head) && bytes.Equal(head, sqliteMagic) {
		tmp, err := os.MkdirTemp("", "cmdex-import-")
		if err != nil {
			return loaded{}, err
		}
		defer os.RemoveAll(tmp)
		b, err := store.BackupFromDatabase(path, tmp)
		return loaded{kind: "db", backup: b}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return loaded{}, err
	}
	if store.IsBackup(data) {
		b, err := store.ParseBackup(data)
		return loaded{kind: "backup", backup: b}, err
	}
	p, err := store.ParsePack(data)
	if err != nil {
		return loaded{}, fmt.Errorf("not a Cmdex backup, pack or database: %w", err)
	}
	return loaded{kind: "pack", pack: p}, nil
}

func (l loaded) preview(path string) ImportPreview {
	pv := ImportPreview{Path: path, Kind: l.kind}
	if l.kind == "pack" {
		pv.Commands, pv.Explains = len(l.pack.Commands), len(l.pack.Explain)
		return pv
	}
	b := l.backup
	pv.Commands, pv.Explains, pv.Connections, pv.Collected = len(b.Commands), len(b.Explain), len(b.Connections), len(b.Collected)
	pv.Exported, pv.App = b.Exported, b.App
	return pv
}

// PickImport asks for a file (backup, pack or database) and describes it.
// An empty Path means the user cancelled.
func (a *App) PickImport() (ImportPreview, error) {
	if a.ctx == nil {
		return ImportPreview{}, errNoDialogs
	}
	path, err := wr.OpenFileDialog(a.ctx, wr.OpenDialogOptions{
		Title: "Cmdex",
		Filters: []wr.FileFilter{
			{DisplayName: "Cmdex backup, pack or database (*.json, *.db)", Pattern: "*.json;*.db"},
			{DisplayName: "*.*", Pattern: "*.*"},
		},
	})
	if err != nil || path == "" {
		return ImportPreview{}, err
	}
	return a.InspectImport(path)
}

// InspectImport describes a file without importing it.
func (a *App) InspectImport(path string) (ImportPreview, error) {
	l, err := loadImport(path)
	if err != nil {
		return ImportPreview{}, err
	}
	return l.preview(path), nil
}

// ImportFrom imports a file. replace empties the library first (backups and
// databases only); otherwise the file is merged in.
func (a *App) ImportFrom(path string, replace bool) (store.BackupResult, error) {
	l, err := loadImport(path)
	if err != nil {
		return store.BackupResult{}, err
	}
	if l.kind == "pack" {
		l.pack.Name = "" // imported content becomes the user's own
		r, err := a.store.ImportPack(l.pack, a.lang())
		return store.BackupResult{Added: r.Added, Kept: r.Skipped, Explains: r.Explains, Rejected: r.Rejected}, err
	}
	return a.store.ImportBackup(l.backup, replace)
}

func stamp() string { return time.Now().Format("2006-01-02") }

// ExportBackupFile saves the whole library as a JSON backup.
func (a *App) ExportBackupFile() (string, error) {
	if a.ctx == nil {
		return "", errNoDialogs
	}
	path, err := wr.SaveFileDialog(a.ctx, wr.SaveDialogOptions{
		Title: "Cmdex", DefaultFilename: "cmdex-backup-" + stamp() + ".json",
		Filters: []wr.FileFilter{{DisplayName: "Cmdex backup (*.json)", Pattern: "*.json"}},
	})
	if err != nil || path == "" {
		return "", err
	}
	return path, a.writeBackup(path)
}

func (a *App) writeBackup(path string) error {
	b, err := a.store.ExportBackup(Version)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// ExportDatabaseFile saves a copy of the database file itself.
func (a *App) ExportDatabaseFile() (string, error) {
	if a.ctx == nil {
		return "", errNoDialogs
	}
	path, err := wr.SaveFileDialog(a.ctx, wr.SaveDialogOptions{
		Title: "Cmdex", DefaultFilename: "cmdex-" + stamp() + ".db",
		Filters: []wr.FileFilter{{DisplayName: "Cmdex database (*.db)", Pattern: "*.db"}},
	})
	if err != nil || path == "" {
		return "", err
	}
	return path, a.writeDatabase(path)
}

func (a *App) writeDatabase(path string) error {
	cur, _ := store.DefaultPath()
	if abs, _ := filepath.Abs(path); strings.EqualFold(abs, cur) {
		return errors.New("choose another file than the database in use")
	}
	os.Remove(path) // the save dialog already asked before overwriting
	return a.store.ExportDatabase(path)
}

// ExportPackFile saves the commands, explanations, synonyms and rules as a
// pack to share (no connections, collected lines or settings).
func (a *App) ExportPackFile() (string, error) { return a.ExportFile() }
