package mask

import (
	"net"
	"regexp"
	"strconv"
	"strings"
)

// Secrets in output that collect.SecretPatterns (made for command lines)
// does not cover. Group 2 is the value.
var outputSecrets = []*regexp.Regexp{
	// netsh wlan show profile key=clear: "Key Content            : x"
	regexp.MustCompile(`(?i)(\bkey content\s*:\s*)(.+)`),
	// Cisco and other devices: key-string, pre-shared-key, ospf/bgp keys…
	regexp.MustCompile(`(?i)((?:^|\s)(?:key-string|pre-shared-key(?:\s+(?:local|remote))?|authentication-key|message-digest-key\s+\d+\s+md5|neighbor\s+\S+\s+password|tacacs-server\s+key|radius-server\s+key|server-private\s+\S+\s+key|key\s+[0-9])\s+(?:[0-9]\s+)?)(\S+)`),
	regexp.MustCompile(`(?i)(\bcrypto\s+isakmp\s+key\s+(?:[0-9]\s+)?)(\S+)`),
	regexp.MustCompile(`(?i)(\b(?:wpa-psk|wpa2-psk|sae-password)\s+(?:ascii|hex)?\s*(?:[0-9]\s+)?)(\S+)`),
	// psk=, wpa_passphrase=, 802-11-wireless-security.psk: …
	regexp.MustCompile(`(?i)((?:^|[\s.])(?:psk|wpa_passphrase|passphrase|pre-?shared-?key|presharedkey|private[_-]?key|secret[_-]?key|client[_-]?secret)\s*[=:]\s*)("[^"]*"|'[^']*'|\S+)`),
	// XML configs (pfSense config.xml…): <password>x</password>
	regexp.MustCompile(`(?i)(<(?:password|passwd|pre-shared-key|privatekey|private-key|prv|bcrypt-hash|sha512-hash|apikey|api_key|secret|psk|community|authkey|tls|ipsecpsk|wpa_passphrase)>)([^<]+)(</)`),
	// /etc/shadow lines: name:$6$…:
	regexp.MustCompile(`^([\w.-]+:)(\$[0-9a-z]+\$[^:\s]+)(:)`),
}

// crypt-style password hashes anywhere: $1$…, $5$…, $6$…, $2b$…, $y$…, Cisco $8$/$9$
var reCryptHash = regexp.MustCompile(`\$(?:1|2[aby]?|5|6|7|8|9|y|gy|argon2id?)\$[./A-Za-z0-9$=,]{8,}`)

func notEmptySecret(line string, a, b int) bool { return strings.Trim(line[a:b], `"'`) != "" }

var (
	reEmail = regexp.MustCompile(`(?i)\b[a-z0-9._%+-]+@[a-z0-9-]+(?:\.[a-z0-9-]+)*\.[a-z]{2,24}\b`)

	// hostname X, Host Name . . . : X, Computer Name: X, Device ID: X (CDP), System Name: X (LLDP)
	reHostLine = regexp.MustCompile(`(?i)(^\s*hostname\s+|\bhost ?name[ .]*:\s*|\bcomputer ?name[ .]*:\s*|\bsystem name[ .]*:\s*|\bdevice id\s*:\s*|\bsysname\s+)([A-Za-z0-9][\w.-]*)`)
	// user@host:~$ and [user@host dir]$
	rePromptHost = regexp.MustCompile(`(?i)^\[?[a-z_][\w.-]{0,31}@([a-z0-9][\w.-]*)[:\s\]]`)
	// Router#, SW-01(config-if)#, Router>
	reDevicePrompt = regexp.MustCompile(`^([A-Za-z][\w.-]{0,62})(?:\([\w./-]*\))?[#>]`)

	reWinHome    = regexp.MustCompile(`(?i)(\b[a-z]:\\(?:users|documents and settings)\\)([^\\/:*?"<>|\s]+)`)
	reUnixHome   = regexp.MustCompile(`((?:^|[\s'"=:(])/(?:home|Users)/)([^/\s:'"]+)`)
	rePromptUser = regexp.MustCompile(`(?i)^(\[?)([a-z_][\w.-]{0,31})@[a-z0-9][\w.-]*[:\s\]]`)
	reLogin      = regexp.MustCompile(`(?i)((?:^|\s)username\s+|\blogin as:\s*|^\s*user(?: ?name)?\s*:\s*|\buser name[ .]*:\s*|\blogged in as\s+|\bowner\s*:\s*)([\w.\\@-]+)`)

	// Serial Number: X, SN: X, Processor board ID X, UUID: X, system-serial-number X
	reSerial = regexp.MustCompile(`(?i)(\b(?:serial[ _-]?(?:number|no|num)|serialnumber|system[ -]serial[ -]number|chassis serial|s/n|sn|processor board id|board id|uuid|identifyingnumber)\.?\s*[:=#]?\s*)([A-Za-z0-9][\w\-/.:]{3,63})`)

	reMAC = regexp.MustCompile(`(?i)\b(?:[0-9a-f]{2}(?:[:-][0-9a-f]{2}){5}|[0-9a-f]{4}\.[0-9a-f]{4}\.[0-9a-f]{4})\b`)

	reIPv4 = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}\b`)
	reIPv6 = regexp.MustCompile(`(?i)[0-9a-f]{0,4}(?::[0-9a-f]{0,4}){2,7}(?:%[\w.]+)?`)

	reDomain = regexp.MustCompile(`(?i)\b(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,24}\b`)
)

func isWord(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

func hasDigit(line string, a, b int) bool { return strings.ContainsAny(line[a:b], "0123456789") }

func hasLetter(line string, a, b int) bool {
	for _, c := range []byte(line[a:b]) {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
			return true
		}
	}
	return false
}

var notUsers = map[string]bool{"public": true, "default": true, "default user": true, "all users": true, "root": false,
	"password": true, "pass": true, "privilege": true, "none": true, "n/a": true}

func realUser(line string, a, b int) bool { return !notUsers[strings.ToLower(line[a:b])] }

// devicePrompt: the line starts with a device prompt.
func devicePrompt(line string, a, b int) bool { return a == 0 && hasLetter(line, a, b) }

func macKey(v string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(v) {
		if c >= '0' && c <= '9' || c >= 'a' && c <= 'f' {
			b.WriteRune(c)
		}
	}
	return b.String()
}

func realMAC(line string, a, b int) bool {
	k := macKey(line[a:b])
	if k == "000000000000" || k == "ffffffffffff" {
		return false
	}
	// part of a longer colon list (an IPv6 address, a fingerprint)
	if b < len(line) && (line[b] == ':' || line[b] == '-') {
		return false
	}
	return a == 0 || (line[a-1] != ':' && line[a-1] != '-')
}

// realIPv4: four octets up to 255, not part of a longer dotted number;
// masks (255.…), wildcards and "any" (0.…) and loopback stay.
func realIPv4(line string, a, b int) bool {
	if a > 0 && (line[a-1] == '.' || isWord(line[a-1])) {
		return false
	}
	if b+1 < len(line) && line[b] == '.' && line[b+1] >= '0' && line[b+1] <= '9' {
		return false
	}
	parts := strings.Split(line[a:b], ".")
	for _, p := range parts {
		if n, err := strconv.Atoi(p); err != nil || n > 255 {
			return false
		}
	}
	switch parts[0] {
	case "0", "255", "127":
		return false
	}
	return true
}

func realIPv6(line string, a, b int) bool {
	v := line[a:b]
	if a > 0 && (isWord(line[a-1]) || line[a-1] == ':' || line[a-1] == '.') {
		return false
	}
	if b < len(line) && (isWord(line[b]) || line[b] == ':') {
		return false
	}
	addr := v
	if i := strings.IndexByte(addr, '%'); i >= 0 {
		addr = addr[:i]
	}
	if !strings.Contains(addr, "::") && strings.Count(addr, ":") < 7 {
		return false // a time (10:30:45), a MAC
	}
	ip := net.ParseIP(addr)
	return ip != nil && ip.To4() == nil && !ip.IsUnspecified() && !ip.IsLoopback()
}

var fileExt = map[string]bool{}

func init() {
	for _, e := range strings.Fields(`txt log exe dll sys ini cfg conf json xml yml yaml md html htm js ts go py sh ps1 bat cmd
		zip gz tar tgz bz2 xz 7z rar iso img bin png jpg jpeg gif svg webp pdf doc docx xls xlsx csv msi deb rpm so lnk tmp bak old
		jar war class java c h cpp cs rb php pl lua sql db sqlite vbs psm psd1 cer crt pem key pub der p12 pfx`) {
		fileExt[e] = true
	}
}

func realDomain(line string, a, b int) bool {
	v := line[a:b]
	if a > 0 && (line[a-1] == '@' || line[a-1] == '.' || line[a-1] == '-' || line[a-1] == '\\') {
		return false
	}
	tld := strings.ToLower(v[strings.LastIndexByte(v, '.')+1:])
	return !fileExt[tld] && hasLetter(line, a, b)
}
