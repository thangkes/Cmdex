// Protection against keyloggers and screen capture.
//
// secureInput() reads a secret without the physical keyboard: an on-screen
// keypad whose keys move to new places after every click, so neither a
// keylogger (there are no keystrokes) nor a click logger (positions mean
// nothing) learns it. While it is open the window is kept out of screen
// captures, whatever the setting, so it cannot be read from a screenshot.

const KEYSETS = {
  lower: "abcdefghijklmnopqrstuvwxyz".split(""),
  upper: "ABCDEFGHIJKLMNOPQRSTUVWXYZ".split(""),
  digits: "0123456789".split(""),
  symbols: "!@#$%^&*()-_=+[]{};:'\",.<>/?\\|`~".split(""),
};

function shuffled(xs) {
  const a = xs.slice();
  const r = new Uint32Array(a.length);
  crypto.getRandomValues(r);
  for (let i = a.length - 1; i > 0; i--) {
    const j = r[i] % (i + 1);
    [a[i], a[j]] = [a[j], a[i]];
  }
  return a;
}

// secureInput(title, label) resolves with the secret, or null if cancelled.
async function secureInput(title, label) {
  let value = [];
  let upper = false, symbols = false, reveal = false;
  const shown = h("div", { class: "kp-value mono", "aria-live": "polite" });
  const pad = h("div", { class: "kp-keys" });

  const draw = () => {
    shown.textContent = value.length ? (reveal ? value.join("") : "•".repeat(value.length)) : t("kp.empty");
    shown.classList.toggle("empty", !value.length);
    clear(pad);
    const letters = symbols ? KEYSETS.symbols : (upper ? KEYSETS.upper : KEYSETS.lower);
    for (const ch of shuffled(KEYSETS.digits).concat(shuffled(letters))) {
      // the label is the character itself: what is pressed is visible on
      // screen only, never typed
      pad.appendChild(h("button", { type: "button", class: "kp-key mono", tabindex: "-1", "aria-label": ch, onclick: () => { value.push(ch); draw(); } }, ch));
    }
  };
  const ctrl = (labelKey, fn, pressed) => h("button", { type: "button", class: "kp-ctrl", tabindex: "-1", "aria-pressed": pressed === undefined ? undefined : String(pressed), onclick: () => { fn(); draw(); refreshCtrls(); } }, t(labelKey));
  const ctrls = h("div", { class: "kp-ctrls" });
  const refreshCtrls = () => {
    clear(ctrls);
    append(ctrls, [
      ctrl("kp.shift", () => { upper = !upper; symbols = false; }, upper),
      ctrl("kp.symbols", () => { symbols = !symbols; }, symbols),
      ctrl("kp.space", () => value.push(" ")),
      ctrl("kp.back", () => value.pop()),
      ctrl("kp.clear", () => { value = []; }),
      ctrl(reveal ? "kp.hide" : "kp.show", () => { reveal = !reveal; }),
    ]);
  };
  draw();
  refreshCtrls();

  // the physical keyboard does nothing here (only Escape, Tab and Enter on
  // the dialog's own buttons)
  const block = e => {
    if (e.key && e.key.length === 1 && !e.ctrlKey && !e.metaKey) { e.preventDefault(); e.stopPropagation(); flashHint(); }
  };
  const hint = h("p", { class: "note" }, t("kp.hint"));
  const flashHint = () => { hint.classList.add("flash"); setTimeout(() => hint.classList.remove("flash"), 600); };
  document.addEventListener("keydown", block, true);
  await api.HoldProtection().catch(() => {});
  try {
    const ok = await dialog(title, [
      label ? h("p", { class: "soft", style: { margin: 0 } }, label) : null,
      shown, pad, ctrls, hint,
    ], [{ label: t("common.cancel"), value: false }, { label: t("kp.done"), pri: true, value: true }]);
    return ok ? value.join("") : null;
  } finally {
    document.removeEventListener("keydown", block, true);
    value = [];
    api.ReleaseProtection().catch(() => {});
  }
}

// ---- screen-capture protection switch in the top bar ----------------------

async function refreshProtection() {
  relabelSettings();
  const b = document.getElementById("protectBtn");
  let st;
  try { st = await api.Protection(); } catch (e) { return; }
  b.hidden = false;
  b.disabled = !st.supported;
  const on = st.supported && st.active;
  b.setAttribute("aria-pressed", String(on));
  b.classList.toggle("on", on);
  b.title = !st.supported ? t("protect.unsupported") : (on ? t("protect.on") : t("protect.off"));
  b.setAttribute("aria-label", b.title);
}

// ---- settings ---------------------------------------------------------

function relabelSettings() {
  const b = document.getElementById("settingsBtn");
  b.title = t("settings.title");
  b.setAttribute("aria-label", b.title);
}

async function openSettings() {
  const st = await api.Autostart().catch(() => null);
  const os = App.info.os === "windows" ? "Windows" : "Linux";
  const where = h("p", { class: "note" }, st && st.on ? t("settings.autostartOn", { p: st.path }) : t("settings.autostartOff"));
  const body = [];
  if (st && st.supported) {
    body.push(sw(t("settings.autostart", { os }), t("settings.autostartHint"), st.on, async on => {
      try {
        const nst = await api.SetAutostart(on);
        where.textContent = nst.on ? t("settings.autostartOn", { p: nst.path }) : t("settings.autostartOff");
      } catch (e) { setStatus(errText(e), true); }
    }), where);
  } else {
    body.push(h("p", { class: "soft" }, t("settings.autostartNo")));
  }
  await dialog(t("settings.title"), h("div", { class: "settings" }, body), [{ label: t("common.ok"), pri: true, value: true }]);
}

function initProtection() {
  document.getElementById("settingsBtn").addEventListener("click", openSettings);
  relabelSettings();
  const b = document.getElementById("protectBtn");
  b.addEventListener("click", async () => {
    const st = await api.Protection().catch(() => null);
    if (!st || !st.supported) return;
    try { await api.SetProtection(!st.wanted); } catch (e) { setStatus(errText(e), true); }
    refreshProtection();
  });
  refreshProtection();
  setTimeout(refreshProtection, 3000); // the window is protected a moment after start
}
