//go:build windows

package app

import (
	"io"
	"os"
	"strings"

	"golang.org/x/sys/windows"

	wr "github.com/wailsapp/wails/v2/pkg/runtime"
)

// installFile runs the downloaded setup. ShellExecute (not CreateProcess)
// so Windows can show a prompt if the installer ever needs one; /SILENT
// keeps a progress window but asks nothing. The installer upgrades in place
// and starts Cmdex again.
func installFile(setup string, _ bool) error {
	verb, _ := windows.UTF16PtrFromString("open")
	file, _ := windows.UTF16PtrFromString(setup)
	args, _ := windows.UTF16PtrFromString("/SILENT /SUPPRESSMSGBOXES /NORESTART")
	return windows.ShellExecute(0, verb, file, args, nil, windows.SW_SHOWNORMAL)
}

// restart leaves room for the installer, which starts the new version.
func (a *App) restart() {
	if a.ctx != nil {
		wr.Quit(a.ctx)
	}
}

// consoleOut writes to the console Cmdex was started from (it is a GUI
// program, so it has none of its own).
func consoleOut() io.Writer {
	const attachParent = ^uint32(0) // ATTACH_PARENT_PROCESS
	k32 := windows.NewLazySystemDLL("kernel32.dll")
	if r, _, _ := k32.NewProc("AttachConsole").Call(uintptr(attachParent)); r == 0 {
		return io.Discard
	}
	f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
	if err != nil {
		return io.Discard
	}
	return f
}

// systemLang is "vi" when Windows' display language is Vietnamese.
func systemLang() string {
	langs, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME)
	if err == nil && len(langs) > 0 && strings.HasPrefix(strings.ToLower(langs[0]), "vi") {
		return "vi"
	}
	return "en"
}
