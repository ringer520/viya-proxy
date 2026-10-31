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

LISTEN=$(systemctl show viya.service --property=Environment --value | tr ' ' '\n' | sed -n 's/^VIYA_LISTEN=//p' | head -1)
[ -n "$LISTEN" ] || LISTEN="127.0.0.1:8181"
case "$LISTEN" in
  \[*\]:*) HOST=${LISTEN#\[}; HOST=${HOST%%\]*}; PORT=${LISTEN##*:} ;;
  *:*)     HOST=${LISTEN%:*}; PORT=${LISTEN##*:} ;;
  *)       echo "Invalid VIYA_LISTEN value: $LISTEN" >&2; exit 1 ;;
esac
case "$HOST" in ""|0.0.0.0|::) HOST=127.0.0.1 ;; esac
case "$HOST" in *:*) HOST="[$HOST]" ;; esac
HEALTH_URL="http://$HOST:$PORT/api/v1/health"

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

