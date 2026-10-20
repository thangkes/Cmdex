package app

import (
	"context"
	"time"

	"github.com/thangkes/cmdex/internal/viewer"
)

// ProtectionState is what the page shows about screen-capture protection.
type ProtectionState struct {
	Supported bool `json:"supported"` // Windows 10 2004+ (not Linux)
	Wanted    bool `json:"wanted"`    // the user's setting
	Active    bool `json:"active"`    // the windows are protected right now
}

// Protection reports the screen-capture protection state.
func (a *App) Protection() ProtectionState {
	return ProtectionState{
		Supported: captureSupported,
		Wanted:    a.store != nil && a.store.Setting("protect.capture", "1") == "1",
		Active:    captureProtected(),
	}
}

// SetProtection turns screen-capture protection on or off and remembers it.
func (a *App) SetProtection(on bool) (ProtectionState, error) {
	v := "0"
	if on {
		v = "1"
	}
	if err := a.store.SetSetting("protect.capture", v); err != nil {
		return a.Protection(), err
	}
	err := setCaptureProtection(on)
	a.notesSend(viewer.Msg{T: "protect", Protect: on})
	return a.Protection(), err
}

// HoldProtection protects the windows for a moment whatever the setting
// (the secure keypad is open); Release puts the setting back.
func (a *App) HoldProtection() error { return setCaptureProtection(true) }

// ReleaseProtection returns to the user's setting.
func (a *App) ReleaseProtection() error {
	return setCaptureProtection(a.store.Setting("protect.capture", "1") == "1")
}

// DomReady applies the protection setting once the window exists. The
// window may be created a moment later than the page, so it retries until
// Windows confirms every window is protected.
func DomReady(a *App, _ context.Context) {
	if !captureSupported || a.store == nil || a.store.Setting("protect.capture", "1") != "1" {
		return
	}
	go func() {
		for i := 0; i < 40; i++ {
			if setCaptureProtection(true) == nil && captureProtected() {
				return
			}
			time.Sleep(250 * time.Millisecond)
		}
	}()
}

// ProtectWindows applies screen-capture protection to the windows of this
// process: Cmdex's notes window, which Cmdex tells whether to protect. Like
// DomReady, it retries while the window is being created.
func ProtectWindows(on bool) {
	if !captureSupported {
		return
	}
	go func() {
		for i := 0; i < 40; i++ {
			if setCaptureProtection(on) == nil && (!on || captureProtected()) {
				return
			}
			time.Sleep(250 * time.Millisecond)
		}
	}()
}
