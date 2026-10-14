//go:build !windows

package app

import "errors"

// X11 and Wayland have no way for a window to keep itself out of
// screenshots; on Linux only the secure keypad protects input.
func setCaptureProtection(bool) error {
	return errors.New("screen-capture protection is available on Windows only")
}

func captureProtected() bool { return false }

const captureSupported = false
