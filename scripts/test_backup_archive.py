import io
import json
from pathlib import Path
import sqlite3
import tarfile
import tempfile
import types
import unittest
from contextlib import closing

from backup_archive import copy_database, create, verify


class BackupTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.data = self.root / 'data'; self.data.mkdir()
        self.key = self.root / 'key'; self.key.write_bytes(b'k' * 32)
        with closing(sqlite3.connect(str(self.data / 'viya.db'))) as db:
            db.execute('CREATE TABLE users(id INTEGER PRIMARY KEY, username TEXT)')
            db.execute("INSERT INTO users VALUES(1,'admin')")
            db.commit()
        site = self.data / 'sites' / 'one'; site.mkdir(parents=True)
        (site / 'index.html').write_text('website')
        self.archive = self.root / 'backup.tar.gz'

    def backup(self, include=True):
        create(types.SimpleNamespace(data=str(self.data), key=str(self.key), archive=str(self.archive), include_key=include, consistency='service-stopped'))

    def test_full_restore_roundtrip(self):
        self.backup()
        target = self.root / 'restored'
        manifest = verify(self.archive, target)
        self.assertTrue(manifest['includes_key'])
        self.assertEqual((target / 'data/master.key').read_bytes(), self.key.read_bytes())
        self.assertEqual((target / 'data/sites/one/index.html').read_text(), 'website')
        with closing(sqlite3.connect(str(target / 'data/viya.db'))) as db:
            self.assertEqual(db.execute('SELECT username FROM users').fetchone()[0], 'admin')

    def test_key_excluded_by_default(self):
        self.backup(False)
        target = self.root / 'restored'
        self.assertFalse(verify(self.archive, target)['includes_key'])
        self.assertFalse((target / 'data/master.key').exists())

    def test_corrupt_file_rejected(self):
        self.backup()
        modified = self.root / 'modified.tar.gz'
        with tarfile.open(self.archive, 'r:gz') as src, tarfile.open(modified, 'w:gz') as dst:
            for member in src:
                if member.isfile():
                    content = src.extractfile(member).read()
                    if member.name.endswith('index.html'): content = b'changed'
                    member.size = len(content); dst.addfile(member, io.BytesIO(content))
                else: dst.addfile(member)
        with self.assertRaisesRegex(ValueError, 'checksum'): verify(modified)

    def test_traversal_links_and_duplicates_rejected(self):
        for name, kind in [('../outside', tarfile.REGTYPE), ('data/link', tarfile.SYMTYPE), ('/data/evil', tarfile.REGTYPE), ('data/../bad', tarfile.REGTYPE)]:
            with self.subTest(name=name):
                with tarfile.open(self.archive, 'w:gz') as archive:
                    member = tarfile.TarInfo(name); member.type = kind; member.linkname = '/etc/passwd'; archive.addfile(member)
                with self.assertRaises(ValueError): verify(self.archive)

    def test_backup_inside_source_rejected(self):
        self.archive = self.data / 'backup.tar.gz'
        with self.assertRaises(ValueError): self.backup()

    def test_copy_database_preserves_values_without_backup_api(self):
        # copy_database has to work on Python 3.6, where Connection.backup does
        # not exist, so exercise it through both code paths on newer runtimes.
        source = self.data / 'viya.db'
        with closing(sqlite3.connect(str(source))) as db:
            db.execute('CREATE TABLE notes(id INTEGER PRIMARY KEY, body TEXT, blob BLOB, score REAL, extra)')
            db.execute("INSERT INTO notes VALUES(1, 'quote ''s and\nnewline', ?, 1.5, NULL)", (b'\x00\x01',))
            db.commit()
        target = self.root / 'copy.db'
        copy_database(source, target)
        with closing(sqlite3.connect(str(target))) as db:
            rows = db.execute('SELECT id, body, hex(blob), score, extra FROM notes').fetchall()
            self.assertEqual(rows, [(1, "quote 's and\nnewline", '0001', 1.5, None)])
            self.assertEqual(db.execute('PRAGMA integrity_check').fetchone()[0], 'ok')

    def test_instance_lock_is_runtime_state_not_backup_content(self):
        (self.data / 'viya.lock').write_text('pid=1234\n')
        self.backup()
        target = self.root / 'restored'
        verify(self.archive, target)
        self.assertFalse((target / 'data/viya.lock').exists())


class Python36CompatibilityTests(unittest.TestCase):
    """The installer runs these files with the host's python3.

    CentOS 7, Rocky 8 and Alibaba Cloud Linux 2 still ship Python 3.6, and a
    TypeError there aborts an upgrade after the archive is already extracted.
    """

    REPO = Path(__file__).resolve().parent.parent
    BANNED = ('file_digest(', '.is_relative_to(', 'missing_ok=', 'dirs_exist_ok=',
              'capture_output=', 'text=True', '.removeprefix(', '.removesuffix(',
              'add_subparsers(required')

    def host_scripts(self):
        paths = [self.REPO / 'install.sh', self.REPO / 'scripts' / 'backup-linux.sh',
                 self.REPO / 'scripts' / 'restore-linux.sh']
        paths += sorted((self.REPO / 'deploy').glob('*.sh'))
        return [path for path in paths if path.is_file()]

    def test_no_apis_newer_than_36(self):
        self.assertTrue(self.host_scripts())
        for path in self.host_scripts():
            text = path.read_text(encoding='utf-8')
            for token in self.BANNED:
                with self.subTest(file=path.name, token=token):
                    self.assertNotIn(token, text,
                                     path.name + ' uses ' + token + ', which needs a newer Python')


if __name__ == '__main__': unittest.main()
