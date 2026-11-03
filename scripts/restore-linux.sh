#!/bin/sh
set -eu

MODE=restore
case "${1:-}" in --verify|--dry-run) MODE=${1#--}; shift;; esac
if [ "$#" -ne 1 ]; then echo "Usage: $0 [--verify|--dry-run] /path/to/viya-backup.tar.gz" >&2; exit 2; fi
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
if [ "$MODE" = verify ]; then python3 "$SCRIPT_DIR/backup_archive.py" verify "$1"; exit; fi
DATA_DIR=${VIYA_DATA_DIR:-/var/lib/viya}
MASTER_KEY_FILE=${VIYA_MASTER_KEY_FILE:-/etc/viya/secrets/master.key}
ARCHIVE=$1
if [ ! -f "$ARCHIVE" ]; then echo "Backup archive not found: $ARCHIVE" >&2; exit 1; fi
case "$DATA_DIR" in /|/var|/var/lib|"") echo "Refusing unsafe VIYA_DATA_DIR: $DATA_DIR" >&2; exit 1;; esac
DATA_DIR=$(realpath -m "$DATA_DIR")
case "$DATA_DIR" in /|/var|/var/lib|/etc|/usr|/opt|/home|/root) echo "Unsafe data directory" >&2; exit 1;; esac
if [ ! -s "$MASTER_KEY_FILE" ] && [ -s "$DATA_DIR/master.key" ]; then MASTER_KEY_FILE="$DATA_DIR/master.key"; fi

legacy_master_key=0
if [ "$MASTER_KEY_FILE" = "$DATA_DIR/master.key" ]; then legacy_master_key=1; fi
stage=$(mktemp -d)
rollback_dir=""
restore_complete=0
service_stopped=0
key_changed=0
original_key=0
cleanup() {
	status=$?
	if [ "$restore_complete" -ne 1 ] && [ -n "$rollback_dir" ] && [ -d "$rollback_dir" ]; then
		systemctl stop viya.service >/dev/null 2>&1 || true
		rm -rf -- "$DATA_DIR"
		mv -- "$rollback_dir" "$DATA_DIR"
		if [ "$key_changed" -eq 1 ]; then
      if [ "$original_key" -eq 1 ]; then cp "$stage/original-master.key" "$MASTER_KEY_FILE"; chmod 0600 "$MASTER_KEY_FILE"; else rm -f -- "$MASTER_KEY_FILE"; fi
    fi
    systemctl start viya.service >/dev/null 2>&1 || true
	fi
	if [ "$restore_complete" -ne 1 ] && [ "$service_stopped" -eq 1 ]; then systemctl start viya.service >/dev/null 2>&1 || true; fi
	rm -rf -- "$stage"
	exit "$status"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

# Verification and extraction finish before stopping or changing the service.
mkdir "$stage/archive"
python3 "$SCRIPT_DIR/backup_archive.py" verify "$ARCHIVE" --extract "$stage/archive" >/dev/null
mv "$stage/archive/data" "$stage/data"
cp "$stage/archive/MANIFEST.txt" "$stage/MANIFEST.txt"
python3 - "$stage/archive/MANIFEST.json" "$MASTER_KEY_FILE" "$stage/data/master.key" "${VIYA_RESTORE_KEY:-0}" <<'PY'
import hashlib,json,pathlib,sys
manifest=pathlib.Path(sys.argv[1]); current=pathlib.Path(sys.argv[2]); archived=pathlib.Path(sys.argv[3]); use_archived=sys.argv[4]=='1'
if use_archived:
    if not archived.is_file() or archived.stat().st_size!=32: raise SystemExit('Archive does not contain a valid master key')
elif not current.is_file(): raise SystemExit('Original master key required; use VIYA_RESTORE_KEY=1 only for a trusted full backup')
key=archived if use_archived else current
if manifest.exists():
    expected=json.loads(manifest.read_text())['master_key_sha256']
    if hashlib.sha256(key.read_bytes()).hexdigest()!=expected: raise SystemExit('Master key does not match this backup; restore cancelled')
PY
if [ "$MODE" = dry-run ]; then echo "Backup, database and master key verified; installation unchanged."; exit 0; fi
if [ -f "$MASTER_KEY_FILE" ]; then cp "$MASTER_KEY_FILE" "$stage/original-master.key"; original_key=1; fi
if [ ! -s "$stage/data/viya.db" ] && [ -s "$stage/data/crpm.db" ]; then
  python3 - "$stage/data/crpm.db" "$stage/data/viya.db" <<'PY'
import sqlite3, sys
src = sqlite3.connect("file:" + sys.argv[1] + "?mode=ro", uri=True)
dst = sqlite3.connect(sys.argv[2])
src.backup(dst)
dst.close(); src.close()
PY
fi
if [ ! -s "$stage/data/viya.db" ] || [ ! -f "$stage/MANIFEST.txt" ]; then echo "Archive is missing the database or manifest." >&2; exit 1; fi
python3 - "$stage/data/viya.db" <<'PY'
import sqlite3, sys
db = sqlite3.connect("file:" + sys.argv[1] + "?mode=ro", uri=True)
result = db.execute("PRAGMA integrity_check").fetchone()[0]
db.close()
if result != "ok": raise SystemExit("Backup database integrity check failed: " + str(result))
PY

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
if [ -f "$DATA_DIR/viya.db" ] || [ -f "$DATA_DIR/crpm.db" ]; then
  echo "Saving the current data before restore:"
  VIYA_DATA_DIR="$DATA_DIR" VIYA_MASTER_KEY_FILE="$MASTER_KEY_FILE" VIYA_BACKUP_INCLUDE_KEY=1 sh "$SCRIPT_DIR/backup-linux.sh"
fi
mkdir -p "$DATA_DIR"
systemctl stop viya.service
service_stopped=1
parent=$(dirname -- "$DATA_DIR")
rollback_dir=$(mktemp -d "$parent/.viya-restore-rollback.XXXXXX")
rmdir "$rollback_dir"
mv -- "$DATA_DIR" "$rollback_dir"
mkdir -m 0700 -- "$DATA_DIR"
cp -a "$stage/data/." "$DATA_DIR/"
if [ "${VIYA_RESTORE_KEY:-0}" = 1 ]; then
  key_changed=1
  if [ ! -d "$(dirname -- "$MASTER_KEY_FILE")" ]; then
    install -d -m 0755 /etc/viya
    install -d -m 0700 -o viya -g viya "$(dirname -- "$MASTER_KEY_FILE")"
  fi
  if [ "$MASTER_KEY_FILE" != "$DATA_DIR/master.key" ]; then cp "$stage/data/master.key" "$MASTER_KEY_FILE"; fi
  chmod 0600 "$MASTER_KEY_FILE"
  if [ "$MASTER_KEY_FILE" != "$DATA_DIR/master.key" ]; then rm -f "$DATA_DIR/master.key"; fi
elif [ "$legacy_master_key" -eq 1 ]; then cp -a "$rollback_dir/master.key" "$DATA_DIR/master.key";
else rm -f "$DATA_DIR/master.key"; fi
chown -R viya:viya "$DATA_DIR"
chown viya:viya "$MASTER_KEY_FILE"
if [ "$legacy_master_key" -eq 1 ]; then chmod 0600 "$DATA_DIR/master.key"; fi
systemctl start viya.service
python3 - "$DATA_DIR/viya.db" <<'PY'
import json, os, shlex, sqlite3, subprocess, sys, time, urllib.request
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
    if not separator:
        raise SystemExit("Invalid VIYA_LISTEN value: " + listen)
else:
    host, separator, port = listen.rpartition(":")
    if not separator:
        raise SystemExit("Invalid VIYA_LISTEN value: " + listen)
database = sys.argv[1]
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
if host in ("", "0.0.0.0", "::", "[::]"):
    host = "::1" if ":" in host else "127.0.0.1"
if ":" in host:
    host = "[" + host + "]"
url = f"http://{host}:{port}/api/v1/health"
deadline = time.time() + 30
last_error = "service did not become ready"
while time.time() < deadline:
    try:
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        with opener.open(url, timeout=2) as response:
            data = json.load(response)
        if data.get("ok") and data.get("caddy_ready"):
            break
        last_error = "Viya or Caddy is not ready: " + repr(data)
    except Exception as error:
        last_error = str(error)
    time.sleep(1)
else:
    raise SystemExit("Restored service failed its readiness check: " + last_error)
PY
restore_complete=1
rm -rf -- "$rollback_dir"
rollback_dir=""
systemctl --no-pager --full status viya.service


