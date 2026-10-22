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
        plugin = Path(self.old_cwd) / 'apps/example'
        plugin.mkdir(parents=True)
        plugin.joinpath('ikesu-plugin.toml').write_text("id='example'\nmin_ikesu_version='0.4.0'\n")
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

    def seed_job_notice(self):
        source = Path(__file__).parents[1].joinpath('internal/dispatch/store.go').read_text()
        schema = source.split('const schemaSQL = decisionSchema + `', 1)[1].split('`', 1)[0]
        with sqlite3.connect(self.state / 'gtx.db') as db:
            db.executescript(schema)
            for identifier, kind, name in [(1,'agent','gtx-run'), (2,'agent','ikesu-run'), (3,'agent','p7')]:
                db.execute("INSERT INTO actors(id,kind,name,first_seen_at,last_seen_at) VALUES(?,?,?,'before','before')",(identifier,kind,name))
            db.execute("INSERT INTO dispatches(kind,room,from_actor,to_actor,body,created_at,status) VALUES('message','gtx-jobs',1,3,'pending completion','before','queued')")
            db.execute("INSERT INTO dispatches(kind,room,from_actor,to_actor,body,created_at,status) VALUES('message','default',3,1,'runner reply','before','delivered')")

    def test_internal_job_names_and_actor_collision(self):
        self.seed_job_notice()
        original = (self.state / 'gtx.db').read_bytes()
        result = self.run_script()
        self.assertEqual(result.returncode, 0, result.stderr)
        target = self.home / '.local/state/ikesu/ikesu.db'
        with sqlite3.connect(target) as db:
            self.assertEqual(db.execute("SELECT room,from_actor,to_actor,status FROM dispatches ORDER BY id").fetchall(),
                             [('ikesu-jobs',2,3,'queued'), ('default',3,2,'delivered')])
            self.assertEqual(db.execute("SELECT id,name FROM actors WHERE name IN ('gtx-run','ikesu-run')").fetchall(),[(2,'ikesu-run')])
            self.assertEqual(db.execute("PRAGMA foreign_key_check").fetchall(),[])
            self.assertEqual(db.execute("SELECT count(*) FROM dispatches WHERE kind='message' AND status='queued' AND room <> 'ikesu-jobs'").fetchone()[0],0)
        self.assertEqual(Path(str(self.state)+'.pre-ikesu').joinpath('gtx.db').read_bytes(),original)
        with sqlite3.connect(Path(str(target).replace('ikesu.db','gtx.db.pre-ikesu'))) as backup:
            self.assertEqual(backup.execute("SELECT room FROM dispatches WHERE id=1").fetchone()[0],'gtx-jobs')

    def test_job_log_bytes_and_reference_are_independent(self):
        log = self.state / 'jobs/j1.log'
        log.write_bytes((str(self.state) + '/original-output\n').encode())
        before = hashlib.sha256(log.read_bytes()).hexdigest()
        result = self.run_script()
        self.assertEqual(result.returncode,0,result.stderr)
        new = self.home / '.local/state/ikesu'
        self.assertEqual(hashlib.sha256(new.joinpath('jobs/j1.log').read_bytes()).hexdigest(),before)
        with sqlite3.connect(new / 'ikesu.db') as db:
            self.assertEqual(db.execute('SELECT log_path FROM jobs').fetchone()[0],str(new/'jobs/j1.log'))

    def test_mini_explicit_source_and_missing_config(self):
        destination = self.home / '.local/state/gtx'
        self.state.rename(destination)
        with sqlite3.connect(destination / 'gtx.db') as db:
            db.execute('UPDATE jobs SET log_path=?',(str(destination / 'jobs/j1.log'),))
        session=destination / 'default/session.json'
        data=json.loads(session.read_text())
        data['log']=str(destination / 'jobs/j1.log')
        session.write_text(json.dumps(data))
        import shutil
        shutil.rmtree(self.config)
        result = self.run_script('--source-state', str(destination), '--source-config', str(self.config))
        self.assertEqual(result.returncode,0,result.stderr)
        self.assertTrue(self.home.joinpath('.local/state/ikesu/ikesu.db').is_file())
        self.assertTrue(self.home.joinpath('.config/ikesu').is_dir())
        with sqlite3.connect(self.home / '.local/state/ikesu/ikesu.db') as db:
            self.assertEqual(db.execute('SELECT log_path FROM jobs').fetchone()[0],str(self.home / '.local/state/ikesu/jobs/j1.log'))
        self.assertFalse(destination.exists())
        self.assertTrue(Path(str(destination)+'.pre-ikesu').is_dir())

    def test_missing_new_manifest_refuses_without_changes(self):
        Path(self.old_cwd).joinpath('apps/example/ikesu-plugin.toml').unlink()
        before=self.snapshot()
        result=self.run_script()
        self.assertNotEqual(result.returncode,0)
        self.assertIn('new plugin manifest is missing',result.stderr)
        self.assertEqual(self.snapshot(),before)

    def test_mac_plugin_worktree_root_becomes_canonical(self):
        old=self.old_cwd+'/.worktree/feat/machines-mac-dialog/apps/mac-dialog-decisions'
        self.config.joinpath('plugins.json').write_text(json.dumps([{'plugin_id':'kazuph.mac-dialog-decisions','plugin_root':old,'manifest_path':old+'/gtx-plugin.toml','min_gtx_version':'0.2.7'}]))
        result=self.run_script()
        self.assertEqual(result.returncode,0,result.stderr)
        plugin=json.loads(self.home.joinpath('.config/ikesu/plugins.json').read_text())[0]
        want=self.old_cwd.replace('/goterrace','/ikesu')+'/apps/mac-dialog-decisions'
        self.assertEqual(plugin['plugin_root'],want)
        self.assertEqual(plugin['manifest_path'],want+'/ikesu-plugin.toml')
        self.assertNotIn('min_gtx_version',plugin)

    def test_db_structured_reference_preserves_historical_body(self):
        with sqlite3.connect(self.state / 'gtx.db') as db:
            db.execute('CREATE TABLE decisions (decision_id TEXT PRIMARY KEY,record TEXT)')
            db.execute('INSERT INTO decisions VALUES(?,?)',('dec-one',json.dumps({'origin':{'cwd':str(self.state)},'body':str(self.state)+'/historical-output'})))
        result=self.run_script()
        self.assertEqual(result.returncode,0,result.stderr)
        with sqlite3.connect(self.home / '.local/state/ikesu/ikesu.db') as db:
            record=json.loads(db.execute('SELECT record FROM decisions').fetchone()[0])
        self.assertEqual(record['origin']['cwd'],str(self.home / '.local/state/ikesu'))
        self.assertEqual(record['body'],str(self.state)+'/historical-output')

    def test_internal_job_actor_without_collision_keeps_identity(self):
        self.seed_job_notice()
        with sqlite3.connect(self.state / 'gtx.db') as db:
            db.execute('DELETE FROM actors WHERE id=2')
        result=self.run_script()
        self.assertEqual(result.returncode,0,result.stderr)
        with sqlite3.connect(self.home / '.local/state/ikesu/ikesu.db') as db:
            self.assertEqual(db.execute("SELECT id,name FROM actors WHERE name='ikesu-run'").fetchall(),[(1,'ikesu-run')])
            self.assertEqual(db.execute('SELECT from_actor,to_actor FROM dispatches WHERE id=1').fetchone(),(1,3))

    def test_dry_run_is_read_only(self):
        before = self.snapshot()
        result = self.run_script('--dry-run')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('REWRITE DB', result.stdout)
        self.assertIn('REWRITE FILE', result.stdout)
        self.assertIn('gtx-unused', result.stdout)
        self.assertEqual(self.snapshot(), before)

    def test_config_values_are_listed_converted_and_backed_up(self):
        text = """# gtx remains in this comment
[ui.toast]
delivery = "gtx"
[guard]
targets = [
  'gtx',
  'herdr',
]
[example]
"gtx" = "system"
inline = { "gtx" = "gtx" }
"""
        path = self.config / 'config.toml'
        path.write_text(text)
        before = self.snapshot()
        dry = self.run_script('--dry-run')
        self.assertEqual(dry.returncode, 0, dry.stderr)
        self.assertIn('ui.toast.delivery: gtx -> ikesu', dry.stdout)
        self.assertIn('guard.targets: gtx -> ikesu', dry.stdout)
        self.assertEqual(self.snapshot(), before)
        result = self.run_script()
        self.assertEqual(result.returncode, 0, result.stderr)
        new = self.home / '.config/ikesu/config.toml'
        after = new.read_text()
        self.assertIn('delivery = "ikesu"', after)
        self.assertIn("'ikesu'", after)
        self.assertIn('"gtx" = "system"', after)
        self.assertIn('inline = { "gtx" = "ikesu" }', after)
        self.assertIn('# gtx remains in this comment', after)
        self.assertEqual(Path(str(new) + '.pre-ikesu').read_text(), text)

    def test_system_delivery_is_unchanged(self):
        text = '[ui.toast]\ndelivery = "system"\n'
        self.config.joinpath('config.toml').write_text(text)
        result = self.run_script()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertNotIn('CONFIG VALUE', result.stdout)
        self.assertEqual(self.home.joinpath('.config/ikesu/config.toml').read_text(), text)

    def test_old_guard_command_is_retained_only_in_backup(self):
        shim = self.state / 'guard/bin/p_1/gtx'
        shim.parent.mkdir(parents=True)
        shim.write_text('#!/bin/sh\nexit 0\n')
        shim.chmod(0o700)
        dry = self.run_script('--dry-run')
        self.assertEqual(dry.returncode, 0, dry.stderr)
        self.assertIn('RETIRE guard entry', dry.stdout)
        self.assertTrue(shim.exists())
        result = self.run_script()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(self.home.joinpath('.local/state/ikesu/guard/bin/p_1/gtx').exists())
        backup = Path(str(self.state) + '.pre-ikesu') / 'guard/bin/p_1/gtx'
        self.assertEqual(backup.read_text(), '#!/bin/sh\nexit 0\n')

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

    def test_commit_permission_failure_rolls_back_in_reverse(self):
        self.seed_job_notice()
        before = self.snapshot()
        # An audit observer changes real directory permissions as the state
        # commit starts. No rename or filesystem result is substituted.
        code = r"""
import os, runpy, sys
from pathlib import Path
home = Path.home()
state = home / '.local/state/gtx-2e01183'
config = home / '.config'
def observe(event, args):
    if event == 'os.rename':
        source = Path(os.fsdecode(args[0]))
        if source == state:
            config.chmod(0o500)
        elif source == Path(str(state) + '.pre-ikesu'):
            config.chmod(0o700)
sys.addaudithook(observe)
sys.argv = [sys.argv[1]]
runpy.run_path(sys.argv[0], run_name='__main__')
"""
        try:
            result = subprocess.run([sys.executable, '-c', code, str(SCRIPT)],
                                    env={**os.environ, 'HOME': str(self.home)},
                                    capture_output=True, text=True)
        finally:
            self.home.joinpath('.config').chmod(0o700)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('Permission denied', result.stderr)
        self.assertIn('ROLLBACK: originals restored', result.stderr)
        self.assertEqual(self.snapshot(), before)
        self.assertTrue(self.state.is_dir())
        self.assertTrue(self.config.is_dir())
        self.assertFalse(self.home.joinpath('.local/state/ikesu').exists())
        self.assertEqual(list(self.home.rglob('.ikesu-migrate-*')), [])


if __name__ == '__main__':
    unittest.main(verbosity=2)
