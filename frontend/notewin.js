// The notes window (notes.html): notes.js as in the main window, its calls
// passed to the Cmdex that opened this window (internal/viewer), which
// answers only the notes' own calls. No database is opened here.

// api.X(...) goes to Cmdex over the window's bridge, as JSON.
function bridgeApi() {
  window.go.app = { App: new Proxy({}, {
    get: (_, name) => async (...args) => {
      const r = await window.go.viewer.Bridge.Call(name, JSON.stringify(args));
      return r === "" ? null : JSON.parse(r);
    },
  }) };
}

async function helloNotes(m) {
  const lang = m.lang === "vi" ? "vi" : "en";
  App.info = { os: m.os || "" };
  App.theme = m.theme || "graphite";
  applyTheme(App.theme);
  if (lang !== App.lang || !Object.keys(App.dict).length) await loadLang(lang);
  document.title = t("notes.windowTitle");
  if (window.runtime.WindowSetTitle) window.runtime.WindowSetTitle(t("notes.windowTitle"));
  if (!document.getElementById("notesPanel")) initNotes();
  else relabelNotes();
}

window.addEventListener("DOMContentLoaded", () => {
  bridgeApi();
  applyTheme("graphite");
  window.runtime.EventsOn("view", m => {
    if (m.t === "hello") helloNotes(m);
    if (m.t === "pin") Notes.pin = m.path || "";
  });
  window.go.viewer.Bridge.Ready();
});
