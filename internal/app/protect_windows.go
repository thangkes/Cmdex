//go:build windows

package app

import (
	"errors"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Screen-capture protection: with WDA_EXCLUDEFROMCAPTURE (Windows 10 2004
// and later) Cmdex's windows show as black in screenshots, screen
// recordings, remote-viewing and screen-sharing tools — whatever reads the
// screen through the normal capture paths. The user still sees them.
const (
	wdaNone               = 0x0
	wdaMonitor            = 0x1 // older Windows: shown black instead of excluded
	wdaExcludeFromCapture = 0x11
)

var (
	user32          = windows.NewLazySystemDLL("user32.dll")
	procSetAffinity = user32.NewProc("SetWindowDisplayAffinity")
	procGetAffinity = user32.NewProc("GetWindowDisplayAffinity")
	procIsVisible   = user32.NewProc("IsWindowVisible")
)

// visible reports whether a window is shown. Hidden helper windows (the
// input method's, GDI+'s) draw nothing a capture could see, and some of
// them, created later by Windows, refuse a display affinity.
func visible(h windows.HWND) bool {
	r, _, _ := procIsVisible.Call(uintptr(h))
	return r != 0
}

// ownWindows lists the top-level windows of this process.
func ownWindows() []windows.HWND {
	pid := windows.GetCurrentProcessId()
	var out []windows.HWND
	cb := syscall.NewCallback(func(h windows.HWND, _ uintptr) uintptr {
		var wpid uint32
		windows.GetWindowThreadProcessId(h, &wpid)
		if wpid == pid {
			out = append(out, h) // hidden ones too: the main window may not be shown yet
		}
		return 1 // continue
	})
	windows.EnumWindows(cb, unsafe.Pointer(nil))
	return out
}

func setCaptureProtection(on bool) error {
	wins := ownWindows()
	if len(wins) == 0 {
		return errors.New("no window yet")
	}
	for _, h := range wins {
		mode := uintptr(wdaNone)
		if on {
			mode = wdaExcludeFromCapture
		}
		if r, _, err := procSetAffinity.Call(uintptr(h), mode); r == 0 {
			if !visible(h) {
				continue // nothing on screen to protect or unprotect
			}
			if !on {
				return err
			}
			// Windows before 10 2004: black instead of excluded is still private
			if r2, _, err2 := procSetAffinity.Call(uintptr(h), wdaMonitor); r2 == 0 {
				return err2
			}
			_ = err
		}
	}
	return nil
}

// captureProtected reports whether every window of Cmdex on the screen is
// protected now (and at least one is shown).
func captureProtected() bool {
	shown := 0
	for _, h := range ownWindows() {
		if !visible(h) {
			continue
		}
		shown++
		var mode uint32
		if r, _, _ := procGetAffinity.Call(uintptr(h), uintptr(unsafe.Pointer(&mode))); r == 0 || mode == wdaNone {
			return false
		}
	}
	return shown > 0
}

const captureSupported = true
