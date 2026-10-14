// Workspace: connections and terminals on the left; find, build and explain
// commands on the right. ▶ Run pastes the command into the active session
// and presses Enter, without asking.

const Work = {
  sessions: new Map(), // id -> {info, term, fit, pane, closed}
  order: [], active: null,
  early: new Map(),    // output that arrived before a session was registered
  profiles: [], editing: null,
  query: "", forced: null, results: [], sel: null, vals: {}, explainRes: null,
};

// ---- terminals --------------------------------------------------------------

function newTerm() {
  const pane = h("div", { class: "termpane" });
  document.getElementById("termhost").appendChild(pane);
  const term = new Terminal({
    fontFamily: '"JetBrains Mono", "Cascadia Mono", Consolas, "DejaVu Sans Mono", monospace',
    fontSize: 13, cursorBlink: true, scrollback: 10000, theme: App.termTheme, allowProposedApi: false,
    // Links printed by a server (OSC 8) never open inside Cmdex: a remote
    // page there could reach the app's functions. Only http(s), only in the
    // system browser, only after asking.
    linkHandler: { activate: (e, uri) => openExternal(uri), allowNonHttpProtocols: false },
  });
  const fit = new FitAddon.FitAddon();
  term.loadAddon(fit);
  term.open(pane);
  try { fit.fit(); } catch (e) { /* hidden pane */ }
  // Ctrl+Shift+C / Ctrl+Shift+V copy and paste; plain Ctrl+C stays an interrupt.
  term.attachCustomKeyEventHandler(e => {
    if (e.type === "keydown" && e.ctrlKey && !e.shiftKey && !e.altKey && e.code === "KeyK") { focusSearch(); return false; }
    if (e.type !== "keydown" || !e.ctrlKey || !e.shiftKey) return true;
    if (e.code === "KeyC") { const s = term.getSelection(); if (s) window.runtime.ClipboardSetText(s); return false; }
    if (e.code === "KeyV") { window.runtime.ClipboardGetText().then(txt => txt && term.paste(txt)); return false; }
    return true;
  });
  return { pane, term, fit };
}

async function openExternal(uri) {
  let u;
  try { u = new URL(uri); } catch (e) { return; }
  if (u.protocol !== "http:" && u.protocol !== "https:") return;
  const ok = await confirmDialog(t("link.title"), t("link.text", { url: u.href }), t("link.open"));
  if (ok) window.runtime.BrowserOpenURL(u.href);
}

function registerSession(info, t0) {
  const s = Object.assign(t0, { info, closed: false });
  // Some shells take seconds to show their prompt (Windows PowerShell 5):
  // say so until the first visible text arrives.
  s.starting = h("div", { class: "termstarting" }, t("term.starting", { name: info.name }));
  s.pane.appendChild(s.starting);
  Work.sessions.set(info.id, s);
  Work.order.push(info.id);
  // Keystrokes must reach the session in order. Calls into Go may run
  // concurrently, so input is queued and sent one call at a time; whatever
  // arrives meanwhile (fast typing, a paste) goes out together next.
  s.pending = "";
  s.sending = false;
  const flush = async () => {
    if (s.sending || !s.pending) return;
    s.sending = true;
    while (s.pending && !s.closed) {
      const chunk = s.pending;
      s.pending = "";
      try { await api.SessionWrite(info.id, textToB64(chunk)); } catch (e) { s.pending = ""; break; }
    }
    s.sending = false;
  };
  s.send = d => { if (s.closed) return; s.pending += d; flush(); };
  s.term.onData(d => s.send(d));
  s.term.onResize(({ cols, rows }) => { if (!s.closed) api.SessionResize(info.id, cols, rows).catch(() => {}); });
  for (const chunk of Work.early.get(info.id) || []) s.term.write(chunk);
  Work.early.delete(info.id);
  activate(info.id);
}

function onSessionData(id, b64) {
  const s = Work.sessions.get(id);
  const bytes = b64ToBytes(b64);
  if (s) s.term.write(bytes, () => {
    if (!s.starting) return;
    const buf = s.term.buffer.active;
    for (let i = 0; i < s.term.rows; i++) {
      const l = buf.getLine(buf.viewportY + i);
      if (l && l.translateToString(true).trim()) { s.starting.remove(); s.starting = null; return; }
    }
  });
  else Work.early.set(id, (Work.early.get(id) || []).concat([bytes]));
}

function onSessionClosed(id, msg) {
  const s = Work.sessions.get(id);
  if (!s) return;
  s.closed = true;
  s.term.write("\r\n\x1b[2m" + t("term.ended") + (msg ? ": " + msg : "") + "\x1b[0m\r\n");
  renderTabs();
}

function activate(id) {
  Work.active = id;
  for (const [sid, s] of Work.sessions) s.pane.hidden = sid !== id;
  document.getElementById("termEmpty").hidden = Work.sessions.size > 0;
  const s = Work.sessions.get(id);
  if (s) requestAnimationFrame(() => { try { s.fit.fit(); } catch (e) {} s.term.focus(); });
  renderTabs();
  refreshRight(); // results and explanations follow the session's platform (a Cisco switch…)
  renderBuilderButtons();
}

function closeSession(id) {
  const s = Work.sessions.get(id);
  if (!s) return;
  if (!s.closed) api.SessionClose(id).catch(() => {});
  s.term.dispose();
  s.pane.remove();
  Work.sessions.delete(id);
  Work.order = Work.order.filter(x => x !== id);
  activate(Work.order[Work.order.length - 1] || null);
}

function sizeFor() {
  const t0 = newTerm();
  return { t0, cols: t0.term.cols, rows: t0.term.rows };
}

async function openLocal() {
  const { t0, cols, rows } = sizeFor();
  try {
    const info = await api.OpenLocal(cols, rows);
    registerSession(info, t0);
    return info.id;
  } catch (e) {
    t0.term.dispose(); t0.pane.remove();
    setStatus(t("term.openFail", { err: errText(e) }), true);
    return null;
  }
}

async function openProfile(p, password, remember) {
  const { t0, cols, rows } = sizeFor();
  let res;
  try {
    res = await api.OpenProfile(p.id, cols, rows, password || "", !!remember);
  } catch (e) {
    t0.term.dispose(); t0.pane.remove();
    setStatus(t("term.openFail", { err: errText(e) }), true);
    return;
  }
  if (res.session) { registerSession(res.session, t0); return; }
  t0.term.dispose(); t0.pane.remove();
  if (res.need === "password") {
    const v = await secureInput(t("conn.passTitle", { name: p.name }), p.auth === "key" ? t("conn.passphrase") : t("conn.password"));
    if (v === null) return;
    const remember = await confirmDialog(t("conn.rememberTitle"), t("conn.rememberQ"), t("conn.remember"));
    openProfile(p, v, remember);
  } else if (res.need === "trust") {
    const ok = await dialog(t("conn.trustTitle"), [
      h("p", { class: "soft" }, t("conn.trustText", { host: p.host })),
      h("div", { class: "fp" }, res.fingerprint),
      h("p", { class: "note" }, t("conn.trustHint")),
    ], [{ label: t("common.cancel"), value: false }, { label: t("conn.trust"), pri: true, value: true }]);
    if (ok) {
      try { await api.TrustHost(p.id, res.fingerprint); openProfile(p, password, remember); }
      catch (e) { setStatus(errText(e), true); }
    }
  }
}

function renderTabs() {
  const tabs = clear(document.getElementById("tabs"));
  for (const id of Work.order) {
    const s = Work.sessions.get(id);
    const sel = id === Work.active;
    tabs.appendChild(h("div", { class: "tab", role: "tab", "aria-selected": String(sel) },
      h("button", { type: "button", onclick: () => activate(id) }, h("span", { class: "dot" + (s.closed ? " off" : "") }), s.info.name, h("span", { class: "kind " + s.info.kind }, s.info.kind.toUpperCase())),
      h("button", { type: "button", "aria-label": t("term.close"), title: t("term.close"), onclick: () => closeSession(id) }, "×")));
  }
  tabs.appendChild(h("div", { class: "tab" }, h("button", { type: "button", class: "add", title: t("term.newLocal"), "aria-label": t("term.newLocal"), onclick: openLocal }, "+")));
  const cur = Work.sessions.get(Work.active);
  if (cur && !cur.closed) {
    tabs.appendChild(h("span", { class: "tabspacer" }));
    tabs.appendChild(h("button", { type: "button", class: "btn small secretsend", title: t("term.secretHint"), onclick: sendSecret }, t("term.secret")));
  }
}

// sendSecret types a password into the session from the secure keypad (for
// sudo, su, Password: prompts...). It is not collected and not kept.
async function sendSecret() {
  const s = Work.sessions.get(Work.active);
  if (!s || s.closed) return;
  const v = await secureInput(t("term.secretTitle", { s: s.info.name }), t("term.secretHint"));
  if (v === null) return;
  s.send(v + "\r");
  s.term.focus();
}

function activePlatform() {
  const s = Work.sessions.get(Work.active);
  return s && !s.closed ? s.info.platform : (App.info ? App.info.os : "");
}

function activeName() {
  const s = Work.sessions.get(Work.active);
  return s && !s.closed ? s.info.name : t("term.thisPC");
}

// run pastes line into the active session (opening a local shell first if
// none is open) and presses Enter.
async function runLine(line, commandID) {
  line = (line || "").trim();
  if (!line) return;
  let s = Work.sessions.get(Work.active);
  if (!s || s.closed) {
    const id = await openLocal();
    if (!id) return;
    await new Promise(r => setTimeout(r, 1200)); // let the shell show its prompt
    s = Work.sessions.get(id);
  }
  while (s.sending) await new Promise(r => setTimeout(r, 10)); // typed input first
  try {
    await api.Run(s.info.id, line, commandID || 0);
    setStatus(t("run.done", { s: s.info.name }));
    s.term.focus();
  } catch (e) { setStatus(errText(e), true); }
}

// ---- connection profiles -------------------------------------------------------

async function loadProfiles() {
  try { Work.profiles = await api.Connections(); } catch (e) { Work.profiles = []; }
  const box = clear(document.getElementById("profiles"));
  if (!Work.profiles.length) box.appendChild(h("span", { class: "muted" }, t("conn.none")));
  for (const p of Work.profiles) {
    box.appendChild(h("span", { class: "chip" },
      h("button", { type: "button", title: describeProfile(p), onclick: () => openProfile(p) }, h("span", { class: "kind " + p.kind }, p.kind.toUpperCase()), p.name),
      h("button", { type: "button", class: "edit", title: t("conn.edit"), "aria-label": t("conn.edit") + " " + p.name, onclick: () => showConnForm(p) }, "✎")));
  }
}

function describeProfile(p) {
  if (p.kind === "serial") return `${p.serial.port} · ${p.serial.baud || 9600} ${p.serial.dataBits || 8}${(p.serial.parity || "none")[0].toUpperCase()}${p.serial.stopBits || 1}`;
  return `${p.user ? p.user + "@" : ""}${p.host}:${p.port || (p.kind === "ssh" ? 22 : 23)}`;
}

async function showConnForm(p) {
  const form = clear(document.getElementById("connForm"));
  if (form.hidden === false && !p && !Work.editing) { form.hidden = true; return; }
  Work.editing = p || null;
  const c = p ? JSON.parse(JSON.stringify(p)) : { id: 0, kind: "ssh", platform: "", name: "", host: "", port: 22, user: "", auth: "key", keyPath: "~/.ssh/id_ed25519", serial: { port: "", baud: 9600, dataBits: 8, parity: "none", stopBits: "1", lineEnd: "cr" } };
  form.hidden = false;
  const ports = (await api.SerialPorts().catch(() => [])) || [];
  const draw = () => {
    clear(form);
    const kindSeg = h("div", { class: "seg", role: "group", "aria-label": t("conn.kind") },
      ["ssh", "telnet", "serial"].map(k => h("button", { type: "button", "aria-pressed": String(c.kind === k), onclick: () => { c.kind = k; if (k === "telnet" && (!c.port || c.port === 22)) c.port = 23; if (k === "ssh" && (!c.port || c.port === 23)) c.port = 22; draw(); } }, t("conn.kind." + k))));
    form.appendChild(kindSeg);
    // the kind of device: suggestions and explanations follow it
    const device = h("select", {}, ["", "linux", "windows", "cisco", "pfsense"].map(d => h("option", { value: d, selected: (c.platform || "") === d }, d ? osLabel(d) : t("conn.deviceAuto"))));
    device.onchange = () => { c.platform = device.value; };
    form.appendChild(h("label", { class: "field" }, t("conn.device"), device));
    const name = h("input", { type: "text", value: c.name, placeholder: t("conn.namePh") });
    name.oninput = () => { c.name = name.value; };
    // the secret is entered on the secure keypad, never typed
    let secret = "";
    const pass = h("button", { type: "button", class: "btn secretbtn" }, p ? t("conn.passKeep") : t("kp.enter"));
    pass.onclick = async () => {
      const v = await secureInput(t("kp.title"), c.auth === "key" ? t("conn.passphrase") : t("conn.password"));
      if (v !== null) { secret = v; pass.textContent = v ? "•".repeat(Math.min(v.length, 12)) : t("kp.enter"); }
    };
    if (c.kind === "serial") {
      const port = h("select", {}, (ports.length ? ports : [c.serial.port || "COM1"]).map(x => h("option", { value: x, selected: x === c.serial.port }, x)));
      if (!c.serial.port) c.serial.port = port.value;
      port.onchange = () => { c.serial.port = port.value; };
      const sel = (key, opts, labelFor) => {
        const s = h("select", {}, opts.map(o => h("option", { value: o, selected: String(c.serial[key]) === String(o) }, labelFor ? labelFor(o) : o)));
        s.onchange = () => { c.serial[key] = typeof c.serial[key] === "number" ? Number(s.value) : s.value; };
        return s;
      };
      form.appendChild(h("div", { class: "grid3" },
        h("label", { class: "field" }, t("conn.serialPort"), port),
        h("label", { class: "field" }, t("conn.baud"), sel("baud", [1200, 2400, 4800, 9600, 19200, 38400, 57600, 115200, 230400])),
        h("label", { class: "field" }, t("conn.name"), name),
        h("label", { class: "field" }, t("conn.dataBits"), sel("dataBits", [8, 7, 6, 5])),
        h("label", { class: "field" }, t("conn.parity"), sel("parity", ["none", "even", "odd"], o => t("conn.parity." + o))),
        h("label", { class: "field" }, t("conn.stopBits"), sel("stopBits", ["1", "2"])),
        h("label", { class: "field" }, t("conn.lineEnd"), sel("lineEnd", ["cr", "lf", "crlf"], o => o.toUpperCase()))));
      form.appendChild(h("p", { class: "note" }, ports.length ? t("conn.serialFound", { n: ports.length }) : t("conn.serialNone")));
    } else {
      const host = h("input", { type: "text", value: c.host, placeholder: "10.0.0.10" });
      host.oninput = () => { c.host = host.value.trim(); };
      const port = h("input", { type: "number", value: c.port || "", min: 1, max: 65535 });
      port.oninput = () => { c.port = Number(port.value) || 0; };
      const user = h("input", { type: "text", value: c.user });
      user.oninput = () => { c.user = user.value.trim(); };
      form.appendChild(h("div", { class: "grid3" },
        h("label", { class: "field span2" }, t("conn.host"), host),
        h("label", { class: "field" }, t("conn.port"), port),
        h("label", { class: "field" }, t("conn.name"), name),
        c.kind === "ssh" ? h("label", { class: "field" }, t("conn.user"), user) : null));
      if (c.kind === "ssh") {
        const auth = h("select", {}, ["key", "password", "agent"].map(a => h("option", { value: a, selected: c.auth === a }, t("conn.auth." + a))));
        auth.onchange = () => { c.auth = auth.value; draw(); };
        const key = h("input", { type: "text", value: c.keyPath });
        key.oninput = () => { c.keyPath = key.value.trim(); };
        form.appendChild(h("div", { class: "grid3" },
          h("label", { class: "field" }, t("conn.auth"), auth),
          c.auth === "key" ? h("label", { class: "field" }, t("conn.keyPath"), key) : null,
          c.auth !== "agent" ? h("label", { class: "field" }, c.auth === "key" ? t("conn.passphrase") : t("conn.password"), pass) : null));
        form.appendChild(h("p", { class: "note" }, t("conn.keychain")));
      } else {
        form.appendChild(h("p", { class: "warn" }, t("conn.telnetWarn")));
      }
    }
    form.appendChild(h("div", { class: "row wrap" },
      h("button", { type: "submit", class: "btn pri" }, t("conn.saveOpen")),
      h("button", { type: "button", class: "btn", onclick: () => save(false) }, t("conn.save")),
      h("button", { type: "button", class: "btn ghost", onclick: () => { form.hidden = true; Work.editing = null; } }, t("common.cancel")),
      p ? h("button", { type: "button", class: "btn danger", onclick: del }, t("conn.delete")) : null));
    form.onsubmit = e => { e.preventDefault(); save(true); };

    async function save(andOpen) {
      if (c.kind !== "serial" && !c.host) { setStatus(t("conn.needHost"), true); return; }
      try {
        const id = await api.SaveConnection(c, secret);
        secret = "";
        form.hidden = true; Work.editing = null;
        await loadProfiles();
        if (andOpen) { const np = Work.profiles.find(x => x.id === id); if (np) openProfile(np); }
      } catch (e) { setStatus(errText(e), true); }
    }
    async function del() {
      if (!await confirmDialog(t("conn.delete"), t("conn.deleteQ", { name: p.name }), t("conn.delete"), true)) return;
      await api.DeleteConnection(p.id).catch(e => setStatus(errText(e), true));
      form.hidden = true; Work.editing = null;
      loadProfiles();
    }
  };
  draw();
}

// ---- find, build, explain ------------------------------------------------------

function isExplainMode() { return Work.forced ? Work.forced === "explain" : !!Work.looksCmd; }

const onQuery = debounce(async () => {
  Work.query = document.getElementById("q").value;
  Work.looksCmd = Work.query.trim() ? await api.LooksLikeCommand(Work.query, activePlatform()).catch(() => false) : false;
  refreshRight();
}, 160);

function refreshRight() {
  const explain = isExplainMode() && Work.query.trim() !== "";
  document.querySelectorAll("#modeSeg button").forEach(b => b.setAttribute("aria-pressed", String((b.dataset.mode === "explain") === (isExplainMode()))));
  document.getElementById("modeHint").textContent = Work.forced ? t("mode.forced") : (Work.looksCmd ? t("mode.autoExplain") : t("mode.autoSearch"));
  document.getElementById("q").classList.toggle("mono", explain);
  document.getElementById("searchPane").hidden = explain;
  document.getElementById("explainPane").hidden = !explain;
  if (explain) runExplain(); else runSearch();
}

// The search pane: the builder of the chosen command stays right under the
// search box; the suggestions below it follow what is typed and scroll in
// the space left.
async function runSearch() {
  const query = Work.query;
  let res = [];
  try { res = await api.Search(query, activePlatform(), 60); } catch (e) { setStatus(errText(e), true); }
  if (query !== Work.query) return; // typed on meanwhile
  Work.results = res;
  if (!res.find(r => r.command.id === Work.sel)) Work.sel = res.length ? res[0].command.id : null;
  document.getElementById("searchHint").textContent = query.trim() ? t("search.found", { n: res.length, s: activeName() }) : t("search.recent");
  const list = clear(document.getElementById("results"));
  for (const r of res) {
    const c = r.command;
    list.appendChild(h("button", { type: "button", class: "result", "aria-pressed": String(c.id === Work.sel), onclick: () => selectResult(c.id) },
      h("span", { class: "row between wrap" }, h("span", { class: "name" }, c.name),
        h("span", { class: "row" }, h("span", { class: "os " + c.platform }, osLabel(c.platform)), query.trim() ? [h("span", { class: "bar" }, h("span", { style: { width: r.percent + "%" } })), h("span", { class: "mono muted" }, r.percent + "%")] : null)),
      h("code", {}, c.template)));
  }
  if (!res.length && query.trim()) list.appendChild(h("div", { class: "empty" }, t("search.none"), " ", h("button", { type: "button", class: "btn small", onclick: () => Library.newFromText(query) }, t("search.add"))));
  list.scrollTop = 0;
  showBuilder();
}

// selectResult builds another suggestion without searching again.
function selectResult(id) {
  Work.sel = id;
  document.querySelectorAll("#results .result").forEach((b, i) => b.setAttribute("aria-pressed", String(Work.results[i] && Work.results[i].command.id === id)));
  showBuilder();
}

// showBuilder puts the chosen command in the builder; the same command is
// left as it is, so values being typed are not lost.
function showBuilder() {
  const slot = document.getElementById("buildSlot");
  const r = Work.results.find(x => x.command.id === Work.sel);
  const key = r ? r.command.id + "\n" + r.command.template + "\n" + App.lang : "";
  if (key && slot.dataset.key === key) return;
  slot.dataset.key = key;
  clear(slot);
  slot.appendChild(r ? builder(r.command) : h("div", { class: "builder empty-build" }, h("div", { class: "label-up" }, t("build.title")), h("p", { class: "soft" }, t("build.pick"))));
}

function builder(c) {
  const box = h("div", { class: "builder" });
  const vals = Work.vals[c.id] || (Work.vals[c.id] = {});
  const preview = h("pre", { class: "preview mono" });
  const update = () => { preview.textContent = fillTemplate(c.template, Object.assign(Object.fromEntries(c.params.map(p => [p.key, p.default])), vals)); };
  box.appendChild(h("div", {}, h("div", { class: "row between" }, h("h3", {}, c.name), h("span", { class: "label-up" }, t("build.title"))), c.purpose ? h("p", { class: "soft", style: { margin: "2px 0 0" } }, c.purpose) : null));
  if (c.params.length) {
    const grid = h("div", { class: "grid2" });
    for (const p of c.params) {
      const inp = h("input", { type: p.type === "secret" ? "password" : "text", class: "mono", value: vals[p.key] !== undefined ? vals[p.key] : p.default, placeholder: p.key });
      inp.oninput = () => { vals[p.key] = inp.value; update(); };
      inp.onkeydown = e => { if (e.key === "Enter") { e.preventDefault(); runLine(preview.textContent, c.id); } };
      grid.appendChild(h("label", { class: "field" }, h("span", {}, h("span", { class: "mono", style: { color: "var(--code)" } }, p.key), p.description ? " · " + p.description : ""), inp));
    }
    box.appendChild(grid);
  }
  update();
  box.appendChild(preview);
  box.appendChild(h("div", { class: "row wrap", id: "buildBtns" },
    h("button", { type: "button", class: "btn pri run", onclick: () => runLine(preview.textContent, c.id) }, "▶ " + t("run.in", { s: activeName() })),
    h("button", { type: "button", class: "btn", onclick: () => { window.runtime.ClipboardSetText(preview.textContent); setStatus(t("common.copied")); } }, t("common.copy")),
    h("button", { type: "button", class: "btn ghost", onclick: () => Library.open(c.id) }, t("build.edit"))));
  if (c.notes) box.appendChild(h("p", { class: "note" }, c.notes));
  return box;
}

function renderBuilderButtons() {
  document.querySelectorAll(".btn.run").forEach(b => { b.textContent = "▶ " + t("run.in", { s: activeName() }); });
}

async function runExplain() {
  const pane = document.getElementById("explainPane");
  const line = Work.query;
  let res;
  try { res = await api.Explain(line, activePlatform()); } catch (e) { setStatus(errText(e), true); return; }
  if (line !== Work.query) return; // typed on meanwhile
  Work.explainRes = res;
  clear(pane);
  const body = h("div", { style: { display: "flex", flexDirection: "column", gap: "12px" } });
  renderExplain(body, res);
  pane.appendChild(body);
  pane.appendChild(h("div", { class: "row wrap", style: { marginTop: "12px" } },
    h("button", { type: "button", class: "btn pri run", onclick: () => runLine(line, 0) }, "▶ " + t("run.in", { s: activeName() })),
    h("button", { type: "button", class: "btn", onclick: () => { window.runtime.ClipboardSetText(line); setStatus(t("common.copied")); } }, t("common.copy")),
    h("button", { type: "button", class: "btn ghost", onclick: () => Library.newFromText(line) }, t("explain.save")),
    res.programs && res.programs.length ? h("button", { type: "button", class: "btn ghost", onclick: () => Library.editExplain(res.programs[0]) }, t("explain.edit", { p: res.programs[0] })) : null));
  pane.appendChild(h("p", { class: "note", style: { marginTop: "10px" } }, t("explain.source")));
}

function focusSearch() {
  showView("work");
  const q = document.getElementById("q");
  q.focus();
  q.select();
}

function initWork() {
  document.getElementById("q").addEventListener("input", () => { Work.forced = null; onQuery(); });
  document.getElementById("q").addEventListener("keydown", e => {
    if (e.key === "Enter" && isExplainMode()) { e.preventDefault(); runLine(Work.query, 0); }
  });
  document.querySelectorAll("#modeSeg button").forEach(b => b.addEventListener("click", () => { Work.forced = b.dataset.mode; refreshRight(); }));
  document.getElementById("newConnBtn").addEventListener("click", () => showConnForm(null));
  document.getElementById("openLocalBtn").addEventListener("click", openLocal);
  new ResizeObserver(() => { const s = Work.sessions.get(Work.active); if (s) try { s.fit.fit(); } catch (e) {} }).observe(document.getElementById("termhost"));
  window.runtime.EventsOn("session:data", onSessionData);
  window.runtime.EventsOn("session:closed", onSessionClosed);
  document.addEventListener("keydown", e => {
    if (e.ctrlKey && !e.shiftKey && !e.altKey && e.code === "KeyK") { e.preventDefault(); e.stopPropagation(); focusSearch(); }
  }, true);
  loadProfiles();
  renderTabs();
  refreshRight();
}

function relabelWork() {
  document.getElementById("openLocalBtn").textContent = t("term.openLocal", { shell: (App.info && App.info.shell) || "shell" });
  renderTabs();
  loadProfiles();
  refreshRight();
}
