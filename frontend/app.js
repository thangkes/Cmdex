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
  const db = document.getElementById("dbPath");
  db.textContent = t("status.db", { p: App.info.dbPath });
  db.title = App.info.defaults ? t("status.defaults", { p: App.info.defaults }) : "";
  setTheme(App.theme); // theme names are translated
  refreshProtection();
  relabelWork();
  if (App.view === "collect") renderCollect();
  if (App.view === "library") renderLibrary();
  refreshCount();
  relabelNotes();
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

// unlockFlow asks what the locked database needs: a new master password
// (no keychain), the master password, or what to do with a lost key.
async function unlockFlow(state) {
  for (;;) {
    if (state === "lost") {
      const v = await dialog(t("lock.lostTitle"), h("p", { class: "soft" }, t("lock.lostText", { p: App.info.dbPath })),
        [{ label: t("lock.retry"), value: "retry" }, { label: t("lock.fresh"), danger: true, value: "fresh" }]);
      if (v !== "fresh") return;
      if (await confirmDialog(t("lock.fresh"), t("lock.freshQ"), t("lock.fresh"), true)) {
        await api.StartNewLibrary().catch(e => setStatus(errText(e), true));
        return;
      }
      continue;
    }
    if (state === "create") {
      await dialog(t("lock.createTitle"), h("p", { class: "soft" }, t("lock.createText")), [{ label: t("common.ok"), pri: true, value: true }]);
      const a = await secureInput(t("lock.createTitle"), t("lock.newPassword"));
      if (a === null) continue;
      const b = await secureInput(t("lock.createTitle"), t("lock.again"));
      if (b === null) continue;
      if (a !== b) { await dialog(t("lock.createTitle"), h("p", { class: "soft" }, t("lock.differ")), [{ label: t("common.ok"), pri: true, value: true }]); continue; }
      try { await api.UnlockDatabase(a); return; }
      catch (e) { await dialog(t("lock.createTitle"), h("p", { class: "soft" }, errText(e)), [{ label: t("common.ok"), pri: true, value: true }]); continue; }
    }
    // the master password
    const pw = await secureInput(t("lock.title"), t("lock.password"));
    if (pw === null) {
      const v = await dialog(t("lock.title"), h("p", { class: "soft" }, t("lock.forgot")),
        [{ label: t("lock.retry"), pri: true, value: "retry" }, { label: t("lock.fresh"), danger: true, value: "fresh" }]);
      if (v === "fresh" && await confirmDialog(t("lock.fresh"), t("lock.freshQ"), t("lock.fresh"), true)) {
        await api.StartNewLibrary().catch(e => setStatus(errText(e), true));
        return;
      }
      continue;
    }
    try { await api.UnlockDatabase(pw); return; }
    catch (e) { await dialog(t("lock.title"), h("p", { class: "soft" }, errText(e)), [{ label: t("common.ok"), pri: true, value: true }]); }
  }
}

async function start() {
  App.info = await api.Init();
  App.theme = App.info.theme || "graphite";
  App.termTheme = applyTheme(App.theme);
  await loadLang(App.info.lang || "en");
  document.getElementById("ver").textContent = App.info.version === "dev" ? "dev" : "v" + App.info.version;
  document.getElementById("env").textContent = `${App.info.hostname} · ${App.info.os === "windows" ? "Windows" : "Linux"}`;
  document.querySelectorAll(".nav button").forEach(b => b.addEventListener("click", () => showView(b.dataset.view)));
  document.querySelectorAll("#langSeg button").forEach(b => b.addEventListener("click", async () => { await api.SetSetting("lang", b.dataset.lang).catch(e => setStatus(errText(e), true)); App.info = await api.Init().catch(() => App.info); setLang(b.dataset.lang); }));
  if (App.info.locked) { // the database waits for its key: nothing else starts before
    await unlockFlow(App.info.locked);
    location.reload();
    return;
  }
  if (App.info.dbError) {
    await dialog(t("db.errorTitle"), h("p", { class: "soft" }, t("db.errorText", { err: App.info.dbError, p: App.info.dbPath })), [{ label: t("common.ok"), pri: true, value: true }]);
  }
  initWork();
  initProtection();
  initNotes();
  await setLang(App.lang);
  showView("work");
  refreshCollectBadge();
  window.runtime.EventsOn("collect:new", () => { refreshCollectBadge(); if (App.view === "collect") renderCollect(); });
  window.runtime.EventsOn("collect:merged", n => { setStatus(t("collect.autoMerged", { n })); refreshCollectBadge(); if (App.view === "collect") renderCollect(); });
  window.runtime.EventsOn("update:available", offerUpdate);
  openLocal(); // a ready shell, like any terminal
  offerToolPacks();
}

window.addEventListener("DOMContentLoaded", () => {
  start().catch(e => { document.body.textContent = "Cmdex failed to start: " + errText(e); });
});
