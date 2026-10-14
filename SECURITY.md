# Security

## Reporting a problem

Please report vulnerabilities privately through GitHub: **Security ▸ Report a vulnerability** on this repository. Do not open a public issue for them.

## What Cmdex protects, and how

| Area | Protection |
|---|---|
| SSH server identity | Keys are checked against `~/.ssh/known_hosts`. A new server shows its SHA-256 fingerprint and is trusted only after you confirm that exact fingerprint. A changed key is refused. |
| Passwords and key passphrases | Kept in the system keychain (Windows Credential Manager, Secret Service on Linux), never in the database, backups or logs. |
| The library database | Encrypted (since 0.1.6): connections (name, host, user, key path, serial settings), your commands (name, template, purpose, notes, tags, parameters) and the collected log (templates and lines) are sealed with AES-256-GCM; lookups use keyed hashes (HMAC-SHA256), not the values. The key is random and kept in the system keychain; where there is none, it comes from a master password (Argon2id). A copied database file cannot be read without it. The first start of 0.1.6 encrypts an existing library (and its `cmdex.db.bak-v1` copy) and rewrites the files so the plain values are gone from the disk. The starter commands' database holds nothing private and is not encrypted. |
| Collected command lines | Secrets are masked before anything is stored: passwords in options, `user:pass@` and token-in-URL, `curl -u`, `Authorization`, cookies, `net use`, `docker login -p`, `ConvertTo-SecureString`, cloud CLI keys, and well-known token shapes (GitHub, GitLab, Slack, AWS, OpenAI-style, JWT). Lines collected by older versions are re-masked once at start. Add your own secret options under Collected ▸ rules. |
| Running commands | Cmdex never runs a command by itself: ▶ **pastes** exactly one line into the terminal and **you press Enter** (since 0.1.6). Commands are labelled by risk: **Warning** (changes the system) and **Critical** (may delete data or cut service: recursive delete, format, restart, firewall off, wiping a device configuration). The label is the higher of the level a command declares and the one Cmdex detects in its text, so imported, collected and typed commands are labelled too. A command holding line breaks, terminal escapes or invisible/direction-changing Unicode ("Trojan Source") is refused everywhere: on save, on pack and backup import, and on paste. |
| Screen capture | On Windows 10 2004+, Cmdex's window is excluded from screenshots, screen recordings, remote viewing and screen sharing (shield button in the top bar, on by default; always on while the secure keypad is open). |
| Keyloggers | Passwords, passphrases and secrets for `sudo`/`Password:` prompts are entered on an on-screen keypad whose keys move after every click: no keystrokes to log, no click positions worth logging. The physical keyboard is ignored there. |
| Terminal output | Links printed by a remote session (OSC 8) open only if they are `http(s)`, only in the system browser, and only after you confirm. Output is never stored. |
| The window | Only Cmdex's own page can reach its functions; dropping files or links on the window does not navigate it. No network port is opened. |
| Help lookup | Never runs an unknown program: it reads `man`, PowerShell `Get-Help`, and `/?` only for console tools in Windows' System32, excluding a list of programs that act at once. |
| Imported files | Backups and packs are validated; a `.db` file is read from a copy with `trusted_schema` off. An imported command is always yours, never shown as a starter command: it cannot hide a starter command behind one of its own, and a copy of a starter command keeps that link only when it runs the same line, never asking less before running. Exported files are readable by their owner only. |
| Clearing the collected log | Deleted lines are erased from the disk (the database file is rewritten, its journal emptied), in the database and in the copy made when upgrading to 0.1.3 (`cmdex.db.bak-v1`). That copy is readable by its owner only and gets the same secret masking as the database. |
| Starter commands | Built at start from the packs inside the program into a separate, read-only database; checked like any command. What you change lives in your own database, which updates never rewrite. |
| Updates | Downloaded from this repository's releases over HTTPS. From 0.1.6, a release is installed only when its `SHA256SUMS.txt` carries a valid Ed25519 signature (`SHA256SUMS.txt.sig`) by the release key kept on the maintainer's computer (or the offline backup key), and the file's SHA-256 matches it: a compromised GitHub account alone cannot ship an update. The release script checks both builds against the checksums before signing. |
| `tools/devserve` (development only) | Listens on loopback only; every call needs a per-run token, the exact `Host`, and a JSON body (no cross-site requests, no DNS rebinding). |

## Known limits

- Telnet is unencrypted by design; use it only on trusted networks.
- Release files are not code-signed (Authenticode) yet, so Windows SmartScreen may warn on first install; updates are protected by the signed checksums above.
- Encryption protects the database file (a lost disk, a copied file, a leaked backup of the folder), not against a program running as you, which can ask the keychain as Cmdex does.
- **JSON backups are not encrypted**: they hold your library in clear (readable by your account only). Keep them as safe as the library itself.
- Interface settings (language, theme, filters) and the names of programs you asked about are not encrypted.
- Going back to 0.1.5 or older after 0.1.6 is not supported: those versions cannot read the encrypted values.
- **Correction (2026-10-14):** in 0.1.5 and earlier, ▶ Run pastes a command and presses Enter at once, whatever its safety level: "ask before running" and "copy only" are stored but **not enforced** (earlier versions of this file and of the 0.1.4/0.1.5 release notes said otherwise). Update to 0.1.6 or later, where ▶ only pastes.
- Risk labels come from patterns: a dangerous command written in an unusual way may not be labelled. Read a command before pressing Enter.
