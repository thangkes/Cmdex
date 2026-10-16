// Colours code shown in notes: commands, options, strings, variables,
// comments, keywords. It only splits text into coloured spans (built one
// by one, never as HTML): nothing here runs, opens or pastes anything.

const HL_LANG = {
  ps: "ps", ps1: "ps", powershell: "ps", pwsh: "ps",
  sh: "sh", bash: "sh", zsh: "sh", shell: "sh", console: "sh", terminal: "sh", fish: "sh",
  cmd: "cmd", bat: "cmd", batch: "cmd", dos: "cmd",
  py: "py", python: "py",
  js: "js", javascript: "js", ts: "js", typescript: "js", node: "js", jsx: "js", tsx: "js",
  go: "go", golang: "go",
  java: "c", c: "c", cpp: "c", "c++": "c", cs: "c", csharp: "c", kotlin: "c", kt: "c", rust: "c", rs: "c", php: "c", swift: "c",
  sql: "sql", mysql: "sql", postgres: "sql", sqlite: "sql",
  json: "json", yaml: "yaml", yml: "yaml", toml: "yaml", ini: "yaml",
};

const HL_KEYWORDS = {
  py: "def class return if elif else for while in import from as with try except finally raise lambda None True False and or not is pass break continue yield async await global nonlocal del assert print self",
  js: "const let var function return if else for while do switch case break continue new class extends import from export default try catch finally throw typeof instanceof async await null undefined true false this of in yield static get set interface type enum implements",
  go: "func package import return if else for range switch case default type struct interface map chan go defer var const nil true false break continue select fallthrough goto error string int int64 bool byte rune",
  c: "public private protected internal class struct static void int long short float double boolean bool char string var val fun let mut fn impl trait enum return if else for foreach while do switch case break continue new import using package namespace try catch finally throw throws null true false this self extends implements interface override abstract final const readonly async await match where pub use mod",
  sql: "select from where insert into values update set delete create table drop alter add column join left right inner outer full on and or not null as order by group having limit offset distinct union all index primary key foreign references default exists in like between case when then else end is count sum avg min max",
};
const HL_SETS = {};
for (const [k, v] of Object.entries(HL_KEYWORDS)) HL_SETS[k] = new Set(v.split(" "));

function hlSpan(out, cls, text) {
  if (!text) return;
  if (!cls) { out.appendChild(document.createTextNode(text)); return; }
  const s = document.createElement("span");
  s.className = "tk-" + cls;
  s.textContent = text;
  out.appendChild(s);
}

// hlGuess: a block without a language name is read as shell commands when
// it looks like them, else left plain.
function hlGuess(code) {
  const first = code.trim().split("\n")[0] || "";
  if (/^\s*(\$ |PS [A-Z]:\\|>|sudo |[A-Z][a-z]+-[A-Z]\w+|(ls|cd|cat|grep|ssh|scp|curl|wget|git|docker|kubectl|apt|apt-get|dnf|yum|pip|pip3|python3?|py|java|javac|mvn|gradle|npm|node|go|nmap|ping|ipconfig|ifconfig|ip|netstat|ss|systemctl|journalctl|tar|chmod|chown|rm|cp|mv|mkdir|echo|export|set|dir|copy|del|tasklist|taskkill|sc|net|reg|winget|choco|wsl|code|make|cargo|rustc|gcc|dotnet|terraform|ansible|helm)\b)/.test(first)) {
    return /^\s*(PS [A-Z]:\\|[A-Z][a-z]+-[A-Z]\w+)/.test(first) || /\$env:|Get-|Set-|New-|-ErrorAction/.test(code) ? "ps" : "sh";
  }
  if (/^\s*[{\[]/.test(first) && /"\s*:/.test(code)) return "json";
  return "";
}

// highlight returns the code as coloured spans in a fragment.
function highlight(code, lang) {
  const out = document.createDocumentFragment();
  let kind = HL_LANG[(lang || "").toLowerCase()] || "";
  if (!kind) kind = hlGuess(code);
  if (kind === "ps" || kind === "sh" || kind === "cmd") hlShell(code, kind, out);
  else if (kind === "json") hlJSON(code, out);
  else if (kind === "yaml") hlYAML(code, out);
  else if (HL_SETS[kind]) hlCode(code, kind, out);
  else hlSpan(out, "", code);
  return out;
}

// ---- shells: the first word of each command is the program -----------------------

function hlShell(code, kind, out) {
  const lines = code.split("\n");
  lines.forEach((line, li) => {
    if (li) out.appendChild(document.createTextNode("\n"));
    let m;
    if (kind === "cmd" && (m = line.match(/^(\s*)(rem\b.*|::.*)$/i))) { hlSpan(out, "", m[1]); hlSpan(out, "com", m[2]); return; }
    // a prompt shown in front of the command ("# " is a comment, not root's prompt)
    if ((m = line.match(/^(\s*)(PS [A-Za-z]:\\[^>]*>|\$ |> )/))) { hlSpan(out, "", m[1]); hlSpan(out, "prompt", m[2]); line = line.slice(m[0].length); }
    const re = /(\s+)|(#.*$)|('(?:[^'\\]|\\.)*'?|"(?:[^"\\]|\\.)*"?)|(\$\{[^}]*\}|\$env:\w+|\$\w+|%\w+%)|(\|\||&&|2>&1|>>|[|;&<>])|(\S+)/g;
    let start = true;
    for (let t; (t = re.exec(line));) {
      const [s, ws, com, str, v, op, word] = t;
      if (ws) { hlSpan(out, "", s); continue; }
      if (com) { hlSpan(out, kind === "cmd" ? "arg" : "com", s); if (kind !== "cmd") break; continue; } // # starts a comment (not in cmd)
      if (str) { hlSpan(out, "str", s); start = false; continue; }
      if (v) { hlSpan(out, "var", s); start = false; continue; }
      if (op) { hlSpan(out, "op", s); start = !/^(>>?|<|2>&1)$/.test(s); continue; } // after a redirection comes a file, not a program
      if (word) {
        if (start) { hlSpan(out, "cmd", word); start = /^(sudo|time|nohup|exec|env|xargs|doas)$/.test(word); continue; }
        if (/^--?[A-Za-z?]/.test(word) || ((kind === "cmd" || kind === "ps") && /^\/[A-Za-z?][\w:-]*$/.test(word))) {
          const eq = word.indexOf("=");
          if (eq > 0) { hlSpan(out, "flag", word.slice(0, eq + 1)); hlSpan(out, "arg", word.slice(eq + 1)); }
          else hlSpan(out, "flag", word);
          continue;
        }
        hlSpan(out, /^\d+(\.\d+)?$/.test(word) ? "num" : "arg", word);
      }
    }
  });
}

// ---- programming languages ---------------------------------------------------------

function hlCode(code, kind, out) {
  const lineCom = kind === "py" ? "#" : kind === "sql" ? "--" : "//";
  const block = kind !== "py";
  const re = new RegExp([
    block ? "(\\/\\*[\\s\\S]*?(?:\\*\\/|$))" : "(?!)",            // 1 block comment
    "(" + lineCom.replace(/[/]/g, "\\/") + "[^\\n]*)",            // 2 line comment
    "(\"\"\"[\\s\\S]*?(?:\"\"\"|$)|`(?:[^`\\\\]|\\\\.)*`?|\"(?:[^\"\\\\\\n]|\\\\.)*\"?|'(?:[^'\\\\\\n]|\\\\.)*'?)", // 3 strings
    "(\\b\\d+(?:\\.\\d+)?\\b)",                                    // 4 numbers
    "([A-Za-z_][\\w]*)(?=\\s*\\()",                                // 5 calls
    "([A-Za-z_][\\w]*)",                                          // 6 words
  ].join("|"), "g");
  const set = HL_SETS[kind];
  let last = 0;
  for (let m; (m = re.exec(code));) {
    if (m.index > last) hlSpan(out, "", code.slice(last, m.index));
    const s = m[0];
    if (m[1] || m[2]) hlSpan(out, "com", s);
    else if (m[3]) hlSpan(out, "str", s);
    else if (m[4]) hlSpan(out, "num", s);
    else if (m[5]) hlSpan(out, set.has(kind === "sql" ? s.toLowerCase() : s) ? "kw" : "fn", s);
    else hlSpan(out, set.has(kind === "sql" ? s.toLowerCase() : s) ? "kw" : "", s);
    last = m.index + s.length;
    if (!s.length) re.lastIndex++;
  }
  if (last < code.length) hlSpan(out, "", code.slice(last));
}

function hlJSON(code, out) {
  const re = /("(?:[^"\\]|\\.)*")(\s*:)?|(\b(?:true|false|null)\b)|(-?\b\d+(?:\.\d+)?(?:[eE][+-]?\d+)?\b)/g;
  let last = 0;
  for (let m; (m = re.exec(code));) {
    if (m.index > last) hlSpan(out, "", code.slice(last, m.index));
    if (m[1]) { hlSpan(out, m[2] ? "key" : "str", m[1]); if (m[2]) hlSpan(out, "op", m[2]); }
    else if (m[3]) hlSpan(out, "kw", m[3]);
    else hlSpan(out, "num", m[4]);
    last = m.index + m[0].length;
  }
  if (last < code.length) hlSpan(out, "", code.slice(last));
}

function hlYAML(code, out) {
  code.split("\n").forEach((line, i) => {
    if (i) out.appendChild(document.createTextNode("\n"));
    let m;
    if ((m = line.match(/^(\s*)([#;].*)$/))) { hlSpan(out, "", m[1]); hlSpan(out, "com", m[2]); return; }
    if ((m = line.match(/^(\s*-?\s*)(\[[^\]]+\]|[\w.\-"' ]+?)(\s*[:=])(.*)$/))) {
      hlSpan(out, "", m[1]); hlSpan(out, "key", m[2]); hlSpan(out, "op", m[3]);
      const v = m[4], c = v.search(/\s#/);
      const val = c >= 0 ? v.slice(0, c) : v, com = c >= 0 ? v.slice(c) : "";
      hlSpan(out, /^\s*(true|false|null|yes|no|on|off)\s*$/i.test(val) ? "kw" : /^\s*-?\d/.test(val) ? "num" : "str", val);
      hlSpan(out, "com", com);
      return;
    }
    hlSpan(out, "", line);
  });
}
