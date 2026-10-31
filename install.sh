#!/bin/sh
# Viya one-line installer for Debian/Ubuntu.
#
#   curl -fsSL https://raw.githubusercontent.com/ringer520/viya-proxy/main/install.sh | sudo sh
#
# The script downloads the latest prebuilt release, extracts it, and hands off
# to the bundled install-debian.sh (which provisions the service user, data
# directory, master key, systemd unit and readiness check).

set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "Please run as root: curl -fsSL ... | sudo sh" >&2
  exit 1
fi

REPO="ringer520/viya-proxy"
PKG="viya-linux-amd64"
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
