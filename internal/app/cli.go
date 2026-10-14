package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/thangkes/cmdex/internal/collect"
	"github.com/thangkes/cmdex/internal/store"
	"github.com/thangkes/cmdex/internal/update"
	"github.com/thangkes/cmdex/packs"
)

// CLI handles command-line use; it reports whether args were for it and
// the exit code.
//
//	cmdex --version                 print the version
//	cmdex --update                  install a newer release, if there is one
//	cmdex --export FILE.json|.db    save a full backup / a copy of the database
//	cmdex --import FILE [--replace] load a backup, pack or database
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
			fmt.Fprintln(out, "usage: cmdex --export FILE.json|FILE.db   or   cmdex --import FILE [--replace]")
			return true, 2
		}
		return true, transferCLI(out, args[0], args[1], len(args) > 2 && args[2] == "--replace")
	}
	return false, 0
}

// transferCLI exports or imports the library without the window:
//
//	cmdex --export backup.json      full JSON backup
//	cmdex --export copy.db          copy of the database file
//	cmdex --import FILE [--replace] backup, pack or database; merged unless --replace
func transferCLI(out io.Writer, op, file string, replace bool) int {
	path, err := store.DefaultPath()
	if err == nil {
		var s *store.Store
		if s, err = store.Open(path); err == nil {
			defer s.Close()
			a := &App{store: s}
			coll := &collect.Collector{Store: s} // as in the app
			coll.Rescrub()
			if err := s.UseDefaults(packs.All(), a.lang()); err != nil {
				fmt.Fprintln(out, "defaults:", err)
			}
			coll.ScrubBackup()
			switch op {
			case "--export":
				if strings.EqualFold(filepath.Ext(file), ".db") {
					err = a.writeDatabase(file)
				} else {
					err = a.writeBackup(file)
				}
				if err == nil {
					fmt.Fprintln(out, "exported to", file)
				}
			case "--import":
				var r store.BackupResult
				if r, err = a.ImportFrom(file, replace); err == nil {
					fmt.Fprintf(out, "imported: %d commands added, %d updated, %d kept; %d explanations, %d rules, %d connections, %d collected groups, %d settings\n",
						r.Added, r.Updated, r.Kept, r.Explains, r.Rules, r.Connections, r.Collected, r.Settings)
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
