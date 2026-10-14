// Collect: command lines from shell history and Cmdex sessions, grouped by
// template, waiting for the user's decision. Nothing reaches the library
// without it.

const Collect = { open: {}, filter: "pending" };

async function renderCollect() {
  const view = clear(document.getElementById("view-collect"));
  const [settings, groups, rules] = await Promise.all([
    api.GetCollectSettings().catch(() => null),
    api.Collected("").catch(e => { setStatus(errText(e), true); return []; }),
    api.Rules().catch(() => []),
  ]);
  const pending = groups.filter(g => g.status === "pending");
  updateCollectBadge(pending.length);

  const main = h("div", { class: "col-main" });
  main.appendChild(h("div", {}, h("h1", { style: { fontSize: "20px" } }, t("collect.title")), h("p", { class: "soft", style: { margin: "4px 0 0" } }, t("collect.lead"))));
  main.appendChild(h("ol", { class: "steps" }, [1, 2, 3, 4, 5].map(i => h("li", {}, h("b", {}, i + ". " + t("collect.step" + i)), t("collect.step" + i + "d")))));

  const shown = Collect.filter === "pending" ? pending : groups.filter(g => g.status !== "pending");
  main.appendChild(h("div", { class: "row between wrap" },
    h("div", { class: "seg", role: "group" },
      h("button", { type: "button", "aria-pressed": String(Collect.filter === "pending"), onclick: () => { Collect.filter = "pending"; renderCollect(); } }, t("collect.pending", { n: pending.length })),
      h("button", { type: "button", "aria-pressed": String(Collect.filter === "done"), onclick: () => { Collect.filter = "done"; renderCollect(); } }, t("collect.done", { n: groups.length - pending.length }))),
    h("button", { type: "button", class: "btn small", onclick: async () => { const n = await api.ScanNow().catch(e => { setStatus(errText(e), true); return 0; }); setStatus(t("collect.scanned", { n })); renderCollect(); } }, t("collect.scan"))));

  if (!shown.length) main.appendChild(h("div", { class: "empty" }, Collect.filter === "pending" ? t("collect.empty") : t("collect.emptyDone")));
  for (const g of shown) main.appendChild(groupCard(g));
  view.appendChild(main);

  const side = h("div", { class: "col-side" });
  if (settings) side.appendChild(sourcesPanel(settings));
  if (settings) side.appendChild(mergePanel(settings));
  side.appendChild(rulesPanel(rules));
  view.appendChild(side);
}

function groupCard(g) {
  const m = g.match || {};
  const pendingG = g.status === "pending";
  const pill = pendingG
    ? (m.kind === "existing" ? t("collect.dup", { name: m.name }) : m.kind === "near" ? t("collect.near", { pct: m.percent, name: m.name }) : t("collect.new"))
    : t(g.auto ? "collect.status.auto" : "collect.status." + g.status);
  const open = !!Collect.open[g.id];
  const card = h("article", { class: "group" + (pendingG ? "" : " done") },
    h("div", { class: "row between wrap" }, h("code", { class: "mono" }, g.signature),
      h("span", { class: "row" }, g.category ? h("span", { class: "tag" }, g.category) : null, h("span", { class: "pill " + (pendingG ? m.kind : g.status) }, pill))),
    h("div", { class: "row wrap muted" },
      g.total ? t("collect.count", { n: g.total, v: g.variants.length }) : t("collect.cleared"), " · ", t("collect.source", { s: g.source }),
      g.secret ? h("span", { style: { color: "var(--bad-text)" } }, " · " + t("collect.secret")) : null));
  if (open && g.variants.length) card.appendChild(h("ul", { class: "variants mono" }, g.variants.map(v => h("li", {}, h("span", {}, v.raw), h("span", { class: "muted" }, "×" + v.count)))));
  const act = (action, target) => async () => {
    try {
      const id = await api.CollectDecide(g.id, action, target || 0);
      if (action === "add" && id) setStatus(t("collect.added"));
      renderCollect(); refreshCount();
    } catch (e) { setStatus(errText(e), true); }
  };
  const row = h("div", { class: "row wrap" });
  if (pendingG) {
    if (m.kind === "existing" || m.kind === "near") row.appendChild(h("button", { type: "button", class: "btn pri small", onclick: act("merge", m.target) }, t("collect.merge")));
    if (m.kind !== "existing") row.appendChild(h("button", { type: "button", class: "btn small" + (m.kind === "new" ? " pri" : ""), onclick: act("add") }, t("collect.add")));
    row.appendChild(h("button", { type: "button", class: "btn small", onclick: act("ignore") }, t("collect.ignore")));
    row.appendChild(h("button", { type: "button", class: "btn small", onclick: () => { document.getElementById("q").value = g.variants[0] ? g.variants[0].raw : g.signature; showView("work"); Work.forced = "explain"; Work.query = document.getElementById("q").value; refreshRight(); } }, t("collect.explain")));
  } else {
    if (g.status === "added" && g.target) row.appendChild(h("button", { type: "button", class: "btn small", onclick: () => Library.open(g.target) }, t("collect.openLib")));
    row.appendChild(h("button", { type: "button", class: "btn small", onclick: act("undo") }, t("collect.undo")));
  }
  if (g.variants.length) row.appendChild(h("button", { type: "button", class: "btn ghost small", "aria-expanded": String(open), onclick: () => { Collect.open[g.id] = !open; renderCollect(); } }, open ? t("collect.hide") : t("collect.show")));
  card.appendChild(row);
  return card;
}

function sw(label, small, on, onToggle) {
  const b = h("button", { type: "button", class: "switch", role: "switch", "aria-checked": String(on), "aria-label": label });
  b.onclick = async () => { const nv = b.getAttribute("aria-checked") !== "true"; b.setAttribute("aria-checked", String(nv)); await onToggle(nv); };
  return h("div", { class: "src" }, h("span", {}, label, small ? h("small", {}, small) : null), b);
}

function sourcesPanel(s) {
  const p = h("section", { class: "panel", style: { display: "flex", flexDirection: "column", gap: "10px" } }, h("h2", {}, t("collect.sources")));
  p.appendChild(sw(t("collect.on"), null, s.enabled, v => api.SetCollectSwitch("enabled", v)));
  for (const src of s.sources) p.appendChild(sw(t("collect.src." + src.id), src.path, src.enabled, v => api.SetCollectSwitch("src." + src.id, v)));
  if (!s.sources.length) p.appendChild(h("p", { class: "note" }, t("collect.noHistory")));
  p.appendChild(sw(t("collect.src.session"), t("collect.src.sessionNote"), s.session, v => api.SetCollectSwitch("session", v)));
  p.appendChild(h("p", { class: "note" }, t("collect.privacy")));
  return p;
}

// mergePanel: the automatic merge and its similarity level, and clearing
// the collected log. Nothing is added to the library without the user.
function mergePanel(s) {
  const p = h("section", { class: "panel", style: { display: "flex", flexDirection: "column", gap: "10px" } }, h("h2", {}, t("collect.mergeTitle")));
  p.appendChild(sw(t("collect.autoMerge"), t("collect.autoMergeNote"), s.autoMerge, async v => {
    await api.SetCollectSwitch("automerge", v).catch(e => setStatus(errText(e), true));
    renderCollect();
  }));
  const levels = h("div", { class: "seg", role: "group", "aria-label": t("collect.level") },
    s.levels.map(l => h("button", { type: "button", "aria-pressed": String(l === s.mergeAt), onclick: async () => {
      await api.SetCollectLevel("mergeAt", l).catch(e => setStatus(errText(e), true));
      renderCollect();
    } }, l + "%")));
  p.appendChild(h("div", { class: "field" }, h("span", {}, t("collect.level")), levels));
  p.appendChild(h("p", { class: "note", style: { margin: "-4px 0 0" } }, t("collect.levelNote")));

  const days = h("select", { "aria-label": t("collect.autoClear") }, s.clearDays.map(d => h("option", { value: d, selected: d === s.autoClear }, d ? t("collect.days", { n: d }) : t("collect.never"))));
  days.onchange = () => api.SetCollectLevel("autoClear", Number(days.value)).catch(e => setStatus(errText(e), true));
  p.appendChild(h("label", { class: "field" }, h("span", {}, t("collect.autoClear")), days));
  p.appendChild(h("button", { type: "button", class: "btn danger small", onclick: async () => {
    if (!await confirmDialog(t("collect.clear"), t("collect.clearQ"), t("collect.clear"), true)) return;
    try { const n = await api.ClearCollected(); setStatus(t("collect.clearedN", { n })); renderCollect(); refreshCollectBadge(); }
    catch (e) { setStatus(errText(e), true); }
  } }, t("collect.clear")));
  p.appendChild(h("p", { class: "note" }, t("collect.clearNote")));
  return p;
}

function rulesPanel(rules) {
  const p = h("section", { class: "panel", style: { display: "flex", flexDirection: "column", gap: "8px" } }, h("h2", {}, t("rules.title")));
  for (const r of rules) {
    p.appendChild(h("div", { class: "rule" }, h("span", { class: "tag" }, t("rules.kind." + r.kind)), h("code", { class: "mono" }, r.match), r.result ? h("span", {}, "→ " + r.result) : null,
      h("button", { type: "button", class: "btn ghost small", "aria-label": t("rules.delete"), title: t("rules.delete"), onclick: async () => { await api.DeleteRule(r.id).catch(e => setStatus(errText(e), true)); renderCollect(); } }, "×")));
  }
  const kind = h("select", {}, ["category", "ignore", "secret"].map(k => h("option", { value: k }, t("rules.kind." + k))));
  const match = h("input", { type: "text", class: "mono", placeholder: t("rules.matchPh") });
  const result = h("input", { type: "text", placeholder: t("rules.resultPh") });
  kind.onchange = () => { result.hidden = kind.value !== "category"; };
  p.appendChild(h("div", { class: "grid2" },
    h("label", { class: "field" }, t("rules.kind"), kind), h("label", { class: "field" }, t("rules.result"), result),
    h("label", { class: "field span2" }, t("rules.match"), match)));
  p.appendChild(h("button", { type: "button", class: "btn small", onclick: async () => {
    if (!match.value.trim()) return;
    await api.SaveRule({ id: 0, kind: kind.value, match: match.value.trim(), result: kind.value === "category" ? result.value.trim() : "" }).catch(e => setStatus(errText(e), true));
    renderCollect();
  } }, t("rules.add")));
  return p;
}

function updateCollectBadge(n) {
  const b = document.getElementById("collectBadge");
  b.hidden = !n;
  b.textContent = n;
}

async function refreshCollectBadge() {
  const list = await api.Collected("pending").catch(() => []);
  updateCollectBadge(list.length);
}
