#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "Run this installer as root." >&2
  exit 1
fi

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
legacy_service=crpm.service
legacy_data=/var/lib/crpm
legacy_config=/etc/crpm
legacy_unit=0
rollback_legacy() {
  result=$?
  trap - EXIT
  if [ "$result" -ne 0 ] && [ "$legacy_unit" -eq 1 ]; then
    systemctl stop viya.service >/dev/null 2>&1 || true
    systemctl start "$legacy_service" >/dev/null 2>&1 || true
  fi
  return "$result"
}
trap rollback_legacy EXIT

if ! id viya >/dev/null 2>&1; then
  useradd --system --home-dir /var/lib/viya --shell /usr/sbin/nologin viya
fi

# Snapshot the legacy installation before migrating its files and names.
if [ -f "$legacy_data/crpm.db" ] && [ -x "$ROOT/scripts/backup-linux.sh" ]; then
  VIYA_DATA_DIR="$legacy_data" VIYA_BACKUP_DIR=/var/backups/crpm sh "$ROOT/scripts/backup-linux.sh"
fi
systemctl stop viya.service >/dev/null 2>&1 || true
if systemctl list-unit-files "$legacy_service" --no-legend 2>/dev/null | grep -q "$legacy_service"; then
  legacy_unit=1
  systemctl stop "$legacy_service" >/dev/null 2>&1 || true
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

# Rename the copied database using SQLite's online backup API so the new file
# is self-contained and never depends on leftover WAL/SHM files.
if [ ! -s /var/lib/viya/viya.db ] && [ -s /var/lib/viya/crpm.db ]; then
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
  rm -f -- /var/lib/viya/crpm.db /var/lib/viya/crpm.db-wal /var/lib/viya/crpm.db-shm
fi

install -d -o root -g root -m 0755 /opt/viya/bin
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
viya_bin=$(ls "$ROOT"/bin/viya-linux-* 2>/dev/null | head -1)
caddy_bin=$(ls "$ROOT"/bin/caddy-linux-* 2>/dev/null | head -1)
if [ -z "$viya_bin" ] || [ -z "$caddy_bin" ]; then
  echo "Package binaries are missing." >&2
  exit 1
fi
install -o root -g root -m 0755 "$viya_bin" /opt/viya/bin/viya
install -o root -g root -m 0755 "$caddy_bin" /opt/viya/bin/caddy
install -o root -g root -m 0644 "$ROOT/deploy/systemd/viya.service" /etc/systemd/system/viya.service

systemctl daemon-reload
systemctl enable viya.service
systemctl restart viya.service

if ! python3 - <<'PY'
import json, shlex, subprocess, time, urllib.request
environment = subprocess.check_output(
    ["systemctl", "show", "viya.service", "--property=Environment", "--value"],
    text=True,
)
listen = next(
    (item.split("=", 1)[1] for item in shlex.split(environment) if item.startswith("VIYA_LISTEN=")),
    "127.0.0.1:8181",
)
if listen.startswith("["):
    host, separator, port = listen[1:].partition("]:")
else:
    host, separator, port = listen.rpartition(":")
if not separator:
    raise SystemExit("Invalid VIYA_LISTEN value: " + listen)
if host in ("", "0.0.0.0", "::", "[::]"):
    host = "127.0.0.1"
if ":" in host:
    host = "[" + host + "]"
url = f"http://{host}:{port}/api/v1/health"
deadline = time.time() + 30
last_error = "service did not become ready"
while time.time() < deadline:
    try:
        with urllib.request.urlopen(url, timeout=2) as response:
            state = json.load(response)
        if state.get("ok") and state.get("caddy_ready"):
            break
        last_error = "Viya or Caddy is not ready: " + repr(state)
    except Exception as error:
        last_error = str(error)
    time.sleep(1)
else:
    raise SystemExit("Viya installation failed readiness check: " + last_error)
PY
then
  systemctl stop viya.service >/dev/null 2>&1 || true
  if systemctl list-unit-files "$legacy_service" --no-legend 2>/dev/null | grep -q "$legacy_service"; then
    systemctl enable "$legacy_service" >/dev/null 2>&1 || true
    systemctl start "$legacy_service" >/dev/null 2>&1 || true
  fi
  exit 1
fi

# Retire legacy runtime names only after Viya is healthy; the legacy snapshot
# remains in /var/backups/viya for recovery with the documented restore tool.
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
trap - EXIT

