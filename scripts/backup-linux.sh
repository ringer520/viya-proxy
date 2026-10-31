#!/bin/sh
set -eu

DATA_DIR=${VIYA_DATA_DIR:-/var/lib/viya}
BACKUP_DIR=${VIYA_BACKUP_DIR:-/var/backups/viya}
if [ -f "$DATA_DIR/viya.db" ]; then
  DB_FILE=viya.db
elif [ -f "$DATA_DIR/crpm.db" ]; then
  DB_FILE=crpm.db
else
  echo "Viya database not found in $DATA_DIR" >&2
  exit 1
fi
mkdir -p "$BACKUP_DIR"
chmod 0700 "$BACKUP_DIR"
umask 077
stamp=$(date -u +%Y%m%dT%H%M%SZ)
output="$BACKUP_DIR/viya-$stamp.tar.gz"
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT HUP INT TERM
mkdir -p "$stage/data"

# SQLite's backup API makes a consistent copy while the service is running,
# including committed pages that may still reside in the WAL.
python3 - "$DATA_DIR/$DB_FILE" "$stage/data/viya.db" <<'PY'
import sqlite3, sys
src = sqlite3.connect("file:" + sys.argv[1] + "?mode=ro", uri=True, timeout=15)
dst = sqlite3.connect(sys.argv[2])
src.backup(dst)
result = dst.execute("PRAGMA integrity_check").fetchone()[0]
dst.close(); src.close()
if result != "ok":
    raise SystemExit("SQLite backup integrity check failed: " + str(result))
PY

# Preserve Caddy certificate storage, application-managed certificates, and
# other runtime state, while keeping the master key out of this archive.
for entry in "$DATA_DIR"/* "$DATA_DIR"/.[!.]* "$DATA_DIR"/..?*; do
  [ -e "$entry" ] || continue
  name=${entry##*/}
  case "$name" in
    viya.db|viya.db-*|crpm.db|crpm.db-*|master.key|access.log|access.log-*) continue ;;
  esac
  cp -a "$entry" "$stage/data/"
done
cat > "$stage/MANIFEST.txt" <<EOF
Viya backup format: 1
Created UTC: $stamp
SQLite: consistent online backup
Included: application database and runtime files except master.key and access logs
Restore requires the same separately protected master.key used by the source instance.
EOF
tar -czf "$output.tmp" -C "$stage" .
chmod 0600 "$output.tmp"
mv "$output.tmp" "$output"
echo "$output"

