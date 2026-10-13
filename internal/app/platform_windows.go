//go:build windows

package app

import (
	"strings"

	"golang.org/x/sys/windows"

	wr "github.com/wailsapp/wails/v2/pkg/runtime"
)

// install runs the downloaded setup. ShellExecute (not CreateProcess) so
// Windows shows the UAC prompt the installer needs; /SILENT keeps a progress
// window but asks nothing. The installer closes Cmdex, upgrades it in place
// and starts it again.
func (a *App) install(setup string) error {
	verb, _ := windows.UTF16PtrFromString("open")
	file, _ := windows.UTF16PtrFromString(setup)
	args, _ := windows.UTF16PtrFromString("/SILENT /SUPPRESSMSGBOXES /NORESTART")
	if err := windows.ShellExecute(0, verb, file, args, nil, windows.SW_SHOWNORMAL); err != nil {
		return err // e.g. the UAC prompt was declined
	}
	wr.Quit(a.ctx)
	return nil
}

// systemLang is "vi" when Windows' display language is Vietnamese.
func systemLang() string {
	langs, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME)
	if err == nil && len(langs) > 0 && strings.HasPrefix(strings.ToLower(langs[0]), "vi") {
		return "vi"
	}
	return "en"
}
