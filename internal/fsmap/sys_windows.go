package fsmap

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// protected: the system's folders (and everything in them), a drive's own
// root, and the folders Windows keeps at a drive's top.
func protected(p string) bool {
	lp := strings.ToLower(p)
	vol := filepath.VolumeName(p)
	if vol == "" || len(lp) <= len(vol)+1 { // C:\ or \\server\share\
		return true
	}
	for _, env := range []string{"SystemRoot", "ProgramFiles", "ProgramFiles(x86)", "ProgramW6432", "ProgramData"} {
		if d := os.Getenv(env); d != "" && Inside(filepath.Clean(d), p) {
			return true
		}
	}
	top := strings.ToLower(strings.TrimPrefix(lp[len(vol):], `\`))
	if i := strings.IndexByte(top, '\\'); i >= 0 {
		top = top[:i]
	}
	switch top {
	case "$recycle.bin", "system volume information", "recovery", "boot", "$windows.~bt", "$windows.~ws":
		return true
	}
	return false
}

var (
	shell32          = windows.NewLazySystemDLL("shell32.dll")
	procSHFileOpertn = shell32.NewProc("SHFileOperationW")
)

// shFileOpStruct is SHFILEOPSTRUCTW (64-bit layout, natural alignment).
type shFileOpStruct struct {
	hwnd                  uintptr
	wFunc                 uint32
	pFrom                 *uint16
	pTo                   *uint16
	fFlags                uint16
	fAnyOperationsAborted int32
	hNameMappings         uintptr
	lpszProgressTitle     *uint16
}

const (
	foDelete          = 3
	fofSilent         = 0x0004
	fofNoConfirmation = 0x0010
	fofAllowUndo      = 0x0040
	fofNoErrorUI      = 0x0400
	driveFixed        = 3
)

// trash moves p to the Recycle Bin. Only fixed drives have one: elsewhere
// (USB sticks, network shares) the shell would delete for good, so Cmdex
// refuses.
func trash(p string) error {
	vol := filepath.VolumeName(p)
	if vol == "" || strings.HasPrefix(vol, `\\`) {
		return ErrNoTrash
	}
	rootp, _ := windows.UTF16PtrFromString(vol + `\`)
	if windows.GetDriveType(rootp) != driveFixed {
		return ErrNoTrash
	}
	from, err := windows.UTF16FromString(p)
	if err != nil {
		return err
	}
	from = append(from, 0) // a list of names, ended by an empty one
	op := shFileOpStruct{wFunc: foDelete, pFrom: &from[0], fFlags: fofAllowUndo | fofNoConfirmation | fofSilent | fofNoErrorUI}
	r, _, _ := procSHFileOpertn.Call(uintptr(unsafe.Pointer(&op)))
	if r != 0 {
		return syscall.Errno(r)
	}
	if op.fAnyOperationsAborted != 0 {
		return errors.New("moving to the Recycle Bin was cancelled")
	}
	if _, err := os.Lstat(p); err == nil {
		return errors.New("the file is still there: it may be open in another program")
	}
	return nil
}

// open hands p to the shell, as a double click in Explorer does.
func open(p string) error {
	verb, _ := windows.UTF16PtrFromString("open")
	file, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return err
	}
	dir, _ := windows.UTF16PtrFromString(filepath.Dir(p))
	return windows.ShellExecute(0, verb, file, nil, dir, windows.SW_SHOWNORMAL)
}

// reveal opens Explorer with p selected.
func reveal(p string) error {
	if strings.ContainsRune(p, '"') {
		return errors.New("unexpected quote in the path")
	}
	exe := filepath.Join(os.Getenv("SystemRoot"), "explorer.exe")
	cmd := exec.Command(exe)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer.exe /select,"` + p + `"`}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}
