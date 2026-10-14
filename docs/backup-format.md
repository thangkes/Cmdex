# Cmdex backup file (`cmdex-backup`, version 2)

[English](#english) · [Tiếng Việt](#tiếng-việt)

## English

A backup is one UTF-8 JSON file holding your database: everything you added, collected or changed. The starter commands that come with Cmdex are not in it (every Cmdex brings them, newer ones with each update); what you did to them is: your edited copies (`baseKey`) and the ones you deleted or ran (`baseState`). Make one with **Library ▸ Export… ▸ Full backup** or `cmdex --export backup.json`; load it with **Library ▸ Import…** or `cmdex --import backup.json [--replace]`. You may also write or edit one by hand: every section except `format` and `version` may be left out or empty. A machine-readable description is in [`cmdex-backup.schema.json`](cmdex-backup.schema.json).

Not in a backup, on purpose:
- **passwords and key passphrases**, which stay in the system keychain;
- the read positions in this computer's shell history files.

Collected command lines are stored with secrets already masked (`••••••`).

```jsonc
{
  "format": "cmdex-backup",          // required, exactly this
  "version": 2,                      // required; 1 (Cmdex 0.1) is read too
  "exported": "2026-10-14T01:00:00Z",
  "app": "0.1.1",                    // Cmdex version that wrote it (informational)

  "commands": [{
    "name": "Ping a host",
    "platform": "windows",           // windows | linux | both
    "shell": "cmd",
    "category": "Network",
    "template": "ping {host} -n {count}",   // {parameters} in curly braces
    "purpose": "Checks a host answers",
    "notes": "",
    "danger": 0,                     // 0 run at once, 1 ask first, 2 copy only
    "tags": ["ping"],
    "envs": ["Lab"],
    "params": [{"key": "host", "type": "host", "default": "8.8.8.8", "description": "Host", "required": true}],
    "uses": 12,                      // times run (ranks it higher)
    "source": "user",                // user | collected | pack:<name>
    "created": 1792000000,           // Unix seconds
    "updated": 1792000000,           // newer copy wins when merging
    "baseKey": "windows ping {host} -n {count}"   // only on your copy of a starter command: "<platform> <template>" of the one it replaces
  }],

  "explain": [{
    "cmd": "ping",                   // program name, lower case, no path or .exe
    "summary": "Sends ICMP echo",
    "flags": {"-n": {"text": "send {v} requests", "value": true}},   // {v} = the option's value
    "letters": {"a": "all"},         // single letters that combine, like -ano
    "args": ["target host"],         // positional arguments in order
    "source": "user"                 // user | help | pack:<name>
  }],

  "synonyms": {"cong": ["port"]},    // normalized word (no accents) -> alternatives

  "rules": [
    {"kind": "category", "match": "ping, tracert", "result": "Network"},
    {"kind": "ignore",   "match": "cd, ls"},
    {"kind": "secret",   "match": "--token"}
  ],

  "connections": [{
    "kind": "ssh",                   // ssh | telnet | serial
    "name": "web-01", "host": "10.0.0.10", "port": 22, "user": "root",
    "auth": "key",                   // key | password | agent
    "keyPath": "~/.ssh/id_ed25519",
    "serial": {"port": "COM3", "baud": 9600, "dataBits": 8, "parity": "none", "stopBits": "1", "lineEnd": "cr"}
  }],

  "collected": [{
    "signature": "ping {host} -n {count}",
    "category": "Network", "source": "powershell",
    "status": "pending",             // pending | added | merged | ignored
    "secret": false,
    "firstSeen": 1792000000, "lastSeen": 1792000500,
    "target": {"platform": "windows", "template": "ping {host} -n {count}"},   // for added / merged
    "auto": false,                   // merged by the automatic merge (Collect settings)
    "variants": [{"raw": "ping 10.0.0.1 -n 4", "count": 3}]
  }],

  "settings": {"lang": "vi", "theme": "graphite", "collect.enabled": "1"},

  "baseState": [                     // what you did to starter commands and rules
    {"kind": "command", "key": "linux ls -la {path}", "uses": 9},       // key: "<platform> <template>"
    {"kind": "command", "key": "windows ipconfig /all", "hidden": true}, // deleted: stays deleted after updates
    {"kind": "rule", "key": "category ssh, scp", "hidden": true}         // key: "<kind> <match>"
  ]
}
```

### How importing works

| | Merge (default) | Replace everything |
|---|---|---|
| Commands (same platform + template) | the copy edited more recently wins; use counts take the larger | the file's |
| Explanations | yours (`source: user`) are kept | the file's |
| Rules, connections, collected groups | added if missing | the file's |
| Collected run counts | the larger one | the file's |
| Settings | taken from the file | taken from the file |
| Starter commands deleted / run counts (`baseState`) | deleted on either side stays deleted; the larger count | the file's |

Version 1 files (Cmdex 0.1) held the starter commands as your own: copies you never edited are matched to the starter commands (only their run counts are kept), edited ones become your commands.

An imported command is always yours: `source: "pack:…"` becomes `user`, and `baseKey` is kept only when the command runs exactly the starter command's line (its safety level is then never lower than the starter command's). So a file cannot pass its commands off as starter commands.

Importing the same file twice changes nothing the second time. A Cmdex database file (`.db`) can be imported the same way; it is read from a copy and never changed.

## Tiếng Việt

File sao lưu là một file JSON (UTF-8) chứa database của bạn: mọi thứ bạn thêm, thu thập hoặc sửa. Lệnh mẫu đi kèm Cmdex không nằm trong file (Cmdex nào cũng có sẵn, mỗi lần cập nhật lại mới hơn). Điều bạn đã làm với lệnh mẫu thì có: bản bạn đã sửa (`baseKey`), lệnh mẫu bạn đã xoá hoặc đã chạy (`baseState`).
- **Tạo:** **Thư viện lệnh ▸ Xuất… ▸ Sao lưu đầy đủ**, hoặc `cmdex --export backup.json`.
- **Nạp:** **Thư viện lệnh ▸ Nhập…**, hoặc `cmdex --import backup.json [--replace]`.
- **Tự viết hoặc sửa bằng tay được:** ngoài `format` và `version`, mọi phần khác có thể bỏ trống. Cấu trúc chính xác nằm trong ví dụ ở phần tiếng Anh phía trên và trong [`cmdex-backup.schema.json`](cmdex-backup.schema.json).

Cố ý **không** có trong file sao lưu:
- mật khẩu và passphrase, vì chúng nằm trong kho bảo mật của hệ điều hành;
- vị trí đã đọc trong file lịch sử shell của máy cũ.

Lệnh thu thập được lưu ở dạng đã che bí mật (`••••••`).

**Hai chế độ khi nạp:**
- **Gộp:**
  - Giữ dữ liệu đang có và thêm phần thiếu.
  - Lệnh trùng (cùng nền tảng và mẫu lệnh) thì bản được sửa gần đây hơn thắng; số lần dùng lấy số lớn hơn.
  - Giải nghĩa bạn tự viết được giữ nguyên.
  - Nạp lại cùng một file lần nữa không thay đổi gì.
- **Thay thế toàn bộ:** xóa sạch thư viện hiện tại rồi nạp file.

File phiên bản 1 (Cmdex 0.1) chứa lệnh mẫu như lệnh của bạn: lệnh mẫu chưa sửa được ghép với lệnh mẫu có sẵn (chỉ giữ số lần dùng), lệnh đã sửa thành lệnh của bạn.

Lệnh nhập từ file luôn là lệnh của bạn: `source: "pack:…"` đổi thành `user`, và `baseKey` chỉ được giữ khi lệnh chạy đúng dòng lệnh của lệnh mẫu (mức an toàn khi đó không thấp hơn lệnh mẫu). Nhờ vậy một file lạ không thể giả làm lệnh mẫu.

File database `.db` cũng nạp được theo cùng cách. Cmdex đọc từ một bản chép nên file gốc không bị thay đổi.
