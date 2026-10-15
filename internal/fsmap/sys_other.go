//go:build !windows

package fsmap

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// protected: everything outside the user's own places. Cmdex changes files
// in the home folder, /tmp and mounted drives only, never the home folder
// itself.
func protected(p string) bool {
	home, _ := os.UserHomeDir()
	if home != "" && p == filepath.Clean(home) {
		return true
	}
	for _, base := range []string{home, "/tmp", "/media", "/mnt", "/run/media"} {
		if base != "" && p != base && Inside(filepath.Clean(base), p) {
			return false
		}
	}
	return true
}

// trashDir is the home trash of the freedesktop.org specification.
func trashDir() (string, error) {
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" || !filepath.IsAbs(data) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		data = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(data, "Trash"), nil
}

func device(p string) (uint64, bool) {
	var st syscall.Stat_t
	if err := syscall.Lstat(p, &st); err != nil {
		return 0, false
	}
	return uint64(st.Dev), true
}

// trash moves p to the home trash, as file managers do: an info file
// records where it came from so "Restore" works. Files on another drive
// than the home trash are refused (their own trash folders are not used).
func trash(p string) error {
	dir, err := trashDir()
	if err != nil {
		return err
	}
	files, info := filepath.Join(dir, "files"), filepath.Join(dir, "info")
	for _, d := range []string{files, info} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	a, ok1 := device(p)
	b, ok2 := device(files)
	if !ok1 || !ok2 || a != b {
		return ErrNoTrash
	}
	base := filepath.Base(p)
	for i := 1; i < 1000; i++ {
		name := base
		if i > 1 {
			name = fmt.Sprintf("%s.%d", base, i)
		}
		// the info file reserves the name (created only if it does not exist)
		f, err := os.OpenFile(filepath.Join(info, name+".trashinfo"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(f, "[Trash Info]\nPath=%s\nDeletionDate=%s\n", (&url.URL{Path: p}).EscapedPath(), time.Now().Format("2006-01-02T15:04:05"))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err == nil {
			err = os.Rename(p, filepath.Join(files, name))
		}
		if err != nil {
			os.Remove(filepath.Join(info, name+".trashinfo"))
		}
		return err
	}
	return errors.New("the trash is full of files with this name")
}

func start(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

// open hands p to the desktop's default program (p is absolute, so it is
// never read as an option).
func open(p string) error { return start("xdg-open", p) }

// reveal opens the folder that holds p (or p, a folder) in the file manager.
func reveal(p string) error {
	if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
		p = filepath.Dir(p)
	}
	return start("xdg-open", p)
}
