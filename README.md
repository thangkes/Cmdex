# Cmdex

**A command library that you build as you work — with a builder, an explainer and built-in terminals (local shell, SSH, Telnet, serial/COM). Windows and Linux.**

[English](#english) · [Tiếng Việt](#tiếng-việt)

---

## English

### What it does

- **Find commands by asking.** Type what you want to do — "show open ports", "restart a service", or in Vietnamese with or without accents — and get the commands of your library ranked from most to least relevant. Commands for the platform of the session you are in come first.
- **Build them.** Every command is a template with `{parameters}`; fill them in and see the final line.
- **Run them.** ▶ Run pastes the line into the active terminal and presses Enter — a local shell (PowerShell, bash…), an SSH server, a Telnet device or a serial console.
- **Understand any command.** Paste a command line and Cmdex draws it with every part labelled — program, options, values, operators — with arrows pointing at each part. Combined options like `-ano` or `-tulpn` are split letter by letter. Knowledge comes from your library and from the help already on your computer (`man`, `Get-Help`, `/?`).
- **Collect while you work.** Cmdex reads your shell history and the commands you run through it, **hides passwords and tokens first**, turns values (IPs, hosts, ports, paths, numbers) into parameters, merges lines that differ only in values, classifies them with your rules, and waits for your approval before anything enters the library.
- **Your library, your environment.** Everything lives in one SQLite file on your computer (`%APPDATA%\Cmdex\cmdex.db`, `~/.config/cmdex/cmdex.db`). It starts with a starter pack of about 100 common Windows and Linux commands; edit or delete any of them, add your own, import and export packs as JSON.
- **English and Vietnamese interface**, five color themes, self-update from GitHub releases (checksum-verified).

### Install

- **Windows 10/11:** download `Cmdex-Windows-Setup-<version>.exe` from [Releases](https://github.com/thangkes/Cmdex/releases). It installs for your user only (no administrator rights).
- **Linux (x86-64):** download `Cmdex-Linux-amd64-<version>.tar.gz`, extract it and run `./install.sh` (installs to `~/.local/bin`, adds a menu entry). It needs WebKitGTK 4.1: `sudo apt install libwebkit2gtk-4.1-0` on Debian/Ubuntu.

### Security

- SSH servers are checked against `~/.ssh/known_hosts`. A new server shows its fingerprint before you trust it; a changed key is refused.
- Passwords and key passphrases are stored in the system keychain (Windows Credential Manager, Secret Service on Linux), never in the database.
- Collected commands have secrets masked before they are stored; collecting can be turned off per source.
- Help lookup never runs an unknown program: it reads `man` pages, PowerShell `Get-Help`, and `/?` only for Windows' own tools in System32.
- Updates are downloaded from this repository's releases and installed only when their SHA-256 matches the published `SHA256SUMS.txt`.

### Build from source

Go 1.27+. On Linux also `libgtk-3-dev libwebkit2gtk-4.1-dev`.

```sh
go test ./...
# Windows (installer needs Inno Setup 6)
./scripts/build-windows.ps1
# Linux
go build -tags desktop,production,webkit2_41 -o cmdex .
```

For interface work without the desktop shell: `CMDEX_DB=./tmp/cmdex.db go run ./tools/devserve`, then open http://127.0.0.1:34115.

### Packs

A pack is a JSON file of commands, explanations, synonyms and rules; texts may be plain strings or `{"en": "…", "vi": "…"}`. See [`packs/`](packs) for the starter packs. Library ▸ Import / Export moves your library between computers.

License: [MIT](LICENSE).

---

## Tiếng Việt

### Cmdex làm gì

- **Tìm lệnh bằng cách hỏi.** Gõ việc cần làm — "xem cổng đang mở", "khởi động lại dịch vụ", có dấu hay không dấu đều được — Cmdex liệt kê lệnh trong thư viện từ liên quan nhất đến ít nhất, ưu tiên lệnh hợp với phiên đang mở.
- **Dựng lệnh.** Mỗi lệnh là một mẫu có `{tham số}`; điền vào là thấy ngay dòng lệnh hoàn chỉnh.
- **Chạy luôn.** Nút ▶ Chạy dán lệnh vào terminal đang chọn và nhấn Enter — shell trên máy (PowerShell, bash…), máy chủ SSH, thiết bị Telnet hay cổng COM.
- **Hiểu mọi dòng lệnh.** Dán một lệnh vào, Cmdex vẽ lệnh đó với từng phần được ghi chú — chương trình, tùy chọn, giá trị, toán tử — có mũi tên chỉ vào đúng chỗ. Tùy chọn ghép như `-ano`, `-tulpn` được tách từng chữ. Giải nghĩa lấy từ thư viện của bạn và từ trợ giúp có sẵn trên máy (`man`, `Get-Help`, `/?`).
- **Tự thu thập khi bạn làm việc.** Cmdex đọc lịch sử shell và các lệnh chạy qua nó, **che mật khẩu/token trước tiên**, đổi giá trị (IP, tên máy, cổng, đường dẫn, số) thành tham số, gộp những dòng chỉ khác giá trị, phân loại theo quy tắc của bạn, và chờ bạn duyệt rồi mới đưa vào thư viện.
- **Thư viện của bạn, theo môi trường của bạn.** Mọi thứ nằm trong một file SQLite trên máy (`%APPDATA%\Cmdex\cmdex.db`, `~/.config/cmdex/cmdex.db`). Lần đầu chạy có sẵn khoảng 100 lệnh Windows và Linux thông dụng; sửa, xóa hay thêm tùy ý, nhập/xuất gói lệnh dạng JSON.
- **Giao diện tiếng Việt và tiếng Anh**, 5 theme màu, tự cập nhật từ GitHub (có kiểm tra mã băm).

### Cài đặt

- **Windows 10/11:** tải `Cmdex-Windows-Setup-<phiên bản>.exe` ở [Releases](https://github.com/thangkes/Cmdex/releases). Cài cho riêng tài khoản của bạn, không cần quyền Administrator.
- **Linux (x86-64):** tải `Cmdex-Linux-amd64-<phiên bản>.tar.gz`, giải nén rồi chạy `./install.sh` (cài vào `~/.local/bin` và thêm vào menu). Cần WebKitGTK 4.1: `sudo apt install libwebkit2gtk-4.1-0` trên Debian/Ubuntu.

### Bảo mật

- Máy chủ SSH được đối chiếu với `~/.ssh/known_hosts`. Máy chủ mới hiện vân tay để bạn xác nhận; khóa bị thay đổi thì Cmdex từ chối kết nối.
- Mật khẩu và passphrase lưu trong kho bảo mật của hệ điều hành (Windows Credential Manager, Secret Service trên Linux), không bao giờ nằm trong database.
- Lệnh thu thập được che bí mật trước khi lưu; tắt được từng nguồn thu thập.
- Tra trợ giúp không bao giờ chạy chương trình lạ: chỉ đọc `man`, `Get-Help` của PowerShell, và `/?` với công cụ hệ thống của Windows trong System32.
- Bản cập nhật tải từ Releases của repo này và chỉ được cài khi mã SHA-256 khớp với `SHA256SUMS.txt` đã công bố.

Giấy phép: [MIT](LICENSE).
