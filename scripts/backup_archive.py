#!/usr/bin/env python3
"""Create, verify and safely extract Viya archives using the standard library."""
import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import sqlite3
import tarfile
import tempfile
from contextlib import closing
from datetime import datetime, timezone


def digest(path):
    with open(path, 'rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def check_db(path):
    with closing(sqlite3.connect(Path(path).resolve().as_uri() + '?mode=ro', uri=True)) as db:
        if db.execute('PRAGMA integrity_check').fetchall() != [('ok',)]:
            raise ValueError('SQLite integrity check failed')


def create(args):
    root = Path(args.data).resolve(strict=True)
    source = root / 'viya.db'
    if not source.exists(): source = root / 'crpm.db'
    key = Path(args.key).resolve(strict=True)
    key_bytes = key.read_bytes()
    if len(key_bytes) != 32: raise ValueError('Master key must be exactly 32 bytes')
    output = Path(args.archive).resolve()
    if output.is_relative_to(root): raise ValueError('Backup destination must be outside the data directory')
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='viya-backup-') as tmp:
        stage = Path(tmp); data = stage / 'data'; data.mkdir(mode=0o700)
        with closing(sqlite3.connect(source.as_uri() + '?mode=ro', uri=True, timeout=30)) as src:
            with closing(sqlite3.connect(data / 'viya.db')) as dst: src.backup(dst)
        check_db(data / 'viya.db')
        excluded = []
        for base, directories, files in os.walk(root, followlinks=False):
            for name in list(directories):
                p = Path(base) / name
                if p.is_symlink(): excluded.append(p.relative_to(root).as_posix()); directories.remove(name)
            for name in files:
                p = Path(base) / name; rel = p.relative_to(root)
                # viya.lock is a kernel-held instance lock, not persistent state:
                # copying it into an archive (and restoring it elsewhere) makes
                # no sense and could confuse a reader of the manifest.
                if name in ('master.key', 'viya.db', 'crpm.db', 'viya.lock') or name.startswith(('viya.db-', 'crpm.db-')) or p == key: continue
                if p.is_symlink() or not p.is_file() or name.startswith(('access', 'l4')) and '.log' in name:
                    excluded.append(rel.as_posix()); continue
                dest = data / rel; dest.parent.mkdir(parents=True, exist_ok=True)
                before = p.stat(); shutil.copyfile(p, dest); after = p.stat()
                if (before.st_size, before.st_mtime_ns) != (after.st_size, after.st_mtime_ns):
                    raise ValueError('Runtime file changed; retry with VIYA_BACKUP_STOP_SERVICE=1')
        if args.include_key: (data / 'master.key').write_bytes(key_bytes)
        manifest = {'format': 2, 'created': datetime.now(timezone.utc).isoformat(),
                    'master_key_sha256': hashlib.sha256(key_bytes).hexdigest(),
                    'includes_key': args.include_key, 'excludes': excluded, 'consistency': args.consistency,
                    'files': {p.relative_to(stage).as_posix(): digest(p) for p in data.rglob('*') if p.is_file()}}
        (stage / 'MANIFEST.json').write_text(json.dumps(manifest, indent=2), encoding='utf-8')
        (stage / 'MANIFEST.txt').write_text('Viya backup format: 2\nIncludes database, managed websites and certificate storage.\nExternal website roots are not included. Contains sensitive data; protect this archive.\n', encoding='utf-8')
        temporary = output.with_suffix(output.suffix + '.tmp')
        try:
            with tarfile.open(temporary, 'w:gz', dereference=True) as archive:
                for name in ('data', 'MANIFEST.json', 'MANIFEST.txt'): archive.add(stage / name, arcname=name)
            os.chmod(temporary, 0o600); verify(temporary); os.replace(temporary, output)
        finally: temporary.unlink(missing_ok=True)
    print(output)


def verify(archive_path, extract=None):
    with tempfile.TemporaryDirectory(prefix='viya-verify-') as tmp:
        stage = Path(tmp); seen = set(); total = 0
        with tarfile.open(archive_path, 'r:gz') as archive:
            for index, member in enumerate(archive):
                path = PurePosixPath(member.name); name = str(path)
                if name == '.': continue
                if path.is_absolute() or '..' in path.parts or '\\' in name or ':' in name or name in seen:
                    raise ValueError('Unsafe or duplicate backup path: ' + member.name)
                seen.add(name)
                if name not in ('MANIFEST.json', 'MANIFEST.txt', 'data') and not name.startswith('data/'):
                    raise ValueError('Unexpected backup path: ' + member.name)
                if not (member.isfile() or member.isdir()): raise ValueError('Links and special files are not allowed')
                total += member.size
                if total > int(os.environ.get('VIYA_BACKUP_MAX_BYTES', str(64 << 30))) or index > 1000000:
                    raise ValueError('Backup exceeds verification limit')
                destination = stage / name
                if member.isdir(): destination.mkdir(parents=True, exist_ok=True)
                else:
                    destination.parent.mkdir(parents=True, exist_ok=True)
                    with archive.extractfile(member) as src, open(destination, 'xb') as dst: shutil.copyfileobj(src, dst)
                    os.chmod(destination, 0o600)
        db = stage / 'data' / 'viya.db'
        if not db.exists(): db = stage / 'data' / 'crpm.db'
        if not (stage / 'MANIFEST.txt').is_file() or not db.is_file(): raise ValueError('Database or manifest missing')
        manifest_path = stage / 'MANIFEST.json'; manifest = {'format': 1, 'includes_key': False}
        if manifest_path.exists():
            manifest = json.loads(manifest_path.read_text(encoding='utf-8'))
            actual = {p.relative_to(stage).as_posix(): digest(p) for p in (stage / 'data').rglob('*') if p.is_file()}
            if manifest.get('format') != 2 or actual != manifest.get('files'): raise ValueError('Backup file list or checksum mismatch')
            if manifest.get('includes_key'):
                key = stage / 'data' / 'master.key'
                if not key.is_file() or key.stat().st_size != 32 or digest(key) != manifest.get('master_key_sha256'):
                    raise ValueError('Backup master key mismatch')
        check_db(db)
        if extract:
            target = Path(extract)
            if target.exists() and any(target.iterdir()): raise ValueError('Extraction directory must be empty')
            target.mkdir(parents=True, exist_ok=True); shutil.copytree(stage, target, dirs_exist_ok=True)
        return manifest


if __name__ == '__main__':
    parser = argparse.ArgumentParser(); sub = parser.add_subparsers(dest='action', required=True)
    c = sub.add_parser('create'); c.add_argument('archive'); c.add_argument('--data', required=True); c.add_argument('--key', required=True)
    c.add_argument('--include-key', action='store_true'); c.add_argument('--consistency', default='online-database')
    v = sub.add_parser('verify'); v.add_argument('archive'); v.add_argument('--extract')
    args = parser.parse_args()
    try:
        if args.action == 'create': create(args)
        else: print(json.dumps(verify(args.archive, args.extract)))
    except (ValueError, OSError, sqlite3.Error, tarfile.TarError) as error:
        raise SystemExit('Backup validation failed: ' + str(error))
