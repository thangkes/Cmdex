#!/bin/sh
# Installs Cmdex for the current user: the program in ~/.local/bin (where
# self-update can replace it) and a menu entry. No root needed. Then checks
# what the system still needs and says how to add it on this distribution.
set -e
here=$(cd "$(dirname "$0")" && pwd)
bin="$HOME/.local/bin"
apps="${XDG_DATA_HOME:-$HOME/.local/share}/applications"
icons="${XDG_DATA_HOME:-$HOME/.local/share}/icons/hicolor/512x512/apps"
mkdir -p "$bin" "$apps" "$icons"
install -m 755 "$here/cmdex" "$bin/cmdex"
install -m 644 "$here/cmdex.png" "$icons/cmdex.png"
sed "s|^Exec=.*|Exec=$bin/cmdex|" "$here/cmdex.desktop" > "$apps/cmdex.desktop"
command -v update-desktop-database >/dev/null 2>&1 && update-desktop-database "$apps" || true
echo "Cmdex installed: $bin/cmdex"

[ -r /etc/os-release ] && . /etc/os-release
family=" ${ID:-} ${ID_LIKE:-} "
pkg() { # the command installing a package on this distribution
	case $family in
	*" debian "* | *" ubuntu "*) echo "sudo apt install $1" ;;
	*" fedora "* | *" rhel "* | *" centos "*) echo "sudo dnf install $2" ;;
	*" arch "*) echo "sudo pacman -S $3" ;;
	*" suse "* | *" opensuse "*) echo "sudo zypper install $4" ;;
	*) echo "install: $1 (Debian/Ubuntu name)" ;;
	esac
}

# libraries the program is linked to (WebKitGTK 4.1, GTK 3)
if command -v ldd >/dev/null 2>&1 && ldd "$bin/cmdex" 2>/dev/null | grep -q "not found"; then
	echo
	echo "Missing libraries:"
	ldd "$bin/cmdex" | grep "not found" | sed 's/^/  /'
	echo "Install WebKitGTK 4.1:"
	echo "  $(pkg libwebkit2gtk-4.1-0 webkit2gtk4.1 webkit2gtk-4.1 libwebkit2gtk-4_1-0)"
	echo "If your distribution has no WebKitGTK 4.1 (RHEL/Rocky/Alma 9, Ubuntu 20.04),"
	echo "build Cmdex from source instead: scripts/build-linux.sh (see README)."
fi

# ~/.local/bin on the PATH, to start it as "cmdex" from a terminal
case ":$PATH:" in *":$bin:"*) ;; *)
	echo
	echo "To start it from a terminal, add ~/.local/bin to your PATH (then open a new terminal):"
	echo "  echo 'export PATH=\"\$HOME/.local/bin:\$PATH\"' >> ~/.profile"
	;;
esac

# serial consoles (COM / USB-serial): the dialout group (uucp on Arch)
group=dialout
case $family in *" arch "*) group=uucp ;; esac
if getent group "$group" >/dev/null 2>&1 && ! id -nG | tr ' ' '\n' | grep -qx "$group"; then
	echo
	echo "For serial consoles (Cisco console cable, USB-serial), join the $group group, then log out and in:"
	echo "  sudo usermod -aG $group \"\$USER\""
fi

# remembering passwords needs a Secret Service keyring (GNOME Keyring, KWallet)
if command -v busctl >/dev/null 2>&1 && ! busctl --user status org.freedesktop.secrets >/dev/null 2>&1; then
	echo
	echo "No password keyring is running: Cmdex works, but cannot remember passwords. To add one:"
	echo "  $(pkg gnome-keyring gnome-keyring gnome-keyring gnome-keyring)"
fi
exit 0
