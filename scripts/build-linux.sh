#!/bin/sh
# Builds Cmdex from source on Linux and installs it for the current user.
#
#   ./scripts/build-linux.sh               libraries (asks for sudo), Go, build, test, install
#   ./scripts/build-linux.sh --no-deps     skip installing the system libraries
#   ./scripts/build-linux.sh --no-install  build only: dist/cmdex
#   ./scripts/build-linux.sh --no-test     skip the tests
#
# Steps (the same as in README.md, "Build from source"):
#   1. system libraries: C compiler, pkg-config, GTK 3 and WebKitGTK
#      development files (Wails needs cgo and them);
#   2. Go: the version in go.mod; downloaded from go.dev into
#      ~/.local/go-<version> (no root) when missing or older, checksum checked;
#   3. Go modules: go mod download, then go mod verify against go.sum;
#   4. go test, then go build (WebKitGTK 4.1 when present, else 4.0);
#   5. packaging/linux/install.sh: ~/.local/bin/cmdex and a menu entry.
set -eu
cd "$(dirname "$0")/.."

DEPS=1 INSTALL=1 TEST=1
for a in "$@"; do
	case $a in
	--no-deps) DEPS=0 ;;
	--no-install) INSTALL=0 ;;
	--no-test) TEST=0 ;;
	*) echo "unknown option: $a" >&2; exit 2 ;;
	esac
done

step() { printf '\n== %s ==\n' "$*"; }
# tools the steps need, only when missing (Rocky's curl-minimal must not be replaced)
missing() { for t in "$@"; do command -v "$t" >/dev/null 2>&1 || printf '%s ' "$t"; done; }
as_root() { if [ "$(id -u)" = 0 ]; then "$@"; else sudo "$@"; fi; }

# ---- 1. system libraries ------------------------------------------------------
if [ "$DEPS" = 1 ]; then
	step "1. system libraries"
	[ -r /etc/os-release ] && . /etc/os-release
	family=" ${ID:-} ${ID_LIKE:-} "
	case $family in
	*" debian "* | *" ubuntu "*)
		apt() { as_root env DEBIAN_FRONTEND=noninteractive apt-get "$@"; }
		apt update
		apt install -y build-essential pkg-config ca-certificates libgtk-3-dev $(missing curl)
		# Ubuntu 22.04+, Debian 12+: WebKitGTK 4.1; older ones only have 4.0
		apt install -y libwebkit2gtk-4.1-dev || apt install -y libwebkit2gtk-4.0-dev
		;;
	*" fedora "* | *" rhel "* | *" centos "*)
		case $family in *" fedora "*) ;; *)
			# RHEL, Rocky, Alma: development files are in CodeReady Builder / CRB
			as_root dnf install -y dnf-plugins-core || true
			as_root dnf config-manager --set-enabled crb 2>/dev/null || as_root dnf config-manager --set-enabled powertools 2>/dev/null || true
		esac
		as_root dnf install -y gcc pkgconf-pkg-config ca-certificates tar gzip 'pkgconfig(gtk+-3.0)' $(missing curl)
		# Fedora, RHEL 10: WebKitGTK 4.1; RHEL/Rocky/Alma 9: only 4.0
		as_root dnf install -y 'pkgconfig(webkit2gtk-4.1)' || as_root dnf install -y 'pkgconfig(webkit2gtk-4.0)'
		;;
	*" arch "*)
		as_root pacman -S --needed --noconfirm base-devel pkgconf ca-certificates gtk3 webkit2gtk-4.1 $(missing curl)
		;;
	*" suse "* | *" opensuse "*)
		as_root zypper --non-interactive install gcc pkg-config ca-certificates tar gzip 'pkgconfig(gtk+-3.0)' $(missing curl)
		as_root zypper --non-interactive install 'pkgconfig(webkit2gtk-4.1)' || as_root zypper --non-interactive install 'pkgconfig(webkit2gtk-4.0)'
		;;
	*)
		echo "Unknown distribution (${ID:-?}). Install a C compiler, pkg-config, and the GTK 3 and" >&2
		echo "WebKitGTK 4.1 (or 4.0) development files, then run: $0 --no-deps" >&2
		exit 1
		;;
	esac
fi

# ---- 2. Go ---------------------------------------------------------------------
step "2. Go"
need=$(sed -n 's/^go //p' go.mod)
newer() { [ "$(printf '%s\n%s\n' "$2" "$1" | sort -V | head -n1)" = "$2" ]; } # $1 >= $2
have=""
if command -v go >/dev/null 2>&1; then have=$(go env GOVERSION | sed 's/^go//'); fi
if [ -z "$have" ] || ! newer "$have" "$need"; then
	case $(uname -m) in
	x86_64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) echo "no Go download for $(uname -m): install Go $need yourself" >&2; exit 1 ;;
	esac
	dir="$HOME/.local/go-$need"
	if [ ! -x "$dir/bin/go" ]; then
		file="go$need.linux-$arch.tar.gz"
		tmp=$(mktemp -d)
		echo "downloading $file"
		curl -fsSL "https://dl.google.com/go/$file" -o "$tmp/$file"
		curl -fsSL "https://dl.google.com/go/$file.sha256" -o "$tmp/sum"
		echo "$(cat "$tmp/sum")  $tmp/$file" | sha256sum -c -
		mkdir -p "$dir"
		tar -C "$dir" --strip-components=1 -xzf "$tmp/$file"
		rm -rf "$tmp"
	fi
	PATH="$dir/bin:$PATH"
	export PATH
fi
export GOTOOLCHAIN=local CGO_ENABLED=1
go version

# ---- 3. Go modules ---------------------------------------------------------------
step "3. Go modules"
go mod download
go mod verify

# ---- 4. test and build ------------------------------------------------------------
tags=desktop,production
ldflags="-w -s -X github.com/thangkes/cmdex/internal/app.Version=$(cat VERSION)"
if pkg-config --exists webkit2gtk-4.1; then
	tags=$tags,webkit2_41
elif pkg-config --exists webkit2gtk-4.0; then
	# released Linux programs need WebKitGTK 4.1: this one updates by building again
	ldflags="$ldflags -X github.com/thangkes/cmdex/internal/app.Updates=off"
	echo "WebKitGTK 4.0: self-update is off for this build; run this script again to update."
else
	echo "WebKitGTK development files not found (pkg-config webkit2gtk-4.1 or webkit2gtk-4.0)." >&2
	exit 1
fi
if [ "$TEST" = 1 ]; then
	step "4. test"
	go test ./...
fi
step "4. build ($tags)"
mkdir -p dist
go build -tags "$tags" -trimpath -buildvcs=false -ldflags "$ldflags" -o dist/cmdex .
cp build/appicon.png dist/cmdex.png
cp packaging/linux/cmdex.desktop packaging/linux/install.sh dist/
echo "built: dist/cmdex"

# ---- 5. install ------------------------------------------------------------------
if [ "$INSTALL" = 1 ]; then
	step "5. install"
	sh dist/install.sh
fi
