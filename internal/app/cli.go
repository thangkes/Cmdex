package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/term"

	"github.com/thangkes/cmdex/internal/collect"
	"github.com/thangkes/cmdex/internal/store"
	"github.com/thangkes/cmdex/internal/update"
)

// CLI handles command-line use; it reports whether args were for it and
// the exit code.
//
//	cmdex --version                 print the version
//	cmdex --update                  install a newer release, if there is one
//	cmdex --export FILE.json|.db [--password]  save a full backup (sealed with a
//	                                           password typed in the terminal) / a copy of the database
//	cmdex --import FILE [--replace]            load a backup, pack or database (asks the
//	                                           password of a sealed backup)
func CLI(args []string) (handled bool, code int) {
	if len(args) == 0 {
		return false, 0
	}
	switch args[0] {
	case "--version", "-v":
		out := consoleOut()
		fmt.Fprintln(out, "Cmdex", Version)
		return true, 0
	case "--update":
		out := consoleOut()
		return true, updateCLI(out)
	case "--export", "--import":
		out := consoleOut()
		if len(args) < 2 {
			fmt.Fprintln(out, "usage: cmdex --export FILE.json|FILE.db [--password]   or   cmdex --import FILE [--replace]")
			return true, 2
		}
		replace, sealed := false, false
		for _, f := range args[2:] {
			switch f {
			case "--replace":
				replace = true
			case "--password":
				sealed = true
			default:
				fmt.Fprintln(out, "unknown option:", f, "(passwords are asked, never given on the command line)")
				return true, 2
			}
		}
		return true, transferCLI(out, args[0], args[1], replace, sealed)
	}
	return false, 0
}

// transferCLI exports or imports the library without the window:
//
//	cmdex --export backup.json [--password]  full JSON backup, sealed with a password if asked
//	cmdex --export copy.db                   copy of the database file
//	cmdex --import FILE [--replace]          backup, pack or database; merged unless --replace
func transferCLI(out io.Writer, op, file string, replace, sealed bool) int {
	path, err := store.DefaultPath()
	if err == nil {
		var s *store.Store
		if s, err = store.Open(path); err == nil {
			defer s.Close()
			a := &App{store: s}
			if err := a.openKeyCLI(out); err != nil {
				fmt.Fprintln(out, strings.TrimPrefix(op, "--"), "failed:", err)
				return 1
			}
			coll := &collect.Collector{Store: s} // as in the app
			coll.Rescrub()
			if err := s.UseDefaults(a.activePacks(), a.lang()); err != nil {
				fmt.Fprintln(out, "defaults:", err)
			}
			coll.ScrubBackup()
			switch op {
			case "--export":
				if strings.EqualFold(filepath.Ext(file), ".db") {
					if sealed {
						err = fmt.Errorf("a database copy is already encrypted (with this computer's key): --password is for .json backups")
						break
					}
					err = a.writeDatabase(file)
				} else {
					pw := ""
					if sealed {
						if pw, err = cliNewPassword(out); err != nil {
							break
						}
					}
					err = a.writeBackup(file, pw)
				}
				if err == nil {
					fmt.Fprintln(out, "exported to", file)
				}
			case "--import":
				var r store.BackupResult
				pw := ""
				if pv, e := a.InspectImport(file, ""); e == nil && pv.Locked {
					if pw, err = cliPassword(out, "Backup password: "); err != nil {
						break
					}
				}
				if r, err = a.ImportFrom(file, replace, pw); err == nil {
					fmt.Fprintf(out, "imported: %d commands added, %d updated, %d kept; %d explanations, %d rules, %d connections, %d collected groups, %d notes, %d settings\n",
						r.Added, r.Updated, r.Kept, r.Explains, r.Rules, r.Connections, r.Collected, r.Notes, r.Settings)
				}
			}
		}
	}
	if err != nil {
		fmt.Fprintln(out, strings.TrimPrefix(op, "--"), "failed:", err)
		return 1
	}
	return 0
}

// cliPassword reads a password typed in the terminal, without echo.
func cliPassword(out io.Writer, prompt string) (string, error) {
	in, closeIn, err := consoleIn()
	if err != nil {
		return "", errors.New("a password is needed: run this in a terminal")
	}
	defer closeIn()
	fmt.Fprint(out, prompt)
	pw, err := term.ReadPassword(in)
	fmt.Fprintln(out)
	return string(pw), err
}

// cliNewPassword asks a new backup password twice.
func cliNewPassword(out io.Writer) (string, error) {
	pw, err := cliPassword(out, fmt.Sprintf("New backup password (at least %d characters): ", store.MinBackupPassword))
	if err != nil {
		return "", err
	}
	if utf8.RuneCountInString(pw) < store.MinBackupPassword {
		return "", store.ErrShortBackupPassword
	}
	again, err := cliPassword(out, "Again: ")
	if err != nil {
		return "", err
	}
	if again != pw {
		return "", errors.New("the passwords differ")
	}
	return pw, nil
}

func updateCLI(out io.Writer) int {
	if Updates == "off" {
		fmt.Fprintln(out, "this build updates by building again (scripts/build-linux.sh): releases need WebKitGTK 4.1")
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	fmt.Fprintf(out, "Cmdex %s: checking %s\n", Version, update.Repo)
	r, err := update.Check(ctx, http.DefaultClient, Version, update.Asset)
	if err != nil {
		fmt.Fprintln(out, "update check failed:", err)
		return 1
	}
	if r == nil {
		fmt.Fprintln(out, "up to date")
		return 0
	}
	fmt.Fprintf(out, "downloading %s (%s)\n", r.Version, r.AssetName)
	path, err := download(ctx, r)
	if err != nil {
		fmt.Fprintln(out, "download failed:", err)
		return 1
	}
	fmt.Fprintln(out, "checksum verified, installing")
	if err := installFile(path, false); err != nil {
		fmt.Fprintln(out, "install failed:", err)
		return 1
	}
	fmt.Fprintln(out, "updated to", r.Version)
	return 0
}

// cleanDownloads removes update files left by earlier updates (the
// installer still runs from its file when Cmdex exits, so it cannot be
// removed right away).
func cleanDownloads() {
	old, _ := filepath.Glob(filepath.Join(os.TempDir(), "cmdex-update-*"))
	for _, d := range old {
		if fi, err := os.Stat(d); err == nil && time.Since(fi.ModTime()) > time.Hour {
			os.RemoveAll(d)
		}
	}
}

// download fetches a release's file into a new temporary folder; the
// checksum is verified against the release's SHA256SUMS.txt.
func download(ctx context.Context, r *update.Release) (string, error) {
	dir, err := os.MkdirTemp("", "cmdex-update-")
	if err != nil {
		return "", err
	}
	dest := filepath.Join(dir, r.AssetName)
	if err := update.Download(ctx, http.DefaultClient, r, dest); err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	return dest, nil
}
