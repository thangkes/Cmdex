// Color themes. Graphite is the default; every theme defines the same
// variables, which style.css uses, plus the terminal colors.
const THEMES = [
  { id: "graphite", name: { en: "Graphite (default)", vi: "Than chì (mặc định)" }, v: { bg: "#0E1217", "on-accent": "#0E1217", panel: "#141A21", "panel-2": "#1E2631", sel: "#1B232D", line: "#263040", "line-2": "#2C3846", "line-3": "#3A4656", text: "#E6EAF0", "text-2": "#D2D8E0", "soft-2": "#C3CAD4", soft: "#A9B2BF", muted: "#9AA4B2", faint: "#7D8796", accent: "#F2A541", "accent-hi": "#FFC879", code: "#F2C77E", term: "#0B0F14", prompt: "#9FE6A0", info: "#5AA9F0", "info-text": "#A9D3FA", "info-bg": "#132433", "info-line": "#2C5C86", "ok-bg": "#1C2A17", "ok-text": "#B8E3A0", "ok-line": "#3F6B2E", "warn-bg": "#2A2216", "warn-line": "#6B5326", "bad-bg": "#2A1714", "bad-line": "#7A3A33", "bad-text": "#FFC9C0", "ok-dot": "#7FD36E", "off-dot": "#5A6573", "done-bg": "#11161C", "done-line": "#22303F", "k-flag": "#7CC0FF", "k-arg": "#8FDB7E", "k-op": "#F08BD8" } },
  { id: "daylight", name: { en: "Daylight", vi: "Sáng" }, light: true, v: { bg: "#F3F4F6", "on-accent": "#FFFFFF", panel: "#FFFFFF", "panel-2": "#EEF1F4", sel: "#FFF4E8", line: "#DDE2E8", "line-2": "#CBD2DB", "line-3": "#B4BDC8", text: "#18202B", "text-2": "#2A3442", "soft-2": "#3B4654", soft: "#4B5563", muted: "#5A6472", faint: "#6B7480", accent: "#B4530C", "accent-hi": "#8F420A", code: "#8A4600", term: "#FFFFFF", prompt: "#17803D", info: "#1D6FD1", "info-text": "#134E96", "info-bg": "#E6F0FC", "info-line": "#9CC3F0", "ok-bg": "#E8F5E4", "ok-text": "#24622C", "ok-line": "#A6D39C", "warn-bg": "#FDF3E2", "warn-line": "#E3C08A", "bad-bg": "#FDECEA", "bad-line": "#E8A49B", "bad-text": "#9B2C1F", "ok-dot": "#22A04B", "off-dot": "#9AA3AE", "done-bg": "#F7F8FA", "done-line": "#E3E7EC", "k-flag": "#1D6FD1", "k-arg": "#24803A", "k-op": "#B4318F" } },
  { id: "midnight", name: { en: "Midnight", vi: "Đêm xanh" }, v: { bg: "#0B1626", "on-accent": "#06121F", panel: "#10213A", "panel-2": "#172C4A", sel: "#1A3355", line: "#1F3A5E", "line-2": "#2A4A73", "line-3": "#3A5D8A", text: "#E5EEF9", "text-2": "#CFDDEE", "soft-2": "#B9CBE0", soft: "#A6B9D0", muted: "#93A8C2", faint: "#7489A3", accent: "#4FC3F7", "accent-hi": "#8AD8FA", code: "#9AD9FF", term: "#07101C", prompt: "#7EE0B5", info: "#82AAFF", "info-text": "#B7CCFF", "info-bg": "#172A4D", "info-line": "#34518A", "ok-bg": "#13302A", "ok-text": "#9EE6C5", "ok-line": "#2F6B57", "warn-bg": "#2E2A17", "warn-line": "#6E6230", "bad-bg": "#3A1A22", "bad-line": "#7D3A49", "bad-text": "#FFC2CE", "ok-dot": "#5EE0A0", "off-dot": "#50627A", "done-bg": "#0D1C30", "done-line": "#1A3150", "k-flag": "#82AAFF", "k-arg": "#7EE0B5", "k-op": "#F78CBF" } },
  { id: "phosphor", name: { en: "Phosphor (green)", vi: "Phosphor (xanh lục)" }, v: { bg: "#070C08", "on-accent": "#04140A", panel: "#0D150F", "panel-2": "#142017", sel: "#172A1B", line: "#1C2E20", "line-2": "#26402B", "line-3": "#34573A", text: "#D7F5DC", "text-2": "#BFE8C7", "soft-2": "#A8D8B1", soft: "#96C79F", muted: "#85B48E", faint: "#6A9672", accent: "#4ADE80", "accent-hi": "#86EFAC", code: "#A7F3B5", term: "#030704", prompt: "#4ADE80", info: "#67E8F9", "info-text": "#A5F3FC", "info-bg": "#0B2226", "info-line": "#1F5560", "ok-bg": "#0F2A17", "ok-text": "#A7F3B5", "ok-line": "#2C6B3D", "warn-bg": "#26240E", "warn-line": "#5E5A22", "bad-bg": "#2A1212", "bad-line": "#6E2E2E", "bad-text": "#FFC4C4", "ok-dot": "#4ADE80", "off-dot": "#3F5A45", "done-bg": "#0A120C", "done-line": "#18281B", "k-flag": "#67E8F9", "k-arg": "#FDE68A", "k-op": "#F9A8D4" } },
  { id: "dusk", name: { en: "Dusk", vi: "Tím hoàng hôn" }, v: { bg: "#15121C", "on-accent": "#15121C", panel: "#1D1928", "panel-2": "#282236", sel: "#2D2640", line: "#332A46", "line-2": "#3F3557", "line-3": "#51456E", text: "#EDE7F8", "text-2": "#DAD1EC", "soft-2": "#C7BDDD", soft: "#B7ACCF", muted: "#A69BC0", faint: "#8C80A8", accent: "#C792EA", "accent-hi": "#DDB8F5", code: "#F5C2E7", term: "#100D16", prompt: "#8BE9A8", info: "#89B4FA", "info-text": "#BCD3FC", "info-bg": "#1E2440", "info-line": "#3B4A7A", "ok-bg": "#1A2A22", "ok-text": "#A6E3B8", "ok-line": "#3C6A4C", "warn-bg": "#2D2418", "warn-line": "#6B5532", "bad-bg": "#321823", "bad-line": "#7A3550", "bad-text": "#FFC1D6", "ok-dot": "#8BE9A8", "off-dot": "#5D5470", "done-bg": "#18141F", "done-line": "#2A2338", "k-flag": "#89B4FA", "k-arg": "#A6E3A1", "k-op": "#F38BA8" } },
];

function themeById(id) {
  return THEMES.find(t => t.id === id) || THEMES[0];
}

// applyTheme sets the CSS variables and returns the matching xterm theme.
function applyTheme(id) {
  const th = themeById(id);
  const root = document.documentElement;
  for (const [k, v] of Object.entries(th.v)) root.style.setProperty("--" + k, v);
  root.style.colorScheme = th.light ? "light" : "dark";
  return termTheme(th);
}

function termTheme(th) {
  const v = th.v;
  return {
    background: v.term, foreground: v["text-2"], cursor: v.accent, cursorAccent: v.term,
    selectionBackground: th.light ? "#B4530C33" : "#FFFFFF33",
    black: th.light ? "#2A3442" : "#1E2631", brightBlack: v.faint,
    red: th.light ? "#B42318" : "#FF7B72", brightRed: th.light ? "#D92D20" : "#FFA198",
    green: v.prompt, brightGreen: v["ok-text"],
    yellow: v.code, brightYellow: v["accent-hi"],
    blue: v["k-flag"], brightBlue: v["info-text"],
    magenta: v["k-op"], brightMagenta: v["k-op"],
    cyan: th.light ? "#0E7490" : "#76E3EA", brightCyan: th.light ? "#0891B2" : "#B3F0FF",
    white: v["text-2"], brightWhite: v.text,
  };
}
