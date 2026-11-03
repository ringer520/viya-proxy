import io
import json
from pathlib import Path
import sqlite3
import tarfile
import tempfile
import types
import unittest
from contextlib import closing

from backup_archive import create, verify


class BackupTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.data = self.root / 'data'; self.data.mkdir()
        self.key = self.root / 'key'; self.key.write_bytes(b'k' * 32)
        with closing(sqlite3.connect(self.data / 'viya.db')) as db:
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
        with closing(sqlite3.connect(target / 'data/viya.db')) as db:
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


if __name__ == '__main__': unittest.main()
