#!/usr/bin/env python3
"""Exercise the migration against real files, locks, sockets and SQLite."""
import fcntl
import hashlib
import json
import os
from pathlib import Path
import socket
import sqlite3
import subprocess
import sys
import tempfile
import unittest

SCRIPT = Path(__file__).with_name('migrate-ikesu.py')


class MigrationTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.home = Path(self.temp.name)
        self.state = self.home / '.local/state/gtx-2e01183'
        self.config = self.home / '.config/gtx'
        self.state.mkdir(parents=True)
        self.config.mkdir(parents=True)
        self.old_cwd = str(self.home / 'src/github.com/kazuph/goterrace')
        self.config.joinpath('config.toml').write_text('onboarding=false\n')
        self.config.joinpath('plugins.json').write_text(json.dumps({'plugins': [{'plugin_root': self.old_cwd + '/apps/example'}]}))
        session = self.state / 'default/session.json'
        session.parent.mkdir()
        session.write_text(json.dumps({'cwd': self.old_cwd, 'log': str(self.state / 'jobs/j1.log')}))
        log = self.state / 'jobs/j1.log'
        log.parent.mkdir()
        log.write_text('real job output\n')
        with sqlite3.connect(self.state / 'gtx.db') as db:
            db.execute('CREATE TABLE jobs(id TEXT PRIMARY KEY, log_path TEXT, payload TEXT)')
            db.execute('INSERT INTO jobs VALUES(?,?,?)', ('j1', str(log), json.dumps({'cwd': self.old_cwd})))
        old = self.home / '.local/state/gtx-unused'
        old.mkdir()
        old.joinpath('gtx.db').write_bytes(b'untouched evidence')

    def run_script(self, *args):
        return subprocess.run([sys.executable, str(SCRIPT), *args], env={**os.environ, 'HOME': str(self.home)}, capture_output=True, text=True)

    def snapshot(self):
        return {str(path.relative_to(self.home)): hashlib.sha256(path.read_bytes()).hexdigest()
                for path in self.home.rglob('*') if path.is_file()}

    def test_dry_run_is_read_only(self):
        before = self.snapshot()
        result = self.run_script('--dry-run')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('REWRITE DB', result.stdout)
        self.assertIn('REWRITE FILE', result.stdout)
        self.assertIn('gtx-unused', result.stdout)
        self.assertEqual(self.snapshot(), before)

    def test_move_rewrite_backup_and_preserve(self):
        result = self.run_script()
        self.assertEqual(result.returncode, 0, result.stderr)
        new = self.home / '.local/state/ikesu'
        self.assertFalse(self.state.exists())
        self.assertFalse(self.config.exists())
        self.assertTrue(Path(str(self.state) + '.pre-ikesu').is_dir())
        self.assertTrue(Path(str(self.config) + '.pre-ikesu').is_dir())
        session = json.loads(new.joinpath('default/session.json').read_text())
        self.assertEqual(session['cwd'], self.old_cwd.replace('/goterrace', '/ikesu'))
        self.assertEqual(session['log'], str(new / 'jobs/j1.log'))
        self.assertTrue(new.joinpath('default/session.json.pre-ikesu').exists())
        with sqlite3.connect(new / 'ikesu.db') as db:
            log, payload = db.execute('SELECT log_path,payload FROM jobs').fetchone()
        self.assertEqual(log, str(new / 'jobs/j1.log'))
        self.assertEqual(json.loads(payload)['cwd'], session['cwd'])
        self.assertEqual(new.joinpath('jobs/j1.log').read_text(), 'real job output\n')
        self.assertEqual(self.home.joinpath('.local/state/gtx-unused/gtx.db').read_bytes(), b'untouched evidence')
        plugin = json.loads(self.home.joinpath('.config/ikesu/plugins.json').read_text())
        self.assertEqual(plugin['plugins'][0]['plugin_root'], session['cwd'] + '/apps/example')

    def test_existing_destination_refuses_without_changes(self):
        self.home.joinpath('.config/ikesu').mkdir()
        before = self.snapshot()
        result = self.run_script()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('destination already exists', result.stderr)
        self.assertEqual(self.snapshot(), before)
        self.assertTrue(self.state.is_dir())

    def test_held_lock_refuses(self):
        lock = self.state / 'default/gtx.lock'
        with lock.open('wb') as handle:
            fcntl.flock(handle, fcntl.LOCK_EX)
            before = self.snapshot()
            result = self.run_script()
            self.assertNotEqual(result.returncode, 0)
            self.assertIn('server lock is held', result.stderr)
            self.assertEqual(self.snapshot(), before)

    def test_live_socket_refuses(self):
        path = self.state / 'default/gtx.sock'
        with socket.socket(socket.AF_UNIX) as server:
            server.bind(str(path))
            server.listen()
            result = self.run_script()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('socket', result.stderr)
        self.assertTrue(self.state.is_dir())

    def test_failed_rewrite_restores_every_original(self):
        self.config.joinpath('plugins.json').write_text('{invalid JSON')
        before = self.snapshot()
        result = self.run_script()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('ROLLBACK', result.stderr)
        self.assertEqual(self.snapshot(), before)
        self.assertFalse(self.home.joinpath('.local/state/ikesu').exists())
        self.assertFalse(self.home.joinpath('.config/ikesu').exists())
        self.assertEqual(list(self.home.rglob('.ikesu-migrate-*')), [])


if __name__ == '__main__':
    unittest.main(verbosity=2)
