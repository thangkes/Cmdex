package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	wr "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/thangkes/cmdex/internal/fsmap"
	"github.com/thangkes/cmdex/internal/store"
)

// The folder map (Work ▸ Map): a folder on this computer and three layers
// below it, and file actions the user starts there. Changes are limited to
// the folder last scanned (fsmap.Inside). Paths are never collected; only
// the folder last opened is kept, encrypted, to open the map there again.

// MapScan reads a folder for the map ("" and "~" are the home folder) and makes it
// the folder file actions are limited to.
func (a *App) MapScan(path string) (*fsmap.Map, error) {
	if path == "" || path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	m, err := fsmap.Scan(ctx, path)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.mapRoot = m.Root
	a.mu.Unlock()
	a.store.SetSealedSetting("map.root", m.Root)
	return m, nil
}

// MapLastFolder is the folder the map showed last, if it still exists.
func (a *App) MapLastFolder() string {
	p := a.store.SealedSetting("map.root")
	if p == "" || !filepath.IsAbs(p) {
		return ""
	}
	if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
		return ""
	}
	return p
}

// MapPickFolder asks for a folder to show.
func (a *App) MapPickFolder() (string, error) {
	if a.ctx == nil {
		return "", errNoDialogs
	}
	return wr.OpenDirectoryDialog(a.ctx, wr.OpenDialogOptions{Title: "Cmdex"})
}

func (a *App) root() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.mapRoot
}

// MapOpen opens a file with its default program (run: the user confirmed
// that it runs a program).
func (a *App) MapOpen(path string, run bool) error { return fsmap.Open(a.root(), path, run) }

// MapReveal shows a file in the system file manager.
func (a *App) MapReveal(path string) error { return fsmap.Reveal(a.root(), path) }

// MapRename renames a file or folder and returns its new path.
func (a *App) MapRename(path, name string) (string, error) { return fsmap.Rename(a.root(), path, name) }

// MapMkdir makes a folder and returns its path.
func (a *App) MapMkdir(parent, name string) (string, error) {
	return fsmap.Mkdir(a.root(), parent, name)
}

// MapTrash moves a file or folder to the trash.
func (a *App) MapTrash(path string) error { return fsmap.Trash(a.root(), path) }

// TrashResult is the outcome for one file of MapTrashMany.
type TrashResult struct {
	Path  string `json:"path"`
	Error string `json:"error,omitempty"`
}

// MapTrashMany moves several files and folders to the trash, one by one;
// one that fails does not stop the others. A path inside another one of
// the list goes with it.
func (a *App) MapTrashMany(paths []string) []TrashResult {
	root := a.root()
	clean := make([]string, 0, len(paths))
	for _, p := range paths {
		if filepath.IsAbs(p) {
			clean = append(clean, filepath.Clean(p))
		}
	}
	sort.Strings(clean)
	out := []TrashResult{}
	var done []string
	for _, p := range clean {
		inside := false
		for _, d := range done {
			inside = inside || fsmap.Inside(d, p)
		}
		if inside {
			out = append(out, TrashResult{Path: p})
			continue
		}
		r := TrashResult{Path: p}
		if err := fsmap.Trash(root, p); err != nil {
			r.Error = err.Error()
		} else {
			done = append(done, p)
		}
		out = append(out, r)
	}
	return out
}

// MapRunnable reports whether opening the file runs a program.
func (a *App) MapRunnable(path string) bool {
	if !filepath.IsAbs(path) || !fsmap.Inside(a.root(), filepath.Clean(path)) {
		return false
	}
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir() && fsmap.Runnable(path, fi)
}

// MapSuggestion is a library command made ready for a file on the map.
type MapSuggestion struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Line string `json:"line"`
	Risk int    `json:"risk"`
}

// suggestByName and suggestByExt name library templates (starter and tool
// packs) that fit a file; the file goes into the first parameter. A
// command without parameters runs in the file's folder.
var (
	suggestByName = map[string][]string{
		"requirements.txt":   {"pip install --requirement {requirements}"},
		"pom.xml":            {"mvn package", "mvn clean package", "mvn dependency:tree"},
		"build.gradle":       {"gradle build", "gradle test", "gradle tasks"},
		"build.gradle.kts":   {"gradle build", "gradle test", "gradle tasks"},
		"docker-compose.yml": {"docker compose ps"},
	}
	suggestByExt = map[string][]string{
		".py":    {"python {file}", "python -m pdb {file}", "python -i {file}"},
		".jar":   {"java -jar {filename}", "jar tf {file}", "jar -xvf {file}"},
		".java":  {"javac {file}"},
		".class": {"javap {file1}"},
		".json":  {"python -m json.tool {file}"},
		".whl":   {"pip install {choice}"},
		".gz":    {"tar -xzvf {archive} -C {dest}"},
	}
	suggestAny = []string{"Get-FileHash {path} -Algorithm SHA256", "sha256sum {file}"}
)

// MapSuggest lists library commands for a file, with its path filled in
// and quoted for the local shell. Only commands in the library show: a tool
// pack that is off brings none.
func (a *App) MapSuggest(path string) ([]MapSuggestion, error) {
	out := []MapSuggestion{}
	if !fsmap.Inside(a.root(), filepath.Clean(path)) || !filepath.IsAbs(path) {
		return out, nil
	}
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return out, nil
	}
	name := strings.ToLower(filepath.Base(path))
	want := append(append(append([]string{}, suggestByName[name]...), suggestByExt[filepath.Ext(name)]...), suggestAny...)
	cmds, err := a.store.Commands()
	if err != nil {
		return nil, err
	}
	here := store.Linux
	if runtime.GOOS == "windows" {
		here = store.Windows
	}
	for _, tpl := range want {
		for _, c := range cmds {
			if c.Template != tpl || !store.Shown(c.Platform, []string{here}) {
				continue
			}
			vals := map[string]string{}
			for _, p := range c.Params {
				vals[p.Key] = p.Default
			}
			line := c.Template
			if keys := store.PlaceholderKeys(c.Template); len(keys) > 0 {
				vals[keys[0]] = quoteLocal(path)
				if len(keys) > 1 && vals[keys[1]] == "" && keys[1] == "dest" {
					vals[keys[1]] = quoteLocal(filepath.Dir(path))
				}
				line = store.Fill(c.Template, vals)
			} else {
				line = "cd " + quoteLocal(filepath.Dir(path)) + "; " + c.Template
			}
			out = append(out, MapSuggestion{ID: c.ID, Name: c.Name, Line: line, Risk: store.RiskOf(c.Danger, line, c.Platform)})
			break
		}
	}
	return out, nil
}

// MapCdLine is the command that changes the local shell to dir.
func (a *App) MapCdLine(dir string) string { return "cd " + quoteLocal(dir) }

// quoteLocal quotes a path for the local shell: single quotes, which
// PowerShell and POSIX shells both take literally.
func quoteLocal(p string) string {
	if runtime.GOOS == "windows" {
		return "'" + strings.ReplaceAll(p, "'", "''") + "'"
	}
	return "'" + strings.ReplaceAll(p, "'", `'\''`) + "'"
}
