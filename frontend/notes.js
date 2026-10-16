// Notes: a chat bubble in the corner opens a list of notes, written like
// messages. A note is plain text with light markup (bold, lists, code,
// colours…) and images, drawn here by building elements one by one: the
// text is never read as HTML, so a note (or an imported backup) cannot run
// anything in the page. Notes are stored encrypted (app_notes.go).

const Notes = { list: [], query: "", open: false, attach: "", editing: 0, images: new Map(), preview: false, hits: [], hit: -1 };

const NOTE_COLORS = ["red", "orange", "yellow", "green", "blue", "purple"];
const NOTE_IMAGE_MAX = 1600; // pixels: larger images are scaled down before saving

function initNotes() {
  const fab = h("button", { type: "button", class: "notes-fab", id: "notesFab", "aria-expanded": "false", onclick: () => notesToggle() });
  // a fixed icon, written here (never note text)
  fab.innerHTML = '<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M21 12a8 8 0 0 1-11.6 7.1L4 20l1-4.6A8 8 0 1 1 21 12z"></path><path d="M8.5 11h7M8.5 14h4"></path></svg>';
  const panel = h("section", { class: "notes-panel", id: "notesPanel", hidden: true, role: "dialog" },
    h("header", {},
      h("b", { id: "notesTitle" }),
      h("div", { class: "notes-find" },
        h("span", { class: "glass", "aria-hidden": "true" }, "⌕"),
        h("input", { type: "text", id: "notesSearch", autocomplete: "off", spellcheck: "false" }),
        h("span", { class: "count", id: "notesCount", "aria-live": "polite" }),
        h("button", { type: "button", class: "arrow", id: "notesPrev", onclick: () => notesStep(-1) }, "▲"),
        h("button", { type: "button", class: "arrow", id: "notesNext", onclick: () => notesStep(1) }, "▼")),
      h("button", { type: "button", class: "ic", id: "notesClose", onclick: () => notesToggle(false) }, "×")),
    h("div", { class: "notes-list", id: "notesList" }),
    h("div", { class: "notes-compose", id: "notesCompose" }));
  document.body.appendChild(fab);
  document.body.appendChild(panel);
  const search = document.getElementById("notesSearch");
  search.oninput = debounce(() => { Notes.query = search.value; Notes.hit = -1; notesFind(true); }, 150);
  search.onkeydown = e => {
    if (e.key === "Enter") { e.preventDefault(); notesStep(e.shiftKey ? -1 : 1); }
    if (e.key === "Escape" && search.value) { e.preventDefault(); e.stopPropagation(); search.value = ""; Notes.query = ""; notesFind(); }
  };
  panel.addEventListener("keydown", e => { if (e.key === "Escape" && !Notes.editing) { e.stopPropagation(); notesToggle(false); } });
  // images dropped anywhere on the panel join the note being written
  panel.addEventListener("dragover", e => { if ([...e.dataTransfer.items].some(i => i.kind === "file")) { e.preventDefault(); panel.classList.add("drop"); } });
  panel.addEventListener("dragleave", () => panel.classList.remove("drop"));
  panel.addEventListener("drop", e => { e.preventDefault(); panel.classList.remove("drop"); notesAddFiles([...e.dataTransfer.files]); });
  document.addEventListener("keydown", e => {
    if (e.ctrlKey && e.shiftKey && !e.altKey && e.code === "KeyN") { e.preventDefault(); notesToggle(); }
    if (Notes.open && (e.ctrlKey || e.metaKey) && !e.shiftKey && e.code === "KeyF" && panel.contains(document.activeElement)) { e.preventDefault(); search.focus(); search.select(); }
  }, true);
  drawCompose();
  relabelNotes();
}

function relabelNotes() {
  const fab = document.getElementById("notesFab");
  if (!fab) return;
  fab.title = t("notes.open") + " (Ctrl+Shift+N)";
  fab.setAttribute("aria-label", t("notes.open"));
  document.getElementById("notesTitle").textContent = t("notes.title");
  document.getElementById("notesSearch").placeholder = t("notes.search");
  document.getElementById("notesPrev").title = t("notes.findPrev") + " (Shift+Enter)";
  document.getElementById("notesNext").title = t("notes.findNext") + " (Enter)";
  notesFind();
  document.getElementById("notesClose").title = t("notes.close");
  drawCompose();
  if (Notes.open) drawNotes();
}

function notesToggle(open) {
  Notes.open = open === undefined ? !Notes.open : open;
  document.getElementById("notesPanel").hidden = !Notes.open;
  document.getElementById("notesFab").setAttribute("aria-expanded", String(Notes.open));
  if (Notes.open) { notesLoad(true); setTimeout(() => { const ta = document.querySelector("#notesCompose textarea"); if (ta) ta.focus(); }, 30); }
}

async function notesLoad(toEnd) {
  try { Notes.list = await api.Notes(""); }
  catch (e) { setStatus(errText(e), true); return; }
  drawNotes(toEnd);
}

// ---- the list ---------------------------------------------------------------------

function noteDay(sec) {
  const d = new Date(sec * 1000), today = new Date();
  const same = (a, b) => a.toDateString() === b.toDateString();
  if (same(d, today)) return t("map.today");
  const y = new Date(today); y.setDate(today.getDate() - 1);
  if (same(d, y)) return t("map.yesterday");
  return d.toLocaleDateString(App.lang === "vi" ? "vi-VN" : undefined, { weekday: "long", day: "numeric", month: "long", year: d.getFullYear() === today.getFullYear() ? undefined : "numeric" });
}

function drawNotes(toEnd) {
  const box = clear(document.getElementById("notesList"));
  if (!Notes.list.length) {
    box.appendChild(h("div", { class: "notes-empty" }, t("notes.empty")));
    notesFind();
    return;
  }
  let lastDay = "";
  for (const n of Notes.list) {
    const day = noteDay(n.created);
    if (day !== lastDay) { box.appendChild(h("div", { class: "notes-day" }, day)); lastDay = day; }
    box.appendChild(noteBubble(n));
  }
  if (toEnd) box.scrollTop = box.scrollHeight;
  notesFind(!toEnd);
}

// ---- finding in the notes ----------------------------------------------------------
//
// Like Ctrl+F in a browser: every match is marked where it is, a counter
// says which one is shown ("2/7"), and the arrows (Enter, Shift+Enter) move
// through them. Case and Vietnamese accents are ignored: "ca phe" finds
// "cà phê".

// fold lowers a text and drops accents, one character for one, so places
// in the folded text are places in the text.
function noteFold(s) {
  let out = "";
  for (const c of s) {
    const f = c === "đ" || c === "Đ" ? "d" : c.normalize("NFD").replace(/[\u0300-\u036f]/g, "").toLowerCase();
    out += f.length === 1 ? f : c.toLowerCase().length === 1 ? c.toLowerCase() : c;
  }
  return out;
}

function notesClearMarks() {
  for (const m of document.querySelectorAll("#notesList mark.hit")) {
    const p = m.parentNode;
    p.replaceChild(document.createTextNode(m.textContent), m);
    p.normalize();
  }
  Notes.hits = [];
}

// notesFind marks the matches of the search; reveal shows the current one.
function notesFind(reveal) {
  const count = document.getElementById("notesCount");
  if (!count) return;
  notesClearMarks();
  const q = noteFold((Notes.query || "").trim());
  const list = document.getElementById("notesList");
  count.textContent = "";
  document.getElementById("notesPrev").disabled = document.getElementById("notesNext").disabled = true;
  if (!q) return;
  const walker = document.createTreeWalker(list, NodeFilter.SHOW_TEXT, { acceptNode: n => n.parentElement.closest(".note-body") ? NodeFilter.FILTER_ACCEPT : NodeFilter.FILTER_REJECT });
  const nodes = [];
  for (let n; (n = walker.nextNode());) nodes.push(n);
  for (const node of nodes) {
    const text = node.nodeValue, folded = noteFold(text);
    if ([...folded].length !== [...text].length) continue; // (never: fold keeps one for one)
    const chars = [...text], fchars = [...folded], fstr = fchars.join("");
    let from = 0, idx, cuts = [];
    while ((idx = fstr.indexOf(q, from)) >= 0) { cuts.push(idx); from = idx + q.length; }
    if (!cuts.length) continue;
    // indexes are in UTF-16 units of the folded string; turn them into characters
    const unitToChar = []; let u = 0;
    fchars.forEach((c, i) => { for (let k = 0; k < c.length; k++) unitToChar[u++] = i; });
    unitToChar[u] = fchars.length;
    const frag = document.createDocumentFragment();
    let at = 0;
    for (const cut of cuts) {
      const a = unitToChar[cut], b = unitToChar[cut + q.length];
      if (a > at) frag.appendChild(document.createTextNode(chars.slice(at, a).join("")));
      const mk = document.createElement("mark");
      mk.className = "hit";
      mk.textContent = chars.slice(a, b).join("");
      frag.appendChild(mk);
      Notes.hits.push(mk);
      at = b;
    }
    if (at < chars.length) frag.appendChild(document.createTextNode(chars.slice(at).join("")));
    node.parentNode.replaceChild(frag, node);
  }
  Notes.hits.sort((a, b) => a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING ? -1 : 1);
  const n = Notes.hits.length;
  if (!n) { count.textContent = t("notes.findNone"); return; }
  document.getElementById("notesPrev").disabled = document.getElementById("notesNext").disabled = false;
  if (Notes.hit < 0 || Notes.hit >= n) Notes.hit = n - 1; // the newest match first
  notesShowHit(reveal);
}

function notesShowHit(reveal) {
  const n = Notes.hits.length;
  Notes.hits.forEach((m, i) => m.classList.toggle("cur", i === Notes.hit));
  document.getElementById("notesCount").textContent = (Notes.hit + 1) + "/" + n;
  if (reveal !== false && Notes.hits[Notes.hit]) Notes.hits[Notes.hit].scrollIntoView({ block: "center", behavior: "smooth" });
}

function notesStep(d) {
  const n = Notes.hits.length;
  if (!n) return;
  Notes.hit = (Notes.hit + d + n) % n;
  notesShowHit(true);
}

function noteBubble(n) {
  const time = new Date(n.created * 1000).toLocaleTimeString(App.lang === "vi" ? "vi-VN" : undefined, { hour: "2-digit", minute: "2-digit" });
  const wrap = h("div", { class: "note-row" });
  const bubble = h("div", { class: "note-bubble" });
  if (Notes.editing === n.id) {
    const ta = h("textarea", { class: "note-edit", rows: 4 });
    ta.value = n.body;
    const save = async () => {
      try { await api.EditNote(n.id, ta.value); Notes.editing = 0; notesLoad(); }
      catch (e) { setStatus(errText(e), true); }
    };
    ta.onkeydown = e => {
      if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) { e.preventDefault(); save(); }
      if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); Notes.editing = 0; drawNotes(); }
      noteShortcut(e, ta);
    };
    bubble.appendChild(noteToolbar(ta, true));
    bubble.appendChild(ta);
    bubble.appendChild(h("div", { class: "note-edit-acts" },
      h("button", { type: "button", onclick: () => { Notes.editing = 0; drawNotes(); } }, t("common.cancel")),
      h("button", { type: "button", class: "pri", onclick: save }, t("lib.save"))));
    wrap.appendChild(bubble);
    requestAnimationFrame(() => { ta.focus(); ta.style.height = Math.min(320, ta.scrollHeight + 4) + "px"; });
    return wrap;
  }
  bubble.appendChild(renderNote(n.body, n));
  if (n.path) bubble.appendChild(h("button", { type: "button", class: "note-path", title: n.path, onclick: () => noteOpenPath(n.path) }, "📎 ", baseName(n.path)));
  bubble.appendChild(h("div", { class: "note-meta" }, time, n.updated > n.created + 1 ? " · " + t("notes.edited") : ""));
  const acts = h("div", { class: "note-acts" },
    h("button", { type: "button", title: t("notes.edit"), onclick: () => { Notes.editing = n.id; drawNotes(); } }, "✎"),
    h("button", { type: "button", title: t("common.copy"), onclick: () => { window.runtime.ClipboardSetText(n.body); setStatus(t("common.copied")); } }, "⧉"),
    h("button", { type: "button", class: "danger", title: t("notes.delete"), onclick: async () => {
      if (!await confirmDialog(t("notes.delete"), t("notes.deleteQ"), t("notes.delete"), true)) return;
      try { await api.DeleteNote(n.id); notesLoad(); } catch (e) { setStatus(errText(e), true); }
    } }, "🗑"));
  wrap.appendChild(acts);
  wrap.appendChild(bubble);
  return wrap;
}

function baseName(p) { const parts = p.split(/[\\/]/).filter(Boolean); return parts[parts.length - 1] || p; }

// noteOpenPath shows a note's file or folder on the map.
async function noteOpenPath(p) {
  showView("work");
  Work.forced = "map";
  refreshRight();
  const isWin = App.info.os === "windows", sep = isWin ? "\\" : "/";
  const dir = p.slice(0, p.lastIndexOf(sep)) || p;
  // a folder opens as the centre; a file opens its folder with it chosen
  try { await mapLoad(p, p); if (FMap.m && FMap.m.root === p) return; } catch (e) { /* a file */ }
  mapLoad(dir, p);
}

// ---- rendering the markup ---------------------------------------------------------
//
//   **bold**  *italic*  __underline__  ~~strike~~  ==highlight==  `code`
//   ::red[coloured]  [text](https://…)  ![name](cmdex-img:ID)
//   # / ## / ### headings, > quotes, - lists, 1. lists, - [ ] / - [x] tasks,
//   ```lang code blocks ```, coloured by highlight.js and only ever copied

const NOTE_INLINE = /(`[^`\n]+`)|(!\[[^\]\n]*\]\(cmdex-img:\d+\))|(\[[^\]\n]+\]\(https?:\/\/[^\s)]+\))|(\*\*[^*\n]+\*\*)|(__[^_\n]+__)|(~~[^~\n]+~~)|(==[^=\n]+==)|(::(?:red|orange|yellow|green|blue|purple)\[[^\]\n]+\])|(\*[^*\s][^*\n]*\*)|(\bhttps?:\/\/[^\s<>()]+)/g;

function renderInline(text, out) {
  let last = 0;
  NOTE_INLINE.lastIndex = 0;
  for (let m; (m = NOTE_INLINE.exec(text));) {
    if (m.index > last) out.appendChild(document.createTextNode(text.slice(last, m.index)));
    const s = m[0];
    if (m[1]) out.appendChild(h("code", {}, highlight(s.slice(1, -1), "")));
    else if (m[2]) out.appendChild(noteImage(+s.match(/cmdex-img:(\d+)/)[1], s.slice(2, s.indexOf("]"))));
    else if (m[3]) {
      const label = s.slice(1, s.indexOf("]")), href = s.slice(s.indexOf("](") + 2, -1);
      out.appendChild(h("a", { href: "#", title: href, onclick: e => { e.preventDefault(); openExternal(href); } }, label));
    } else if (m[4]) renderInline(s.slice(2, -2), out.appendChild(h("strong")));
    else if (m[5]) renderInline(s.slice(2, -2), out.appendChild(h("u")));
    else if (m[6]) renderInline(s.slice(2, -2), out.appendChild(h("s")));
    else if (m[7]) renderInline(s.slice(2, -2), out.appendChild(h("mark")));
    else if (m[8]) { const c = s.slice(2, s.indexOf("[")); renderInline(s.slice(s.indexOf("[") + 1, -1), out.appendChild(h("span", { class: "nc-" + c }))); }
    else if (m[9]) renderInline(s.slice(1, -1), out.appendChild(h("em")));
    else if (m[10]) out.appendChild(h("a", { href: "#", title: s, onclick: e => { e.preventDefault(); openExternal(s); } }, s));
    last = m.index + s.length;
    NOTE_INLINE.lastIndex = last;
  }
  if (last < text.length) out.appendChild(document.createTextNode(text.slice(last)));
  return out;
}

function noteImage(id, alt) {
  const img = h("img", { class: "note-img", alt: alt || "", loading: "lazy" });
  const show = src => { img.src = src; };
  if (Notes.images.has(id)) show(Notes.images.get(id));
  else api.NoteImage(id).then(src => { if (/^data:image\/(png|jpeg|gif|webp);base64,/.test(src)) { Notes.images.set(id, src); show(src); } })
    .catch(() => { img.replaceWith(h("span", { class: "note-missing" }, "🖼 " + (alt || t("notes.imageMissing")))); });
  img.onclick = () => { if (img.src) dialog(alt || t("notes.image"), h("img", { src: img.src, class: "note-img-big" }), [{ label: t("common.ok"), pri: true, value: true }]); };
  return img;
}

// renderNote draws a note; n, when given, lets its task boxes be ticked.
function renderNote(body, n) {
  const root = h("div", { class: "note-body" });
  const lines = body.split("\n");
  let list = null, listType = "";
  const closeList = () => { list = null; listType = ""; };
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    if (/^```/.test(line)) { // a code block, kept as it is
      const lang = line.slice(3).trim(), code = [];
      for (i++; i < lines.length && !/^```/.test(lines[i]); i++) code.push(lines[i]);
      closeList();
      const text = code.join("\n");
      // shown and copied, never run: no button sends it to a terminal
      root.appendChild(h("div", { class: "note-code" },
        h("div", { class: "codebar" }, h("span", { class: "lang" }, lang || ""),
          h("button", { type: "button", title: t("notes.copyCode"), onclick: e => { e.stopPropagation(); window.runtime.ClipboardSetText(text); setStatus(t("common.copied")); } }, "⧉ " + t("common.copy"))),
        h("pre", {}, h("code", {}, highlight(text, lang)))));
      continue;
    }
    let m;
    if ((m = line.match(/^(#{1,3})\s+(.*)$/))) { closeList(); root.appendChild(renderInline(m[2], h("h" + (m[1].length + 2)))); continue; }
    if ((m = line.match(/^>\s?(.*)$/))) { closeList(); root.appendChild(renderInline(m[1], h("blockquote"))); continue; }
    if ((m = line.match(/^\s*[-*]\s+\[([ xX])\]\s+(.*)$/))) {
      if (listType !== "task") { list = root.appendChild(h("ul", { class: "tasks" })); listType = "task"; }
      const lineNo = i, done = m[1] !== " ";
      const box = h("input", { type: "checkbox", checked: done, disabled: !n });
      if (n) box.onchange = async () => {
        const ls = n.body.split("\n");
        ls[lineNo] = ls[lineNo].replace(/\[([ xX])\]/, box.checked ? "[x]" : "[ ]");
        try { await api.EditNote(n.id, ls.join("\n")); notesLoad(); } catch (e) { setStatus(errText(e), true); }
      };
      list.appendChild(h("li", { class: done ? "done" : "" }, box, renderInline(m[2], h("span"))));
      continue;
    }
    if ((m = line.match(/^\s*[-*]\s+(.*)$/))) {
      if (listType !== "ul") { list = root.appendChild(h("ul")); listType = "ul"; }
      list.appendChild(renderInline(m[1], h("li")));
      continue;
    }
    if ((m = line.match(/^\s*\d+[.)]\s+(.*)$/))) {
      if (listType !== "ol") { list = root.appendChild(h("ol")); listType = "ol"; }
      list.appendChild(renderInline(m[1], h("li")));
      continue;
    }
    closeList();
    if (!line.trim()) { root.appendChild(h("div", { class: "gap" })); continue; }
    root.appendChild(renderInline(line, h("p")));
  }
  return root;
}

// ---- writing ----------------------------------------------------------------------

function noteWrap(ta, before, after, placeholder) {
  const s = ta.selectionStart, e = ta.selectionEnd, sel = ta.value.slice(s, e) || placeholder || "";
  ta.setRangeText(before + sel + after, s, e, "end");
  if (!ta.value.slice(s, e).length || sel === placeholder) ta.setSelectionRange(s + before.length, s + before.length + sel.length);
  ta.focus();
  ta.dispatchEvent(new Event("input"));
}

function noteLinePrefix(ta, prefix) {
  const s = ta.selectionStart, start = ta.value.lastIndexOf("\n", s - 1) + 1;
  ta.setRangeText(prefix, start, start, "end");
  ta.focus();
  ta.dispatchEvent(new Event("input"));
}

function noteShortcut(e, ta) {
  if (!(e.ctrlKey || e.metaKey) || e.altKey) return;
  const k = e.key.toLowerCase();
  if (k === "b") { e.preventDefault(); noteWrap(ta, "**", "**", t("notes.fmt.bold")); }
  if (k === "i") { e.preventDefault(); noteWrap(ta, "*", "*", t("notes.fmt.italic")); }
  if (k === "u") { e.preventDefault(); noteWrap(ta, "__", "__", t("notes.fmt.underline")); }
}

function noteToolbar(ta, compact) {
  const b = (label, title, fn, cls) => h("button", { type: "button", class: cls || "", title, onmousedown: e => e.preventDefault(), onclick: fn }, label);
  const bar = h("div", { class: "note-tools" + (compact ? " compact" : "") },
    b(h("b", {}, "B"), t("notes.fmt.bold") + " (Ctrl+B)", () => noteWrap(ta, "**", "**", t("notes.fmt.bold"))),
    b(h("i", {}, "I"), t("notes.fmt.italic") + " (Ctrl+I)", () => noteWrap(ta, "*", "*", t("notes.fmt.italic"))),
    b(h("u", {}, "U"), t("notes.fmt.underline") + " (Ctrl+U)", () => noteWrap(ta, "__", "__", t("notes.fmt.underline"))),
    b(h("s", {}, "S"), t("notes.fmt.strike"), () => noteWrap(ta, "~~", "~~", t("notes.fmt.strike"))),
    b(h("mark", {}, "H"), t("notes.fmt.mark"), () => noteWrap(ta, "==", "==", t("notes.fmt.mark"))),
    b("H₁", t("notes.fmt.heading"), () => noteLinePrefix(ta, "## ")),
    b("•", t("notes.fmt.list"), () => noteLinePrefix(ta, "- ")),
    b("1.", t("notes.fmt.numbered"), () => noteLinePrefix(ta, "1. ")),
    b("☑", t("notes.fmt.task"), () => noteLinePrefix(ta, "- [ ] ")),
    b("❝", t("notes.fmt.quote"), () => noteLinePrefix(ta, "> ")),
    b("</>", t("notes.fmt.code"), () => {
      const sel = ta.value.slice(ta.selectionStart, ta.selectionEnd);
      if (sel.includes("\n")) noteWrap(ta, "```\n", "\n```", ""); else noteWrap(ta, "`", "`", "code");
    }));
  const colors = h("span", { class: "note-colors" });
  for (const c of NOTE_COLORS) colors.appendChild(b(h("i", { class: "nc-dot nc-" + c }), t("notes.fmt.color") + ": " + t("notes.color." + c), () => noteWrap(ta, "::" + c + "[", "]", t("notes.fmt.color"))));
  bar.appendChild(colors);
  return bar;
}

function drawCompose() {
  const box = document.getElementById("notesCompose");
  if (!box) return;
  const keep = box.querySelector("textarea");
  const draft = keep ? keep.value : "";
  clear(box);
  const ta = h("textarea", { rows: 2, placeholder: t("notes.placeholder"), spellcheck: "true" });
  ta.value = draft;
  const grow = () => { ta.style.height = "auto"; ta.style.height = Math.min(220, ta.scrollHeight + 2) + "px"; ta.style.overflowY = ta.scrollHeight > 220 ? "auto" : "hidden"; drawPreview(); };
  ta.oninput = grow;
  ta.onkeydown = e => {
    if (e.key === "Enter" && !e.shiftKey && !e.isComposing) { e.preventDefault(); notesSend(ta); return; }
    noteShortcut(e, ta);
  };
  ta.onpaste = e => {
    const files = [...(e.clipboardData ? e.clipboardData.files : [])].filter(f => f.type.startsWith("image/"));
    if (files.length) { e.preventDefault(); notesAddFiles(files, ta); }
  };
  const fileIn = h("input", { type: "file", accept: "image/png,image/jpeg,image/gif,image/webp", multiple: true, hidden: true, onchange: () => { notesAddFiles([...fileIn.files], ta); fileIn.value = ""; } });
  const preview = h("div", { class: "note-preview", hidden: !Notes.preview });
  const drawPreview = () => { if (Notes.preview) { clear(preview); if (ta.value.trim()) preview.appendChild(renderNote(ta.value)); } };
  const chip = Notes.attach ? h("span", { class: "note-attach", title: Notes.attach }, "📎 ", baseName(Notes.attach),
    h("button", { type: "button", title: t("notes.detach"), onclick: () => { Notes.attach = ""; drawCompose(); } }, "×")) : null;
  const tools = noteToolbar(ta);
  box.appendChild(tools);
  box.appendChild(preview);
  box.appendChild(h("div", { class: "note-input" },
    ta,
    h("div", { class: "note-side" },
      h("button", { type: "button", class: "ic", title: t("notes.addImage"), onclick: () => fileIn.click() }, "🖼"),
      h("button", { type: "button", class: "ic", title: t("notes.attach"), onclick: () => { const p = noteMapPath(); if (p) { Notes.attach = p; drawCompose(); } else setStatus(t("notes.attachNone")); } }, "📎"),
      h("button", { type: "button", class: "ic" + (Notes.preview ? " on" : ""), title: t("notes.preview"), onclick: () => { Notes.preview = !Notes.preview; drawCompose(); } }, "👁"),
      h("button", { type: "button", class: "send", title: t("notes.send") + " (Enter)", onclick: () => notesSend(ta) }, "➤"))),
    fileIn);
  if (chip) box.appendChild(chip);
  box.appendChild(h("div", { class: "note-hint" }, t("notes.hint")));
  requestAnimationFrame(grow);
}

// noteMapPath is what the map shows chosen (or its centre), to pin a note to.
function noteMapPath() {
  if (typeof FMap === "undefined" || !FMap.m) return "";
  return FMap.sel && !FMap.sel.data.more ? mapPath(FMap.sel) : FMap.m.root;
}

async function notesSend(ta) {
  const body = ta.value;
  if (!body.trim()) return;
  try {
    await api.AddNote(body, Notes.attach);
    ta.value = "";
    Notes.attach = "";
    drawCompose();
    await notesLoad(true);
    const nta = document.querySelector("#notesCompose textarea");
    if (nta) nta.focus();
  } catch (e) { setStatus(errText(e), true); }
}

// notesAddFiles scales images down (a photo of several MB becomes a few
// hundred KB), stores them and puts them in the text being written.
async function notesAddFiles(files, ta) {
  ta = ta || document.querySelector("#notesCompose textarea");
  for (const f of files) {
    if (!/^image\/(png|jpeg|gif|webp)$/.test(f.type)) { setStatus(t("notes.notImage", { name: f.name }), true); continue; }
    try {
      const b64 = await noteShrink(f);
      const id = await api.AddNoteImage(b64);
      const name = (f.name || "image").replace(/[\[\]\n]/g, "").slice(0, 60);
      const s = ta.selectionStart, pre = s > 0 && ta.value[s - 1] !== "\n" ? "\n" : "";
      ta.setRangeText(pre + "![" + name + "](cmdex-img:" + id + ")\n", s, ta.selectionEnd, "end");
      ta.dispatchEvent(new Event("input"));
      ta.focus();
    } catch (e) { setStatus(errText(e), true); }
  }
}

function noteShrink(file) {
  const readB64 = blob => new Promise((res, rej) => { const r = new FileReader(); r.onload = () => res(String(r.result).split(",")[1]); r.onerror = rej; r.readAsDataURL(blob); });
  // GIFs keep their animation; small images stay as they are
  if (file.type === "image/gif" || file.size < 400 * 1024) return readB64(file);
  return new Promise((res, rej) => {
    const url = URL.createObjectURL(file), img = new Image();
    img.onload = () => {
      const k = Math.min(1, NOTE_IMAGE_MAX / Math.max(img.width, img.height));
      const c = document.createElement("canvas");
      c.width = Math.round(img.width * k); c.height = Math.round(img.height * k);
      c.getContext("2d").drawImage(img, 0, 0, c.width, c.height);
      URL.revokeObjectURL(url);
      const type = file.type === "image/png" ? "image/png" : "image/jpeg";
      c.toBlob(b => b ? readB64(b).then(res, rej) : rej(new Error("image")), type, .86);
    };
    img.onerror = () => { URL.revokeObjectURL(url); rej(new Error(t("notes.notImage", { name: file.name }))); };
    img.src = url;
  });
}
