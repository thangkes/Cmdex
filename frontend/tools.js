// Tool packs: built-in commands of programs such as nmap or python. Cmdex
// offers one when it finds its program on PATH (looking at file names only),
// and the user turns packs on or off under Library › Tool packs.

const Tools = { asked: new Set(), last: 0 };

function toolTitle(p) {
  return p.title + " · " + t("tools.commands", { n: p.commands }) + (App.lang === "en" ? "" : " · " + t("tools.translated", { n: p.translated, total: p.commands }));
}

// offerToolPacks asks about each tool pack whose program turned up, once
// per session; "later" asks again at the next start.
async function offerToolPacks() {
  if (Date.now() - Tools.last < 30000 || !document.getElementById("modal").hidden) return;
  Tools.last = Date.now();
  const offers = (await api.NewToolPacks().catch(() => [])).filter(p => !Tools.asked.has(p.name));
  for (const p of offers) {
    Tools.asked.add(p.name);
    const where = Object.values(p.found)[0] || "";
    const v = await dialog(t("tools.offerTitle", { name: p.title }), [
      h("p", { class: "soft" }, t("tools.offerText", { name: p.title, n: p.commands })),
      h("p", { class: "mono", style: { wordBreak: "break-all", fontSize: "12px", margin: 0 } }, where),
      h("p", { class: "note" }, t("tools.origin", { o: p.origin })),
    ], [{ label: t("tools.never"), value: "never" }, { label: t("common.later"), value: null }, { label: t("tools.add"), pri: true, value: "add" }]);
    try {
      if (v === "add") { await api.SetToolPack(p.name, true); setStatus(t("tools.added", { name: p.title, n: p.commands })); afterToolChange(); }
      else if (v === "never") await api.DeclineToolPack(p.name);
    } catch (e) { setStatus(errText(e), true); }
  }
}

function afterToolChange() {
  Library.cmds = [];
  if (App.view === "library") renderLibrary(); else refreshCount();
  if (App.view === "work") refreshRight();
}

// toolPacksDialog lists every tool pack with a switch.
async function toolPacksDialog() {
  const list = await api.ToolPacks().catch(e => { setStatus(errText(e), true); return null; });
  if (!list) return;
  const body = [h("p", { class: "soft" }, t("tools.lead"))];
  if (!list.length) body.push(h("div", { class: "empty" }, t("tools.none")));
  for (const p of list) {
    const found = Object.values(p.found);
    body.push(sw(toolTitle(p), found.length ? t("tools.found", { p: found.join(", ") }) : t("tools.notFound", { p: p.tools.join(", ") }), p.on, async on => {
      try { await api.SetToolPack(p.name, on); setStatus(t(on ? "tools.added" : "tools.removed", { name: p.title, n: p.commands })); afterToolChange(); }
      catch (e) { setStatus(errText(e), true); }
    }));
  }
  if (list.length) body.push(h("p", { class: "note" }, t("tools.note")));
  await dialog(t("tools.title"), h("div", { style: { display: "grid", gap: "12px" } }, body), [{ label: t("common.ok"), pri: true, value: true }]);
}

// a program installed while Cmdex runs is noticed when the window comes back
window.addEventListener("focus", () => { if (App.info && !App.info.locked) offerToolPacks(); });
