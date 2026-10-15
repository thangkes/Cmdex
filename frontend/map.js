// Folder map (Work ▸ Map): a folder and three layers below it, drawn as a
// galaxy, or the same stars arranged as branches from left to right. Hovering shows a file's details and traces its path
// from the centre; clicking a folder flies into it and makes it the centre.
// Files can be opened, renamed or thrown into the black hole (the system
// trash, so they can be restored). Commands for a file are pasted into the
// local terminal, never run.

const FMap = {
  m: null, mode: "galaxy", sel: null, trace: null, hover: null, pos: {},
  loading: false, started: false, angle: 0, auto: true, renaming: false, confirming: false,
};

const MAP_COLORS = { code: "#7fa8ff", doc: "#6ff0c8", media: "#ff9a6b", data: "#ffd36b", run: "#ff7f9f", other: "#c9cbe0", dir: "#ffffff" };
const MAP_EXT = {
  code: "py go js ts mjs cjs java kt kts c h cpp hpp cs rs rb php swift scala sql sh ps1 psm1 bat cmd lua r pl html css scss vue jsx tsx xml gradle ipynb",
  doc: "md txt pdf doc docx rtf odt log rst tex",
  media: "png jpg jpeg gif bmp svg webp ico mp4 mkv mov avi mp3 wav flac ogg zip 7z rar gz tgz xz bz2 tar iso",
  data: "json csv tsv xlsx xls db sqlite yml yaml toml ini conf cfg env parquet",
  run: "exe msi com scr dll jar appimage deb rpm apk",
};
const MAP_KIND = {};
for (const [k, list] of Object.entries(MAP_EXT)) for (const e of list.split(" ")) MAP_KIND[e] = k;

function mapExt(name) { const i = name.lastIndexOf("."); return i > 0 ? name.slice(i + 1).toLowerCase() : ""; }
function mapKind(d) { return d.data.dir ? "dir" : d.data.more ? "other" : (MAP_KIND[mapExt(d.data.name)] || "other"); }
function mapColor(d) { return MAP_COLORS[mapKind(d)]; }
function mapPath(d) {
  const names = d.ancestors().reverse().slice(1).map(x => x.data.name);
  const r = FMap.m.root;
  return names.length ? (r.endsWith(FMap.m.sep) ? r : r + FMap.m.sep) + names.join(FMap.m.sep) : r;
}
function mapSize(n) {
  if (n < 1024) return n + " B";
  const u = ["KB", "MB", "GB", "TB"];
  let v = n / 1024, i = 0;
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; }
  return (v < 10 ? v.toFixed(1) : Math.round(v)) + " " + u[i];
}
function mapDate(sec) {
  if (!sec) return "";
  const d = new Date(sec * 1000), days = Math.floor((Date.now() - d) / 864e5);
  if (days <= 0) return t("map.today");
  if (days === 1) return t("map.yesterday");
  if (days < 7) return t("map.daysAgo", { n: days });
  return d.toLocaleDateString(App.lang === "vi" ? "vi-VN" : undefined);
}

// ---- building the pane -----------------------------------------------------------

function initMap() {
  const pane = document.getElementById("mapPane");
  pane.appendChild(h("div", { class: "map-hud" },
    h("div", { class: "map-seg", role: "group", id: "mapModes" },
      h("button", { type: "button", "data-m": "galaxy" }), h("button", { type: "button", "data-m": "tree" })),
    h("button", { type: "button", class: "map-ic", id: "mapUp" }, "⬆"),
    h("div", { class: "map-crumb", id: "mapCrumb" }),
    h("input", { type: "text", class: "map-search", id: "mapSearch", autocomplete: "off", spellcheck: "false" }),
    h("button", { type: "button", class: "map-ic", id: "mapTerm" }, "⌖"),
    h("button", { type: "button", class: "map-ic", id: "mapPick" }, "📁")));
  pane.appendChild(document.createElementNS("http://www.w3.org/2000/svg", "svg")).setAttribute("class", "map-sky");
  pane.appendChild(h("div", { class: "map-legend", id: "mapLegend" }));
  pane.appendChild(h("div", { class: "map-card", id: "mapCard" }));
  pane.appendChild(h("div", { class: "map-tip", id: "mapTip", hidden: true }));
  pane.appendChild(h("div", { class: "map-menu", id: "mapMenu", hidden: true }));
  pane.appendChild(h("div", { class: "map-busy", id: "mapBusy", hidden: true }));
  document.querySelectorAll("#mapModes button").forEach(b => b.onclick = () => { if (FMap.mode === b.dataset.m) return; FMap.mode = b.dataset.m; mapModeButtons(); Galaxy.morph(); });
  document.getElementById("mapUp").onclick = () => { if (FMap.m && FMap.m.parent) mapLoad(FMap.m.parent); };
  document.getElementById("mapPick").onclick = async () => { const p = await api.MapPickFolder().catch(e => { setStatus(errText(e), true); return ""; }); if (p) mapLoad(p); };
  document.getElementById("mapTerm").onclick = () => mapLoad(mapTerminalDir() || "");
  const q = document.getElementById("mapSearch");
  q.oninput = () => mapFind(q.value);
  q.onkeydown = e => { if (e.key === "Enter" && FMap.trace) { mapSelect(FMap.trace); Galaxy.fly(FMap.trace.children ? FMap.trace : (FMap.trace.parent || FMap.trace), false); } };
  pane.addEventListener("keydown", e => {
    if (e.target.tagName === "INPUT" || !FMap.sel) return;
    if (e.key === "F2") { e.preventDefault(); mapAct("rename", FMap.sel); }
    if (e.key === "Delete" && FMap.sel.depth && !FMap.sel.data.more) { e.preventDefault(); mapAct("trash", FMap.sel); }
  });
  pane.tabIndex = -1;
  document.addEventListener("click", () => { document.getElementById("mapMenu").hidden = true; });
  Galaxy.init(pane.querySelector(".map-sky"));
  relabelMap();
}

function relabelMap() {
  document.querySelectorAll("#mapModes button").forEach(b => { b.textContent = b.dataset.m === "galaxy" ? "✦ " + t("map.galaxy") : "☰ " + t("map.tree"); });
  document.getElementById("mapUp").title = t("map.up");
  document.getElementById("mapTerm").title = t("map.fromTerm");
  document.getElementById("mapPick").title = t("map.pick");
  document.getElementById("mapSearch").placeholder = t("map.search");
  const leg = clear(document.getElementById("mapLegend"));
  for (const k of ["code", "doc", "media", "data", "run", "dir"]) leg.appendChild(h("span", {}, h("i", { style: { background: MAP_COLORS[k] } }), t("map.kind." + k)));
  Galaxy.relabel();
  if (FMap.m) { mapDrawCrumb(); mapDrawCard(); }
}

// mapShow is called whenever the Map mode is chosen; the first time it
// opens the folder of the local terminal (or the home folder).
function mapShow() {
  if (!document.getElementById("mapPane").childElementCount) initMap();
  if (!FMap.started) { FMap.started = true; mapLoad(mapTerminalDir() || ""); return; }
  Galaxy.resize();
}

// mapTerminalDir reads the folder from the prompt of the local terminal
// ("PS C:\work>" or "user@host:~/work$"), "" when there is none to read.
function mapTerminalDir() {
  const s = mapLocalSession();
  if (!s) return "";
  const buf = s.term.buffer.active;
  for (let y = buf.baseY + buf.cursorY; y >= Math.max(0, buf.baseY + buf.cursorY - 40); y--) {
    const line = buf.getLine(y);
    if (!line) continue;
    const txt = line.translateToString(true);
    let m = txt.match(/PS ([A-Za-z]:\\[^>]*|\\\\[^>]+)>/);
    if (m) return m[1];
    m = txt.match(/^[\w.-]+@[\w.-]+:((?:~|\/)[^$#]*)[$#]/);
    if (m) return m[1].trim();
  }
  return "";
}

function mapLocalSession() {
  const a = Work.sessions.get(Work.active);
  if (a && !a.closed && a.info.kind === "local") return a;
  return [...Work.sessions.values()].find(s => !s.closed && s.info.kind === "local") || null;
}

// mapPaste puts a line into the local terminal (opening one if needed),
// without pressing Enter.
async function mapPaste(line, id) {
  const s = mapLocalSession();
  if (s) { if (Work.active !== s.info.id) activate(s.info.id); }
  else {
    const nid = await openLocal();
    if (!nid) return;
    await new Promise(r => setTimeout(r, 1200));
  }
  runLine(line, id || 0);
}

// ---- data --------------------------------------------------------------------------

async function mapLoad(path, keepSel) {
  if (FMap.loading) return;
  FMap.loading = true;
  const busy = document.getElementById("mapBusy");
  busy.hidden = false;
  busy.textContent = t("map.reading");
  try {
    const m = await api.MapScan(path);
    const centreChanged = !FMap.m || FMap.m.root !== m.root;
    FMap.m = m;
    FMap.root = d3.hierarchy(m.tree, n => {
      if (!n.dir) return null;
      const kids = (n.kids || []).slice();
      if (n.more) kids.push({ name: "+" + n.more, more: n.more, size: n.moreSize || 0, mod: 0 });
      return kids;
    });
    if (centreChanged) FMap.pos = {};
    const want = keepSel || "";
    FMap.sel = want ? mapByPath(want) : null;
    FMap.trace = FMap.sel;
    document.getElementById("mapSearch").value = "";
    mapDraw(centreChanged);
    mapDrawCard();
  } catch (e) {
    setStatus(t("map.readFail", { err: errText(e) }), true);
  } finally {
    FMap.loading = false;
    busy.hidden = true;
  }
}

function mapByPath(p) {
  if (!FMap.root) return null;
  const low = s => App.info.os === "windows" ? s.toLowerCase() : s;
  return FMap.root.descendants().find(d => low(mapPath(d)) === low(p)) || null;
}

function mapFind(q) {
  q = q.trim().toLowerCase();
  if (!q || !FMap.root) { mapTrace(FMap.sel); return; }
  const hit = FMap.root.descendants().find(d => d.depth && !d.data.more && d.data.name.toLowerCase().includes(q));
  if (hit) mapTrace(hit);
}

function mapKey(d) { return d.ancestors().reverse().slice(1).map(x => x.data.name).join("/"); }

// ---- selection, trace, card --------------------------------------------------------

function mapTrace(d) {
  FMap.trace = d;
  mapDrawCrumb();
  Galaxy.paint();
}

function mapSelect(d) {
  FMap.sel = d;
  FMap.renaming = FMap.confirming = false;
  Galaxy.unhold();
  mapTrace(d);
  mapDrawCard();
  document.getElementById("mapPane").focus({ preventScroll: true });
}

function mapDrawCrumb() {
  const box = clear(document.getElementById("mapCrumb"));
  if (!FMap.m) return;
  const parts = FMap.m.root.split(/[\\/]/).filter(Boolean);
  const isWin = FMap.m.sep === "\\";
  let acc = isWin ? "" : "/";
  parts.forEach((p, i) => {
    acc = isWin ? (i === 0 ? p + "\\" : acc + (acc.endsWith("\\") ? "" : "\\") + p) : acc + (acc.endsWith("/") ? "" : "/") + p;
    const target = acc;
    if (i) box.appendChild(h("span", { class: "sep" }, "›"));
    box.appendChild(h("button", { type: "button", onclick: () => mapLoad(target) }, p));
  });
  if (FMap.trace && FMap.trace.depth) {
    for (const a of FMap.trace.ancestors().reverse().slice(1)) {
      box.appendChild(h("span", { class: "sep" }, "›"));
      box.appendChild(h("b", {}, a.data.name));
    }
  }
  box.scrollLeft = box.scrollWidth;
}

function mapTip(e, d) {
  const tip = document.getElementById("mapTip");
  if (!d) { tip.hidden = true; return; }
  const n = d.data, rows = [];
  if (n.more) rows.push([t("map.tip.hidden"), t("map.tip.moreText", { n: n.more })]);
  else {
    rows.push([t("map.tip.type"), n.dir ? t("map.tip.folder") : n.link ? t("map.tip.link") : (mapExt(n.name) ? mapExt(n.name).toUpperCase() + " · " + t("map.kind." + mapKind(d)) : t("map.tip.file"))]);
    rows.push([t("map.tip.size"), mapSize(n.size) + (n.dir && n.partial ? "+" : "")]);
    if (n.mod) rows.push([t("map.tip.mod"), mapDate(n.mod)]);
    if (n.dir) rows.push([t("map.tip.inside"), n.read ? t("map.tip.counts", { f: n.files, d: n.dirs }) : t("map.tip.notRead")]);
  }
  clear(tip).appendChild(h("div", { class: "t" }, h("i", { style: { background: mapColor(d) } }), n.name));
  tip.appendChild(h("table", {}, rows.map(([k, v]) => h("tr", {}, h("td", { class: "k" }, k), h("td", {}, v)))));
  if (!n.more) tip.appendChild(h("div", { class: "p" }, mapPath(d)));
  tip.hidden = false;
  const r = document.getElementById("mapPane").getBoundingClientRect();
  let x = e.clientX - r.left + 16, y = e.clientY - r.top + 16;
  if (x + tip.offsetWidth > r.width - 6) x = e.clientX - r.left - tip.offsetWidth - 16;
  if (y + tip.offsetHeight > r.height - 6) y = e.clientY - r.top - tip.offsetHeight - 16;
  tip.style.left = Math.max(6, x) + "px";
  tip.style.top = Math.max(6, y) + "px";
}

async function mapDrawCard() {
  const card = clear(document.getElementById("mapCard"));
  const d = FMap.sel;
  if (!FMap.m) return;
  if (!d) { card.appendChild(h("span", { class: "soft" }, t("map.hint"))); return; }
  const n = d.data;
  if (n.more) {
    card.appendChild(h("b", {}, t("map.tip.moreText", { n: n.more })));
    card.appendChild(h("p", { class: "soft" }, t("map.moreHint")));
    card.appendChild(h("div", { class: "acts" }, h("button", { type: "button", onclick: () => mapAct("reveal", d.parent) }, t("map.act.reveal"))));
    return;
  }
  const title = h("div", { class: "head" }, h("b", {}, h("i", { style: { background: mapColor(d) } }), h("span", { class: "nm" }, n.name)),
    h("span", { class: "muted" }, [mapSize(n.size) + (n.dir && n.partial ? "+" : ""), mapDate(n.mod)].filter(Boolean).join(" · ")));
  card.appendChild(title);
  if (FMap.confirming) {
    card.appendChild(h("p", { class: "soft" }, t("map.trashQ", { name: n.name }) + (n.dir && n.read ? " " + t("map.trashInside", { f: n.files, d: n.dirs }) : "")));
    card.appendChild(h("div", { class: "acts" },
      h("button", { type: "button", onclick: () => { FMap.confirming = false; mapDrawCard(); Galaxy.unhold(); } }, t("common.cancel")),
      h("button", { type: "button", class: "danger", onclick: () => mapTrash(d) }, "◉ " + t("map.act.trashGo"))));
    return;
  }
  if (FMap.renaming) {
    const inp = h("input", { type: "text", class: "map-rename", value: n.name });
    const done = async () => {
      const v = inp.value.trim();
      FMap.renaming = false;
      if (!v || v === n.name) { mapDrawCard(); return; }
      try { const np = await api.MapRename(mapPath(d), v); await mapLoad(FMap.m.root, np); setStatus(t("map.renamed")); }
      catch (e) { setStatus(errText(e), true); FMap.renaming = true; }
    };
    inp.onkeydown = e => { if (e.key === "Enter") { e.preventDefault(); done(); } if (e.key === "Escape") { FMap.renaming = false; mapDrawCard(); } };
    title.querySelector(".nm").replaceWith(inp);
    requestAnimationFrame(() => { inp.focus(); const dot = n.dir ? -1 : n.name.lastIndexOf("."); inp.setSelectionRange(0, dot > 0 ? dot : n.name.length); });
    return;
  }
  const a = (key, label, cls) => h("button", { type: "button", class: cls || "", onclick: e => { e.stopPropagation(); mapAct(key, d); } }, label);
  const acts = h("div", { class: "acts" });
  if (n.dir) {
    if (d.depth) acts.appendChild(a("enter", "⤵ " + t("map.act.enter")));
    acts.appendChild(a("cd", "↳ cd"));
    acts.appendChild(a("mkdir", "＋ " + t("map.act.mkdir")));
  } else acts.appendChild(a("open", t("map.act.open")));
  acts.appendChild(a("reveal", t("map.act.reveal")));
  if (d.depth) acts.appendChild(a("rename", t("map.act.rename")));
  acts.appendChild(a("copy", t("map.act.copy")));
  if (d.depth) acts.appendChild(a("trash", "◉ " + t("map.act.trash"), "danger"));
  card.appendChild(acts);
  if (!n.dir) {
    const path = mapPath(d);
    const list = await api.MapSuggest(path).catch(() => []);
    if (FMap.sel !== d || FMap.renaming || FMap.confirming) return;
    for (const s of list) {
      card.appendChild(h("div", { class: "sug" },
        h("div", { class: "txt" }, h("div", {}, s.name, " ", riskBadge(s.risk)), h("code", {}, s.line)),
        h("button", { type: "button", title: t("run.pasteHint"), onclick: e => { e.stopPropagation(); mapPaste(s.line, s.id); } }, "▶ " + t("map.paste"))));
    }
  }
}

async function mapAct(key, d) {
  document.getElementById("mapMenu").hidden = true;
  if (FMap.sel !== d) mapSelect(d);
  const path = mapPath(d);
  try {
    switch (key) {
      case "enter": Galaxy.fly(d, true); break;
      case "cd": mapPaste(await api.MapCdLine(path), 0); break;
      case "open":
        if (await api.MapRunnable(path)) {
          if (!await confirmDialog(t("map.runTitle"), t("map.runText", { name: d.data.name }), t("map.runOk"), true)) return;
          await api.MapOpen(path, true);
        } else await api.MapOpen(path, false);
        break;
      case "reveal": await api.MapReveal(path); break;
      case "copy": window.runtime.ClipboardSetText(path); setStatus(t("map.copied")); break;
      case "rename": FMap.renaming = true; FMap.confirming = false; mapDrawCard(); break;
      case "mkdir": {
        const base = t("map.newFolder");
        const names = new Set((d.children || []).map(c => c.data.name.toLowerCase()));
        let name = base, i = 2;
        while (names.has(name.toLowerCase())) name = base + " (" + (i++) + ")";
        const np = await api.MapMkdir(path, name);
        await mapLoad(FMap.m.root, np);
        FMap.renaming = true; mapDrawCard();
        break;
      }
      case "trash": FMap.confirming = true; FMap.renaming = false; mapDrawCard(); break;
    }
  } catch (e) { setStatus(errText(e), true); }
}

// mapTrash throws the file into the black hole: the animation plays while
// the file goes to the system trash.
async function mapTrash(d) {
  const path = mapPath(d), parent = d.parent ? mapPath(d.parent) : FMap.m.root;
  FMap.confirming = false;
  const anim = Galaxy.devour(d);
  try {
    await api.MapTrash(path);
    await anim;
    setStatus(t("map.trashed", { name: d.data.name }));
    await mapLoad(FMap.m.root, parent);
  } catch (e) {
    await anim;
    setStatus(errText(e), true);
    await mapLoad(FMap.m.root, path);
  }
}

function mapMenu(e, d) {
  e.preventDefault(); e.stopPropagation();
  mapTip(null);
  if (d.data.more) return;
  mapSelect(d);
  const menu = clear(document.getElementById("mapMenu")), n = d.data;
  const item = (key, label, cls, keyHint) => h("div", { class: cls || "", onclick: ev => { ev.stopPropagation(); mapAct(key, d); } }, label, keyHint ? h("small", {}, keyHint) : null);
  menu.appendChild(h("div", { class: "head" }, n.name));
  if (n.dir) { if (d.depth) menu.appendChild(item("enter", "⤵ " + t("map.act.enter"))); menu.appendChild(item("cd", "↳ cd")); menu.appendChild(item("mkdir", "＋ " + t("map.act.mkdir"))); }
  else menu.appendChild(item("open", t("map.act.open")));
  menu.appendChild(item("reveal", t("map.act.reveal")));
  menu.appendChild(item("copy", t("map.act.copy")));
  if (d.depth) { menu.appendChild(item("rename", t("map.act.rename"), "", "F2")); menu.appendChild(h("hr")); menu.appendChild(item("trash", "◉ " + t("map.act.trash"), "danger", "Del")); }
  menu.hidden = false;
  const r = document.getElementById("mapPane").getBoundingClientRect();
  let x = e.clientX - r.left, y = e.clientY - r.top;
  if (x + menu.offsetWidth > r.width) x -= menu.offsetWidth;
  if (y + menu.offsetHeight > r.height) y -= menu.offsetHeight;
  menu.style.left = Math.max(4, x) + "px";
  menu.style.top = Math.max(4, y) + "px";
}

// ---- drawing -------------------------------------------------------------------------

function mapModeButtons() {
  document.querySelectorAll("#mapModes button").forEach(b => b.setAttribute("aria-pressed", String(b.dataset.m === FMap.mode)));
}

function mapDraw(fresh) {
  if (!FMap.m) return;
  mapModeButtons();
  document.getElementById("mapUp").disabled = !FMap.m.parent;
  mapDrawCrumb();
  Galaxy.draw(fresh);
}

// ---- the galaxy ----------------------------------------------------------------------

// Two layouts of the same stars: the galaxy (a force layout round the
// centre, turning slowly) and branches (a tree from the centre on the left
// to the deepest layer on the right). Switching morphs one into the other.
const Galaxy = (() => {
  let svg, el, W = 600, H = 400, bg, world, spin, neb, fx, hole, linkSel, haloSel, starSel, labelSel, nodes = [], links = [], delaunay = null, fit = 1, home, zoom;
  let hoverNode = null, drag = null, held = null, morphing = false;
  const COL = 210, ROW = 20; // branches: a column per layer, a row per star
  const ring = d => [0, 150, 240, 315][d.depth] || 330;
  const rad = d => d.depth === 0 ? 18 : d.data.dir ? 4.5 + Math.min(10, Math.sqrt((d.data.files || 0) + (d.data.dirs || 0) * 3)) : d.data.more ? 3 : 1.8 + Math.min(6, Math.log10(1 + d.data.size) * 0.9);

  function init(svgEl) {
    el = svgEl; svg = d3.select(el);
    const defs = svg.append("defs");
    const glow = (id, sd) => { const f = defs.append("filter").attr("id", id).attr("x", "-200%").attr("y", "-200%").attr("width", "500%").attr("height", "500%"); f.append("feGaussianBlur").attr("stdDeviation", sd).attr("result", "b"); const m = f.append("feMerge"); m.append("feMergeNode").attr("in", "b"); m.append("feMergeNode").attr("in", "SourceGraphic"); };
    glow("mapG1", 2); glow("mapG2", 5); glow("mapG3", 9);
    const grad = (id, stops) => { const g = defs.append("radialGradient").attr("id", id); stops.forEach(([o, c, a]) => g.append("stop").attr("offset", o).attr("stop-color", c).attr("stop-opacity", a)); };
    grad("mapCore", [[0, "#fff", 1], [.25, "#d6e0ff", .9], [.6, "#7c8cff", .25], [1, "#3b2a7a", 0]]);
    grad("mapNeb", [[0, "#8f7bff", .22], [.5, "#4b6bff", .08], [1, "#000", 0]]);
    grad("mapDisk", [[0, "#000", 0], [.42, "#000", 0], [.5, "#ffb36b", .95], [.62, "#ff6a3d", .55], [.8, "#9b3cff", .18], [1, "#000", 0]]);
    bg = svg.append("g");
    world = svg.append("g");
    spin = world.append("g");
    hole = svg.append("g").attr("class", "map-hole");
    fx = svg.append("g").style("pointer-events", "none");
    zoom = d3.zoom().scaleExtent([.25, 10])
      .filter(e => !e.button && !(e.type === "mousedown" && pick(e)) && e.type !== "dblclick")
      .on("zoom", e => { world.attr("transform", e.transform); labels(); });
    svg.call(zoom);
    svg.on("mousemove", onMove).on("mouseleave", () => { setHover(null); mapTip(null); })
      .on("mousedown", onDown).on("click", onClick).on("contextmenu", e => { const d = pick(e); if (d) mapMenu(e, d); else e.preventDefault(); })
      .on("dblclick", e => { const d = pick(e); if (d && d.data.dir && d.depth) fly(d, true); });
    window.addEventListener("mouseup", onUp);
    window.addEventListener("mousemove", onDrag);
    new ResizeObserver(() => resize()).observe(el);
    d3.timer(() => {
      if (FMap.mode !== "galaxy" || morphing || !FMap.auto || hoverNode || drag || document.getElementById("mapPane").hidden) return;
      FMap.angle = (FMap.angle + 0.025) % 360;
      spin.attr("transform", `rotate(${FMap.angle})`);
      labelSel && labelSel.attr("transform", d => `rotate(${-FMap.angle},${d.x},${d.y})`);
    });
  }

  function resize() {
    if (!el || !el.clientWidth) return;
    W = el.clientWidth; H = el.clientHeight;
    svg.attr("viewBox", [-W / 2, -H / 2, W, H]);
    bg.selectAll("*").remove();
    const rnd = d3.randomLcg(7);
    for (let i = 0; i < 220; i++) bg.append("circle").attr("cx", (rnd() - .5) * W * 1.6).attr("cy", (rnd() - .5) * H * 1.6).attr("r", rnd() < .92 ? .6 : 1.2).attr("class", "map-twinkle").style("animation-delay", (rnd() * 4).toFixed(2) + "s");
    drawHole();
    if (nodes.length && !morphing) { fitTo(); svg.call(zoom.transform, home); }
  }

  // the black hole, bottom left: an accretion disk, a photon ring and the
  // shadow; files thrown in go to the system trash.
  function holeXY() { return [-W / 2 + 64, H / 2 - 64]; }
  function drawHole() {
    hole.selectAll("*").remove();
    const [x, y] = holeXY();
    const g = hole.attr("transform", `translate(${x},${y})`);
    g.append("circle").attr("r", 52).attr("fill", "url(#mapDisk)").attr("class", "disk-glow");
    const disk = g.append("g").attr("class", "disk");
    disk.append("ellipse").attr("rx", 46).attr("ry", 13).attr("fill", "none").attr("stroke", "#ff9a5c").attr("stroke-width", 4).attr("opacity", .7).attr("filter", "url(#mapG2)");
    disk.append("ellipse").attr("rx", 34).attr("ry", 9).attr("fill", "none").attr("stroke", "#ffd9a8").attr("stroke-width", 1.5).attr("opacity", .9);
    g.append("circle").attr("class", "photon").attr("r", 17).attr("fill", "none").attr("stroke", "#ffe6c4").attr("stroke-width", 1.6).attr("filter", "url(#mapG1)");
    g.append("circle").attr("r", 15).attr("fill", "#000");
    g.append("path").attr("d", "M-46,0 A46,13 0 0 0 46,0").attr("fill", "none").attr("stroke", "#ffb87a").attr("stroke-width", 3).attr("opacity", .85).attr("filter", "url(#mapG1)");
    g.append("text").attr("class", "map-hole-label").attr("y", 40).attr("text-anchor", "middle").text(t("map.hole"));
  }
  function holeNear(px, py) { const [x, y] = holeXY(); return Math.hypot(px - x, py - y) < 58; }

  // fitTo frames a layout: the galaxy round its centre, the branches by
  // their bounds (names included), never smaller than readable.
  function fitTo(mode) {
    if ((mode || FMap.mode) === "galaxy") {
      const maxR = d3.max(nodes, d => Math.hypot(d.gx, d.gy) + rad(d) + 16) || 200;
      fit = Math.min(W, H - 70) / 2 / maxR * .95;
      home = d3.zoomIdentity.translate(0, 18).scale(fit);
      return;
    }
    const x0 = d3.min(nodes, d => d.bx) - 30, x1 = d3.max(nodes, d => d.bx) + 170;
    const y0 = d3.min(nodes, d => d.by) - 20, y1 = d3.max(nodes, d => d.by) + 20;
    fit = Math.max(.45, Math.min(1.3, (W - 40) / (x1 - x0), (H - 90) / (y1 - y0)));
    const cy = (y1 - y0) * fit > H - 90 ? y0 + (H - 90) / 2 / fit : (y0 + y1) / 2; // tall: start at the top
    home = d3.zoomIdentity.translate(0, 22).scale(fit).translate(-(x0 + x1) / 2, -cy);
  }

  // branches: d3.tree from the centre (left) to the deepest layer (right);
  // folders get a little more room than files
  function layoutBranches() {
    const t = d3.tree().nodeSize([ROW, COL]).separation((a, b) => (a.parent === b.parent ? 1 : 1.5) + (a.data.dir || b.data.dir ? .3 : 0));
    const r = t(FMap.root.copy());
    const by = new Map(r.descendants().map(x => [mapKey(x), x]));
    const depth = d3.max(nodes, d => d.depth) || 1;
    for (const d of nodes) { const x = by.get(mapKey(d)); d.bx = x.y - depth * COL / 2; d.by = x.x; }
  }

  function linkPath(l) {
    const s = l.source, d = l.target;
    if (FMap.mode !== "galaxy" || morphing === "tree") {
      const mx = (s.x + d.x) / 2;
      return `M${s.x},${s.y}C${mx},${s.y} ${mx},${d.y} ${d.x},${d.y}`;
    }
    const mx = (s.x + d.x) / 2, my = (s.y + d.y) / 2, dx = d.x - s.x, dy = d.y - s.y;
    return `M${s.x},${s.y}Q${mx - dy * .18},${my + dx * .18} ${d.x},${d.y}`;
  }

  // place moves every mark to the stars' current positions
  function place(branchLabels) {
    starSel.attr("cx", d => d.x).attr("cy", d => d.y);
    haloSel.attr("cx", d => d.x).attr("cy", d => d.y);
    linkSel.attr("d", linkPath);
    // branches: a folder with stars after it is named above itself (its
    // links leave to the right), the others to the right
    if (branchLabels) labelSel.attr("x", d => d.children ? d.x : d.x + rad(d) + 7).attr("y", d => d.children ? d.y - rad(d) - 7 : d.y + 4).attr("text-anchor", d => d.children ? "middle" : "start");
    else labelSel.attr("x", d => d.x).attr("y", d => d.y - rad(d) - (d.data.dir ? 8 : 5)).attr("text-anchor", "middle");
    spin.attr("transform", `rotate(${FMap.angle})`);
    labelSel.attr("transform", d => `rotate(${-FMap.angle},${d.x},${d.y})`);
  }

  // morph slides the stars from one layout to the other: the galaxy stops
  // turning and unrolls into branches, or the branches curl back up.
  function morph() {
    if (!nodes.length) return;
    const to = FMap.mode, dur = 1100;
    const from = nodes.map(d => [d.x, d.y]), target = nodes.map(d => to === "galaxy" ? [d.gx, d.gy] : [d.bx, d.by]);
    const a0 = FMap.angle;
    morphing = to;
    setHover(null); mapTip(null);
    fitTo(to);
    svg.transition().duration(dur).ease(d3.easeCubicInOut).call(zoom.transform, home);
    neb.transition().duration(dur).attr("opacity", to === "galaxy" ? 1 : 0);
    const tm = d3.timer(ms => {
      const t = Math.min(1, ms / dur), e = d3.easeCubicInOut(t);
      nodes.forEach((d, i) => { d.x = from[i][0] + (target[i][0] - from[i][0]) * e; d.y = from[i][1] + (target[i][1] - from[i][1]) * e; });
      if (to !== "galaxy") FMap.angle = a0 * (1 - e);
      place(to === "galaxy" ? e < .5 : e >= .5);
      if (t >= 1) { tm.stop(); done(); }
    });
    // a hidden window draws no frames: finish anyway
    const safety = setTimeout(() => { if (morphing) { tm.stop(); nodes.forEach((d, i) => { d.x = target[i][0]; d.y = target[i][1]; }); if (to !== "galaxy") FMap.angle = 0; done(); } }, dur + 600);
    function done() {
      clearTimeout(safety);
      morphing = false;
      place(to !== "galaxy");
      delaunay = d3.Delaunay.from(nodes, d => d.x, d => d.y);
      FMap.auto = to === "galaxy";
      paint();
    }
  }

  function draw(fresh) {
    if (!el.clientWidth) { requestAnimationFrame(() => draw(fresh)); return; }
    if (W !== el.clientWidth || H !== el.clientHeight) { W = 0; resize(); }
    spin.selectAll("*").remove();
    nodes = FMap.root.descendants();
    links = FMap.root.links();
    nodes.forEach((d, i) => {
      const o = FMap.pos[mapKey(d)];
      if (o) { d.x = o[0]; d.y = o[1]; }
      else if (d.parent && FMap.pos[mapKey(d.parent)]) { const p = FMap.pos[mapKey(d.parent)]; d.x = p[0] * 1.12 + (i % 7) - 3; d.y = p[1] * 1.12 + (i % 5) - 2; }
      else { const a = i * 2.399; d.x = Math.cos(a) * ring(d); d.y = Math.sin(a) * ring(d); }
    });
    FMap.root.fx = 0; FMap.root.fy = 0;
    const many = nodes.length > 600;
    const sim = d3.forceSimulation(nodes)
      .force("link", d3.forceLink(links).distance(l => l.target.data.dir ? 90 : 30).strength(.7))
      .force("charge", d3.forceManyBody().strength(d => d.data.dir ? -380 : many ? -18 : -40).distanceMax(400))
      .force("radial", d3.forceRadial(ring).strength(d => d.depth ? .16 : 1))
      .force("col", d3.forceCollide(d => rad(d) + (many ? 2 : 5))).stop();
    for (let i = 0; i < (many ? 220 : 380); i++) sim.tick();
    nodes.forEach(d => { FMap.pos[mapKey(d)] = [d.x, d.y]; d.gx = d.x; d.gy = d.y; });
    layoutBranches();
    const branches = FMap.mode !== "galaxy";
    if (branches) { nodes.forEach(d => { d.x = d.bx; d.y = d.by; }); FMap.angle = 0; }
    delaunay = d3.Delaunay.from(nodes, d => d.x, d => d.y);
    fitTo();
    neb = spin.append("circle").attr("r", 360).attr("fill", "url(#mapNeb)").attr("opacity", branches ? 0 : 1);
    linkSel = spin.append("g").selectAll("path").data(links).join("path").attr("class", "map-link")
      .attr("stroke-width", d => d.target.data.dir ? 1.1 : .6).attr("d", linkPath);
    haloSel = spin.append("g").selectAll("circle").data(nodes.filter(d => d.data.dir)).join("circle")
      .attr("cx", d => d.x).attr("cy", d => d.y).attr("r", d => d.depth ? rad(d) * 3 : 72).attr("fill", d => d.depth ? "rgba(140,160,255,.07)" : "url(#mapCore)");
    starSel = spin.append("g").selectAll("circle").data(nodes).join("circle")
      .attr("cx", d => d.x).attr("cy", d => d.y).attr("r", rad)
      .attr("fill", d => mapColor(d)).attr("fill-opacity", d => d.data.more ? .45 : 1)
      .attr("stroke", d => d.data.dir && !d.data.read && d.depth ? "rgba(255,255,255,.6)" : null)
      .attr("stroke-dasharray", d => d.data.dir && !d.data.read && d.depth ? "2 2" : null)
      .attr("filter", d => d.data.dir ? "url(#mapG2)" : many ? null : "url(#mapG1)");
    labelSel = spin.append("g").selectAll("text").data(nodes).join("text").attr("class", "map-label")
      .attr("x", d => d.x).attr("y", d => d.y - rad(d) - (d.data.dir ? 8 : 5)).attr("text-anchor", "middle")
      .style("font-size", d => d.depth === 0 ? "14px" : d.data.dir ? "12px" : "10.5px").text(d => d.data.name);
    place(branches);
    if (fresh) {
      // a new centre: its stars unfold from it
      const [ox, oy] = [FMap.root.x, FMap.root.y];
      starSel.attr("cx", ox).attr("cy", oy).transition().duration(900).ease(d3.easeCubicOut).attr("cx", d => d.x).attr("cy", d => d.y);
      linkSel.attr("opacity", 0).transition().delay(300).duration(700).attr("opacity", 1);
      svg.call(zoom.transform, home.scale(.6)).transition().duration(900).call(zoom.transform, home);
      FMap.auto = !branches;
    } else if (!d3.zoomTransform(el).k || d3.zoomTransform(el).k === 1) svg.call(zoom.transform, home);
    paint();
  }

  function labels() {
    if (!labelSel) return;
    const k = d3.zoomTransform(el).k, on = new Set(FMap.trace ? FMap.trace.ancestors() : []);
    const branches = FMap.mode !== "galaxy" && !morphing;
    labelSel.attr("opacity", d => (branches && k >= .55) || d.depth <= 1 || (d.data.dir && k > fit * 1.2) || k > fit * 2.2 || on.has(d) || d === hoverNode || d === FMap.sel ? 1 : 0);
  }

  // paint shows the trace: the path from the centre to the hovered (or
  // chosen) star lights up and flows; the rest dims.
  function paint() {
    if (!linkSel) return;
    const d = hoverNode || FMap.trace, on = new Set(d ? d.ancestors() : []);
    linkSel.classed("trace", l => on.has(l.target)).attr("opacity", l => !d || on.has(l.target) ? 1 : .3);
    labelSel.classed("on", x => on.has(x));
    starSel.attr("stroke", x => x === FMap.sel ? "#ffe08a" : x.data.dir && !x.data.read && x.depth ? "rgba(255,255,255,.6)" : null)
      .attr("stroke-width", x => x === FMap.sel ? 2 : 1);
    labels();
  }

  // pick finds the star nearest the pointer, within 22 screen pixels of its
  // edge: small stars need no aim.
  function pick(e) {
    if (!delaunay || !nodes.length || morphing) return null;
    const [x, y] = d3.pointer(e, spin.node());
    const i = delaunay.find(x, y), d = nodes[i];
    if (!d) return null;
    const k = d3.zoomTransform(el).k;
    return Math.hypot(d.x - x, d.y - y) * k <= rad(d) * k + 22 ? d : null;
  }

  function setHover(d) {
    if (d === hoverNode) return;
    if (hoverNode && starSel) starSel.filter(x => x === hoverNode).transition().duration(250).attr("r", rad);
    hoverNode = d;
    if (d) starSel.filter(x => x === d).transition().duration(120).attr("r", x => rad(x) * 1.8);
    el.style.cursor = d ? "pointer" : "grab";
    paint();
  }

  function onMove(e) {
    if (drag && drag.on) return;
    const d = pick(e);
    setHover(d);
    mapTip(d ? e : null, d);
  }

  function onClick(e) {
    if (drag && drag.moved) return;
    const d = pick(e);
    if (!d) { mapSelect(null); FMap.auto = true; svg.transition().duration(800).call(zoom.transform, home); return; }
    e.stopPropagation();
    if (d.data.dir && d.depth && FMap.sel === d) { fly(d, true); return; } // a second click goes in
    mapSelect(d);
    if (d.data.dir && d.depth) fly(d, false);
  }

  // dragging a star (not the centre) towards the black hole
  function onDown(e) {
    if (e.button) return;
    const d = pick(e);
    if (!d || !d.depth || d.data.more) return;
    drag = { d, x0: e.clientX, y0: e.clientY, on: false, moved: false };
  }
  function onDrag(e) {
    if (!drag) return;
    if (!drag.on && Math.hypot(e.clientX - drag.x0, e.clientY - drag.y0) > 6) {
      drag.on = drag.moved = true;
      mapTip(null);
      drag.ghost = fx.append("circle").attr("r", rad(drag.d) * Math.max(1.4, d3.zoomTransform(el).k)).attr("fill", mapColor(drag.d)).attr("filter", "url(#mapG2)");
      starSel.filter(x => x === drag.d).attr("opacity", .25);
    }
    if (!drag.on) return;
    const [px, py] = d3.pointer(e, el);
    drag.px = px; drag.py = py;
    drag.ghost.attr("cx", px).attr("cy", py);
    hole.classed("hungry", holeNear(px, py));
  }
  function onUp() {
    if (!drag) return;
    const dr = drag;
    setTimeout(() => { if (drag === dr) drag = null; }, 0);
    if (!dr.on) return;
    hole.classed("hungry", false);
    if (holeNear(dr.px, dr.py)) {
      mapSelect(dr.d);
      held = dr;
      FMap.confirming = true;
      mapDrawCard();
    } else {
      dr.ghost.remove();
      starSel.filter(x => x === dr.d).attr("opacity", 1);
    }
  }
  function unhold() {
    if (!held) return;
    held.ghost.remove();
    starSel && starSel.filter(x => x === held.d).attr("opacity", 1);
    held = null;
  }

  function screenOf(d) {
    const a = FMap.angle * Math.PI / 180, tr = d3.zoomTransform(el);
    const xr = d.x * Math.cos(a) - d.y * Math.sin(a), yr = d.x * Math.sin(a) + d.y * Math.cos(a);
    return [tr.applyX(xr), tr.applyY(yr)];
  }

  // devour pulls a star into the black hole: tides stretch it into a thin
  // strand (spaghettification) that winds round the hole, heats up and
  // vanishes past the photon ring, which flashes.
  function devour(d) {
    return new Promise(resolve => {
      let sx, sy;
      if (held && held.d === d) { sx = held.px; sy = held.py; held.ghost.remove(); held = null; }
      else [sx, sy] = screenOf(d);
      if (!isFinite(sx) || !isFinite(sy)) { resolve(); return; } // not drawn yet
      starSel && starSel.filter(x => x === d).attr("opacity", 0);
      const [hx, hy] = holeXY(), color = d3.color(mapColor(d)), hot = d3.interpolateRgb(color, "#fff1d6");
      const r0 = Math.hypot(sx - hx, sy - hy), a0 = Math.atan2(sy - hy, sx - hx), size = Math.max(4, rad(d) * 1.6);
      const trail = [], noodle = fx.append("path").attr("fill", "none").attr("stroke-linecap", "round").attr("filter", "url(#mapG1)");
      const head = fx.append("ellipse").attr("filter", "url(#mapG2)");
      const dur = 1700;
      let over = false;
      const finish = () => { if (over) return; over = true; tm.stop(); head.remove(); noodle.remove(); };
      // a hidden window draws no frames: never wait for the animation
      setTimeout(() => { if (!over) { finish(); resolve(); } }, dur + 900);
      const tm = d3.timer(el2 => {
        const t = Math.min(1, el2 / dur), e = d3.easeCubicIn(t);
        const r = r0 * Math.pow(1 - e, 1.25) + 15 * (1 - e) * e, ang = a0 + e * e * 5.5;
        const x = hx + r * Math.cos(ang), y = hy + r * Math.sin(ang) * (0.55 + 0.45 * (1 - e));
        trail.push([x, y]);
        if (trail.length > 46) trail.shift();
        const prev = trail.length > 1 ? trail[trail.length - 2] : [sx, sy];
        const dir = Math.atan2(y - prev[1], x - prev[0]) * 180 / Math.PI;
        const stretch = 1 + 30 * e * e, thin = Math.max(0.12, 1 - e * 0.92);
        const c = hot(Math.min(1, e * 1.3));
        head.attr("cx", x).attr("cy", y).attr("rx", size * stretch * 0.5 + size * 0.5).attr("ry", size * thin * 0.6)
          .attr("transform", `rotate(${dir},${x},${y})`).attr("fill", c).attr("opacity", 1 - Math.pow(e, 6));
        noodle.attr("d", d3.line().curve(d3.curveCatmullRom)(trail)).attr("stroke", c)
          .attr("stroke-width", Math.max(0.4, size * thin * 0.7)).attr("opacity", (0.25 + 0.65 * e) * (1 - Math.pow(e, 8)));
        if (t >= 1 && !over) {
          finish();
          hole.classed("flash", true);
          setTimeout(() => { hole.classed("flash", false); resolve(); }, 420);
        }
      });
    });
  }

  // fly moves to a folder; with enter it then opens it as the new centre
  function fly(d, enter) {
    FMap.auto = false;
    const [x, y] = (() => { const a = FMap.angle * Math.PI / 180; return [d.x * Math.cos(a) - d.y * Math.sin(a), d.x * Math.sin(a) + d.y * Math.cos(a)]; })();
    const k = enter ? fit * 5 : (d.depth ? fit * 2.2 : fit);
    const tr = svg.transition().duration(enter ? 700 : 900).ease(d3.easeCubicInOut).call(zoom.transform, d3.zoomIdentity.translate(0, 18).scale(k).translate(-x, -y));
    if (enter) tr.on("end", () => mapLoad(mapPath(d)));
  }

  return { init, resize, draw, paint, fly, devour, unhold, morph, relabel: drawHole };
})();
