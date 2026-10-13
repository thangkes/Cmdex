// Command devserve runs the Cmdex page in an ordinary browser for interface
// work: it serves frontend/, calls the real App methods over HTTP, and
// streams events (terminal output…) with server-sent events. Localhost only.
//
//	CMDEX_DB=./tmp/cmdex.db go run ./tools/devserve   # then open http://127.0.0.1:34115
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"reflect"
	"strings"
	"sync"

	"github.com/thangkes/cmdex/internal/app"
)

const shim = `<script>
// Stands in for the Wails bridge: same calls, over HTTP.
window.go = { app: { App: new Proxy({}, { get: (_, name) => (...args) =>
  fetch("/bridge/" + name, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(args) })
    .then(async r => { const j = await r.json(); if (!r.ok) throw new Error(j.error); return j.result; }) }) } };
const handlers = {};
const es = new EventSource("/events");
es.onmessage = m => { const e = JSON.parse(m.data); (handlers[e.name] || []).forEach(f => f(...e.data)); };
let clip = "";
window.runtime = {
  EventsOn: (name, f) => { (handlers[name] = handlers[name] || []).push(f); },
  ClipboardSetText: t => { clip = t; return navigator.clipboard ? navigator.clipboard.writeText(t).catch(() => {}) : Promise.resolve(); },
  ClipboardGetText: () => Promise.resolve(clip),
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

	a := app.New()
	h := &hub{subs: map[chan []byte]bool{}}
	app.SetEmitter(a, h.send)
	app.Startup(a, nil)
	defer app.Shutdown(a, nil)

	av := reflect.ValueOf(a)
	mux := http.NewServeMux()
	mux.HandleFunc("/bridge/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
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
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path != "/" && r.URL.Path != "/index.html" {
			files.ServeHTTP(w, r)
			return
		}
		b, err := http.Dir(*dir).Open("index.html")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer b.Close()
		page, _ := io.ReadAll(b)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(strings.Replace(string(page), "<script src=\"vendor/xterm.js\"></script>", shim+"\n<script src=\"vendor/xterm.js\"></script>", 1)))
	})
	log.Printf("Cmdex page on http://%s", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
