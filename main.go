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
	wr "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/thangkes/cmdex/internal/app"
	"github.com/thangkes/cmdex/internal/viewer"
)

//go:embed all:frontend
var frontend embed.FS

//go:embed build/appicon.png
var icon []byte

func main() {
	if w, ok := windowKinds[argOne()]; ok {
		runWindow(w)
		return
	}
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
		OnDomReady:       func(ctx context.Context) { app.DomReady(a, ctx) },
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

// window is one of Cmdex's other windows (package viewer).
type window struct {
	page, title   string
	width, height int
	minW, minH    int
	protect       bool // follows Cmdex's screen-capture protection
}

var windowKinds = map[string]window{
	"--viewer": {page: "viewer.html", title: "Cmdex · log", width: 1000, height: 720, minW: 520, minH: 360},
	"--notes":  {page: "notes.html", title: "Cmdex · notes", width: 480, height: 760, minW: 360, minH: 420, protect: true},
}

// runWindow runs a window Cmdex starts: the same page files with the
// window's own start page, nothing bound but the bridge to the Cmdex that
// started it (which answers what the page may ask).
func runWindow(w window) {
	assets, err := fs.Sub(frontend, "frontend")
	if err != nil {
		log.Fatal(err)
	}
	var ctx context.Context
	ready := make(chan struct{})
	h := viewer.Hooks{
		Emit: func(name string, data ...any) { wr.EventsEmit(ctx, name, data...) },
		Quit: func() { <-ready; wr.Quit(ctx) },
		Focus: func() {
			<-ready
			wr.WindowUnminimise(ctx)
			wr.WindowShow(ctx)
			wr.WindowSetAlwaysOnTop(ctx, true) // to the front, then back to normal
			wr.WindowSetAlwaysOnTop(ctx, false)
		},
	}
	if w.protect {
		h.Protect = app.ProtectWindows
	}
	b := viewer.NewBridge(os.Stdout, h)
	err = wails.Run(&options.App{
		Title:            w.title,
		Width:            w.width,
		Height:           w.height,
		MinWidth:         w.minW,
		MinHeight:        w.minH,
		BackgroundColour: &options.RGBA{R: 14, G: 18, B: 23, A: 255},
		AssetServer:      &assetserver.Options{Assets: startPage{assets, w.page}},
		OnStartup: func(c context.Context) {
			ctx = c
			close(ready)
			go b.Serve(os.Stdin)
		},
		Bind:        []any{b},
		DragAndDrop: &options.DragAndDrop{DisableWebViewDrop: true},
		Windows:     &windows.Options{WebviewIsTransparent: false, WindowIsTranslucent: false},
		Linux:       &linux.Options{Icon: icon, ProgramName: "cmdex"},
	})
	if err != nil {
		log.Fatal(err)
	}
}

// startPage serves page as index.html.
type startPage struct {
	fs.FS
	page string
}

func (p startPage) Open(name string) (fs.File, error) {
	if name == "index.html" {
		name = p.page
	}
	return p.FS.Open(name)
}

func argOne() string {
	if len(os.Args) == 2 {
		return os.Args[1]
	}
	return ""
}
