// Draws a command line with its explained parts: each part is underlined in
// its color and a label above or below points at it with a line and arrow.
// Positions use the "ch" unit of the monospace font, so they line up with
// the characters exactly; offsets from Go are in characters (runes).

const KIND_COLOR = { cmd: "var(--accent)", flag: "var(--k-flag)", arg: "var(--k-arg)", op: "var(--k-op)", unknown: "var(--muted)" };

function renderExplain(container, res) {
  clear(container);
  const segs = res.segments || [];
  if (!segs.length) return;

  // structure: program → | → program
  const flow = h("div", { class: "flow" }, h("span", { class: "label-up" }, t("explain.structure")));
  for (const s of segs) {
    if (s.kind === "cmd" || s.kind === "unknown") flow.appendChild(h("span", { class: "stage mono" + (s.kind === "unknown" ? " unknown" : "") }, s.title));
    if (s.kind === "op") flow.appendChild(h("span", { class: "op mono" }, s.title + " →"));
  }
  container.appendChild(flow);

  const box = h("div", { class: "diagram-box" });
  box.appendChild(diagram(res.line, segs));
  container.appendChild(box);

  container.appendChild(h("div", { class: "legend" },
    ["cmd", "flag", "arg", "op"].map(k => h("span", {}, h("i", { style: { background: KIND_COLOR[k] } }), t("kind." + k)))));
  if (res.missing && res.missing.length) {
    container.appendChild(h("p", { class: "note" }, t("explain.missing", { list: res.missing.join(", ") })));
  }
}

function diagram(line, segs) {
  const chars = Array.from(line);
  const LW = 28, ROW = 84, BOX = 64, LINE = 30, GAP = 20;
  const above = segs.filter((_, i) => i % 2 === 1);
  const below = segs.filter((_, i) => i % 2 === 0);
  const cmdTop = above.length * ROW + GAP;
  const height = cmdTop + LINE + GAP + below.length * ROW;
  const maxEnd = segs.reduce((a, s) => Math.max(a, s.end), chars.length);
  const el = h("div", { class: "diagram mono", style: { width: (maxEnd + LW + 2) + "ch", height: height + "px" } });

  // the command line, each part underlined in its color
  const lineEl = h("div", { class: "line", style: { top: cmdTop + "px", height: LINE + "px", lineHeight: LINE + "px" } });
  let pos = 0;
  for (const s of segs) {
    if (s.start > pos) lineEl.appendChild(h("span", { class: "gap" }, chars.slice(pos, s.start).join("")));
    lineEl.appendChild(h("span", { class: "seg-" + s.kind }, chars.slice(s.start, s.end).join("")));
    pos = s.end;
  }
  if (pos < chars.length) lineEl.appendChild(h("span", { class: "gap" }, chars.slice(pos).join("")));
  el.appendChild(lineEl);

  // labels: one per level, the rightmost part nearest to the line, so no
  // connector ever crosses a label
  const place = (list, up) => list.forEach((s, i) => {
    const level = list.length - 1 - i;
    const x = s.start + (s.end - s.start) / 2;
    const c = KIND_COLOR[s.kind];
    let boxTop, lineTop, lineH, headTop, head;
    if (up) {
      boxTop = (above.length - 1 - level) * ROW;
      lineTop = boxTop + BOX;
      lineH = cmdTop - 8 - lineTop;
      headTop = cmdTop - 9;
      head = { borderTop: "8px solid " + c };
    } else {
      boxTop = cmdTop + LINE + GAP + level * ROW;
      lineTop = cmdTop + LINE + 8;
      lineH = boxTop - lineTop;
      headTop = cmdTop + LINE + 1;
      head = { borderBottom: "8px solid " + c };
    }
    el.appendChild(h("span", { class: "conn", "aria-hidden": "true", style: { left: `calc(${x}ch - 1px)`, top: lineTop + "px", height: Math.max(0, lineH) + "px", background: c } }));
    el.appendChild(h("span", { class: "head", "aria-hidden": "true", style: Object.assign({ left: `calc(${x}ch - 6px)`, top: headTop + "px" }, head) }));
    el.appendChild(h("span", { class: "note-box" + (s.known ? "" : " guess"), title: s.text, style: { left: s.start + "ch", top: boxTop + "px", width: LW + "ch", height: BOX + "px", borderColor: c } },
      h("span", { class: "k", style: { color: c } }, t("kind." + s.kind)),
      h("span", { class: "t" }, s.title),
      h("span", { class: "d" }, s.text || "")));
  });
  place(above, true);
  place(below, false);
  return el;
}
