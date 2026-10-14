// Library: the user's commands (starter packs included) and the
// explanations used to break commands down. All of it can be edited.

const Library = {
  cmds: [], sel: null, draft: null, filter: "",

  async open(id) {
    Library.sel = id;
    Library.draft = null;
    showView("library");
  },

  // newFromText starts a new command from a typed or pasted line.
  async newFromText(text) {
    const tpl = (text || "").trim();
    Library.sel = null;
    Library.draft = { id: 0, name: "", platform: App.info.os === "windows" ? "windows" : "linux", shell: "", category: "", template: tpl, purpose: "", notes: "", danger: 0, tags: [], envs: [], params: [], uses: 0, source: "user" };
    Library.draft.params = await api.TemplateParams(tpl, []).catch(() => []);
    showView("library");
  },

  async editExplain(program) {
    const e = await api.GetExplain(program);
    explainDialog(e);
  },
};

async function renderLibrary() {
  const view = clear(document.getElementById("view-library"));
  Library.cmds = await api.Commands().catch(e => { setStatus(errText(e), true); return []; });
  refreshCount();
  let cur = Library.draft || Library.cmds.find(c => c.id === Library.sel);
  if (!cur && Library.cmds.length) { cur = Library.cmds[0]; Library.sel = cur.id; }

  // list
  const list = h("section", { class: "liblist panel" });
  const filter = h("input", { type: "text", value: Library.filter, placeholder: t("lib.filter"), "aria-label": t("lib.filter") });
  const items = h("div", { class: "libitems" });
  const drawItems = () => {
    clear(items);
    const f = Library.filter.toLowerCase();
    const shown = Library.cmds.filter(c => shownByFilter(c) && (!f || (c.name + " " + c.template + " " + c.category + " " + c.tags.join(" ")).toLowerCase().includes(f)));
    for (const c of shown) {
      items.appendChild(h("button", { type: "button", class: "libitem", "aria-pressed": String(!Library.draft && c.id === Library.sel), onclick: () => { Library.sel = c.id; Library.draft = null; renderLibrary(); } },
        h("span", { class: "row between" }, h("b", {}, c.name), h("span", { class: "os " + c.platform }, osLabel(c.platform))),
        h("code", { class: "mono" }, c.template),
        h("span", { class: "muted" }, [c.category, t("lib.uses", { n: c.uses })].filter(Boolean).join(" · "))));
    }
    if (!shown.length) items.appendChild(h("p", { class: "muted" }, t("lib.noneMatch")));
  };
  filter.oninput = () => { Library.filter = filter.value; drawItems(); };
  const inFilter = Library.cmds.filter(shownByFilter).length;
  list.appendChild(h("div", { class: "row between wrap" }, h("h2", {}, t("lib.title")),
    h("span", { class: "muted" }, inFilter === Library.cmds.length ? t("lib.count", { n: Library.cmds.length }) : t("lib.countFiltered", { n: inFilter, total: Library.cmds.length }))));
  list.appendChild(filterBar(renderLibrary));
  list.appendChild(h("div", { class: "row wrap" },
    h("button", { type: "button", class: "btn pri small", onclick: () => Library.newFromText("") }, t("lib.new")),
    h("button", { type: "button", class: "btn small", onclick: importAny }, t("lib.import")),
    h("button", { type: "button", class: "btn small", onclick: exportMenu }, t("lib.export")),
    h("button", { type: "button", class: "btn ghost small", onclick: reimport }, t("lib.reimport"))));
  list.appendChild(filter);
  list.appendChild(items);
  drawItems();
  view.appendChild(list);

  view.appendChild(cur ? editor(cur) : h("section", { class: "editor panel" }, h("p", { class: "soft" }, t("lib.empty"))));
}

function editor(c0) {
  const c = JSON.parse(JSON.stringify(c0));
  const box = h("section", { class: "editor panel" });
  const field = (label, el, cls) => h("label", { class: "field" + (cls ? " " + cls : "") }, label, el);
  const text = (key, opts) => { const el = h("input", Object.assign({ type: "text", value: c[key] || "" }, opts || {})); el.oninput = () => { c[key] = el.value; }; return el; };
  const area = key => { const el = h("textarea", { rows: 3 }); el.value = c[key] || ""; el.oninput = () => { c[key] = el.value; }; return el; };
  const select = (key, opts) => { const el = h("select", {}, opts.map(([v, l]) => h("option", { value: v, selected: String(c[key]) === String(v) }, l))); el.onchange = () => { c[key] = typeof c[key] === "number" ? Number(el.value) : el.value; }; return el; };

  box.appendChild(h("div", { class: "row between wrap" }, h("h2", {}, c.id ? t("lib.edit") : t("lib.new")), originNote(c)));
  const tpl = text("template", { class: "mono" });
  const paramsBox = h("div", {});
  const drawParams = () => {
    clear(paramsBox);
    paramsBox.appendChild(h("div", { class: "row between" }, h("b", {}, t("lib.params")), h("span", { class: "muted" }, c.params.length ? t("lib.paramsFound", { n: c.params.length }) : t("lib.paramsNone"))));
    if (!c.params.length) return;
    const rows = c.params.map(p => {
      const inp = (k, cls) => { const el = h("input", { type: "text", value: p[k] || "", class: cls || "", "aria-label": t("lib.p." + k) + " " + p.key }); el.oninput = () => { p[k] = el.value; }; return el; };
      const type = h("select", { "aria-label": t("lib.p.type") + " " + p.key }, ["text", "number", "host", "path", "secret"].map(x => h("option", { value: x, selected: p.type === x }, t("lib.type." + x))));
      type.onchange = () => { p.type = type.value; };
      const req = h("input", { type: "checkbox", checked: p.required, "aria-label": t("lib.p.required") + " " + p.key });
      req.onchange = () => { p.required = req.checked; };
      return h("tr", {}, h("td", { class: "mono", style: { color: "var(--code)" } }, p.key), h("td", {}, type), h("td", {}, req), h("td", {}, inp("default", "mono")), h("td", {}, inp("description")));
    });
    paramsBox.appendChild(h("div", { class: "tablewrap" }, h("table", { class: "params" },
      h("thead", {}, h("tr", {}, ["key", "type", "required", "default", "description"].map(k => h("th", { scope: "col" }, t("lib.p." + k))))),
      h("tbody", {}, rows))));
  };
  tpl.oninput = debounce(async () => { c.template = tpl.value; c.params = await api.TemplateParams(c.template, c.params).catch(() => c.params); drawParams(); }, 250);

  box.appendChild(h("div", { class: "grid3" },
    field(t("lib.name"), text("name"), "span3"),
    field(t("lib.platform"), select("platform", [["windows", "Windows"], ["linux", "Linux"], ["both", "Windows + Linux"], ["cisco", "Cisco"], ["pfsense", "pfSense"]])),
    field("Shell", text("shell", { placeholder: "PowerShell, cmd, bash…" })),
    field(t("lib.category"), text("category")),
    h("label", { class: "field span3" }, t("lib.template"), tpl, h("span", { class: "muted" }, t("lib.templateHint")))));
  drawParams();
  box.appendChild(paramsBox);
  const tags = h("input", { type: "text", value: (c.tags || []).join(", ") });
  tags.oninput = () => { c.tags = tags.value.split(",").map(s => s.trim()).filter(Boolean); };
  const envs = h("input", { type: "text", value: (c.envs || []).join(", "), placeholder: t("lib.envsPh") });
  envs.oninput = () => { c.envs = envs.value.split(",").map(s => s.trim()).filter(Boolean); };
  box.appendChild(h("div", { class: "grid2" },
    field(t("lib.purpose"), area("purpose")), field(t("lib.notes"), area("notes")),
    field(t("lib.tags"), tags), field(t("lib.envs"), envs),
    field(t("lib.safety"), select("danger", [[0, t("lib.safety0")], [1, t("lib.safety1")], [2, t("lib.safety2")]]))));

  const save = async () => {
    try {
      const id = await api.SaveCommand(c);
      Library.sel = id; Library.draft = null;
      setStatus(t("lib.saved"));
      renderLibrary();
    } catch (e) { setStatus(errText(e), true); }
  };
  box.appendChild(h("div", { class: "row between wrap" },
    h("div", { class: "row wrap" },
      h("button", { type: "button", class: "btn pri", onclick: save }, t("lib.save")),
      c.id ? h("button", { type: "button", class: "btn", onclick: () => { Library.draft = Object.assign(JSON.parse(JSON.stringify(c)), { id: 0, name: c.name + " (2)", source: "user" }); renderLibrary(); } }, t("lib.duplicate")) : null,
      h("button", { type: "button", class: "btn", onclick: () => { showView("work"); document.getElementById("q").value = fillTemplate(c.template, Object.fromEntries(c.params.map(p => [p.key, p.default]))); Work.forced = "explain"; Work.query = document.getElementById("q").value; refreshRight(); } }, t("lib.explain")),
      h("button", { type: "button", class: "btn ghost", onclick: () => Library.editExplain(explainKey(c.platform, c.template.trim().split(/\s+/)[0] || "")) }, t("lib.editExplain"))),
    c.id ? h("button", { type: "button", class: "btn danger", onclick: async () => {
      if (!await confirmDialog(t("lib.delete"), t("lib.deleteQ", { name: c.name }), t("lib.delete"), true)) return;
      await api.DeleteCommand(c.id).catch(e => setStatus(errText(e), true));
      Library.sel = null; renderLibrary();
    } }, t("lib.delete")) : null));
  return box;
}

// explainDialog edits what Cmdex says about one program.
async function explainDialog(e) {
  e.flags = e.flags || {}; e.letters = e.letters || {}; e.args = e.args || [];
  const summary = h("input", { type: "text", value: e.summary || "" });
  const rows = Object.entries(e.flags).map(([k, f]) => ({ k, text: f.text, value: !!f.value }));
  const tbody = h("tbody", {});
  const draw = () => {
    clear(tbody);
    rows.forEach((r, i) => {
      const k = h("input", { type: "text", class: "mono", value: r.k, "aria-label": t("ex.option") }); k.oninput = () => { r.k = k.value.trim(); };
      const tx = h("input", { type: "text", value: r.text, "aria-label": t("ex.meaning") }); tx.oninput = () => { r.text = tx.value; };
      const v = h("input", { type: "checkbox", checked: r.value, "aria-label": t("ex.takesValue") }); v.onchange = () => { r.value = v.checked; };
      tbody.appendChild(h("tr", {}, h("td", {}, k), h("td", {}, tx), h("td", {}, v), h("td", {}, h("button", { type: "button", class: "btn ghost small", onclick: () => { rows.splice(i, 1); draw(); } }, "×"))));
    });
  };
  draw();
  const letters = h("input", { type: "text", class: "mono", value: Object.entries(e.letters).map(([k, v]) => k + "=" + v).join("; ") });
  const args = h("input", { type: "text", value: e.args.join("; ") });
  const ok = await dialog(t("ex.title", { p: e.cmd }), [
    h("label", { class: "field" }, t("ex.summary"), summary),
    h("div", { class: "tablewrap" }, h("table", { class: "params" }, h("thead", {}, h("tr", {}, h("th", {}, t("ex.option")), h("th", {}, t("ex.meaning")), h("th", {}, t("ex.takesValue")), h("th", {}))), tbody)),
    h("button", { type: "button", class: "btn small", onclick: () => { rows.push({ k: "-", text: "", value: false }); draw(); } }, t("ex.addOption")),
    h("label", { class: "field" }, t("ex.letters"), letters),
    h("label", { class: "field" }, t("ex.args"), args),
    h("p", { class: "note" }, t("ex.hint")),
  ], [{ label: t("common.cancel"), value: false }, { label: t("lib.save"), pri: true, value: true }]);
  if (!ok) return;
  const out = { cmd: e.cmd, summary: summary.value, flags: {}, letters: {}, args: args.value.split(";").map(s => s.trim()).filter(Boolean), source: "user" };
  for (const r of rows) if (r.k && r.k !== "-") out.flags[r.k] = { text: r.text, value: r.value };
  for (const part of letters.value.split(";")) { const [k, ...v] = part.split("="); if (k && k.trim().length === 1 && v.length) out.letters[k.trim()] = v.join("=").trim(); }
  try { await api.SaveExplain(out); setStatus(t("lib.saved")); if (App.view === "work") refreshRight(); }
  catch (err) { setStatus(errText(err), true); }
}

// importAny loads a full backup (.json), a Cmdex database (.db) or a pack:
// it shows what is in the file and asks whether to merge or replace.
async function importAny() {
  let pv;
  try { pv = await api.PickImport(); } catch (e) { setStatus(errText(e), true); return; }
  if (!pv || !pv.path) return;
  const body = [
    h("p", { class: "soft" }, t("io.kind." + pv.kind)),
    h("p", { class: "mono", style: { wordBreak: "break-all", fontSize: "12px", margin: 0 } }, pv.path),
    h("ul", { class: "soft", style: { margin: 0, paddingLeft: "18px", lineHeight: 1.7 } },
      h("li", {}, t("io.count.commands", { n: pv.commands })),
      h("li", {}, t("io.count.explains", { n: pv.explains })),
      pv.kind !== "pack" ? [h("li", {}, t("io.count.connections", { n: pv.connections })), h("li", {}, t("io.count.collected", { n: pv.collected }))] : null),
    pv.exported ? h("p", { class: "note" }, t("io.exported", { when: new Date(pv.exported).toLocaleString(), app: pv.app || "?" })) : null,
    pv.kind !== "pack" ? h("p", { class: "note" }, t("io.modes")) : null,
  ];
  const buttons = [{ label: t("common.cancel"), value: null }];
  if (pv.kind !== "pack") buttons.push({ label: t("io.replace"), danger: true, value: "replace" });
  buttons.push({ label: t("io.merge"), pri: true, value: "merge" });
  const mode = await dialog(t("io.importTitle"), body, buttons);
  if (!mode) return;
  if (mode === "replace" && !await confirmDialog(t("io.replace"), t("io.replaceQ"), t("io.replace"), true)) return;
  try {
    const r = await api.ImportFrom(pv.path, mode === "replace");
    await dialog(t("io.doneTitle"), h("p", { class: "soft" }, t("io.done", r)), [{ label: t("common.ok"), pri: true, value: true }]);
    if (pv.kind !== "pack") location.reload(); // language, theme and every list may have changed
    else renderLibrary();
  } catch (e) { setStatus(errText(e), true); }
}

async function exportMenu() {
  const kind = await dialog(t("io.exportTitle"), [
    h("p", { class: "soft" }, h("b", {}, t("io.exp.backup")), " — ", t("io.exp.backupD")),
    h("p", { class: "soft" }, h("b", {}, t("io.exp.db")), " — ", t("io.exp.dbD")),
    h("p", { class: "soft" }, h("b", {}, t("io.exp.pack")), " — ", t("io.exp.packD")),
    h("p", { class: "note" }, t("io.noPasswords")),
  ], [
    { label: t("common.cancel"), value: null },
    { label: t("io.exp.pack"), value: "pack" },
    { label: t("io.exp.db"), value: "db" },
    { label: t("io.exp.backup"), pri: true, value: "backup" },
  ]);
  if (!kind) return;
  try {
    const fn = { backup: api.ExportBackupFile, db: api.ExportDatabaseFile, pack: api.ExportPackFile }[kind];
    const path = await fn();
    if (path) setStatus(t("lib.exported", { p: path }));
  } catch (e) { setStatus(errText(e), true); }
}

async function reimport() {
  if (!await confirmDialog(t("lib.reimport"), t("lib.reimportQ"), t("lib.reimport"))) return;
  try { const r = await api.ReimportStarter(); setStatus(t("lib.restored", { n: r.added })); renderLibrary(); }
  catch (e) { setStatus(errText(e), true); }
}

// originNote says where a command comes from: a default shipped with Cmdex
// (updated with it; saving makes it the user's), or the user's copy of one.
function originNote(c) {
  const pack = c.source && c.source.startsWith("pack:") ? c.source.slice(5) : "";
  if (c.base) return h("span", { class: "muted", title: t("lib.defaultHint") }, t("lib.isDefault", { p: pack }));
  if (c.baseKey) return h("span", { class: "muted", title: t("lib.editedHint") }, t("lib.editedDefault", { p: pack }));
  return pack ? h("span", { class: "muted" }, t("lib.fromPack", { p: pack })) : null;
}

async function refreshCount() {
  try {
    const n = Library.cmds.length || (await api.Commands()).length;
    document.getElementById("libCount").textContent = t("lib.count", { n });
  } catch (e) { /* ignore */ }
}
