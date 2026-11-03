"""Installer control-flow checks in a temporary filesystem with mocked systemd.
Real shell/Python backup code runs; no host services or accounts are modified.
"""
import os
import http.server
import threading
from pathlib import Path
import shutil
import socket
import sqlite3
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]

@unittest.skipUnless(os.name == 'posix', 'POSIX installation flow runs on Linux')
class LinuxInstallerTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='viya-install-test-')
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.pkg = self.root/'package'
        self.mock = self.root/'mock'; self.mock.mkdir()
        self.env = dict(os.environ, PATH=str(self.mock)+':'+os.environ['PATH'], TEST_ROOT=str(self.root))
        with socket.socket() as s:
            s.bind(('127.0.0.1',0)); self.port=s.getsockname()[1]
        self.env['TEST_PORT']=str(self.port)
        for name in ['run/systemd/system','etc/systemd/system','var/lib','opt','var/backups']:
            (self.root/name).mkdir(parents=True,exist_ok=True)
        for name in ['deploy/install-debian.sh','deploy/systemd/viya.service','scripts/backup-linux.sh','scripts/restore-linux.sh','scripts/backup_archive.py']:
            dest=self.pkg/name;dest.parent.mkdir(parents=True,exist_ok=True)
            text=(ROOT/name).read_text()
            for path in ['/etc','/var','/opt','/run']:
                text=text.replace(path,str(self.root)+path)
            dest.write_text(text);dest.chmod(0o755)
        for name in ['viya','caddy']:
            p=self.pkg/f'bin/{name}-linux-amd64';p.parent.mkdir(exist_ok=True);p.write_text('#!/bin/sh\nexit 0\n');p.chmod(0o755)
        self.tool('id', 'echo 0\n')
        self.tool('chown', 'exit 0\n')
        self.tool('getent', 'case "$*" in *crpm*) exit 1;;*) exit 0;;esac\n')
        self.tool('sleep', 'exit 0\n')
        self.tool('uname','echo x86_64\n')
        self.tool('curl','if [ "${TEST_FAIL_HEALTH:-0}" = 1 ] && [ ! -f "$TEST_ROOT/rollback" ];then exit 1;fi\nprintf \'{"ok":true,"caddy_ready":true}\'\n')
        self.tool('install', '''args=''
while [ "$#" -gt 0 ];do
 case "$1" in -o|-g) shift 2;;*) args="$args '$1'";shift;;esac
done
eval 'exec /usr/bin/install '"$args"
''')
        self.tool('systemctl', '''case "$1" in
 show-environment) exit 0;;
 show) echo "VIYA_LISTEN=127.0.0.1:$TEST_PORT VIYA_DATA_DIR=$TEST_ROOT/var/lib/viya VIYA_MASTER_KEY_FILE=$TEST_ROOT/etc/viya/secrets/master.key";;
 is-active|is-enabled) case "$*" in *crpm*) exit 1;;esac; [ -f "$TEST_ROOT/running" ];;
 start) touch "$TEST_ROOT/running" "$TEST_ROOT/rollback";;
 restart|enable) touch "$TEST_ROOT/running";;
 stop|disable) rm -f "$TEST_ROOT/running";;
 list-unit-files) exit 0;;
 *) exit 0;;esac
''')
    def tool(self,name,body):
        p=self.mock/name;p.write_text('#!/bin/sh\n'+body);p.chmod(0o755)
    def run_install(self,override=True,fail=False):
        env=dict(self.env)
        if override:env['VIYA_INSTALL_LISTEN']='127.0.0.1:'+str(self.port)
        if fail:env['TEST_FAIL_HEALTH']='1'
        return subprocess.run(['sh',str(self.pkg/'deploy/install-debian.sh')],env=env,text=True,capture_output=True,timeout=30)
    def test_fresh_install_lan_override_and_key_permissions(self):
        result=self.run_install();self.assertEqual(result.returncode,0,result.stdout+result.stderr)
        self.assertTrue((self.root/'opt/viya/bin/viya').exists())
        key=self.root/'etc/viya/secrets/master.key';self.assertEqual(len(key.read_bytes()),32)
        self.assertEqual(key.stat().st_mode&0o777,0o600)
        self.assertIn(str(self.port),(self.root/'etc/systemd/system/viya.service.d/90-installer-listen.conf').read_text())
    def test_port_conflict_does_not_install(self):
        with socket.socket() as occupied:
            occupied.bind(('127.0.0.1',self.port));occupied.listen()
            result=self.run_install()
        self.assertNotEqual(result.returncode,0)
        self.assertFalse((self.root/'opt/viya/bin/viya').exists())
    def seed_upgrade(self):
        result=self.run_install();self.assertEqual(result.returncode,0,result.stderr)
        with sqlite3.connect(self.root/'var/lib/viya/viya.db') as db:
            db.execute('CREATE TABLE panel_settings(key TEXT PRIMARY KEY,value TEXT)')
            db.execute("INSERT INTO panel_settings VALUES('keep','original')")
        self.key=(self.root/'etc/viya/secrets/master.key').read_bytes()
        class Health(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                self.send_response(200);self.end_headers();self.wfile.write(b'{"ok":true,"caddy_ready":true}')
            def log_message(self,*args):pass
        server=http.server.ThreadingHTTPServer(('127.0.0.1',self.port),Health)
        threading.Thread(target=server.serve_forever,daemon=True).start()
        self.addCleanup(server.server_close);self.addCleanup(server.shutdown)

    def test_upgrade_keeps_database_and_key(self):
        self.seed_upgrade();result=self.run_install(override=False)
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)
        self.assertEqual(self.key,(self.root/'etc/viya/secrets/master.key').read_bytes())
        with sqlite3.connect(self.root/'var/lib/viya/viya.db') as db:self.assertEqual(db.execute('SELECT value FROM panel_settings').fetchone()[0],'original')
        self.assertTrue(list((self.root/'var/backups/viya').glob('installer-*/*.tar.gz')))
    def test_upgrade_health_failure_rolls_back_binary(self):
        self.seed_upgrade();old=(self.root/'opt/viya/bin/viya').read_bytes()
        (self.pkg/'bin/viya-linux-amd64').write_text('#!/bin/sh\n# new candidate\nexit 0\n')
        result=self.run_install(override=False,fail=True)
        self.assertNotEqual(result.returncode,0)
        self.assertEqual(old,(self.root/'opt/viya/bin/viya').read_bytes())
        self.assertEqual(self.key,(self.root/'etc/viya/secrets/master.key').read_bytes())

if __name__=='__main__':unittest.main()
