// Shared helpers: the Go bridge, translations, safe DOM building, dialogs.
// Every piece of text from the library, the history or a server goes into
// the page as text nodes, never as HTML.

const api = new Proxy({}, {
  get: (_, name) => (...args) => window.go.app.App[name](...args),
});

const App = {
  info: null, lang: "en", dict: {}, theme: "graphite", termTheme: null,
  view: "work", commandCount: 0,
};

function t(key, vars) {
  let s = App.dict[key];
  if (s === undefined) s = key;
  if (vars) for (const [k, v] of Object.entries(vars)) s = s.split("{" + k + "}").join(String(v));
  return s;
}

async function loadLang(lang) {
  const res = await fetch("locales/" + lang + ".json");
  App.dict = await res.json();
  App.lang = lang;
  document.documentElement.lang = lang;
  document.querySelectorAll("[data-i18n]").forEach(el => { el.textContent = t(el.dataset.i18n); });
  document.querySelectorAll("[data-i18n-ph]").forEach(el => { el.placeholder = t(el.dataset.i18nPh); });
}

// h("div", {class: "x", onclick: fn}, "text", child, [more children])
function h(tag, attrs, ...children) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v === undefined || v === null || v === false) continue;
    if (k.startsWith("on") && typeof v === "function") el.addEventListener(k.slice(2), v);
    else if (k === "class") el.className = v;
    else if (k === "style" && typeof v === "object") Object.assign(el.style, v);
    else if (k === "value") el.value = v;
    else if (k === "checked") el.checked = !!v;
    else if (k === "selected") el.selected = !!v;
    else el.setAttribute(k, v === true ? "" : v);
  }
  append(el, children);
  return el;
}

function append(el, children) {
  for (const c of children.flat(Infinity)) {
    if (c === null || c === undefined || c === false) continue;
    el.appendChild(c instanceof Node ? c : document.createTextNode(String(c)));
  }
}

function clear(el) { while (el.firstChild) el.removeChild(el.firstChild); return el; }

function setStatus(msg, isErr) {
  const el = document.getElementById("statusMsg");
  el.textContent = msg || "";
  el.classList.toggle("err", !!isErr);
  clearTimeout(setStatus.timer);
  if (msg) setStatus.timer = setTimeout(() => { el.textContent = ""; }, 6000);
}

function errText(e) { return (e && (e.message || e.toString())) || String(e); }

// dialog(title, body nodes, buttons [{label, pri, danger, value}]) resolves
// with the clicked button's value (null when closed with Escape).
function dialog(title, body, buttons) {
  const modal = document.getElementById("modal");
  const box = clear(document.getElementById("dialog"));
  return new Promise(resolve => {
    const close = v => { modal.hidden = true; document.removeEventListener("keydown", onKey, true); resolve(v); };
    const onKey = e => { if (e.key === "Escape") { e.stopPropagation(); close(null); } };
    document.addEventListener("keydown", onKey, true);
    box.appendChild(h("h2", {}, title));
    append(box, [body]);
    const acts = h("div", { class: "actions" });
    for (const b of buttons) {
      acts.appendChild(h("button", { type: "button", class: "btn" + (b.pri ? " pri" : "") + (b.danger ? " danger" : ""), onclick: () => close(typeof b.value === "function" ? b.value() : b.value) }, b.label));
    }
    box.appendChild(acts);
    modal.hidden = false;
    const first = box.querySelector("input, select, textarea") || acts.lastChild;
    if (first) first.focus();
  });
}

function confirmDialog(title, text, okLabel, danger) {
  return dialog(title, h("p", { class: "soft" }, text), [
    { label: t("common.cancel"), value: false },
    { label: okLabel || t("common.ok"), pri: !danger, danger, value: true },
  ]);
}

// Same rules as the Go side: {key} with letters, digits, _ and -.
function fillTemplate(tpl, values) {
  return tpl.replace(/\{([A-Za-z0-9_-]+)\}/g, (m, k) => (values[k] !== undefined && values[k] !== "" ? values[k] : m));
}

function b64ToBytes(b64) {
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

function textToB64(s) {
  const bytes = new TextEncoder().encode(s);
  let bin = "";
  for (let i = 0; i < bytes.length; i += 0x8000) bin += String.fromCharCode.apply(null, bytes.subarray(i, i + 0x8000));
  return btoa(bin);
}

function debounce(fn, ms) {
  let timer;
  return (...a) => { clearTimeout(timer); timer = setTimeout(() => fn(...a), ms); };
}

function osLabel(p) { return { windows: "Windows", linux: "Linux", cisco: "Cisco", pfsense: "pfSense" }[p] || "Win + Linux"; }

// The platform filter: which systems' commands are shown (empty: all), as
// store.Shown decides. Commands keep their platform badge.
const FILTER_PLATFORMS = ["windows", "linux", "cisco", "pfsense"];
function shownByFilter(c) {
  const f = (App.info && App.info.filter) || [];
  return !f.length || f.includes(c.platform) || (c.platform === "both" && (f.includes("windows") || f.includes("linux")));
}

// filterBar draws the filter: "All" or any set of systems; onChange runs
// after it is saved.
function filterBar(onChange) {
  const f = (App.info && App.info.filter) || [];
  const set = async next => {
    if (next.length === FILTER_PLATFORMS.length) next = [];
    try { await api.SetPlatformFilter(next); App.info.filter = next; } catch (e) { setStatus(errText(e), true); return; }
    onChange();
  };
  return h("div", { class: "row wrap filterbar" },
    h("span", { class: "muted" }, t("filter.label")),
    h("div", { class: "seg", role: "group", "aria-label": t("filter.label") },
      h("button", { type: "button", "aria-pressed": String(!f.length), onclick: () => set([]) }, t("filter.all")),
      FILTER_PLATFORMS.map(p => h("button", { type: "button", "aria-pressed": String(f.includes(p)), onclick: () => set(f.includes(p) ? f.filter(x => x !== p) : f.concat(p)) }, osLabel(p)))));
}

// explainKey: devices keep their own explanations ("cisco:show"), as the
// app does (store.ExplainKey).
function explainKey(platform, program) { return platform === "cisco" || platform === "pfsense" ? platform + ":" + program.toLowerCase() : program; }
