package store

import (
	"regexp"
	"strings"
)

// Risk levels of a command (Command.Danger declares one, Command.Risk is
// the one shown): what running it may do. Cmdex never runs a command by
// itself: ▶ pastes it and the user presses Enter, warned by the label.
const (
	RiskNormal   = 0
	RiskWarning  = 1 // changes the system: services, packages, firewall, users, configuration
	RiskCritical = 2 // destroys data or cuts service: recursive delete, format, restart, wiping a configuration
)

func res(patterns ...string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(patterns))
	for i, p := range patterns {
		out[i] = regexp.MustCompile(`(?i)` + p)
	}
	return out
}

// On computers (Windows, Linux, and pfSense's FreeBSD shell).
var (
	criticalComputer = res(
		// deleting recursively, wiping and formatting
		`\brm\s+(?:\S+\s+)*(?:-[a-z]*r|--recursive)`, `\bremove-item\b.*-recurse`, `\b(?:rd|rmdir)\s+(?:\S+\s+)*/s\b`,
		`\b(?:del|erase)\s+(?:\S+\s+)*/s\b`, `\bformat\s+[a-z]:`, `\bformat-volume\b`, `\bclear-disk\b`, `\bdiskpart\b`,
		`\bmkfs(?:\.\w+)?\b`, `\bwipefs\b`, `\bshred\b`, `\bdd\b.*\bof=`, `>\s*/dev/(?:sd|nvme|hd|vd|mmcblk)`,
		`\brobocopy\b.*\s/(?:mir|purge)\b`, `:\(\)\s*\{\s*:\s*\|\s*:`,
		// restarting and powering off
		`\bshutdown\b`, `\b(?:reboot|poweroff|halt)\b`, `\binit\s+[06]\b`, `\b(?:restart|stop)-computer\b`,
		// turning protection off
		`\bnetsh\s+advfirewall\s+set\s+\S+\s+state\s+off`, `\bufw\s+disable\b`, `\bsystemctl\s+(?:stop|disable|mask)\s+(?:firewalld|ufw|nftables|iptables)\b`,
		`\biptables\s+(?:-t\s+\S+\s+)?-(?:F|X)\b`, `\bnft\s+flush\s+ruleset\b`, `\bset-mppreference\b.*-disable`,
		// pfSense
		`\bpfctl\s+(?:\S+\s+)*-d\b`, `\bpfctl\s+(?:\S+\s+)*-F\s*all\b`, `\benableallowallwan\b`,
		// databases and containers
		`\bdrop\s+(?:database|table|schema)\b`, `\btruncate\s+table\b`, `\bdocker\s+(?:system|volume)\s+prune\b`,
		// a Java debugger open to the whole network runs any code it is sent
		`-(?:agentlib:jdwp|xrunjdwp:)\S*address=(?:\*|0\.0\.0\.0):`,
		// nmap's dos scripts may crash the service they test
		`\bnmap\b.*--script[=\s]+\S*\bdos\b`,
	)
	warningComputer = res(
		`\brm\b`, `\b(?:del|erase|rmdir|rd)\s`, `\bremove-item\b`,
		`\bsystemctl\s+(?:restart|stop|start|disable|enable|mask|reload)\b`, `\bservice\s+\S+\s+(?:restart|stop|start)\b`,
		`\b(?:restart|stop|start|set)-service\b`, `\bsc(?:\.exe)?\s+(?:stop|start|config|delete)\b`, `\bnet\s+(?:stop|start)\b`,
		`\btaskkill\b`, `\b(?:kill|pkill|killall)\b`, `\bstop-process\b`,
		`\bchmod\b`, `\bchown\b`, `\bicacls\b.*/(?:grant|deny|reset|setowner|remove)`, `\btakeown\b`,
		`\b(?:apt|apt-get|dnf|yum|zypper|winget|choco|snap|pip3?|npm)\s+(?:\S+\s+)*(?:install|remove|purge|upgrade|update|uninstall|autoremove)\b`,
		`\bpacman\s+-[a-z]*[SRU]`, `\bpkg\s+(?:install|delete|remove|upgrade)\b`,
		`\b(?:useradd|userdel|usermod|passwd|groupadd|groupdel|chpasswd)\b`, `\b(?:new|remove|set|disable|enable|unlock)-(?:localuser|aduser|adaccount|adgroupmember)\b`,
		`\bnet\s+(?:user|localgroup)\s+\S+\s+\S`,
		`\bnetsh\s+(?:int|interface|winsock|advfirewall)\b.*\b(?:reset|add|delete|set)\b`, `\bipconfig\s+/(?:release|renew)\b`,
		`\bufw\s+(?:allow|deny|reject|delete|enable|reset)\b`, `\bfirewall-cmd\b.*--(?:add|remove|set|reload)`, `\biptables\s+(?:-t\s+\S+\s+)?-[ADIRP]\b`,
		`\bip\s+(?:-\S+\s+)*(?:addr|address|link|route|neigh)\s+(?:add|del|delete|set|flush|change|replace)\b`, `\bnmcli\b.*\b(?:modify|down|delete|up)\b`,
		`\broute\s+(?:add|delete|change)\b`, `\bnet\s+use\b.*\s(?:\\\\|/delete)`,
		`\b(?:mount|umount)\b`, `\bchkdsk\b.*/[fr]\b`, `\bfsck\b`, `\bsfc\s+/scannow\b`, `\bdism\b.*/(?:restorehealth|cleanup)`,
		`\bw32tm\s+/resync\b`, `\bschtasks\s+/(?:create|delete|change|run)\b`, `\bcrontab\s+-[er]\b`, `\breg\s+(?:add|delete|import)\b`,
		`\bset-itemproperty\b`, `\bbcdedit\b`, `\bmanage-bde\s+-(?:on|off|lock)\b`, `\bupdate-mp\w+\b`, `\bremove-printjob\b`,
		`\bdocker\s+(?:rm|rmi|stop|kill|restart|compose\s+(?:up|down|restart|rm))\b`, `\bkubectl\s+(?:delete|apply|scale|drain)\b`,
		`\bgit\s+(?:push\s+.*(?:\s-f\b|--force)|reset\s+--hard|clean\s+-\w*f)`,
		`\bpfctl\s+(?:\S+\s+)*-(?:k|e|F)\b`, `\bpfctl\b.*-T\s+(?:add|delete|flush|replace)\b`, `\beasyrule\s+(?:block|unblock|pass)\b`,
		`\bpfssh\.php\s+playback\s+(?:svc|changepassword|install|uninstall|remove|restart|disable|enable)`, `/etc/rc\.(?:restart_webgui|filter_configure|reload_all)\b`,
		`\bnginx\s+-s\b`, `\btimedatectl\s+set-`, `>\s*/etc/`,
		// overwriting a file with ">" (">>" appends; /dev/null and 2>&1 do not count)
		`(?:^|[^>0-9&])>\s*(?:[^\s&>/]|/[^d]|/d[^e])`,
		// development tools: debugging ports, shared folders, memory dumps, keys
		`-(?:agentlib:jdwp|xrunjdwp:)`, `\b(?:python[0-9.]*|py)\s+(?:-\S+\s+)*-m\s+(?:http\.server|SimpleHTTPServer)\b`, `\bphp\s+-S\b`,
		`\bjmap\b.*-dump`, `\bjcmd\b.*\b(?:GC\.heap_dump|Thread\.dump_to_file)\b`,
		`\bkeytool\b.*-(?:genkeypair|genkey|storepasswd|keypasswd|delete|importcert|importkeystore|import|changealias|gencert)\b`, `\bjarsigner\b`,
		`\bjar\s+-?[a-z]*[cux]`,
		`\bmvn\b.*\b(?:clean|install|deploy|release:\w+|dependency:(?:add|purge-local-repository)|archetype:generate|versions:set)\b`,
		`\bgradle[w]?\b.*\b(?:clean|wrapper|init|publish\w*|uploadArchives)\b`,
		`\bpip3?\s+(?:\S+\s+)*cache\s+(?:purge|remove)\b`,
		// nmap: loud or intrusive scans, spoofing and firewall evasion, flooding, files written.
		// Options are case-sensitive: -D decoys but -d debug, -f fragments but -F fast.
		`\bnmap\b.*(?:\s-A\b|--script[=\s]+\S*\b(?:vuln|exploit|brute|intrusive|fuzzer|malware)\b|--script-updatedb\b)`,
		`\bnmap\b.*(?-i:\s-(?:D|S|f|g|b|iR|sI|T5)\b)`,
		`\bnmap\b.*--(?:mtu|spoof-mac|badsum|data|data-string|data-length|ip-options|source-port|proxies|min-rate)\b`,
		`\bnmap\b.*(?-i:\s-o[NXSGA]\s)`,
	)
	// warningExempt: lines a warning pattern matches that only check something
	warningExempt = res(`\bjarsigner\b.*\s-verify\b`)
	// On Cisco (IOS): EXEC and configuration commands, abbreviations included.
	criticalCisco = res(
		`^\s*wr(?:i(?:t(?:e)?)?)?\s+er`, `^\s*erase\s+(?:startup-config|nvram:|flash:|/all)`, `^\s*reload\s*$`,
		`^\s*(?:delete|del)\s+(?:/\S+\s+)*(?:flash|bootflash|nvram|disk\d*|usbflash\d*):`, `^\s*format\s+\S+:`,
		`^\s*no\s+(?:vlan|interface\s+(?:vlan|port-channel))\b`, `^\s*(?:do\s+)?clear\s+(?:ip\s+bgp\s+\*|line)`,
	)
	warningCisco = res(
		`^\s*conf(?:i(?:g(?:u(?:r(?:e)?)?)?)?)?\b`, `^\s*int(?:e(?:r(?:f(?:a(?:c(?:e)?)?)?)?)?)?\b`, `^\s*no\s`, `^\s*sh(?:u(?:t(?:d(?:o(?:w(?:n)?)?)?)?)?)?\s*$`,
		`^\s*(?:switchport|vlan|name|description|hostname|username|enable\s+secret|crypto|line|transport|login|spanning-tree|channel-group|snmp-server|ntp|logging|aaa|access-list|ip\s+(?:address|route|default-gateway|domain-name|ssh|http|access-list|dhcp|nat))\b`,
		`^\s*(?:copy|wr(?:i(?:t(?:e)?)?)?)\s+(?:r|m|startup)`, `^\s*(?:do\s+)?clear\s`, `^\s*reload\s+(?:in|at|cancel)\b`,
	)
)

// DetectRisk is the risk Cmdex sees in a command line for platform (one of
// the Platforms, or "" for this computer). It errs on the side of warning.
func DetectRisk(line, platform string) int {
	critical, warning := criticalComputer, warningComputer
	if platform == Cisco {
		critical, warning = criticalCisco, warningCisco
	}
	risk := RiskNormal
	for _, part := range append([]string{line}, splitCommands(line)...) {
		for _, re := range critical {
			if re.MatchString(part) && !harmless(part) {
				return RiskCritical
			}
		}
		for _, re := range warning {
			if re.MatchString(part) && !matchesAny(warningExempt, part) {
				risk = RiskWarning
			}
		}
	}
	return risk
}

// harmless lists lines a critical pattern matches by its words only:
// cancelling a shutdown.
var harmlessRe = res(`\bshutdown\s+(?:/a|-c)\s*$`)

func matchesAny(res []*regexp.Regexp, s string) bool {
	for _, re := range res {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

func harmless(part string) bool {
	for _, re := range harmlessRe {
		if re.MatchString(strings.TrimSpace(part)) {
			return true
		}
	}
	return false
}

// splitCommands cuts a line at ; && || and | so "cd x && rm -rf y" is
// judged command by command (Cisco rules anchor at the start of each).
func splitCommands(line string) []string {
	return strings.FieldsFunc(line, func(r rune) bool { return r == ';' || r == '&' || r == '|' })
}

// RiskOf is the risk shown for a command: the higher of the level it
// declares and the one Cmdex detects.
func RiskOf(declared int, template, platform string) int {
	if d := DetectRisk(template, platform); d > declared {
		return d
	}
	if declared > RiskCritical {
		return RiskCritical
	}
	if declared < RiskNormal {
		return RiskNormal
	}
	return declared
}
