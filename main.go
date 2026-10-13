// Command cmdex is a command library, builder and explainer with built-in
// terminals (local shell, SSH, Telnet, serial) for Windows and Linux.
package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"github.com/thangkes/cmdex/internal/app"
)

//go:embed all:frontend
var frontend embed.FS

//go:embed build/appicon.png
var icon []byte

func main() {
	if handled, code := app.CLI(os.Args[1:]); handled {
		os.Exit(code)
	}
	assets, err := fs.Sub(frontend, "frontend")
	if err != nil {
		log.Fatal(err)
	}
	a := app.New()
	err = wails.Run(&options.App{
		Title:            "Cmdex",
		Width:            1360,
		Height:           860,
		MinWidth:         960,
		MinHeight:        600,
		BackgroundColour: &options.RGBA{R: 14, G: 18, B: 23, A: 255},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        func(ctx context.Context) { app.Startup(a, ctx) },
		OnShutdown:       func(ctx context.Context) { app.Shutdown(a, ctx) },
		Bind:             []any{a},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "cmdex-2c6f1d0e-6b0a-4c55-9f0e-3a4f2a7d9b11",
		},
		// Dropping a file or link on the window must not navigate it: only
		// Cmdex's own page may reach the app's functions.
		DragAndDrop: &options.DragAndDrop{DisableWebViewDrop: true},
		Windows:     &windows.Options{WebviewIsTransparent: false, WindowIsTranslucent: false},
		Linux:       &linux.Options{Icon: icon, ProgramName: "cmdex"},
	})
	if err != nil {
		log.Fatal(err)
	}
}
