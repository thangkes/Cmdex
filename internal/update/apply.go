package update

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// AssetFor reports whether a release file is the one this build installs:
// the Windows setup, or the Linux archive for this CPU.
func AssetFor(goos, goarch string) func(name string) bool {
	return func(name string) bool {
		switch goos {
		case "windows":
			return strings.HasPrefix(name, "Cmdex-Windows-Setup-") && strings.HasSuffix(name, ".exe")
		case "linux":
			return strings.HasPrefix(name, "Cmdex-Linux-"+goarch+"-") && strings.HasSuffix(name, ".tar.gz")
		}
		return false
	}
}

// Asset is AssetFor this build.
var Asset = AssetFor(runtime.GOOS, runtime.GOARCH)

// ReplaceBinary installs the "cmdex" program from a downloaded Linux archive
// over the running executable (atomically, by rename) and returns its path.
// It fails cleanly, changing nothing, when the folder is not writable (a
// system-wide install): the user then updates the way they installed.
func ReplaceBinary(archive string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return "", err
	}
	return exe, ReplaceBinaryAt(archive, exe)
}

// ReplaceBinaryAt installs the "cmdex" program from archive at exe.
func ReplaceBinaryAt(archive, exe string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	tmp := exe + ".new"
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return errors.New("the archive has no cmdex program")
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg || filepath.Base(h.Name) != "cmdex" {
			continue
		}
		out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return fmt.Errorf("cannot write next to %s (installed system-wide?): %w", exe, err)
		}
		_, err = io.Copy(out, io.LimitReader(tr, 512<<20))
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err == nil {
			err = os.Rename(tmp, exe)
		}
		if err != nil {
			os.Remove(tmp)
			return err
		}
		return nil
	}
}
