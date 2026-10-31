#!/bin/sh
set -eu

if [ "$#" -ne 1 ]; then echo "Usage: $0 /path/to/viya-backup.tar.gz" >&2; exit 2; fi
DATA_DIR=${VIYA_DATA_DIR:-/var/lib/viya}
MASTER_KEY_FILE=${VIYA_MASTER_KEY_FILE:-/etc/viya/secrets/master.key}
ARCHIVE=$1
if [ ! -f "$ARCHIVE" ]; then echo "Backup archive not found: $ARCHIVE" >&2; exit 1; fi
case "$DATA_DIR" in /|/var|/var/lib|"") echo "Refusing unsafe VIYA_DATA_DIR: $DATA_DIR" >&2; exit 1;; esac
DATA_DIR=$(realpath -e "$DATA_DIR")
if [ ! -s "$MASTER_KEY_FILE" ] && [ -s "$DATA_DIR/master.key" ]; then MASTER_KEY_FILE="$DATA_DIR/master.key"; fi
if [ ! -s "$MASTER_KEY_FILE" ]; then echo "The existing master key is required at $MASTER_KEY_FILE and must be preserved separately from the backup." >&2; exit 1; fi
legacy_master_key=0
if [ "$MASTER_KEY_FILE" = "$DATA_DIR/master.key" ]; then legacy_master_key=1; fi
stage=$(mktemp -d)
rollback_dir=""
restore_complete=0
cleanup() {
	status=$?
	if [ "$restore_complete" -ne 1 ] && [ -n "$rollback_dir" ] && [ -d "$rollback_dir" ]; then
		systemctl stop viya.service >/dev/null 2>&1 || true
		rm -rf -- "$DATA_DIR"
		mv -- "$rollback_dir" "$DATA_DIR"
		systemctl start viya.service >/dev/null 2>&1 || true
	fi
	rm -rf -- "$stage"
	exit "$status"
}
trap cleanup EXIT HUP INT TERM

# Reject absolute paths, traversal, links, and special files before extraction.
python3 - "$ARCHIVE" <<'PY'
import pathlib, sys, tarfile
with tarfile.open(sys.argv[1], "r:gz") as archive:
    for member in archive.getmembers():
        path = pathlib.PurePosixPath(member.name)
        normalized = str(path).removeprefix("./")
        if path.is_absolute() or ".." in path.parts or normalized not in ("MANIFEST.txt", ".", "data") and not normalized.startswith("data/"):
            raise SystemExit("Unsafe path in backup archive: " + member.name)
        if not (member.isfile() or member.isdir()):
            raise SystemExit("Links and special files are not allowed in backup archives")
PY
python3 - "$ARCHIVE" "$stage" <<'PY'
import pathlib, sys, tarfile
with tarfile.open(sys.argv[1], "r:gz") as archive:
    members = archive.getmembers()
    archive.extractall(sys.argv[2], members=members)
PY
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
echo "Saving the current data before restore:"
VIYA_DATA_DIR="$DATA_DIR" sh "$SCRIPT_DIR/backup-linux.sh"
systemctl stop viya.service
parent=$(dirname -- "$DATA_DIR")
rollback_dir=$(mktemp -d "$parent/.viya-restore-rollback.XXXXXX")
rmdir "$rollback_dir"
mv -- "$DATA_DIR" "$rollback_dir"
mkdir -m 0700 -- "$DATA_DIR"
cp -a "$stage/data/." "$DATA_DIR/"
if [ "$legacy_master_key" -eq 1 ]; then cp -a "$rollback_dir/master.key" "$DATA_DIR/master.key"; fi
chown -R viya:viya "$DATA_DIR"
if [ "$legacy_master_key" -eq 1 ]; then chmod 0600 "$DATA_DIR/master.key"; fi
systemctl start viya.service
python3 - <<'PY'
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
    if not separator:
        raise SystemExit("Invalid VIYA_LISTEN value: " + listen)
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


