package app

import (
	"context"
	"os"
	"runtime"
	"sync"

	wr "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/thangkes/cmdex/internal/collect"
	"github.com/thangkes/cmdex/internal/store"
	"github.com/thangkes/cmdex/internal/update"
	"github.com/thangkes/cmdex/packs"
)

// Version is set at build time
// (-X github.com/thangkes/cmdex/internal/app.Version=1.2.3); "dev" builds
// never update themselves.
var Version = "dev"

// App holds everything the interface calls. Its exported methods are bound
// to the page (window.go.main.App.*); everything else is a package function
// so it is not exposed.
type App struct {
	ctx     context.Context
	emitter func(name string, data ...any) // set by tools that run the page without Wails
	store   *store.Store
	coll    *collect.Collector
	dbErr   string

	mu       sync.Mutex
	sessions map[string]*session
	nextID   int
	pending  map[int64]*pendingHost // unknown SSH host keys waiting for the user
	release  *update.Release
	updating bool
}

// New makes the application.
func New() *App {
	return &App{sessions: map[string]*session{}, pending: map[int64]*pendingHost{}}
}

// Startup opens the database and starts the background work. ctx is the
// Wails context (nil when the page runs without Wails).
func Startup(a *App, ctx context.Context) { a.startup(ctx) }

// Shutdown closes sessions and the database.
func Shutdown(a *App, ctx context.Context) { a.shutdown(ctx) }

// SetEmitter sends events somewhere else than Wails (development server).
func SetEmitter(a *App, f func(name string, data ...any)) { a.emitter = f }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	path, err := store.DefaultPath()
	if err == nil {
		a.store, err = store.Open(path)
	}
	if err != nil {
		a.dbErr = err.Error()
		return
	}
	a.coll = &collect.Collector{Store: a.store}
	// secrets missed by an older Cmdex are masked once, before anything
	// (the backup copy made on upgrade) is copied
	a.coll.Rescrub()
	if err := a.store.UseDefaults(packs.All(), a.lang()); err != nil {
		a.dbErr = err.Error() // the user's own library still works
	}
	a.coll.ScrubBackup() // a copy made by Cmdex 0.1.3 before that
	go a.collectLoop()
	go a.updateLoop()
	go cleanDownloads()
}

func (a *App) shutdown(context.Context) {
	a.mu.Lock()
	for _, s := range a.sessions {
		s.conn.Close()
	}
	a.mu.Unlock()
	if a.store != nil {
		a.store.Close()
	}
}

func (a *App) lang() string { return a.store.Setting("lang", systemLang()) }

// Info is what the page needs at start.
type Info struct {
	Version  string   `json:"version"`
	OS       string   `json:"os"`
	Lang     string   `json:"lang"`
	Theme    string   `json:"theme"`
	DBPath   string   `json:"dbPath"`
	Defaults string   `json:"defaults"` // the defaults database
	Filter   []string `json:"filter"`   // systems shown (empty: all)
	DBError  string   `json:"dbError"`
	Shell    string   `json:"shell"`
	Hostname string   `json:"hostname"`
}

// Init returns the start-up information.
func (a *App) Init() Info {
	host, _ := os.Hostname()
	info := Info{Version: Version, OS: runtime.GOOS, DBError: a.dbErr, Hostname: host}
	info.DBPath, _ = store.DefaultPath()
	if a.store == nil {
		info.Lang, info.Theme = systemLang(), "graphite"
		return info
	}
	info.Lang = a.lang()
	info.Defaults = a.store.DefaultsPath()
	info.Filter = a.PlatformFilter()
	info.Theme = a.store.Setting("theme", "graphite")
	info.Shell = shellLabel()
	return info
}

// SetSetting stores an interface preference (lang, theme, collect.*).
// Changing the language rebuilds the defaults in that language (commands
// the user changed stay as they are).
func (a *App) SetSetting(key, value string) error {
	if err := a.store.SetSetting(key, value); err != nil {
		return err
	}
	if key == "lang" {
		return a.store.UseDefaults(packs.All(), value)
	}
	return nil
}

// Setting reads a preference.
func (a *App) Setting(key, def string) string { return a.store.Setting(key, def) }

func (a *App) emit(name string, data ...any) {
	if a.emitter != nil {
		a.emitter(name, data...)
		return
	}
	if a.ctx != nil {
		wr.EventsEmit(a.ctx, name, data...)
	}
}
