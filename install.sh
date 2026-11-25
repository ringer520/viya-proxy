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

REPO="ringer520/viya-proxy"

# Mirror candidates in one place: the Go self-updater probes the same hosts, and
# third-party mirrors come and go, so keeping the list here avoids editing three
# loops whenever it changes.
MIRRORS="https://ghfast.top/ https://ghproxy.net/ https://gh-proxy.com/"

# Normalise GH_PROXY before anything reads it: the sudo re-exec, the OpenWrt
# bootstrap and the release URLs all concatenate it with an absolute URL, so a
# missing trailing slash produces "https://ghproxy.nethttps://…".
GH_PROXY="${GH_PROXY:-}"
if [ -n "$GH_PROXY" ]; then
  case "$GH_PROXY" in
    */) ;;
    *) GH_PROXY="${GH_PROXY}/" ;;
  esac
fi

# Pick a mirror when direct GitHub access is unavailable. This runs before the
# OpenWrt branch: routers in mainland China are exactly the hosts that cannot
# reach raw.githubusercontent.com, and they used to hit the bootstrap download
# with no mirror at all.
probe_mirror() {
  [ -z "${GH_PROXY:-}" ] || return 0
  target="https://github.com/${REPO}/releases/latest/download/sha256sums.txt"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL -I --connect-timeout 4 --max-time 8 "$target" >/dev/null 2>&1 && return 0
    for mirror in $MIRRORS; do
      if curl -fsSL -I --connect-timeout 4 --max-time 8 "${mirror}${target}" >/dev/null 2>&1; then
        GH_PROXY="$mirror"
        echo "==> Direct GitHub releases unreachable; using mirror: $GH_PROXY"
        return 0
      fi
    done
  elif command -v wget >/dev/null 2>&1; then
    wget --spider -T 6 -q "$target" >/dev/null 2>&1 && return 0
    for mirror in $MIRRORS; do
      if wget --spider -T 6 -q "${mirror}${target}" >/dev/null 2>&1; then
        GH_PROXY="$mirror"
        echo "==> Direct GitHub releases unreachable; using mirror: $GH_PROXY"
        return 0
      fi
    done
  fi
  return 0
}

# One download attempt. "$3" picks the timeout policy:
#   fast  small files (checksums, bootstrap script)
#   big   release archive: low-speed circuit breaker
#   slow  last resort: no speed guard and a generous ceiling, so a working but
#         slow link still finishes instead of failing the install
download_once() {
  url="$1"
  dest="$2"
  mode="${3:-fast}"
  if command -v curl >/dev/null 2>&1; then
    case "$mode" in
      big)
        # Abort once the transfer stays below 120 KB/s for 15 seconds. The
        # release is 27 MB, so at 40 KB/s --max-time alone would burn the full
        # ten minutes and then fail; retries are left to the mirror loop, since
        # --retry would restart that same stopwatch on the same slow host.
        curl -fL --connect-timeout 10 --max-time 900 \
          --speed-limit 122880 --speed-time 15 --progress-bar "$url" -o "$dest"
        ;;
      slow)
        curl -fL --retry 1 --connect-timeout 15 --max-time 1800 --progress-bar "$url" -o "$dest"
        ;;
      *)
        curl -fL --retry 1 --connect-timeout 10 --max-time 120 --progress-bar "$url" -o "$dest"
        ;;
    esac
  elif command -v wget >/dev/null 2>&1; then
    # -T bounds a stalled read on both GNU and busybox wget; --tries differs
    # between the two, so leave retries to the mirror loop below. wget has no
    # byte-rate guard, which is why the slow mode also raises its timeout.
    if [ "$mode" = slow ]; then
      wget -T 60 "$url" -O "$dest"
    else
      wget -T 20 "$url" -O "$dest"
    fi
  else
    echo "Error: neither curl nor wget is available. Install one and retry." >&2
    exit 1
  fi
}

# Sample ~512 KiB from a candidate and print its speed in bytes per second.
# A HEAD request only proves reachability; mirrors routinely differ by 10x in
# throughput, so the fixed order in MIRRORS can pick a working-but-slow route.
sample_speed() {
  url="$1"
  # --max-filesize makes a server that ignores Range fail immediately instead of
  # streaming the whole 27 MB archive into /dev/null just to time it. Throughput
  # is computed from the transfer window only: on a 512 KiB sample, TLS and
  # time-to-first-byte would otherwise dominate and flatten every fast mirror
  # into the same few hundred KB/s.
  metrics=$(curl -s -o /dev/null -r 0-524287 -m 6 --max-filesize 1048576 \
    -w '%{size_download} %{time_total} %{time_starttransfer}' "$url" 2>/dev/null || true)
  # shellcheck disable=SC2086
  set -- $metrics
  awk -v size="${1:-0}" -v total="${2:-0}" -v ttfb="${3:-0}" 'BEGIN {
    if (size <= 0) { print 0; exit }
    window = total - ttfb
    if (window < 0.05) window = 0.05
    printf "%.0f\n", size / window
  }'
}

# Print the candidates fastest-first, keeping failures at the end so the caller
# still tries them. Falls back to the plain URL when curl is unavailable or the
# sampling produced nothing usable.
rank_download_routes() {
  url="$1"
  if ! command -v curl >/dev/null 2>&1; then
    printf '%s\n' "$url"
    return 0
  fi
  ranked=$(
    {
      printf '%s\n' "$url"
      for mirror in $MIRRORS; do printf '%s\n' "${mirror}${url}"; done
      [ -z "${GH_PROXY:-}" ] || printf '%s\n' "${GH_PROXY}${url}"
    } | awk '!seen[$0]++' | while IFS= read -r candidate; do
      printf '%s %s\n' "$(sample_speed "$candidate")" "$candidate"
    done | sort -k1,1 -rn | awk '{ sub(/^[^ ]+ /, ""); print }'
  )
  if [ -n "$ranked" ]; then
    printf '%s\n' "$ranked"
  else
    printf '%s\n' "$url"
  fi
}

# Try the chosen route first, then every other mirror. The URL may already
# carry a mirror prefix, so reduce it to the canonical GitHub address and skip
# the mirror that just failed.
download() {
  url="$1"
  dest="$2"
  mode="${3:-fast}"
  if [ "$mode" = big ]; then
    # Large files are worth ranking first: sampling costs a couple of megabytes
    # but avoids spending minutes on a route that only just clears the guard.
    ranked=$(rank_download_routes "$url")
    while IFS= read -r candidate; do
      [ -n "$candidate" ] || continue
      [ "$candidate" = "$url" ] || echo "==> Trying ranked route: $candidate"
      if download_once "$candidate" "$dest" "$mode"; then
        return 0
      fi
    done <<EOF
$ranked
EOF
    echo "==> No ranked route completed; retrying $url without the speed guard."
    echo "    This can take several minutes on a slow link; progress is shown below."
    if download_once "$url" "$dest" slow; then
      return 0
    fi
    echo "Error: failed to download $url" >&2
    exit 1
  fi
  if download_once "$url" "$dest" "$mode"; then
    return 0
  fi

  raw_url=""
  case "$url" in
    https://github.com/*|https://raw.githubusercontent.com/*)
      raw_url="$url"
      ;;
    *https://github.com/*)
      raw_url="https://github.com/${url##*https://github.com/}"
      ;;
    *https://raw.githubusercontent.com/*)
      raw_url="https://raw.githubusercontent.com/${url##*https://raw.githubusercontent.com/}"
      ;;
  esac
  if [ -n "$raw_url" ]; then
    for mirror in $MIRRORS; do
      mirror_url="${mirror}${raw_url}"
      if [ "$mirror_url" = "$url" ]; then
        continue
      fi
      echo "==> Download failed. Retrying via mirror: ${mirror_url}"
      if download_once "$mirror_url" "$dest" "$mode"; then
        return 0
      fi
    done
  fi

  # Nothing answered at a usable speed. Retry the original URL with the speed
  # guard removed and a much longer ceiling: a genuinely slow line must still be
  # able to install, which the old --max-time 600 download allowed.
  if [ "$mode" = big ]; then
    echo "==> No route reached 120 KB/s; downloading from $url without the speed guard."
    echo "    This can take several minutes on a slow link; progress is shown below."
    if download_once "$url" "$dest" slow; then
      return 0
    fi
  fi

  echo "Error: failed to download $url" >&2
  exit 1
}

if [ "$(id -u)" -ne 0 ]; then
  if ! command -v sudo >/dev/null 2>&1; then
    echo "Error: this installer needs root privileges. Install sudo or run the command as root." >&2
    exit 1
  fi
  if [ -f "$0" ]; then
    # Started from a saved file (`curl -o install.sh` then `sh install.sh`):
    # reuse it instead of fetching the script again over the same flaky link.
    exec sudo env VERSION="${VERSION:-}" VIYA_INSTALL_LISTEN="${VIYA_INSTALL_LISTEN:-}" \
      GH_PROXY="${GH_PROXY:-}" sh "$0"
  fi
  # Piped through `curl | sh`: the script body lives on stdin, so it cannot be
  # handed to a second shell and has to be fetched again under sudo.
  exec sudo env VERSION="${VERSION:-}" VIYA_INSTALL_LISTEN="${VIYA_INSTALL_LISTEN:-}" GH_PROXY="${GH_PROXY:-}" \
    sh -c 'set -eu; task=$(mktemp -d); trap "rm -rf \"$task\"" EXIT; if command -v curl >/dev/null 2>&1; then curl -fL --retry 2 --connect-timeout 15 --max-time 120 "${GH_PROXY:-}https://raw.githubusercontent.com/ringer520/viya-proxy/main/install.sh" -o "$task/install.sh"; else wget -O "$task/install.sh" "${GH_PROXY:-}https://raw.githubusercontent.com/ringer520/viya-proxy/main/install.sh"; fi; sh "$task/install.sh"'
fi

if [ "$(uname -s)" != "Linux" ]; then
  echo "Error: Viya supports Linux only." >&2
  exit 1
fi

# OpenWrt uses procd and its persistent-storage installer.
if [ -r /etc/openwrt_release ]; then
  probe_mirror
  stage=$(mktemp -d /tmp/viya-bootstrap.XXXXXX)
  trap 'rm -rf "$stage"' EXIT
  export GH_PROXY
  download "${GH_PROXY}https://raw.githubusercontent.com/${REPO}/main/deploy/install-openwrt.sh" "$stage/install.sh" fast
  sh "$stage/install.sh"
  exit $?
fi

# The bundled installer provisions a systemd unit. On procd (OpenWrt) or other
# init systems, install manually — see the README's procd section.
if [ ! -d /run/systemd/system ] || ! command -v systemctl >/dev/null 2>&1 || ! systemctl show-environment >/dev/null 2>&1; then
  echo "Error: this installer must run on a system booted with systemd." >&2
  echo "For OpenWrt / procd, follow the manual install steps in the README." >&2
  exit 1
fi

# Probing takes up to a few seconds per mirror, so it only runs once the host is
# known to be a supported systemd machine (or the OpenWrt branch above).
probe_mirror

# Detect the CPU architecture and pick the matching prebuilt release.
case "$(uname -m)" in
  x86_64|amd64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  armv7l)         ARCH="armv7" ;;
  *) echo "Error: unsupported architecture '$(uname -m)'. Viya supports x86_64, aarch64 and armv7." >&2; exit 1 ;;
esac

PKG="viya-linux-$ARCH"
TARBALL="$PKG.tar.gz"

# Pin a version with: curl ... | VERSION=v1.2.3 sh
if [ -n "${VERSION:-}" ]; then
  RELEASE_URL="${GH_PROXY}https://github.com/${REPO}/releases/download/${VERSION}"
else
  RELEASE_URL="${GH_PROXY}https://github.com/${REPO}/releases/latest/download"
fi
BASE_URL="${RELEASE_URL}/${TARBALL}"
CHECKSUM_URL="${RELEASE_URL}/sha256sums.txt"

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
# The backup helper sticks to the Python 3.6 standard library so older
# long-term distributions keep working; anything older fails with one clear line.
if ! python3 -c 'import sys; raise SystemExit(0 if sys.version_info >= (3, 6) else 1)' 2>/dev/null; then
  echo "Error: $(python3 -V 2>&1) is too old. Viya needs Python 3.6 or newer for safe database backup and migration." >&2
  exit 1
fi

TASK_TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TASK_TMPDIR"' EXIT

echo "==> Downloading Viya from ${BASE_URL}"
download "$BASE_URL" "$TASK_TMPDIR/$TARBALL" big
download "$CHECKSUM_URL" "$TASK_TMPDIR/sha256sums.txt" fast

EXPECTED=$(awk -v file="$TARBALL" '$2 == file { print $1; exit }' "$TASK_TMPDIR/sha256sums.txt")
if [ "${#EXPECTED}" -ne 64 ]; then
  echo "Error: no valid SHA-256 entry found for $TARBALL." >&2
  exit 1
fi
ACTUAL=$(sha256sum "$TASK_TMPDIR/$TARBALL" | awk '{print $1}')
if [ "$ACTUAL" != "$EXPECTED" ]; then
  echo "Error: release archive checksum mismatch; refusing to install." >&2
  exit 1
fi
echo "==> Release archive checksum verified."

echo "==> Extracting $TARBALL"
tar -xzf "$TASK_TMPDIR/$TARBALL" -C "$TASK_TMPDIR"
if [ ! -f "$TASK_TMPDIR/$PKG/deploy/install-debian.sh" ] || [ ! -x "$TASK_TMPDIR/$PKG/bin/viya-linux-$ARCH" ] || [ ! -x "$TASK_TMPDIR/$PKG/bin/caddy-linux-$ARCH" ]; then
  echo "Error: release archive is incomplete or does not match architecture $ARCH." >&2
  exit 1
fi

echo "==> Installing"
sh "$TASK_TMPDIR/$PKG/deploy/install-debian.sh"

echo
echo "==> Viya installed successfully."
echo "    Open the panel from a trusted LAN:"
PANEL_PORT=${VIYA_INSTALL_LISTEN:-}
PANEL_PORT=${PANEL_PORT##*:}
if [ -z "$PANEL_PORT" ]; then
  PANEL_PORT=$(python3 - <<'PY' 2>/dev/null || true
import shlex, sqlite3, subprocess
port = "8181"
try:
    env = subprocess.check_output(["systemctl", "show", "viya.service", "--property=Environment", "--value"], universal_newlines=True)
    settings = dict(item.split("=", 1) for item in shlex.split(env) if "=" in item)
    listen = settings.get("VIYA_LISTEN", "")
    if listen.startswith("["):
        port = listen[listen.rfind("]:") + 2:]
    elif ":" in listen:
        port = listen.rsplit(":", 1)[1]
except Exception:
    pass
try:
    db = sqlite3.connect("file:/var/lib/viya/viya.db?mode=ro", uri=True, timeout=2)
    row = db.execute("SELECT value FROM panel_settings WHERE key='listen_port'").fetchone()
    if row and str(row[0]).isdigit(): port = str(row[0])
except Exception:
    pass
print(port if port.isdigit() else "8181")
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
# Describe the address the unit was actually installed with. The bare binary
# defaults to loopback, while the installer defaults to a wildcard bind; a
# custom VIYA_INSTALL_LISTEN can be either, so a fixed "all interfaces" line
# would be wrong for tunnel-only and single-LAN-IP installs.
PANEL_BIND_HOST=$(python3 - <<'PY' 2>/dev/null || true
import shlex, subprocess
try:
    env = subprocess.check_output(
        ["systemctl", "show", "viya.service", "--property=Environment", "--value"],
        universal_newlines=True,
    )
    settings = dict(item.split("=", 1) for item in shlex.split(env) if "=" in item)
    listen = settings.get("VIYA_LISTEN", "")
    host = listen.rsplit(":", 1)[0] if ":" in listen else listen
    print(host.strip("[]").lower())
except Exception:
    pass
PY
  )
if [ "$PANEL_BIND_HOST" = "127.0.0.1" ] || [ "$PANEL_BIND_HOST" = "::1" ] || [ "$PANEL_BIND_HOST" = "localhost" ]; then
  printf '    The panel is bound to loopback only; open it through an SSH tunnel.\n'
elif [ "$PANEL_BIND_HOST" = "0.0.0.0" ] || [ "$PANEL_BIND_HOST" = "::" ] || [ -z "$PANEL_BIND_HOST" ]; then
  printf '    The panel listens on all interfaces; restrict TCP %s to your LAN in the host firewall if needed.\n' "$PANEL_PORT"
else
  printf '    The panel is bound to %s; make sure that address is reachable from your LAN.\n' "$PANEL_BIND_HOST"
fi
