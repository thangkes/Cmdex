package explain

import (
	"bytes"
	"context"
	"debug/pe"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/thangkes/cmdex/internal/store"
)

var reProgName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)

// FromHelp reads the help already installed for a program and turns its
// option list into an explanation. It never runs the program itself, except
// Windows' own tools in System32 with "/?", which only prints help:
//   - Linux: man -P cat <prog>
//   - Windows: Get-Help <cmdlet> -Parameter * (PowerShell cmdlets),
//     or <System32 tool> /?
func FromHelp(ctx context.Context, prog string) (store.Explain, bool) {
	if !reProgName.MatchString(prog) {
		return store.Explain{}, false
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	var out []byte
	var parse func(string) store.Explain
	if runtime.GOOS == "windows" {
		if strings.Contains(prog, "-") { // Verb-Noun: a PowerShell cmdlet
			// UTF-8 out: PowerShell 5 writes to a pipe in the console code page
			out = run(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
				"[Console]::OutputEncoding = [Text.Encoding]::UTF8; Get-Help "+prog+" -Full | Out-String -Width 250")
			parse = parsePowerShellHelp
		} else if sys := os.Getenv("SystemRoot"); sys != "" {
			exe := filepath.Join(sys, "System32", prog+".exe")
			if _, err := os.Stat(exe); err == nil && safeForHelp(prog, exe) {
				out = fromConsole(run(ctx, exe, "/?")) // in the console code page (localized Windows)
				parse = parseOptionList
			}
		}
	} else {
		cmd := exec.CommandContext(ctx, "man", "-P", "cat", prog)
		cmd.Env = append(os.Environ(), "MANWIDTH=200", "MAN_KEEP_FORMATTING=0")
		out, _ = cmd.Output()
		parse = parseOptionList
	}
	if len(bytes.TrimSpace(out)) == 0 || parse == nil {
		return store.Explain{}, false
	}
	e := parse(stripBackspaces(string(out)))
	e.Cmd, e.Source = prog, "help"
	if e.Summary == "" && len(e.Flags) == 0 {
		return store.Explain{}, false
	}
	return e, true
}

func run(ctx context.Context, name string, args ...string) []byte {
	cmd := exec.CommandContext(ctx, name, args...)
	hideWindow(cmd)
	out, _ := cmd.Output()
	return out
}

// stripBackspaces removes man's overstrike bold ("N\bNA\bAM\bME").
func stripBackspaces(s string) string {
	if !strings.Contains(s, "\b") {
		return s
	}
	var b []rune
	for _, r := range s {
		if r == '\b' {
			if len(b) > 0 {
				b = b[:len(b)-1]
			}
			continue
		}
		b = append(b, r)
	}
	return string(b)
}

var (
	// "  -a, --all            do not ignore entries"  /  "  -n count   Number of echo requests"
	reOptLine = regexp.MustCompile(`^\s{1,12}((?:(?:--?|/)[A-Za-z0-9?][\w?-]*(?:[ =]?(?:<[^>]+>|\[[^\]]+\]|[A-Z][A-Z_]*|[a-z_]+))?(?:,\s*)?)+?)(?:\s{2,}|\t+)(\S.*)$`)
	// option alone on its line, text on the following lines
	reOptOnly = regexp.MustCompile(`^\s{1,12}((?:--?|/)[A-Za-z0-9?][\w?-]*(?:[ =]?(?:<[^>]+>|\[[^\]]+\]|[A-Z][A-Z_]*))?(?:,\s*(?:--?|/)[\w?-]+(?:[ =]?(?:<[^>]+>|[A-Z][A-Z_]*))?)*)\s*$`)
	reOptName = regexp.MustCompile(`(?:--?|/)[A-Za-z0-9?][\w?-]*`)
	reNameSec = regexp.MustCompile(`(?m)^NAME\s*\n\s+\S+\s+[-–—]\s+(.+)$`)
)

// parseOptionList reads man pages and Windows "/?" output.
func parseOptionList(text string) store.Explain {
	e := store.Explain{Flags: map[string]store.Flag{}}
	if m := reNameSec.FindStringSubmatch(text); m != nil {
		e.Summary = strings.TrimSpace(m[1])
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r", ""), "\n")
	if e.Summary == "" {
		for _, l := range lines {
			if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "Usage") && !strings.HasPrefix(l, "USAGE") {
				e.Summary = l
				break
			}
		}
	}
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		var spec, desc string
		if m := reOptLine.FindStringSubmatch(l); m != nil {
			spec, desc = m[1], m[2]
		} else if m := reOptOnly.FindStringSubmatch(l); m != nil && i+1 < len(lines) {
			next := strings.TrimSpace(lines[i+1])
			if next == "" || reOptName.MatchString(strings.Fields(next)[0]) && strings.HasPrefix(next, "-") {
				continue
			}
			spec, desc = m[1], next
		} else {
			continue
		}
		desc = strings.TrimSpace(desc)
		takesValue := strings.ContainsAny(strings.TrimSpace(reOptName.ReplaceAllString(spec, "")), "<[ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz=")
		for _, name := range reOptName.FindAllString(spec, -1) {
			k := name
			if _, dup := e.Flags[k]; dup {
				continue
			}
			text := desc
			if takesValue {
				text += " ({v})"
			}
			e.Flags[k] = store.Flag{Text: text, Value: takesValue}
		}
	}
	return e
}

var reParamHead = regexp.MustCompile(`^\s{4}-(\w+)\s*(<[^>]+>)?\s*$`)

// parsePowerShellHelp reads "Get-Help <cmdlet> -Full" output.
func parsePowerShellHelp(text string) store.Explain {
	e := store.Explain{Flags: map[string]store.Flag{}}
	lines := strings.Split(strings.ReplaceAll(text, "\r", ""), "\n")
	section := ""
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if l != "" && l[0] != ' ' {
			section = strings.TrimSpace(l)
			continue
		}
		switch section {
		case "SYNOPSIS":
			if e.Summary == "" && strings.TrimSpace(l) != "" {
				e.Summary = strings.TrimSpace(l)
			}
		case "PARAMETERS":
			m := reParamHead.FindStringSubmatch(l)
			if m == nil {
				continue
			}
			var desc string
			for j := i + 1; j < len(lines) && j < i+6; j++ {
				if d := strings.TrimSpace(lines[j]); d != "" {
					desc = d
					break
				}
			}
			text := desc
			if m[2] != "" && !strings.EqualFold(m[2], "<SwitchParameter>") {
				text += " ({v})"
			}
			e.Flags["-"+strings.ToLower(m[1])] = store.Flag{Text: text, Value: m[2] != "" && !strings.EqualFold(m[2], "<SwitchParameter>")}
		}
	}
	return e
}

// neverRunForHelp lists System32 programs Cmdex never starts to read their
// help: they act at once, open an interactive console, or run other code.
var neverRunForHelp = map[string]bool{
	"cmd": true, "powershell": true, "pwsh": true, "wscript": true, "cscript": true, "mshta": true,
	"rundll32": true, "regsvr32": true, "msiexec": true, "runas": true, "diskpart": true, "format": true,
	"logoff": true, "tsdiscon": true, "shutdown": true, "systemreset": true, "bash": true, "wsl": true,
	"ftp": true, "telnet": true, "ssh": true, "sftp": true, "scp": true, "netsh": true, "wmic": true,
}

// safeForHelp: only console programs (a window program would just open) and
// none on the list above.
func safeForHelp(prog, exe string) bool {
	if neverRunForHelp[strings.ToLower(prog)] {
		return false
	}
	f, err := pe.Open(exe)
	if err != nil {
		return false
	}
	defer f.Close()
	const consoleSubsystem = 3 // IMAGE_SUBSYSTEM_WINDOWS_CUI
	switch h := f.OptionalHeader.(type) {
	case *pe.OptionalHeader64:
		return h.Subsystem == consoleSubsystem
	case *pe.OptionalHeader32:
		return h.Subsystem == consoleSubsystem
	}
	return false
}
