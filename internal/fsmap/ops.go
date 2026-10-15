package fsmap

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"
)

var (
	ErrOutside   = errors.New("the file is not in the folder on the map")
	ErrProtected = errors.New("Cmdex does not change this folder: it belongs to the system or is the folder on the map")
	ErrExists    = errors.New("a file or folder with that name already exists")
	ErrNoTrash   = errors.New("this drive has no trash: delete it in the file manager if you are sure")
	ErrRunnable  = errors.New("opening this file runs a program: confirm first")
)

// Inside reports whether path is root or below it (case-insensitively on
// Windows). Both must be clean absolute paths.
func Inside(root, path string) bool {
	if runtime.GOOS == "windows" {
		root, path = strings.ToLower(root), strings.ToLower(path)
	}
	if path == root {
		return true
	}
	if !strings.HasSuffix(root, string(filepath.Separator)) {
		root += string(filepath.Separator)
	}
	return strings.HasPrefix(path, root)
}

// target checks a path given by the page for a change: absolute, inside
// the scanned folder, not that folder, and not a protected one.
func target(root, path string) (string, error) {
	p, err := clean(path)
	if err != nil {
		return "", err
	}
	if root == "" || !Inside(root, p) {
		return "", ErrOutside
	}
	if strings.EqualFold(p, root) || protected(p) {
		return "", ErrProtected
	}
	return p, nil
}

var reserved = map[string]bool{"con": true, "prn": true, "aux": true, "nul": true}

// ValidName checks a new file or folder name: one plain name that works on
// both Windows and Linux.
func ValidName(name string) error {
	if strings.TrimSpace(name) == "" || name == "." || name == ".." || len(name) > 255 {
		return errors.New("enter a name")
	}
	for _, r := range name {
		if r < 32 || strings.ContainsRune(`\/:*?"<>|`, r) || unicode.Is(unicode.Cf, r) {
			return fmt.Errorf("a name cannot contain %q", r)
		}
	}
	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") || strings.HasPrefix(name, " ") {
		return errors.New("a name cannot begin with a space or end with a dot or space")
	}
	base := strings.ToLower(strings.SplitN(name, ".", 2)[0])
	if reserved[base] || len(base) == 4 && (strings.HasPrefix(base, "com") || strings.HasPrefix(base, "lpt")) && base[3] >= '0' && base[3] <= '9' {
		return errors.New("Windows reserves that name")
	}
	return nil
}

// Rename gives path a new name in the same folder and returns the new path.
func Rename(root, path, name string) (string, error) {
	p, err := target(root, path)
	if err != nil {
		return "", err
	}
	if err := ValidName(name); err != nil {
		return "", err
	}
	to := filepath.Join(filepath.Dir(p), name)
	if _, err := os.Lstat(to); err == nil && !(runtime.GOOS == "windows" && strings.EqualFold(to, p)) {
		return "", ErrExists // (only the case of the letters changes on Windows: allowed)
	}
	if err := os.Rename(p, to); err != nil {
		return "", err
	}
	return to, nil
}

// Mkdir makes a folder in parent (root itself or a folder inside it).
func Mkdir(root, parent, name string) (string, error) {
	p, err := clean(parent)
	if err != nil {
		return "", err
	}
	if root == "" || !Inside(root, p) {
		return "", ErrOutside
	}
	if protected(p) && !strings.EqualFold(p, root) {
		return "", ErrProtected
	}
	if err := ValidName(name); err != nil {
		return "", err
	}
	to := filepath.Join(p, name)
	if err := os.Mkdir(to, 0o755); err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", ErrExists
		}
		return "", err
	}
	return to, nil
}

// Trash moves path to the system trash (Windows Recycle Bin, or the
// freedesktop trash in the home folder). Where there is none it refuses:
// it never deletes for good.
func Trash(root, path string) error {
	p, err := target(root, path)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(p); err != nil {
		return err
	}
	return trash(p)
}

// Open opens path with its default program. A file that would run as a
// program (Runnable) opens only with run set, after the user confirmed.
func Open(root, path string, run bool) error {
	p, err := clean(path)
	if err != nil {
		return err
	}
	if root == "" || !Inside(root, p) {
		return ErrOutside
	}
	fi, err := os.Stat(p)
	if err != nil {
		return err
	}
	if !fi.IsDir() && Runnable(p, fi) && !run {
		return ErrRunnable
	}
	return open(p)
}

// Reveal shows path in the system file manager.
func Reveal(root, path string) error {
	p, err := clean(path)
	if err != nil {
		return err
	}
	if root == "" || !Inside(root, p) {
		return ErrOutside
	}
	if _, err := os.Lstat(p); err != nil {
		return err
	}
	return reveal(p)
}

// runnableExt are endings Windows runs (or hands to a script host) when a
// file is opened; on Linux these and executable files count.
var runnableExt = map[string]bool{
	".exe": true, ".com": true, ".bat": true, ".cmd": true, ".ps1": true, ".psm1": true, ".vbs": true, ".vbe": true,
	".js": true, ".jse": true, ".wsf": true, ".wsh": true, ".msi": true, ".msp": true, ".scr": true, ".hta": true,
	".cpl": true, ".msc": true, ".lnk": true, ".url": true, ".reg": true, ".inf": true, ".pif": true, ".jar": true,
	".application": true, ".appref-ms": true, ".sh": true, ".desktop": true, ".appimage": true, ".run": true,
}

// Runnable reports whether opening the file runs a program.
func Runnable(path string, fi os.FileInfo) bool {
	if runnableExt[strings.ToLower(filepath.Ext(path))] {
		return true
	}
	return runtime.GOOS != "windows" && fi != nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0
}
