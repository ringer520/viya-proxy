#!/usr/bin/env python3
"""Create, verify and safely extract Viya archives using the standard library.

Deliberately compatible with Python 3.6, because that is still what several
long-term distributions ship as ``python3`` (CentOS 7, Rocky 8, Alibaba Cloud
Linux 2). Do not introduce APIs newer than 3.6 here; the installer calls this
script on the target host, where a traceback aborts the upgrade halfway.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import sqlite3
import sys
import tarfile
import tempfile
from contextlib import closing
from datetime import datetime, timezone

if sys.version_info < (3, 6):
    raise SystemExit('Viya backup requires Python 3.6 or newer; found ' + sys.version.split()[0])

# Read in fixed chunks instead of pulling the whole file into memory: archives
# may hold multi-gigabyte site data, and hashlib.file_digest is Python 3.11+.
CHUNK = 1 << 20


def digest(path):
    hasher = hashlib.sha256()
    with open(path, 'rb') as stream:
        for block in iter(lambda: stream.read(CHUNK), b''):
            hasher.update(block)
    return hasher.hexdigest()


def is_within(path, parent):
    """Path.is_relative_to is 3.9+; both sides are already resolved here."""
    try:
        path.relative_to(parent)
    except ValueError:
        return False
    return True


def remove_quietly(path):
    """Path.unlink with the missing_ok flag is 3.8+."""
    try:
        os.unlink(path)
    except OSError:
        pass


def check_db(path):
    with closing(sqlite3.connect(Path(path).resolve().as_uri() + '?mode=ro', uri=True)) as db:
        if db.execute('PRAGMA integrity_check').fetchall() != [('ok',)]:
            raise ValueError('SQLite integrity check failed')


def copy_database(source, destination):
    """Copy a live database so the result never needs leftover WAL/SHM files.

    sqlite3.Connection.backup is Python 3.7+, and the fallback SQLite would
    need (VACUUM INTO, 3.27+) is missing from the SQLite that ships with those
    same distributions. Replaying a logical dump inside one read transaction is
    the portable option: readers see a single snapshot, and TEXT quotes, BLOBs,
    NULLs and REALs all round-trip (the dump quotes them as SQL literals).
    """
    source = Path(source).resolve(strict=True)
    destination = Path(destination)
    destination.parent.mkdir(parents=True, exist_ok=True)
    with closing(sqlite3.connect(source.as_uri() + '?mode=ro', uri=True, timeout=30)) as src:
        with closing(sqlite3.connect(str(destination))) as dst:
            if hasattr(src, 'backup'):
                src.backup(dst)
            else:
                src.isolation_level = None
                src.execute('BEGIN')
                try:
                    dump = '\n'.join(src.iterdump())
                finally:
                    src.execute('COMMIT')
                dst.executescript(dump)
            dst.commit()


def create(args):
    root = Path(args.data).resolve(strict=True)
    source = root / 'viya.db'
    if not source.exists(): source = root / 'crpm.db'
    key = Path(args.key).resolve(strict=True)
    key_bytes = key.read_bytes()
    if len(key_bytes) != 32: raise ValueError('Master key must be exactly 32 bytes')
    output = Path(args.archive).resolve()
    if is_within(output, root): raise ValueError('Backup destination must be outside the data directory')
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='viya-backup-') as tmp:
        stage = Path(tmp); data = stage / 'data'; data.mkdir(mode=0o700)
        copy_database(source, data / 'viya.db')
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
            with tarfile.open(str(temporary), 'w:gz', dereference=True) as archive:
                for name in ('data', 'MANIFEST.json', 'MANIFEST.txt'): archive.add(stage / name, arcname=name)
            os.chmod(str(temporary), 0o600); verify(temporary); os.replace(str(temporary), str(output))
        finally: remove_quietly(temporary)
    print(output)


def verify(archive_path, extract=None):
    with tempfile.TemporaryDirectory(prefix='viya-verify-') as tmp:
        stage = Path(tmp); seen = set(); total = 0
        with tarfile.open(str(archive_path), 'r:gz') as archive:
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
            if target.exists():
                if not target.is_dir() or any(target.iterdir()): raise ValueError('Extraction directory must be empty')
            else:
                target.mkdir(parents=True, exist_ok=True)
            # The dirs_exist_ok flag on shutil.copytree is 3.8+; the target is empty
            # by now, so copy each top-level entry on its own.
            for child in sorted(stage.iterdir()):
                destination = target / child.name
                if child.is_dir(): shutil.copytree(str(child), str(destination))
                else: shutil.copyfile(str(child), str(destination))
        return manifest


if __name__ == '__main__':
    parser = argparse.ArgumentParser(); sub = parser.add_subparsers(dest='action')
    c = sub.add_parser('create'); c.add_argument('archive'); c.add_argument('--data', required=True); c.add_argument('--key', required=True)
    c.add_argument('--include-key', action='store_true'); c.add_argument('--consistency', default='online-database')
    v = sub.add_parser('verify'); v.add_argument('archive'); v.add_argument('--extract')
    d = sub.add_parser('copy-db'); d.add_argument('source'); d.add_argument('destination')
    args = parser.parse_args()
    # The required flag on add_subparsers is 3.7+; enforce it by hand so an old
    # python3 prints one clear line instead of a TypeError traceback.
    if not args.action: parser.error('choose an action: create, verify or copy-db')
    try:
        if args.action == 'create': create(args)
        elif args.action == 'copy-db': copy_database(args.source, args.destination)
        else: print(json.dumps(verify(args.archive, args.extract)))
    except (ValueError, OSError, sqlite3.Error, tarfile.TarError) as error:
        raise SystemExit('Viya backup helper failed: ' + str(error))
