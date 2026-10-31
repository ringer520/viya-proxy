#!/bin/sh
# Viya one-line installer for Linux (systemd), any CPU architecture.
#
#   curl -fsSL https://raw.githubusercontent.com/ringer520/viya-proxy/main/install.sh | sudo sh
#
# The script detects the CPU architecture, downloads the matching prebuilt
# release, extracts it, and hands off to the bundled install-debian.sh (which
# provisions the service user, data directory, master key, systemd unit and
# readiness check).

set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "Please run as root: curl -fsSL ... | sudo sh" >&2
  exit 1
fi

if [ "$(uname -s)" != "Linux" ]; then
  echo "Error: Viya supports Linux only." >&2
  exit 1
fi

# The bundled installer provisions a systemd unit. On procd (OpenWrt) or other
# init systems, install manually — see the README's procd section.
if [ ! -d /run/systemd/system ] && ! command -v systemctl >/dev/null 2>&1; then
  echo "Error: this installer needs systemd (systemctl not found)." >&2
  echo "For OpenWrt / procd, follow the manual install steps in the README." >&2
  exit 1
fi

# Detect the CPU architecture and pick the matching prebuilt release.
case "$(uname -m)" in
  x86_64|amd64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  armv7l|armv6l) ARCH="armv7" ;;
  *) echo "Error: unsupported architecture '$(uname -m)'. Viya supports x86_64, aarch64 and armv7." >&2; exit 1 ;;
esac

REPO="ringer520/viya-proxy"
PKG="viya-linux-$ARCH"
TARBALL="$PKG.tar.gz"
BASE_URL="https://github.com/${REPO}/releases/latest/download/${TARBALL}"

# Allow pinning a version: VERSION=v1.2.3 curl ... | sudo sh
if [ -n "${VERSION:-}" ]; then
  BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}/${TARBALL}"
fi

download() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then
    wget -q "$1" -O "$2"
  else
    echo "Error: neither curl nor wget is available. Install one and retry." >&2
    exit 1
  fi
}

command -v tar >/dev/null 2>&1 || { echo "Error: tar is required." >&2; exit 1; }

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

echo "==> Downloading Viya from ${BASE_URL}"
download "$BASE_URL" "$TMPDIR/$TARBALL"

echo "==> Extracting $TARBALL"
tar -xzf "$TMPDIR/$TARBALL" -C "$TMPDIR"

echo "==> Installing"
sh "$TMPDIR/$PKG/deploy/install-debian.sh"

echo
echo "==> Viya installed successfully."
echo "    Open http://127.0.0.1:8181 (or your configured listener) and create your admin account."
