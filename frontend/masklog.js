// Masked log: a copy of each terminal's output with secrets and identifying
// values masked, shown in a separate window that can be shared on screen
// (app_mask.go, viewer.html). This panel lives in the main window, which
// is the protected one: it alone shows the real values behind IP-1, SN-2…

const MaskLog = { counts: new Map(), viewer: false, panel: null, reveal: false };

function maskCatLabel(c) {
  const k = "mask.cat." + c;
  const v = t(k);
  return v === k ? c : v;
}

// maskSummary: "12 masked: 3 secrets, 5 IP addresses…"
function maskSummary(stats, total) {
  if (!total) return t("mask.none");
  const parts = Object.entries(stats || {}).filter(([, n]) => n > 0).sort((a, b) => b[1] - a[1])
    .map(([c, n]) => maskCatLabel(c) + " " + n);
  return t("mask.summary", { n: total, list: parts.join(", ") });
}

function maskButton() {
  const n = MaskLog.counts.get(Work.active) || 0;
  return h("button", { type: "button", class: "btn small maskbtn" + (MaskLog.viewer ? " on" : ""), title: t("mask.buttonHint"), onclick: openMaskPanel },
    t("mask.button"), n ? h("span", { class: "badge" }, String(n)) : null);
}

async function openMaskPanel() {
  const body = h("div", { class: "maskpanel" });
  MaskLog.panel = body;
  MaskLog.reveal = false;
  await renderMaskPanel();
  await dialog(t("mask.title"), body, [{ label: t("common.close"), pri: true, value: true }]);
  MaskLog.panel = null;
  MaskLog.reveal = false;
}

async function renderMaskPanel() {
  const body = MaskLog.panel;
  if (!body) return;
  let cfg, info, prot;
  try {
    [cfg, info, prot] = await Promise.all([api.MaskSettings(), api.MaskInfo(Work.active || ""), api.Protection()]);
  } catch (e) { clear(body).appendChild(h("p", { class: "err" }, errText(e))); return; }
  MaskLog.viewer = info.viewer;
  clear(body);

  // the shared window
  body.appendChild(h("div", { class: "maskrow" },
    h("div", {},
      h("strong", {}, t(info.viewer ? "mask.viewerOn" : "mask.viewerOff")),
      h("p", { class: "soft small" }, t("mask.viewerHint"))),
    h("button", { type: "button", class: "btn" + (info.viewer ? "" : " pri"), onclick: async () => {
      try { if (info.viewer) await api.CloseViewer(); else await api.OpenViewer(); }
      catch (e) { setStatus(t("mask.viewerFail", { err: errText(e) }), true); }
      setTimeout(renderMaskPanel, 300);
    } }, t(info.viewer ? "mask.closeViewer" : "mask.openViewer"))));

  // this tab
  body.appendChild(h("h3", {}, t("mask.thisTab", { tab: info.label || "—" })));
  body.appendChild(h("p", { class: "soft" }, info.label ? t("mask.lines", { n: info.lines }) + " · " + maskSummary(info.stats, info.total) : t("mask.noTab")));

  // categories
  body.appendChild(h("h3", {}, t("mask.cats")));
  const cats = h("div", { class: "maskcats" });
  for (const c of cfg.cats) {
    cats.appendChild(h("label", { class: "check", title: t("mask.catHint." + c) },
      h("input", { type: "checkbox", checked: c.on, onchange: async e => {
        try { await api.SetMaskCategory(c.cat, e.target.checked); } catch (err) { setStatus(errText(err), true); }
        renderMaskPanel();
      } }), " ", maskCatLabel(c.cat)));
  }
  body.appendChild(cats);

  // the user's rules
  body.appendChild(h("h3", {}, t("mask.rules")));
  const list = h("div", { class: "maskrules" });
  if (!cfg.rules.length) list.appendChild(h("p", { class: "muted small" }, t("mask.noRules")));
  cfg.rules.forEach((r, i) => {
    list.appendChild(h("div", { class: "maskrule" },
      h("span", { class: "kind" }, t("mask.kind." + r.kind)),
      h("code", {}, r.value),
      h("span", { class: "muted" }, r.hide ? "••••••" : (r.label || "TEXT").toUpperCase() + "-n"),
      h("button", { type: "button", class: "btn small", "aria-label": t("common.delete"), title: t("common.delete"), onclick: async () => {
        try { await api.DeleteMaskRule(i); } catch (e) { setStatus(errText(e), true); }
        renderMaskPanel();
      } }, "×")));
  });
  body.appendChild(list);
  const kind = h("select", {}, ["text", "after", "pattern"].map(k => h("option", { value: k }, t("mask.kind." + k))));
  const value = h("input", { type: "text", spellcheck: "false", autocomplete: "off", placeholder: t("mask.valuePh") });
  const label = h("input", { type: "text", spellcheck: "false", autocomplete: "off", maxlength: "20", placeholder: t("mask.labelPh"), class: "short" });
  const hide = h("input", { type: "checkbox" });
  const err = h("p", { class: "err small", hidden: true });
  kind.onchange = () => { value.placeholder = t(kind.value === "pattern" ? "mask.patternPh" : kind.value === "after" ? "mask.afterPh" : "mask.valuePh"); };
  const add = async () => {
    try {
      await api.AddMaskRule({ kind: kind.value, value: value.value, label: label.value, hide: hide.checked });
      renderMaskPanel();
    } catch (e) { err.hidden = false; err.textContent = errText(e); }
  };
  value.addEventListener("keydown", e => { if (e.key === "Enter") { e.preventDefault(); add(); } });
  body.appendChild(h("div", { class: "maskadd" }, kind, value, label,
    h("label", { class: "check", title: t("mask.hideHint") }, hide, " ", t("mask.hide")),
    h("button", { type: "button", class: "btn", onclick: add }, t("mask.add"))));
  body.appendChild(err);
  body.appendChild(h("p", { class: "muted small" }, t("mask.rulesHint")));

  // pseudonyms and their real values: hidden until asked
  body.appendChild(h("h3", {}, t("mask.mapping")));
  if (!info.mapping.length) body.appendChild(h("p", { class: "muted small" }, t("mask.noMapping")));
  else if (!MaskLog.reveal) {
    body.appendChild(h("div", { class: "maskrow" },
      h("p", { class: "soft small" }, prot.supported && !prot.active ? t("mask.revealUnprotected") : t("mask.revealHint")),
      h("button", { type: "button", class: "btn", onclick: () => { MaskLog.reveal = true; renderMaskPanel(); } }, t("mask.reveal", { n: info.mapping.length }))));
  } else {
    const table = h("table", { class: "masktable" }, h("tr", {}, h("th", {}, t("mask.colName")), h("th", {}, t("mask.colValue")), h("th", {}, t("mask.colCat"))));
    for (const p of info.mapping) table.appendChild(h("tr", {}, h("td", {}, h("code", {}, p.name)), h("td", {}, p.value), h("td", { class: "muted" }, maskCatLabel(p.cat))));
    body.appendChild(h("div", { class: "masktablebox" }, table));
    body.appendChild(h("button", { type: "button", class: "btn small", onclick: () => { MaskLog.reveal = false; renderMaskPanel(); } }, t("mask.hideValues")));
  }
  body.appendChild(h("p", { class: "muted small" }, t("mask.caveat")));
}

function initMaskLog() {
  window.runtime.EventsOn("mask:count", (id, n) => {
    const before = MaskLog.counts.get(id);
    MaskLog.counts.set(id, n);
    if (before !== n && id === Work.active) renderTabs();
  });
  window.runtime.EventsOn("viewer:state", (on, failed) => {
    MaskLog.viewer = !!on;
    if (failed) setStatus(t("mask.viewerFail", { err: t("mask.viewerQuit") }), true);
    renderTabs();
    renderMaskPanel();
  });
  window.runtime.EventsOn("mask:added", () => setStatus(t("mask.addedFromViewer")));
  window.runtime.EventsOn("mask:rules", () => renderMaskPanel());
}
