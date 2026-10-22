"""Real-binary two-HOME cutover tests. The SSH transport alone is redirected,
per CUTOVER-BRIEF; servers, CLI, SQLite, migration and git worktrees are real.
Run explicitly with CUTOVER_TEST_OLD / CUTOVER_TEST_NEW.
"""
import hashlib
import json
import os
from pathlib import Path
import shutil
import socket
import sqlite3
import subprocess
import tempfile
import tarfile
import unittest

HERE = Path(__file__).resolve().parent
OLD = os.environ['CUTOVER_TEST_OLD']
NEW = os.environ['CUTOVER_TEST_NEW']


def run(args, **kw):
    return subprocess.run([str(x) for x in args], text=True, stdout=subprocess.PIPE,
                          stderr=subprocess.STDOUT, **kw)


def sha_tree(root):
    return {str(p.relative_to(root)): hashlib.sha256(p.read_bytes()).hexdigest()
            for p in root.rglob('*') if p.is_file() and not p.is_symlink()
            and p.name not in ('gtx.db-wal', 'gtx.db-shm')}


class CutoverTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='co-', dir='/tmp')
        self.root = Path(self.temp.name)
        self.local = self.root / 'l'
        self.mini = self.root / 'm'
        self.envs = {}
        self.servers = []
        self.addCleanup(self.cleanup_fixture, self.temp, self.envs, self.servers)
        for home, mini in ((self.local, False), (self.mini, True)):
            home.mkdir()
            old = home / '.local/lib/gtx/releases/49f0363/gtx-2e01183'
            old.parent.mkdir(parents=True)
            shutil.copy2(OLD, old)
            other = home / '.local/lib/gtx/releases/2e01183/gtx-2e01183'
            other.parent.mkdir(parents=True)
            shutil.copy2(OLD, other)
            bins = home / '.local/bin'
            bins.mkdir()
            for name in ('gtx', 'gtx-2e01183', 'ikesu'):
                (bins / name).symlink_to(old)
            state = home / '.local/state' / ('gtx' if mini else 'gtx-2e01183')
            env = {k: v for k, v in os.environ.items() if not k.startswith(('IKESU_', 'GTX_', 'GOTERRACE_'))}
            env.update(HOME=str(home), SHELL='/bin/sh', IKESU_DB_PATH=str(state / 'gtx.db'), IKESU_DISABLE_SOUND='1')
            # Mini deliberately has no config directory. Suppress sound using the
            # real environment switch; no terminal UI is attached.
            if not mini:
                config = home / '.config/gtx'
                config.mkdir(parents=True)
                (home / '.config/gtx-2e01183').symlink_to(config)
                (config / 'config.toml').write_text('[ui.sound]\nenabled=false\n[session]\nresume_agents_on_restore=false\n[experimental]\nallow_nested=true\n')
                env['IKESU_CONFIG'] = str(config / 'config.toml')
            self.envs[home] = env
            if mini: old = bins / 'gtx'
            log = (home / 'old-server.log').open('w')
            server = subprocess.Popen([str(old), '--session', 'default', 'server'], env=env, cwd=home,
                             stdin=subprocess.DEVNULL, stdout=log, stderr=log, start_new_session=True)
            self.servers.append(server)
            log.close()
            self.wait_cli(home, ['server', 'ping'])
            self.checked([old, '--session', 'default', 'workspace', 'create', '--cwd', home, '--label', 'retained'], env=env, cwd=home)
            pane = json.loads(self.cli(home, ['pane', 'list']).stdout)['result']['panes'][0]['pane_id']
            self.rpc(home, 'pane.report_agent', {'pane_id': pane, 'source': 'cutover-test', 'agent': 'codex', 'state': 'blocked'})
            self.rpc(home, 'msg.send', {'room': 'retained', 'from_agent': 'fixture', 'to': pane, 'body': 'retained-message'})
        self.repo = self.local / 'src/github.com/kazuph/goterrace'
        self.repo.mkdir(parents=True)
        with tarfile.open(os.environ['CUTOVER_TEST_SOURCE_TAR']) as source:
            source.extractall(self.repo, filter='data')
        self.checked(['git', 'init', '-b', 'main', self.repo])
        self.checked(['git', '-C', self.repo, 'add', '.'])
        self.checked(['git', '-C', self.repo, '-c', 'user.name=test', '-c', 'user.email=test@example.invalid', 'commit', '--allow-empty', '-m', 'fixture'])
        self.sha = self.checked(['git', '-C', self.repo, 'rev-parse', 'HEAD']).strip()
        config = self.local / '.config/gtx'
        (config / 'plugins.json').write_text(json.dumps([{'plugin_id': 'kazuph.mac-dialog-decisions', 'plugin_root': str(self.repo / 'apps/mac-dialog-decisions'), 'manifest_path': str(self.repo / 'apps/mac-dialog-decisions/gtx-plugin.toml'), 'enabled': True}]))
        self.scripts = self.local / 'ikesu-cutover-scripts'
        self.scripts.mkdir()
        for name in ('cutover-local.sh', 'cutover_local.py', 'migrate-ikesu.py'):
            shutil.copy2(HERE / name, self.scripts / name)
        self.checked(['git', '-C', self.repo, 'worktree', 'add', '-b', 'task', self.repo / '.worktree/task'])
        prefix = str(self.repo).replace('/', '-')
        for name in (prefix, prefix + '--worktree-task'):
            directory = self.local / '.claude/projects' / name
            directory.mkdir(parents=True)
            (directory / 'conversation.jsonl').write_text('{"evidence":"original preserved"}\n')
        ssh = self.root / 'ssh'
        ssh.write_text('#!/bin/sh\nset -eu\nshift\nexport HOME=' + str(self.mini) + '\ncd "$HOME"\nexec /bin/sh -c "$1"\n')
        ssh.chmod(0o755)
        self.ssh = ssh

    def rpc(self, home, method, params):
        state = home / '.local/state' / ('gtx' if home == self.mini else 'gtx-2e01183')
        with socket.socket(socket.AF_UNIX) as client:
            client.connect(str(state / 'default/gtx.sock'))
            client.sendall((json.dumps({'id': 'cutover-test', 'method': method, 'params': params}) + '\n').encode())
            response = json.loads(client.makefile().readline())
        self.assertNotIn('error', response, response)
        return response['result']

    def checked(self, args, **kw):
        result = run(args, **kw)
        self.assertEqual(result.returncode, 0, result.stdout)
        return result.stdout

    def cli(self, home, args, new=False):
        binary = home / ('.local/bin/ikesu' if new else ('.local/bin/gtx' if home == self.mini else '.local/lib/gtx/releases/49f0363/gtx-2e01183'))
        env = dict(self.envs[home])
        if new:
            state = home / '.local/state/ikesu'
            env.update(IKESU_STATE_DIR=str(state), IKESU_DB_PATH=str(state / 'ikesu.db'), IKESU_CONFIG=str(home / '.config/ikesu/config.toml'))
        return run([binary, '--session', 'default', *args], env=env, cwd=home)

    def wait_cli(self, home, args, new=False):
        import time
        end = time.monotonic() + 15
        while time.monotonic() < end:
            result = self.cli(home, args, new)
            if result.returncode == 0:
                return result.stdout
            time.sleep(.05)
        self.fail(result.stdout)

    def cleanup_fixture(self, temp, envs, servers):
        for home, original in envs.items():
            for new in (True, False):
                env = dict(original)
                if new:
                    state = home / '.local/state/ikesu'
                    env.update(IKESU_STATE_DIR=str(state), IKESU_DB_PATH=str(state / 'ikesu.db'), IKESU_CONFIG=str(home / '.config/ikesu/config.toml'))
                    binary = home / '.local/bin/ikesu'
                else:
                    binary = home / '.local/bin/gtx'
                    if home.name == 'l': binary = home / '.local/lib/gtx/releases/49f0363/gtx-2e01183'
                if binary.exists(): run([binary, '--session', 'default', 'server', 'stop'], env=env, cwd=home)
        for server in servers:
            if server.poll() is None: server.terminate()
            server.wait(timeout=15)
        temp.cleanup()

    def tearDown(self):
        self.cleanup_fixture(self.temp, self.envs, self.servers)

    def cutover(self, *args):
        env = dict(self.envs[self.local])
        result = run([self.scripts / 'cutover-local.sh', '--binary', NEW, '--mini-binary', NEW,
                      *([] if '--dry-run' in args else ['--sha', self.sha]), '--ssh', self.ssh, '--source-socket', self.local / '.local/state/gtx-2e01183/default/gtx.sock', *args], env=env, cwd=self.root)
        logs = '\n'.join(p.read_text() for p in self.local.glob('ikesu-cutover-*.log'))
        return result, logs

    def assert_old(self):
        for home in (self.local, self.mini):
            self.assertIn('0.3.0', self.wait_cli(home, ['server', 'ping']))
            panes = json.loads(self.cli(home, ['pane', 'list']).stdout)['result']['panes']
            self.assertEqual(len(panes), 2)
            state = home / '.local/state' / ('gtx' if home == self.mini else 'gtx-2e01183')
            with sqlite3.connect(state / 'gtx.db') as db:
                self.assertEqual(db.execute("SELECT count(*) FROM dispatches WHERE body='retained-message'").fetchone()[0], 1)
            self.assertFalse((home / '.local/state/ikesu').exists())
            self.assertTrue((home / '.local/lib/gtx/releases/49f0363/gtx-2e01183').is_file())
            self.assertFalse((home / '.local/lib/ikesu').exists())
            self.assertEqual((home / '.local/bin/ikesu').resolve(), (home / '.local/lib/gtx/releases/49f0363/gtx-2e01183').resolve())
        self.assertTrue(self.repo.is_dir())
        self.assertFalse(self.repo.with_name('ikesu').exists())
        self.checked(['git', '-C', self.repo / '.worktree/task', 'status', '--porcelain'])

    def test_dry_run(self):
        before = {home: sha_tree(home) for home in (self.local, self.mini)}
        result, logs = self.cutover('--dry-run')
        self.assertEqual(result.returncode, 0, result.stdout)
        self.assertEqual(result.stdout.count('DRY RUN: no files changed'), 2)
        self.assertEqual(logs, '')
        self.assertEqual(before, {home: sha_tree(home) for home in (self.local, self.mini)})
        self.assert_old()

    def test_success(self):
        result, logs = self.cutover()
        self.assertEqual(result.returncode, 0, result.stdout + logs)
        self.assertIn('SUCCESS', logs)
        self.assertIn('VERIFIED relocated plugin manifest', logs)
        for home in (self.local, self.mini):
            self.assertIn('0.4.0', self.wait_cli(home, ['server', 'ping'], True))
            self.assertFalse((home / '.local/lib/gtx').exists())
            for name in ('gtx', 'gtx-2e01183'):
                self.assertFalse(os.path.lexists(home / '.local/bin' / name))
            self.assertTrue((home / '.local/state' / ('gtx.pre-ikesu' if home == self.mini else 'gtx-2e01183.pre-ikesu')).is_dir())
        moved = self.repo.with_name('ikesu')
        self.checked(['git', '-C', moved / '.worktree/task', 'status', '--porcelain'])
        projects = self.local / '.claude/projects'
        for old in projects.glob('*goterrace*'):
            new = projects / old.name.replace('goterrace', 'ikesu')
            self.assertEqual(sha_tree(old), sha_tree(new))

    def test_failure_checkpoints(self):
        # Each subtest gets two freshly running real old servers.
        self.tearDown()
        points = ['preflight', 'stop', 'migrate', 'repo', 'claude', 'install', 'verify',
                  'mini:preflight', 'mini:stop', 'mini:migrate', 'mini:install', 'mini:verify']
        for point in points:
            with self.subTest(point=point):
                self.setUp()
                try:
                    result, logs = self.cutover('--fail-after', point)
                    self.assertEqual(result.returncode, 1, result.stdout + logs)
                    self.assertIn('FAILED', logs)
                    self.assertIn('injected failure after ' + point.removeprefix('mini:'), logs)
                    self.assertNotIn('ROLLBACK FAILED', logs)
                    self.assert_old()
                finally:
                    self.tearDown()
        self.setUp()

    def test_actual_legacy_handoff_socket_separate_from_db(self):
        # Reproduce the live host: the renamed old executable inherited an API
        # socket under gtx, while its DB/session are under gtx-2e01183.
        self.checked([self.local / '.local/lib/gtx/releases/49f0363/gtx-2e01183', '--session', 'default', 'server', 'stop'], env=self.envs[self.local], cwd=self.local)
        gtx = self.local / '.local/bin/gtx'
        log = (self.local / 'legacy-handoff.log').open('w')
        server = subprocess.Popen([str(gtx), '--session', 'default', 'server'], env=self.envs[self.local], cwd=self.local,
                                  stdin=subprocess.DEVNULL, stdout=log, stderr=log, start_new_session=True)
        self.servers.append(server)
        log.close()
        import time
        deadline = time.monotonic() + 15
        while True:
            pong = run([gtx, '--session', 'default', 'server', 'ping'], env=self.envs[self.local], cwd=self.local)
            if pong.returncode == 0: break
            self.assertLess(time.monotonic(), deadline, pong.stdout)
            time.sleep(.05)
        self.checked([gtx, '--session', 'default', 'workspace', 'create', '--cwd', self.local, '--label', 'handoff'], env=self.envs[self.local], cwd=self.local)
        self.checked([gtx, '--session', 'default', 'server', 'live-handoff', '--import-exe', self.local / '.local/lib/gtx/releases/49f0363/gtx-2e01183'], env=self.envs[self.local], cwd=self.local)
        source = self.local / '.local/state/gtx/default/gtx.sock'
        self.assertTrue(source.exists())
        result, logs = self.cutover('--source-socket', source)
        self.assertEqual(result.returncode, 0, result.stdout + logs)
        self.assertIn('SUCCESS', logs)
        if os.environ.get('CUTOVER_TEST_EVIDENCE'):
            evidence = Path(os.environ['CUTOVER_TEST_EVIDENCE'])
            evidence.mkdir(parents=True, exist_ok=True)
            (evidence / 'success-real-handoff.log').write_text(result.stdout + logs)
        self.assertIn('0.4.0', self.wait_cli(self.local, ['server', 'ping'], True))

    def test_missing_canonical_manifest_refuses(self):
        (self.repo / 'apps/mac-dialog-decisions/ikesu-plugin.toml').unlink()
        result, logs = self.cutover()
        self.assertEqual(result.returncode, 1, result.stdout + logs)
        self.assertIn('canonical new plugin manifest missing', logs)
        self.assert_old()

    def test_mini_failure_after_delete_keeps_both_new(self):
        result, logs = self.cutover('--fail-after', 'mini:delete')
        self.assertEqual(result.returncode, 1, result.stdout + logs)
        self.assertIn('ROLLFORWARD REQUIRED', logs)
        for home in (self.local, self.mini):
            self.assertIn('0.4.0', self.wait_cli(home, ['server', 'ping'], True))
            self.assertFalse((home / '.local/lib/gtx').exists())

    def test_active_job_refuses_without_stopping(self):
        pane = json.loads(self.cli(self.local, ['pane', 'list']).stdout)['result']['panes'][0]['pane_id']
        fifo = self.local / 'job-input'
        os.mkfifo(fifo)
        job = self.rpc(self.local, 'run.start', {'caller_pane': pane, 'label': 'active-proof', 'cwd': str(self.local), 'argv': ['/bin/sh', '-c', 'exec /bin/cat < ' + str(fifo)], 'completion': 'none'})
        try:
            result, logs = self.cutover()
            self.assertEqual(result.returncode, 1, result.stdout + logs)
            self.assertIn('active jobs prevent shutdown', logs)
            self.assert_old()
        finally:
            self.rpc(self.local, 'run.cancel', {'job_id': job['job']})

    def test_failure_after_delete_keeps_new_servers(self):
        result, logs = self.cutover('--fail-after', 'delete')
        self.assertEqual(result.returncode, 1, result.stdout + logs)
        self.assertIn('ROLLFORWARD REQUIRED', logs)
        for home in (self.local, self.mini):
            self.assertIn('0.4.0', self.wait_cli(home, ['server', 'ping'], True))
        self.assertFalse((self.local / '.local/lib/gtx').exists())
        self.assertTrue((self.mini / '.local/lib/gtx').exists())


if __name__ == '__main__':
    unittest.main()
