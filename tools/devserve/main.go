// Command devserve runs the Cmdex page in an ordinary browser for interface
// work: it serves frontend/, calls the real App methods over HTTP, and
// streams events (terminal output…) with server-sent events. Localhost only.
//
//	CMDEX_DB=./tmp/cmdex.db go run ./tools/devserve   # then open http://127.0.0.1:34115
package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"reflect"
	"strings"
	"sync"

	"github.com/thangkes/cmdex/internal/app"
)

// notesShim stands in for the notes window's bridge (internal/viewer): its
// calls reach the same App methods.
const notesShim = `<script>
window.go.viewer = { Bridge: {
  Call: (name, args) => fetch("/bridge/" + name, { method: "POST", headers: { "Content-Type": "application/json", "X-Cmdex-Token": CMDEX_TOKEN }, body: args })
    .then(async r => { const j = await r.json(); if (!r.ok) throw new Error(j.error); return JSON.stringify(j.result); }),
  Ready: () => window.go.app.App.Setting("lang", "en").then(lang => setTimeout(() => (handlers.view || []).forEach(f => f({ t: "hello", lang, theme: "graphite", os: navigator.platform.startsWith("Win") ? "windows" : "linux" })), 0)),
} };
</script>`

// The bridge runs commands on this computer, so only the page served here may
// use it: every call carries a random token that only this page knows (a
// web page elsewhere cannot read it), the Host header must be this address
// (no DNS rebinding), and bodies must be JSON (no simple cross-site posts).
const shim = `<script>
// Stands in for the Wails bridge: same calls, over HTTP.
const CMDEX_TOKEN = "{{TOKEN}}";
window.go = { app: { App: new Proxy({}, { get: (_, name) => (...args) =>
  fetch("/bridge/" + name, { method: "POST", headers: { "Content-Type": "application/json", "X-Cmdex-Token": CMDEX_TOKEN }, body: JSON.stringify(args) })
    .then(async r => { const j = await r.json(); if (!r.ok) throw new Error(j.error); return j.result; }) }) } };
const handlers = {};
const es = new EventSource("/events?t=" + CMDEX_TOKEN);
es.onmessage = m => { const e = JSON.parse(m.data); (handlers[e.name] || []).forEach(f => f(...e.data)); };
let clip = "";
window.runtime = {
  EventsOn: (name, f) => { (handlers[name] = handlers[name] || []).push(f); },
  ClipboardSetText: t => { clip = t; return navigator.clipboard ? navigator.clipboard.writeText(t).catch(() => {}) : Promise.resolve(); },
  ClipboardGetText: () => Promise.resolve(clip),
  BrowserOpenURL: u => window.open(u, "_blank", "noopener"),
};
</script>`

type hub struct {
	mu   sync.Mutex
	subs map[chan []byte]bool
}

func (h *hub) send(name string, data ...any) {
	b, _ := json.Marshal(map[string]any{"name": name, "data": data})
	h.mu.Lock()
	for c := range h.subs {
		select {
		case c <- b:
		default: // a slow page drops events rather than blocking sessions
		}
	}
	h.mu.Unlock()
}

func main() {
	addr := flag.String("addr", "127.0.0.1:34115", "listen address (keep it on localhost)")
	dir := flag.String("frontend", "frontend", "frontend folder")
	flag.Parse()

	if host, _, err := net.SplitHostPort(*addr); err != nil || !net.ParseIP(host).IsLoopback() {
		log.Fatal("devserve runs commands on this computer: listen on a loopback address only (127.0.0.1:port)")
	}
	tb := make([]byte, 24)
	rand.Read(tb)
	token := hex.EncodeToString(tb)
	allowed := func(w http.ResponseWriter, r *http.Request, given string) bool {
		if r.Host != *addr || subtle.ConstantTimeCompare([]byte(given), []byte(token)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return false
		}
		return true
	}

	a := app.New()
	h := &hub{subs: map[chan []byte]bool{}}
	app.SetEmitter(a, h.send)
	app.Startup(a, nil)
	defer app.Shutdown(a, nil)

	av := reflect.ValueOf(a)
	mux := http.NewServeMux()
	mux.HandleFunc("/bridge/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			http.Error(w, "POST application/json only", http.StatusMethodNotAllowed)
			return
		}
		if !allowed(w, r, r.Header.Get("X-Cmdex-Token")) {
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/bridge/")
		m := av.MethodByName(name)
		if !m.IsValid() {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "no method " + name})
			return
		}
		var raw []json.RawMessage
		if err := json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(&raw); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		t := m.Type()
		if len(raw) != t.NumIn() {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": fmt.Sprintf("%s takes %d arguments", name, t.NumIn())})
			return
		}
		args := make([]reflect.Value, len(raw))
		for i := range raw {
			p := reflect.New(t.In(i))
			if err := json.Unmarshal(raw[i], p.Interface()); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
				return
			}
			args[i] = p.Elem()
		}
		out := m.Call(args)
		var result any
		errType := reflect.TypeOf((*error)(nil)).Elem()
		for i, o := range out {
			if t.Out(i) == errType {
				if !o.IsNil() {
					writeJSON(w, http.StatusInternalServerError, map[string]any{"error": o.Interface().(error).Error()})
					return
				}
				continue
			}
			result = o.Interface()
		}
		writeJSON(w, http.StatusOK, map[string]any{"result": result})
	})
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		if !allowed(w, r, r.URL.Query().Get("t")) {
			return
		}
		fl, ok := w.(http.Flusher)
		if !ok {
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		c := make(chan []byte, 4096)
		h.mu.Lock()
		h.subs[c] = true
		h.mu.Unlock()
		defer func() { h.mu.Lock(); delete(h.subs, c); h.mu.Unlock() }()
		fl.Flush()
		for {
			select {
			case b := <-c:
				fmt.Fprintf(w, "data: %s\n\n", b)
				fl.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})
	files := http.FileServer(http.Dir(*dir))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Host != *addr { // a rebound host name must not get the page and its token
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
		name, before := "index.html", `<script src="vendor/xterm.js"></script>`
		switch r.URL.Path {
		case "/", "/index.html":
		case "/notes.html": // the notes window
			name, before = "notes.html", `<script src="themes.js"></script>`
		default:
			files.ServeHTTP(w, r)
			return
		}
		b, err := http.Dir(*dir).Open(name)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer b.Close()
		page, _ := io.ReadAll(b)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		js := strings.Replace(shim, "{{TOKEN}}", token, 1)
		if name == "notes.html" {
			js += notesShim
		}
		w.Write([]byte(strings.Replace(string(page), before, js+"\n"+before, 1)))
	})
	log.Printf("Cmdex page on http://%s", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
