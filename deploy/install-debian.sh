#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "Run this installer as root." >&2
  exit 1
fi

if [ ! -d /run/systemd/system ] || ! command -v systemctl >/dev/null 2>&1 || ! systemctl show-environment >/dev/null 2>&1; then
  echo "This installer requires a running systemd system." >&2
  exit 1
fi
if ! command -v python3 >/dev/null 2>&1; then
  echo "python3 is required for consistent database backup, migration and readiness checks." >&2
  exit 1
fi
if ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; then
  echo "curl or wget is required for the service readiness check." >&2
  exit 1
fi

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  armv7l) ARCH=armv7 ;;
  *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac
viya_bin="$ROOT/bin/viya-linux-$ARCH"
caddy_bin="$ROOT/bin/caddy-linux-$ARCH"
if [ ! -x "$viya_bin" ] || [ ! -x "$caddy_bin" ]; then
  echo "The release package does not contain executable binaries for $ARCH." >&2
  exit 1
fi

legacy_service=crpm.service
legacy_data=/var/lib/crpm
legacy_config=/etc/crpm
legacy_unit=0
viya_upgrade=0
viya_was_active=0
viya_was_enabled=0
rollback_dir=""
rollback_archive=""
rollback_ready=0
listen_dropin_written=0
custom_listen_dropin=/etc/systemd/system/viya.service.d/90-installer-listen.conf

if { [ -s "$legacy_data/crpm.db" ] || [ -s "$legacy_data/viya.db" ]; } && [ -s /var/lib/viya/viya.db ]; then
  echo "Both legacy CRPM and Viya databases exist. No files were changed." >&2
  echo "Back up both installations and choose which database to keep before retrying." >&2
  exit 1
fi
if [ -s "$legacy_data/crpm.db" ] && [ -s "$legacy_data/viya.db" ]; then
  echo "Both crpm.db and viya.db exist in $legacy_data. No files were changed; choose which database to migrate." >&2
  exit 1
fi
if [ -s /var/lib/viya/crpm.db ] && [ -s /var/lib/viya/viya.db ]; then
  echo "Both crpm.db and viya.db exist in /var/lib/viya. No files were changed." >&2
  echo "Resolve the database conflict before retrying the installation." >&2
  exit 1
fi
for keypath in \
  /var/lib/viya/master.key \
  /etc/viya/secrets/master.key \
  "$legacy_data/master.key" \
  "$legacy_config/master.key"; do
  if [ -L "$keypath" ]; then
    echo "Master key path $keypath is a symbolic link. Resolve it before retrying." >&2
    exit 1
  fi
done
for pair in \
  "/var/lib/viya/master.key:/etc/viya/secrets/master.key" \
  "$legacy_data/master.key:$legacy_config/master.key" \
  "/var/lib/viya/master.key:$legacy_config/master.key" \
  "$legacy_data/master.key:/etc/viya/secrets/master.key"; do
  left=${pair%%:*}
  right=${pair#*:}
  if [ -s "$left" ] && [ -s "$right" ] && ! cmp -s "$left" "$right"; then
    echo "Conflicting master keys found at $left and $right. No service was stopped; resolve the key conflict before retrying." >&2
    exit 1
  fi
done

if [ -x /opt/viya/bin/viya ] && [ -f /etc/systemd/system/viya.service ]; then
  viya_upgrade=1
  systemctl is-active --quiet viya.service && viya_was_active=1 || true
  systemctl is-enabled --quiet viya.service && viya_was_enabled=1 || true
fi

if [ -n "${VIYA_INSTALL_LISTEN:-}" ]; then
  if [ -s /var/lib/viya/viya.db ] || [ -s "$legacy_data/crpm.db" ] || [ -s "$legacy_data/viya.db" ]; then
    echo "VIYA_INSTALL_LISTEN is only for a fresh install. Existing installations keep their saved panel address." >&2
    exit 1
  fi
  python3 - "$VIYA_INSTALL_LISTEN" <<'PY'
import ipaddress, re, sys
value = sys.argv[1]
if "\n" in value or "\r" in value:
    raise SystemExit("Invalid VIYA_INSTALL_LISTEN: newlines are not allowed")
if value.startswith("["):
    host, sep, port = value[1:].partition("]:")
    if not sep:
        raise SystemExit("Use an address such as 127.0.0.1:8182 or [::1]:8182")
else:
    host, sep, port = value.rpartition(":")
    if not sep:
        raise SystemExit("Use an address such as 127.0.0.1:8182 or [::1]:8182")
if not port.isdigit() or not 1024 <= int(port) <= 65535:
    raise SystemExit("Panel port must be between 1024 and 65535")
if host:
    try:
        ipaddress.ip_address(host)
    except ValueError:
        if not re.fullmatch(r"[A-Za-z0-9.-]+", host):
            raise SystemExit("Panel host must be an IP address or hostname")
PY
fi

SERVICE_ENV=$(systemctl show viya.service --property=Environment --value 2>/dev/null || true)
if [ -z "$SERVICE_ENV" ] && systemctl list-unit-files "$legacy_service" --no-legend 2>/dev/null | grep -q "$legacy_service"; then
  SERVICE_ENV=$(systemctl show "$legacy_service" --property=Environment --value 2>/dev/null || true)
fi
PANEL_ENDPOINT=$(python3 - "$SERVICE_ENV" "${VIYA_INSTALL_LISTEN:-}" /var/lib/viya/viya.db "$legacy_data/crpm.db" <<'PY'
import os, shlex, sqlite3, sys
environment, override, *databases = sys.argv[1:]
settings = {}
for item in shlex.split(environment):
    if "=" in item:
        key, value = item.split("=", 1)
        settings[key] = value
listen = override or settings.get("VIYA_LISTEN") or settings.get("CRPM_LISTEN") or "0.0.0.0:8181"
if listen.startswith("["):
    host, sep, port = listen[1:].partition("]:")
    if not sep: raise SystemExit("Invalid panel listen address: " + listen)
else:
    host, sep, port = listen.rpartition(":")
    if not sep: raise SystemExit("Invalid panel listen address: " + listen)
for database in databases:
    if not os.path.isfile(database): continue
    try:
        db = sqlite3.connect("file:" + database + "?mode=ro", uri=True, timeout=2)
        row = db.execute("SELECT value FROM panel_settings WHERE key='listen_port'").fetchone()
        db.close()
        if row and str(row[0]).isdigit(): port = str(row[0])
    except sqlite3.Error:
        pass
if not port.isdigit() or not 1024 <= int(port) <= 65535:
    raise SystemExit("Invalid saved panel port: " + str(port))
probe_host = host
if probe_host in ("", "0.0.0.0"): probe_host = "127.0.0.1"
elif probe_host in ("::", "[::]"): probe_host = "::1"
url_host = "[" + probe_host + "]" if ":" in probe_host else probe_host
print(url_host + "\t" + (host or "0.0.0.0") + "\t" + str(port))
PY
)
set -- $PANEL_ENDPOINT
HEALTH_HOST=$1
PANEL_BIND_HOST=$2
PANEL_PORT=$3
HEALTH_URL="http://$HEALTH_HOST:$PANEL_PORT/api/v1/health"

# A running Viya instance may own this port. Otherwise probe the exact saved
# panel address before changing files or stopping any service.
check_panel_port() {
  python3 - "$HEALTH_URL" "$PANEL_BIND_HOST" "$PANEL_PORT" <<'PY'
import socket, sys, urllib.request
url, host, port = sys.argv[1], sys.argv[2], int(sys.argv[3])
try:
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    with opener.open(url, timeout=2) as response:
        import json
        body = json.load(response)
    if body.get("ok") and body.get("caddy_ready"):
        raise SystemExit(0)
except Exception:
    pass
try:
    addresses = socket.getaddrinfo(host, port, type=socket.SOCK_STREAM, flags=socket.AI_PASSIVE)
    last_error = None
    for family, kind, proto, _, address in addresses:
        sock = socket.socket(family, kind, proto)
        try:
            sock.bind(address)
        except OSError as error:
            last_error = error
        finally:
            sock.close()
    if last_error:
        raise last_error
    if not addresses:
        raise OSError("no bind address available")
except OSError as error:
    raise SystemExit("Panel port " + str(port) + " is already occupied at " + host + ": " + str(error))
PY
}

if ! systemctl is-active --quiet viya.service && ! systemctl is-active --quiet "$legacy_service"; then
  if ! check_panel_port; then
    if [ "$viya_upgrade" -ne 0 ]; then
      echo "The configured panel port is occupied. No files were changed; change the port after resolving access to the existing installation, then retry." >&2
      exit 1
    fi
    if [ ! -r /dev/tty ] || [ ! -w /dev/tty ]; then
      echo "Rerun this installer in an interactive terminal to choose another panel port, or set VIYA_INSTALL_LISTEN=0.0.0.0:PORT." >&2
      exit 1
    fi

    while :; do
      printf 'The panel port %s is occupied. Enter a new port (1024-65535) [8182]: ' "$PANEL_PORT" >&2
      if ! IFS= read -r NEW_PANEL_PORT < /dev/tty; then
        echo "Could not read a new panel port from the terminal." >&2
        exit 1
      fi
      [ -n "$NEW_PANEL_PORT" ] || NEW_PANEL_PORT=8182
      case "$NEW_PANEL_PORT" in
        *[!0-9]*)
          echo "Enter a numeric port between 1024 and 65535." >&2
          continue
          ;;
      esac
      if [ "${#NEW_PANEL_PORT}" -gt 5 ]; then
        echo "Enter a port between 1024 and 65535." >&2
        continue
      fi
      if [ "$NEW_PANEL_PORT" -lt 1024 ] || [ "$NEW_PANEL_PORT" -gt 65535 ]; then
        echo "Enter a port between 1024 and 65535." >&2
        continue
      fi

      PANEL_PORT=$NEW_PANEL_PORT
      case "$PANEL_BIND_HOST" in
        *:*) VIYA_INSTALL_LISTEN="[$PANEL_BIND_HOST]:$PANEL_PORT" ;;
        *) VIYA_INSTALL_LISTEN="$PANEL_BIND_HOST:$PANEL_PORT" ;;
      esac
      HEALTH_URL="http://$HEALTH_HOST:$PANEL_PORT/api/v1/health"
      if check_panel_port; then
        echo "Using panel address $VIYA_INSTALL_LISTEN."
        break
      fi
    done
  fi
fi

if [ "$viya_upgrade" -eq 0 ] && ! systemctl is-active --quiet "$legacy_service"; then
  python3 - <<'PY'
import socket
checks = [("TCP", "0.0.0.0", 80), ("TCP", "0.0.0.0", 443), ("UDP", "0.0.0.0", 443)]
for protocol, host, port in checks:
    kind = socket.SOCK_STREAM if protocol == "TCP" else socket.SOCK_DGRAM
    sock = socket.socket(socket.AF_INET, kind)
    try:
        sock.bind((host, port))
    except OSError:
        print("Warning: host port " + protocol + "/" + str(port) + " is already occupied. Viya can still install; choose another Web listener port or stop the conflicting service before adding a site.")
    finally:
        sock.close()
PY
fi

rollback_install() {
  result=$?
  trap - EXIT
  if [ "$result" -eq 0 ]; then return; fi

  if [ "$viya_upgrade" -eq 1 ]; then
    if [ "$rollback_ready" -eq 1 ]; then
      echo "Installation failed; restoring the previous Viya binaries and configuration." >&2
      systemctl stop viya.service >/dev/null 2>&1 || true
      install -o root -g root -m 0755 "$rollback_dir/viya" /opt/viya/bin/viya
      install -o root -g root -m 0755 "$rollback_dir/caddy" /opt/viya/bin/caddy
      install -o root -g root -m 0644 "$rollback_dir/viya.service" /etc/systemd/system/viya.service
      if [ -e "$rollback_dir/90-installer-listen.conf" ]; then
        install -d -o root -g root -m 0755 /etc/systemd/system/viya.service.d
        cp -a "$rollback_dir/90-installer-listen.conf" "$custom_listen_dropin"
      else
        rm -f -- "$custom_listen_dropin"
      fi
      systemctl daemon-reload
      if [ -n "$rollback_archive" ] && [ -x "$ROOT/scripts/restore-linux.sh" ]; then
        VIYA_DATA_DIR=/var/lib/viya VIYA_MASTER_KEY_FILE=/etc/viya/secrets/master.key sh "$ROOT/scripts/restore-linux.sh" "$rollback_archive" || {
          echo "Automatic data rollback failed. The protected backup is at $rollback_archive" >&2
          systemctl start viya.service >/dev/null 2>&1 || true
        }
      elif [ "$viya_was_active" -eq 1 ]; then
        systemctl start viya.service >/dev/null 2>&1 || true
      fi
      if [ "$viya_was_enabled" -eq 1 ]; then systemctl enable viya.service >/dev/null 2>&1 || true; else systemctl disable viya.service >/dev/null 2>&1 || true; fi
    fi
  elif [ "$legacy_unit" -eq 1 ]; then
    systemctl stop viya.service >/dev/null 2>&1 || true
    systemctl start "$legacy_service" >/dev/null 2>&1 || true
  else
    systemctl stop viya.service >/dev/null 2>&1 || true
    systemctl disable viya.service >/dev/null 2>&1 || true
    if [ "$listen_dropin_written" -eq 1 ]; then rm -f -- "$custom_listen_dropin"; fi
    systemctl daemon-reload >/dev/null 2>&1 || true
  fi
  exit "$result"
}
trap rollback_install EXIT

if [ "$viya_upgrade" -eq 1 ]; then
  stamp=$(date -u +%Y%m%dT%H%M%SZ)
  rollback_dir="/var/backups/viya/installer-$stamp"
  install -d -o root -g root -m 0700 "$rollback_dir"
  cp -a /opt/viya/bin/viya "$rollback_dir/viya"
  cp -a /opt/viya/bin/caddy "$rollback_dir/caddy"
  cp -a /etc/systemd/system/viya.service "$rollback_dir/viya.service"
  if [ -f "$custom_listen_dropin" ]; then cp -a "$custom_listen_dropin" "$rollback_dir/90-installer-listen.conf"; fi
  if [ -s /etc/viya/secrets/master.key ]; then cp -a /etc/viya/secrets/master.key "$rollback_dir/master.key"; fi
  if [ -s /var/lib/viya/viya.db ]; then
    rollback_archive=$(VIYA_DATA_DIR=/var/lib/viya VIYA_BACKUP_DIR="$rollback_dir" sh "$ROOT/scripts/backup-linux.sh")
  fi
  rollback_ready=1
fi

if ! id viya >/dev/null 2>&1; then
  useradd --system --home-dir /var/lib/viya --shell /usr/sbin/nologin viya
fi

# Snapshot the legacy installation before migrating its files and names.
if { [ -f "$legacy_data/crpm.db" ] || [ -f "$legacy_data/viya.db" ]; } && [ -x "$ROOT/scripts/backup-linux.sh" ]; then
  VIYA_DATA_DIR="$legacy_data" VIYA_BACKUP_DIR=/var/backups/crpm sh "$ROOT/scripts/backup-linux.sh"
fi
if [ "$viya_was_active" -eq 1 ]; then
  systemctl stop viya.service || { echo "Could not stop the running Viya service; installation cancelled." >&2; exit 1; }
else
  systemctl stop viya.service >/dev/null 2>&1 || true
fi
if systemctl list-unit-files "$legacy_service" --no-legend 2>/dev/null | grep -q "$legacy_service"; then
  legacy_unit=1
  if systemctl is-active --quiet "$legacy_service"; then
    systemctl stop "$legacy_service" || { echo "Could not stop the legacy CRPM service; installation cancelled." >&2; exit 1; }
  fi
fi

# Copy the old data first so the old service remains a usable rollback target
# until the new service has passed its readiness check.
if [ -d "$legacy_data" ] && [ ! -e /var/lib/viya/viya.db ]; then
  install -d -o viya -g viya -m 0700 /var/lib/viya
  cp -a "$legacy_data"/. /var/lib/viya/
fi
if [ -d "$legacy_config" ] && [ ! -s /etc/viya/secrets/master.key ]; then
  install -d -o root -g viya -m 0750 /etc/viya/secrets
  cp -a "$legacy_config"/. /etc/viya/
fi

# Preserve service overrides such as a custom LAN listener, translating their
# environment variable names for the Viya unit.
old_dropin=/etc/systemd/system/crpm.service.d
new_dropin=/etc/systemd/system/viya.service.d
if [ -d "$old_dropin" ] && [ ! -d "$new_dropin" ]; then
  install -d -o root -g root -m 0755 "$new_dropin"
  for file in "$old_dropin"/*.conf; do
    [ -f "$file" ] || continue
    sed 's/CRPM_/VIYA_/g' "$file" > "$new_dropin/${file##*/}"
  done
fi

# Refuse an ambiguous in-place database state instead of deleting either copy.
if [ -s /var/lib/viya/crpm.db ] && [ -s /var/lib/viya/viya.db ]; then
  echo "Both crpm.db and viya.db are present in /var/lib/viya; refusing to discard either one." >&2
  exit 1
fi

# Rename the copied database using SQLite's online backup API so the new file
# is self-contained and never depends on leftover WAL/SHM files.
if [ ! -s /var/lib/viya/viya.db ] && [ -s /var/lib/viya/crpm.db ]; then
  if ! command -v python3 >/dev/null 2>&1; then
    echo "python3 is required to migrate the legacy crpm database." >&2
    exit 1
  fi
  python3 - /var/lib/viya/crpm.db /var/lib/viya/viya.db <<'PY'
import sqlite3, sys
src = sqlite3.connect("file:" + sys.argv[1] + "?mode=ro", uri=True, timeout=15)
dst = sqlite3.connect(sys.argv[2])
src.backup(dst)
result = dst.execute("PRAGMA integrity_check").fetchone()[0]
dst.close(); src.close()
if result != "ok":
    raise SystemExit("Legacy Viya database migration failed integrity_check: " + str(result))
PY
fi
if [ -s /var/lib/viya/crpm.db ] && [ -s /var/lib/viya/viya.db ]; then
  echo "Database migration produced both database files; refusing to remove the legacy copy." >&2
  exit 1
fi

install -d -o viya -g viya -m 0755 /opt/viya/bin
chown viya:viya /opt/viya/bin
install -d -o viya -g viya -m 0700 /var/lib/viya
install -d -o root -g viya -m 0750 /etc/viya/secrets

legacy_key=/var/lib/viya/master.key
master_key=/etc/viya/secrets/master.key
if [ -L "$legacy_key" ] || [ -L "$master_key" ]; then
  echo "Master key paths must be regular files, not symbolic links." >&2
  exit 1
fi
if [ -s "$legacy_key" ]; then
  if [ -e "$master_key" ]; then
    if ! cmp -s "$legacy_key" "$master_key"; then
      echo "Conflicting legacy and external master keys; resolve them before upgrading." >&2
      exit 1
    fi
  else
    install -o viya -g viya -m 0600 "$legacy_key" "$master_key"
  fi
fi
if [ ! -s "$master_key" ]; then
  umask 077
  head -c 32 /dev/urandom > "$master_key"
fi
chown viya:viya "$master_key"
chmod 0600 "$master_key"

chown -R viya:viya /var/lib/viya
chmod 0700 /var/lib/viya
install -o viya -g viya -m 0755 "$viya_bin" /opt/viya/bin/viya
install -o viya -g viya -m 0755 "$caddy_bin" /opt/viya/bin/caddy
install -o root -g root -m 0644 "$ROOT/deploy/systemd/viya.service" /etc/systemd/system/viya.service
if [ -n "${VIYA_INSTALL_LISTEN:-}" ]; then
  install -d -o root -g root -m 0755 /etc/systemd/system/viya.service.d
  printf '[Service]\nEnvironment=VIYA_LISTEN=%s\n' "$VIYA_INSTALL_LISTEN" > "$custom_listen_dropin"
  chmod 0644 "$custom_listen_dropin"
  listen_dropin_written=1
fi

systemctl daemon-reload
systemctl enable viya.service
systemctl restart viya.service

# Readiness probe: the health endpoint returns {"ok":true,"caddy_ready":true,...}.
# curl/wget are far more common than python3 on minimal systems, so probe with
# whichever is available and only shell out to python3 as a last resort.
probe_health() {
  url="$1"
  if command -v curl >/dev/null 2>&1; then
    curl -fsS -m 2 "$url" 2>/dev/null
  elif command -v wget >/dev/null 2>&1; then
    wget -q -T 2 -O - "$url" 2>/dev/null
  elif command -v python3 >/dev/null 2>&1; then
    python3 -c 'import sys,urllib.request;print(urllib.request.urlopen(sys.argv[1],timeout=2).read().decode())' "$url" 2>/dev/null
  else
    echo "Viya installation needs curl, wget or python3 for the readiness check." >&2
    return 2
  fi
}

SERVICE_ENV=$(systemctl show viya.service --property=Environment --value)
HEALTH_URL=$(python3 - "$SERVICE_ENV" "${VIYA_INSTALL_LISTEN:-}" /var/lib/viya/viya.db <<'PY'
import os, shlex, sqlite3, sys
environment, override, database = sys.argv[1:]
settings = {}
for item in shlex.split(environment):
    if "=" in item:
        key, value = item.split("=", 1)
        settings[key] = value
listen = override or settings.get("VIYA_LISTEN") or "0.0.0.0:8181"
if listen.startswith("["):
    host, sep, port = listen[1:].partition("]:")
    if not sep: raise SystemExit("Invalid panel listen address: " + listen)
else:
    host, sep, port = listen.rpartition(":")
    if not sep: raise SystemExit("Invalid panel listen address: " + listen)
if os.path.isfile(database):
    try:
        db = sqlite3.connect("file:" + database + "?mode=ro", uri=True, timeout=2)
        row = db.execute("SELECT value FROM panel_settings WHERE key='listen_port'").fetchone()
        db.close()
        if row and str(row[0]).isdigit(): port = str(row[0])
    except sqlite3.Error:
        pass
if not port.isdigit() or not 1024 <= int(port) <= 65535:
    raise SystemExit("Invalid saved panel port: " + str(port))
if host in ("", "0.0.0.0"): host = "127.0.0.1"
elif host in ("::", "[::]"): host = "::1"
if ":" in host: host = "[" + host + "]"
print("http://" + host + ":" + port + "/api/v1/health")
PY
)

ready=0
last_error="service did not become ready"
i=0
while [ "$i" -lt 30 ]; do
  i=$((i + 1))
  body=$(probe_health "$HEALTH_URL") || body=""
  case "$body" in
    *'"ok":true'*'"caddy_ready":true'*|*'"caddy_ready":true'*'"ok":true'*)
      ready=1; break ;;
    *)
      [ -n "$body" ] && last_error="Viya or Caddy is not ready: $body" ;;
  esac
  sleep 1
done
if [ "$ready" -ne 1 ]; then
  echo "Viya installation failed readiness check: $last_error" >&2
  systemctl stop viya.service >/dev/null 2>&1 || true
  if systemctl list-unit-files "$legacy_service" --no-legend 2>/dev/null | grep -q "$legacy_service"; then
    systemctl enable "$legacy_service" >/dev/null 2>&1 || true
    systemctl start "$legacy_service" >/dev/null 2>&1 || true
  fi
  exit 1
fi
echo "Panel address: ${HEALTH_URL%/api/v1/health}"

# From here onward Viya is healthy. Retire the old installation only after
# the new service passed its readiness check; cleanup failure must not roll
# back a working Viya service after legacy files have started moving.
trap - EXIT

# Retire legacy runtime names only after Viya is healthy; the legacy snapshot
# remains in /var/backups/viya for recovery with the documented restore tool.
if [ -d /var/log/crpm ]; then
  legacy_log_backup="/var/backups/viya/crpm-logs-$(date -u +%Y%m%dT%H%M%SZ)"
  install -d -o root -g root -m 0700 "$legacy_log_backup"
  cp -a /var/log/crpm/. "$legacy_log_backup/"
fi
if systemctl list-unit-files "$legacy_service" --no-legend 2>/dev/null | grep -q "$legacy_service"; then
  systemctl disable "$legacy_service" >/dev/null 2>&1 || true
  rm -f -- "/etc/systemd/system/$legacy_service"
fi
legacy_unit=0
rm -rf -- "$old_dropin" "$legacy_data" "$legacy_config" /opt/crpm /var/log/crpm
if [ -d /var/backups/crpm ]; then
  install -d -o root -g root -m 0700 /var/backups/viya
  cp -a /var/backups/crpm/. /var/backups/viya/
  rm -rf -- /var/backups/crpm
fi
if id crpm >/dev/null 2>&1; then userdel crpm >/dev/null 2>&1 || true; fi
if getent group crpm >/dev/null 2>&1; then groupdel crpm >/dev/null 2>&1 || true; fi
systemctl daemon-reload
systemctl --no-pager --full status viya.service
if [ -s "$legacy_key" ]; then rm -f -- "$legacy_key"; fi

