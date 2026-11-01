#!/bin/sh
# Viya one-line installer for Linux systems booted with systemd.
#
#   curl -fsSL https://raw.githubusercontent.com/ringer520/viya-proxy/main/install.sh | sh
#
# The script detects the CPU architecture, downloads the matching prebuilt
# release, extracts it, and hands off to the bundled install-debian.sh (which
# provisions the service user, data directory, master key, systemd unit and
# readiness check).

set -eu

if [ "$(id -u)" -ne 0 ]; then
  if ! command -v sudo >/dev/null 2>&1; then
    echo "Error: this installer needs root privileges. Install sudo or run the command as root." >&2
    exit 1
  fi

  # Re-fetch under sudo: this script may itself be arriving through a pipe,
  # whose stdin cannot safely be handed to a second shell after self-copying.
  exec sudo env VERSION="${VERSION:-}" VIYA_INSTALL_LISTEN="${VIYA_INSTALL_LISTEN:-}" \
    sh -c 'curl -fsSL https://raw.githubusercontent.com/ringer520/viya-proxy/main/install.sh | sh'
fi

if [ "$(uname -s)" != "Linux" ]; then
  echo "Error: Viya supports Linux only." >&2
  exit 1
fi

# The bundled installer provisions a systemd unit. On procd (OpenWrt) or other
# init systems, install manually — see the README's procd section.
if [ ! -d /run/systemd/system ] || ! command -v systemctl >/dev/null 2>&1 || ! systemctl show-environment >/dev/null 2>&1; then
  echo "Error: this installer must run on a system booted with systemd." >&2
  echo "For OpenWrt / procd, follow the manual install steps in the README." >&2
  exit 1
fi

# Detect the CPU architecture and pick the matching prebuilt release.
case "$(uname -m)" in
  x86_64|amd64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  armv7l)         ARCH="armv7" ;;
  *) echo "Error: unsupported architecture '$(uname -m)'. Viya supports x86_64, aarch64 and armv7." >&2; exit 1 ;;
esac

REPO="ringer520/viya-proxy"
PKG="viya-linux-$ARCH"
TARBALL="$PKG.tar.gz"
# Allow pinning a version: VERSION=v1.2.3 curl ... | sudo sh
if [ -n "${VERSION:-}" ]; then
  RELEASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"
else
  RELEASE_URL="https://github.com/${REPO}/releases/latest/download"
fi
BASE_URL="${RELEASE_URL}/${TARBALL}"
CHECKSUM_URL="${RELEASE_URL}/sha256sums.txt"

download() {
  if command -v curl >/dev/null 2>&1; then
    curl -fL --progress-bar "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then
    wget -q "$1" -O "$2"
  else
    echo "Error: neither curl nor wget is available. Install one and retry." >&2
    exit 1
  fi
}

# --- Dependency preflight ---------------------------------------------------
# The release tarball ships the Viya and Caddy binaries as static builds, so
# there are no runtime libraries to install. Downloading and extracting the
# archive still needs tar + curl/wget, and every HTTPS request afterwards
# (release download, Let's Encrypt ACME, one-click self-update) needs the
# system CA bundle. Minimal images often lack one or more of these, so install
# the missing ones through the host package manager.
have() { command -v "$1" >/dev/null 2>&1; }

ca_bundle_present() {
  [ -s /etc/ssl/certs/ca-certificates.crt ] && return 0
  [ -s /etc/pki/tls/certs/ca-bundle.crt ] && return 0
  [ -s /etc/ssl/cert.pem ] && return 0
  [ -d /etc/ssl/certs ] && [ -n "$(ls -A /etc/ssl/certs 2>/dev/null)" ] && return 0
  return 1
}

MISSING=""
have tar || MISSING="$MISSING tar"
have curl || have wget || MISSING="$MISSING curl"
ca_bundle_present || MISSING="$MISSING ca-certificates"
have sha256sum || MISSING="$MISSING sha256sum"
have python3 || MISSING="$MISSING python3"

if [ -n "$MISSING" ]; then
  echo "==> Installing required tools:$MISSING"
  PKGS=""
  case "$MISSING" in *tar*) PKGS="$PKGS tar" ;; esac
  case "$MISSING" in *curl*) PKGS="$PKGS curl" ;; esac
  case "$MISSING" in *ca-certificates*) PKGS="$PKGS ca-certificates" ;; esac
  case "$MISSING" in *sha256sum*) PKGS="$PKGS coreutils" ;; esac
  case "$MISSING" in *python3*) PKGS="$PKGS python3" ;; esac
  if have apt-get; then
    apt-get update -qq && apt-get install -y -qq $PKGS
  elif have dnf; then
    dnf install -y -q $PKGS
  elif have yum; then
    yum install -y -q $PKGS
  elif have apk; then
    apk add --no-cache $PKGS
  elif have zypper; then
    zypper --non-interactive install $PKGS
  elif have pacman; then
    PKGS=$(echo "$PKGS" | sed 's/python3/python/')
    pacman -Sy --noconfirm $PKGS
  else
    echo "Error: missing required tools:$MISSING and no supported package manager found." >&2
    echo "Install them manually (e.g. apt-get install$PKGS) and re-run this script." >&2
    exit 1
  fi
fi

# Fail early with a clear message rather than a cryptic download error.
have tar || { echo "Error: tar is required." >&2; exit 1; }
{ have curl || have wget; } || { echo "Error: neither curl nor wget is available." >&2; exit 1; }
have sha256sum || { echo "Error: sha256sum is required to verify the release archive." >&2; exit 1; }
have python3 || { echo "Error: python3 is required for safe database backup and migration." >&2; exit 1; }

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

echo "==> Downloading Viya from ${BASE_URL}"
download "$BASE_URL" "$TMPDIR/$TARBALL"
download "$CHECKSUM_URL" "$TMPDIR/sha256sums.txt"

EXPECTED=$(awk -v file="$TARBALL" '$2 == file { print $1; exit }' "$TMPDIR/sha256sums.txt")
if [ "${#EXPECTED}" -ne 64 ]; then
  echo "Error: no valid SHA-256 entry found for $TARBALL." >&2
  exit 1
fi
ACTUAL=$(sha256sum "$TMPDIR/$TARBALL" | awk '{print $1}')
if [ "$ACTUAL" != "$EXPECTED" ]; then
  echo "Error: release archive checksum mismatch; refusing to install." >&2
  exit 1
fi
echo "==> Release archive checksum verified."

echo "==> Extracting $TARBALL"
tar -xzf "$TMPDIR/$TARBALL" -C "$TMPDIR"
if [ ! -f "$TMPDIR/$PKG/deploy/install-debian.sh" ] || [ ! -x "$TMPDIR/$PKG/bin/viya-linux-$ARCH" ] || [ ! -x "$TMPDIR/$PKG/bin/caddy-linux-$ARCH" ]; then
  echo "Error: release archive is incomplete or does not match architecture $ARCH." >&2
  exit 1
fi

echo "==> Installing"
sh "$TMPDIR/$PKG/deploy/install-debian.sh"

echo
echo "==> Viya installed successfully."
echo "    Open the panel from a trusted LAN:"
PANEL_PORT=${VIYA_INSTALL_LISTEN:-}
PANEL_PORT=${PANEL_PORT##*:}
if [ -z "$PANEL_PORT" ]; then
  PANEL_PORT=$(python3 - <<'PY' 2>/dev/null || true
import sqlite3
try:
    db = sqlite3.connect("file:/var/lib/viya/viya.db?mode=ro", uri=True, timeout=2)
    row = db.execute("SELECT value FROM panel_settings WHERE key='listen_port'").fetchone()
    print(row[0] if row and str(row[0]).isdigit() else "8181")
except Exception:
    print("8181")
PY
  )
fi
[ -n "$PANEL_PORT" ] || PANEL_PORT=8181
if command -v hostname >/dev/null 2>&1; then
  LAN_IPS=$(hostname -I 2>/dev/null || true)
  if [ -n "$LAN_IPS" ]; then
    for LAN_IP in $LAN_IPS; do
      case "$LAN_IP" in
        *:*) printf '      http://[%s]:%s\n' "$LAN_IP" "$PANEL_PORT" ;;
        *) printf '      http://%s:%s\n' "$LAN_IP" "$PANEL_PORT" ;;
      esac
    done
  else
    printf '      http://<server-LAN-IP>:%s\n' "$PANEL_PORT"
  fi
else
  printf '      http://<server-LAN-IP>:%s\n' "$PANEL_PORT"
fi
printf '    The panel listens on all interfaces by default; restrict TCP %s to your LAN in the host firewall if needed.\n' "$PANEL_PORT"
