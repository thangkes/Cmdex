// Start-up: settings, language, theme, views, update offer.

function showView(view) {
  App.view = view;
  for (const v of ["work", "collect", "library"]) document.getElementById("view-" + v).hidden = v !== view;
  document.querySelectorAll(".nav button").forEach(b => b.setAttribute("aria-current", b.dataset.view === view ? "page" : "false"));
  if (view === "collect") renderCollect();
  if (view === "library") renderLibrary();
  if (view === "work") {
    const s = Work.sessions.get(Work.active);
    if (s) requestAnimationFrame(() => { try { s.fit.fit(); } catch (e) {} });
  }
}

function setTheme(id) {
  App.theme = id;
  App.termTheme = applyTheme(id);
  for (const s of Work.sessions.values()) s.term.options.theme = App.termTheme;
  const box = clear(document.getElementById("themes"));
  box.setAttribute("aria-label", t("theme.label"));
  for (const th of THEMES) {
    box.appendChild(h("button", { type: "button", "aria-pressed": String(th.id === id), title: th.name[App.lang] || th.name.en, "aria-label": th.name[App.lang] || th.name.en, onclick: () => { setTheme(th.id); api.SetSetting("theme", th.id); } },
      h("span", { style: { background: `linear-gradient(135deg, ${th.v.bg} 0 50%, ${th.v.accent} 50% 100%)`, border: "1px solid " + th.v["line-3"] } })));
  }
}

async function setLang(lang) {
  await loadLang(lang);
  document.querySelectorAll("#langSeg button").forEach(b => b.setAttribute("aria-pressed", String(b.dataset.lang === lang)));
  document.getElementById("langSeg").setAttribute("aria-label", t("lang.label"));
  document.getElementById("q").placeholder = t("ask.placeholder");
  document.getElementById("dbPath").textContent = t("status.db", { p: App.info.dbPath });
  setTheme(App.theme); // theme names are translated
  refreshProtection();
  relabelWork();
  if (App.view === "collect") renderCollect();
  if (App.view === "library") renderLibrary();
  refreshCount();
}

function offerUpdate(r) {
  const b = document.getElementById("updateBtn");
  b.hidden = false;
  b.textContent = t("update.button", { v: r.version });
  b.onclick = async () => {
    const ok = await dialog(t("update.title", { v: r.version }), [
      h("p", { class: "soft" }, t("update.text", { cur: App.info.version, v: r.version })),
      r.notes ? h("pre", { class: "preview", style: { maxHeight: "240px", overflow: "auto", color: "var(--text-2)" } }, r.notes) : null,
    ], [{ label: t("common.later"), value: false }, { label: t("update.install"), pri: true, value: true }]);
    if (!ok) return;
    b.disabled = true;
    b.textContent = t("update.downloading");
    try { await api.ApplyUpdate(); }
    catch (e) { setStatus(t("update.fail", { err: errText(e) }), true); b.disabled = false; b.textContent = t("update.button", { v: r.version }); }
  };
}

async function start() {
  App.info = await api.Init();
  App.theme = App.info.theme || "graphite";
  App.termTheme = applyTheme(App.theme);
  await loadLang(App.info.lang || "en");
  document.getElementById("ver").textContent = App.info.version === "dev" ? "dev" : "v" + App.info.version;
  document.getElementById("env").textContent = `${App.info.hostname} · ${App.info.os === "windows" ? "Windows" : "Linux"}`;
  document.querySelectorAll(".nav button").forEach(b => b.addEventListener("click", () => showView(b.dataset.view)));
  document.querySelectorAll("#langSeg button").forEach(b => b.addEventListener("click", async () => { await api.SetSetting("lang", b.dataset.lang).catch(e => setStatus(errText(e), true)); setLang(b.dataset.lang); }));
  if (App.info.dbError) {
    await dialog(t("db.errorTitle"), h("p", { class: "soft" }, t("db.errorText", { err: App.info.dbError, p: App.info.dbPath })), [{ label: t("common.ok"), pri: true, value: true }]);
  }
  initWork();
  initProtection();
  await setLang(App.lang);
  showView("work");
  refreshCollectBadge();
  window.runtime.EventsOn("collect:new", () => { refreshCollectBadge(); if (App.view === "collect") renderCollect(); });
  window.runtime.EventsOn("update:available", offerUpdate);
  openLocal(); // a ready shell, like any terminal
}

window.addEventListener("DOMContentLoaded", () => {
  start().catch(e => { document.body.textContent = "Cmdex failed to start: " + errText(e); });
});
