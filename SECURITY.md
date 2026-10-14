# Security

## Reporting a problem

Please report vulnerabilities privately through GitHub: **Security ▸ Report a vulnerability** on this repository. Do not open a public issue for them.

## What Cmdex protects, and how

| Area | Protection |
|---|---|
| SSH server identity | Keys are checked against `~/.ssh/known_hosts`. A new server shows its SHA-256 fingerprint and is trusted only after you confirm that exact fingerprint. A changed key is refused. |
| Passwords and key passphrases | Kept in the system keychain (Windows Credential Manager, Secret Service on Linux), never in the database, backups or logs. |
| Collected command lines | Secrets are masked before anything is stored: passwords in options, `user:pass@` and token-in-URL, `curl -u`, `Authorization`, cookies, `net use`, `docker login -p`, `ConvertTo-SecureString`, cloud CLI keys, and well-known token shapes (GitHub, GitLab, Slack, AWS, OpenAI-style, JWT). Lines collected by older versions are re-masked once at start. Add your own secret options under Collected ▸ rules. |
| Running commands | ▶ Run pastes exactly one line. A command holding line breaks, terminal escapes or invisible/direction-changing Unicode ("Trojan Source") is refused everywhere: on save, on pack and backup import, and on run. |
| Screen capture | On Windows 10 2004+, Cmdex's window is excluded from screenshots, screen recordings, remote viewing and screen sharing (shield button in the top bar, on by default; always on while the secure keypad is open). |
| Keyloggers | Passwords, passphrases and secrets for `sudo`/`Password:` prompts are entered on an on-screen keypad whose keys move after every click: no keystrokes to log, no click positions worth logging. The physical keyboard is ignored there. |
| Terminal output | Links printed by a remote session (OSC 8) open only if they are `http(s)`, only in the system browser, and only after you confirm. Output is never stored. |
| The window | Only Cmdex's own page can reach its functions; dropping files or links on the window does not navigate it. No network port is opened. |
| Help lookup | Never runs an unknown program: it reads `man`, PowerShell `Get-Help`, and `/?` only for console tools in Windows' System32, excluding a list of programs that act at once. |
| Imported files | Backups and packs are validated; a `.db` file is read from a copy with `trusted_schema` off. Exported files are readable by their owner only. |
| Updates | Downloaded from this repository's releases over HTTPS and installed only when the SHA-256 matches the published `SHA256SUMS.txt`. |
| `tools/devserve` (development only) | Listens on loopback only; every call needs a per-run token, the exact `Host`, and a JSON body (no cross-site requests, no DNS rebinding). |

## Known limits

- Telnet is unencrypted by design; use it only on trusted networks.
- Release files are not code-signed yet, so their integrity rests on this GitHub repository.
- The library database is not encrypted at rest; it holds host names, user names and masked command history, protected by your user account's file permissions.
- A command you add or import runs at once unless you set its safety to "ask before running" or "copy only".
