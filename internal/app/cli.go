package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/thangkes/cmdex/internal/update"
)

// CLI handles command-line use; it reports whether args were for it and
// the exit code.
//
//	cmdex --version   print the version
//	cmdex --update    install a newer release, if there is one
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
	}
	return false, 0
}

func updateCLI(out io.Writer) int {
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
