// The shared log window (internal/viewer): shows the masked logs Cmdex
// sends and nothing else. Every line goes into the page as text, never as
// HTML. It can ask Cmdex for one thing: mask a selected piece of text too.

const V = {
  dict: {}, tabs: [], logs: new Map(), // id -> {lines, stats, total}
  cur: null, follow: true, find: "", hits: [], hit: -1,
};

const bridge = () => window.go.viewer.Bridge;

function t(key, vars) {
  let s = V.dict[key];
  if (s === undefined) s = key;
  if (vars) for (const [k, v] of Object.entries(vars)) s = s.split("{" + k + "}").join(String(v));
  return s;
}

function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}

const $ = id => document.getElementById(id);

// ---- language and theme -------------------------------------------------------

async function hello(m) {
  try {
    const res = await fetch("locales/" + (m.lang === "vi" ? "vi" : "en") + ".json");
    V.dict = await res.json();
  } catch (e) { /* keep the keys */ }
  document.documentElement.lang = m.lang === "vi" ? "vi" : "en";
  applyTheme(m.theme || "graphite");
  document.title = t("viewer.title");
  if (window.runtime && window.runtime.WindowSetTitle) window.runtime.WindowSetTitle(t("viewer.title"));
  $("followLabel").textContent = t("viewer.follow");
  $("find").placeholder = t("viewer.find");
  $("maskBtn").textContent = t("viewer.maskThis");
  $("toBottom").textContent = t("viewer.toBottom");
  $("notice").textContent = t("viewer.notice");
  $("empty").textContent = t("viewer.empty");
  renderTabs();
  renderStats();
}

// ---- tabs ---------------------------------------------------------------------

function onTabs(tabs) {
  V.tabs = tabs || [];
  const ids = new Set(V.tabs.map(x => x.id));
  for (const id of [...V.logs.keys()]) if (!ids.has(id)) V.logs.delete(id); // closed in Cmdex: gone here too
  const active = V.tabs.find(x => x.active);
  let next = V.cur;
  if (V.follow && active) next = active.id;
  if (!ids.has(next)) next = active ? active.id : (V.tabs[0] ? V.tabs[0].id : null);
  renderTabs();
  if (next !== V.cur) show(next);
  else if (!V.cur) show(null);
}

function renderTabs() {
  const box = $("tabs");
  box.textContent = "";
  for (const tab of V.tabs) {
    const b = el("button", "vtab", tab.label);
    b.type = "button";
    b.setAttribute("role", "tab");
    b.setAttribute("aria-selected", String(tab.id === V.cur));
    if (tab.ended) b.appendChild(el("span", "vended", " · " + t("viewer.ended")));
    b.onclick = () => {
      V.follow = false;
      $("follow").checked = false;
      show(tab.id);
    };
    box.appendChild(b);
  }
}

function show(id) {
  V.cur = id;
  renderTabs();
  renderLog();
}

// ---- the log ------------------------------------------------------------------

const reBad = /(\b(?:down|err-disabled|errors?|failed|failure|fail|denied|refused|timed? ?out|unreachable|invalid|critical|fatal|reject(?:ed)?|unknown|incomplete|disabled|blocked)\b|%[A-Z0-9_]+-[0-3]-)/i;
const reGood = /\b(?:up|connected|success(?:ful(?:ly)?)?|established|ok|full|enabled|active|listen(?:ing)?|reachable|forwarding)\b/i;
const reMasked = /(••••••|\b[A-Z][A-Z0-9]*(?:-[A-Z0-9]+)*-\d+\b)/g;

// row draws one line: masked parts stand out; the line is coloured by the
// words it holds (errors red, links up green).
function row(text, n) {
  const r = el("div", "vline");
  if (reBad.test(text)) r.classList.add("bad");
  else if (reGood.test(text)) r.classList.add("good");
  let last = 0;
  for (const m of text.matchAll(reMasked)) {
    if (m.index > last) r.appendChild(document.createTextNode(text.slice(last, m.index)));
    r.appendChild(el("span", "mk", m[0]));
    last = m.index + m[0].length;
  }
  if (last < text.length) r.appendChild(document.createTextNode(text.slice(last)));
  if (!text) r.appendChild(document.createTextNode("​"));
  r.dataset.text = text;
  if (n > 1) r.appendChild(el("span", "vrep", t("viewer.repeat", { n })));
  r.dataset.n = String(n || 1);
  return r;
}

// group folds runs of the same line ("× 12").
function group(lines) {
  const out = [];
  for (const s of lines) {
    const p = out[out.length - 1];
    if (p && p.text === s && s.trim()) p.n++;
    else out.push({ text: s, n: 1 });
  }
  return out;
}

function atBottom() {
  const box = $("log");
  return box.scrollHeight - box.scrollTop - box.clientHeight < 40;
}

function toBottom() {
  const box = $("log");
  box.scrollTop = box.scrollHeight;
  $("toBottom").hidden = true;
}

function renderLog() {
  const box = $("log");
  box.textContent = "";
  const log = V.logs.get(V.cur);
  $("empty").hidden = !!V.tabs.length;
  const frag = document.createDocumentFragment();
  if (log) for (const g of group(log.lines)) frag.appendChild(row(g.text, g.n));
  box.appendChild(frag);
  renderStats();
  runFind(false);
  toBottom();
}

function appendLines(lines) {
  const box = $("log");
  const stay = atBottom();
  for (const s of lines) {
    const last = box.lastElementChild;
    if (last && last.dataset.text === s && s.trim()) {
      box.replaceChild(row(s, Number(last.dataset.n) + 1), last);
      continue;
    }
    box.appendChild(row(s, 1));
  }
  if (V.find) runFind(false);
  if (stay) toBottom(); else $("toBottom").hidden = false;
}

function renderStats() {
  const log = V.logs.get(V.cur);
  const box = $("stats");
  if (!log || !log.total) { box.textContent = log ? t("mask.none") : ""; return; }
  const parts = Object.entries(log.stats || {}).filter(([, n]) => n > 0).sort((a, b) => b[1] - a[1])
    .map(([c, n]) => { const k = "mask.cat." + c; const name = t(k); return (name === k ? c : name) + " " + n; });
  box.textContent = t("mask.summary", { n: log.total, list: parts.join(", ") });
}

function onLog(m, reset) {
  let log = V.logs.get(m.tab);
  if (!log || reset) { log = { lines: [] }; V.logs.set(m.tab, log); }
  if (reset) log.lines = m.lines || [];
  else log.lines.push(...(m.lines || []));
  if (log.lines.length > 20000) log.lines.splice(0, log.lines.length - 20000);
  log.stats = m.stats || {};
  log.total = m.total || 0;
  if (m.tab !== V.cur) return;
  if (reset) { const keep = !atBottom() ? $("log").scrollTop : null; renderLog(); if (keep !== null) $("log").scrollTop = keep; }
  else { appendLines(m.lines || []); renderStats(); }
}

// ---- find -----------------------------------------------------------------------

function runFind(jump) {
  V.find = $("find").value.trim().toLowerCase();
  V.hits = [];
  for (const r of $("log").children) {
    const on = !!V.find && (r.dataset.text || "").toLowerCase().includes(V.find);
    r.classList.toggle("hit", on);
    r.classList.remove("cur");
    if (on) V.hits.push(r);
  }
  $("count").textContent = V.find ? t("viewer.matches", { n: V.hits.length }) : "";
  if (jump && V.hits.length) {
    V.hit = (V.hit + 1) % V.hits.length;
    V.hits[V.hit].classList.add("cur");
    V.hits[V.hit].scrollIntoView({ block: "center" });
  } else if (!jump) V.hit = -1;
}

// ---- mask this too ----------------------------------------------------------------

function selectedText() {
  const sel = window.getSelection();
  if (!sel || sel.isCollapsed || !$("log").contains(sel.anchorNode)) return "";
  return sel.toString().replace(/​/g, "").trim();
}

function placeMaskButton() {
  const b = $("maskBtn");
  const text = selectedText();
  if (!text) { b.hidden = true; return; }
  const rect = window.getSelection().getRangeAt(0).getBoundingClientRect();
  b.hidden = false;
  b.style.left = Math.max(8, Math.min(window.innerWidth - b.offsetWidth - 8, rect.left)) + "px";
  b.style.top = Math.max(8, rect.top - b.offsetHeight - 6) + "px";
}

function toast(msg) {
  const box = $("toast");
  box.textContent = msg;
  box.classList.add("on");
  clearTimeout(toast.timer);
  toast.timer = setTimeout(() => box.classList.remove("on"), 3500);
}

async function maskThis() {
  const text = selectedText();
  $("maskBtn").hidden = true;
  if (text.length < 3 || /[\r\n]/.test(text)) { toast(t("viewer.maskShort")); return; }
  window.getSelection().removeAllRanges();
  try { await bridge().MaskThis(text); toast(t("viewer.masked")); } catch (e) { toast(String(e && e.message || e)); }
}

// ---- start ------------------------------------------------------------------------

function init() {
  applyTheme("graphite");
  window.runtime.EventsOn("view", m => {
    switch (m.t) {
      case "hello": hello(m); break;
      case "tabs": onTabs(m.tabs); break;
      case "reset": onLog(m, true); break;
      case "add": onLog(m, false); break;
    }
  });
  $("follow").addEventListener("change", e => {
    V.follow = e.target.checked;
    const active = V.tabs.find(x => x.active);
    if (V.follow && active) show(active.id);
  });
  $("find").addEventListener("input", () => runFind(false));
  $("find").addEventListener("keydown", e => { if (e.key === "Enter") runFind(true); });
  $("log").addEventListener("scroll", () => { if (atBottom()) $("toBottom").hidden = true; });
  $("toBottom").addEventListener("click", toBottom);
  document.addEventListener("selectionchange", () => requestAnimationFrame(placeMaskButton));
  $("maskBtn").addEventListener("mousedown", e => e.preventDefault()); // keep the selection
  $("maskBtn").addEventListener("click", maskThis);
  bridge().Ready();
}

window.addEventListener("DOMContentLoaded", init);
