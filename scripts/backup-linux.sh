#!/bin/sh
set -eu
umask 077
DATA_DIR=${VIYA_DATA_DIR:-/var/lib/viya}
BACKUP_DIR=${VIYA_BACKUP_DIR:-/var/backups/viya}
MASTER_KEY_FILE=${VIYA_MASTER_KEY_FILE:-}
if [ -z "$MASTER_KEY_FILE" ]; then
  if [ -s "$DATA_DIR/master.key" ]; then MASTER_KEY_FILE="$DATA_DIR/master.key"; else MASTER_KEY_FILE=/etc/viya/secrets/master.key; fi
fi
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
if [ ! -s "$MASTER_KEY_FILE" ] && [ -s "$DATA_DIR/master.key" ]; then MASTER_KEY_FILE="$DATA_DIR/master.key"; fi
mkdir -p "$BACKUP_DIR"
chmod 0700 "$BACKUP_DIR"
restart=0
cleanup() { if [ "$restart" -eq 1 ]; then systemctl start viya.service; fi; }
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
consistency=online-database
if [ "${VIYA_BACKUP_STOP_SERVICE:-0}" = 1 ]; then
  command -v systemctl >/dev/null 2>&1 || { echo "Stop Viya before backup on non-systemd installations." >&2; exit 1; }
  if systemctl is-active --quiet viya.service; then restart=1; systemctl stop viya.service; fi
  consistency=service-stopped
fi
stamp=$(date -u +%Y%m%dT%H%M%SZ)
set -- create "$BACKUP_DIR/viya-$stamp-$$.tar.gz" --data "$DATA_DIR" --key "$MASTER_KEY_FILE" --consistency "$consistency"
if [ "${VIYA_BACKUP_INCLUDE_KEY:-0}" = 1 ]; then set -- "$@" --include-key; fi
python3 "$SCRIPT_DIR/backup_archive.py" "$@"
