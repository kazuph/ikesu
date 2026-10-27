"""Real isolated servers; only the Homebrew command is the approved local emulator."""
import json
import os
from pathlib import Path
import shlex
import shutil
import subprocess
import tempfile
import time
import unittest

ROOT = Path(__file__).resolve().parents[1]
OLD = Path(os.environ['IKESU_UPGRADE_TEST_OLD']).resolve()
NEW = Path(os.environ['IKESU_UPGRADE_TEST_NEW']).resolve()
INCOMPATIBLE = Path(os.environ['IKESU_UPGRADE_TEST_INCOMPATIBLE']).resolve()


def command(*args, env):
    return subprocess.run([str(a) for a in args], env=env, capture_output=True, text=True)


def until(check):
    deadline = time.monotonic() + 15  # existing CLI restart readiness budget
    while time.monotonic() < deadline:
        value = check()
        if value:
            return value
        time.sleep(.05)
    raise AssertionError('isolated server condition did not become ready')


class UpgradeTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='ik-brew-', dir='/tmp')
        self.root = Path(self.temp.name)
        self.env = {k: v for k, v in os.environ.items() if not k.startswith(('IKESU_', 'HERDR_'))}
        self.env.update(HOME=str(self.root), SHELL='/bin/sh', IKESU_STATE_DIR=str(self.root/'state'), IKESU_CONFIG=str(self.root/'config.toml'))
        Path(self.env['IKESU_CONFIG']).write_text('onboarding=false\n[experimental]\nallow_nested=true\n[session]\nresume_agents_on_restore=false\n')
        self.entry = self.root/'.local/bin/ikesu'
        self.entry.parent.mkdir(parents=True)
        self.old = self.root/'.local/lib/ikesu/releases/old/ikesu'
        self.old.parent.mkdir(parents=True)
        shutil.copy2(OLD, self.old)
        self.entry.symlink_to(self.old)
        self.prefix = self.root/'Cellar/ikesu/current'
        (self.prefix/'bin').mkdir(parents=True)
        shutil.copy2(NEW, self.prefix/'bin/ikesu')
        self.brew = self.root/'brew'
        self.brew.write_text('''#!/usr/bin/env python3
import json,os,sys,pathlib
root=pathlib.Path(os.environ['HOME'])
with (root/'brew-calls.jsonl').open('a') as f:f.write(json.dumps({'args':sys.argv[1:],'cleanup':os.environ.get('HOMEBREW_NO_INSTALL_CLEANUP')})+'\\n')
if sys.argv[1:]==['list','--versions','ikesu']:print('ikesu '+os.environ['TEST_BREW_VERSION'])
elif sys.argv[1:]==['--prefix','ikesu']:print(root/'Cellar/ikesu/current')
elif sys.argv[1:]==['info','--json=v2','ikesu']:print(json.dumps({'formulae':[{'versions':{'stable':os.environ['TEST_BREW_VERSION']}}]}))
elif sys.argv[1:] not in [['upgrade','ikesu'],['install','kazuph/tap/ikesu']]:raise SystemExit(2)
''')
        self.brew.chmod(0o755)
        self.old_version = command(OLD, '--version', env=self.env).stdout.strip().split()[-1]
        self.new_version = command(NEW, '--version', env=self.env).stdout.strip().split()[-1]
        self.env['TEST_BREW_VERSION'] = self.new_version
        self.servers = []
        self.addCleanup(self.cleanup)

    def cli(self, *args, session='default'):
        return command(self.old, '--session', session, *args, env=self.env)

    def start(self, session='default'):
        log = open(self.root/(session+'.log'), 'w')
        child = subprocess.Popen([self.old, '--session', session, 'server'], env=self.env, stdout=log, stderr=log)
        self.servers.append((session, child, log))
        until(lambda: self.cli('server', 'ping', session=session).returncode == 0)
        return json.loads(self.cli('pane', 'list', session=session).stdout)['result']['panes']

    def upgrade(self, *sessions):
        args = [ROOT/'scripts/ikesu-upgrade.sh', '--brew', self.brew]
        for session in sessions:
            args += ['--session', session]
        return command(*args, env=self.env)

    def cleanup(self):
        for session, child, log in self.servers:
            self.cli('server', 'stop', session=session)
            try:
                child.wait(timeout=15)
            except subprocess.TimeoutExpired:
                child.terminate()
                child.wait(timeout=15)
            log.close()
        self.temp.cleanup()

    def test_success_preserves_panes_io_and_immutable_binary(self):
        before = self.start()
        pane = before[0]['pane_id']
        pidfile = self.root/'pane-pid'
        self.assertEqual(self.cli('pane', 'run', pane, 'printf "%s" $$ > '+shlex.quote(str(pidfile))).returncode, 0)
        until(pidfile.exists)
        pid = pidfile.read_bytes()
        result = self.upgrade()
        self.assertEqual(result.returncode, 0, result.stdout+result.stderr)
        self.assertEqual(self.entry.resolve(), self.root/'.local/lib/ikesu/releases'/self.new_version/'ikesu')
        after = json.loads(self.cli('pane', 'list').stdout)['result']['panes']
        self.assertEqual([p['pane_id'] for p in before], [p['pane_id'] for p in after])
        pidfile.unlink()
        self.cli('pane', 'run', pane, 'printf "%s" $$ > '+shlex.quote(str(pidfile)))
        until(pidfile.exists)
        self.assertEqual(pid, pidfile.read_bytes())
        shutil.rmtree(self.root/'Cellar')
        self.assertEqual(command(self.entry,'--version',env=self.env).stdout.strip(), 'ikesu '+self.new_version)
        calls=[json.loads(l) for l in (self.root/'brew-calls.jsonl').read_text().splitlines()]
        self.assertTrue(all(c['cleanup']=='1' for c in calls))

    def test_failed_real_protocol_handoff_retains_old_entry_and_server(self):
        self.start()
        shutil.copy2(INCOMPATIBLE, self.prefix/'bin/ikesu')
        result = self.upgrade()
        self.assertNotEqual(result.returncode, 0, result.stdout+result.stderr)
        self.assertEqual(self.entry.resolve(), self.old)
        self.assertEqual(json.loads(self.cli('server','ping').stdout)['result']['version'], self.old_version)

    def test_brew_binary_version_mismatch_never_handoffs(self):
        self.start()
        self.env['TEST_BREW_VERSION'] = '999.999.999'
        result = self.upgrade()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.entry.resolve(), self.old)
        self.assertEqual(json.loads(self.cli('server','ping').stdout)['result']['version'], self.old_version)

    def test_ordered_sessions_stop_before_remaining_session(self):
        self.start('first')
        self.start('second')
        self.start('third')
        # A successful first migration followed by a non-server socket error.
        # Keep second's real server, but deliberately make its state path unavailable
        # to the CLI by removing only its own API socket directory entry.
        (self.root/'state/second/ikesu.sock').unlink()
        result = self.upgrade('first','second','third')
        self.assertNotEqual(result.returncode,0)
        self.assertEqual(self.entry.resolve(),self.old)
        self.assertEqual(json.loads(self.cli('server','ping',session='first').stdout)['result']['version'], self.new_version)
        self.assertEqual(json.loads(self.cli('server','ping',session='third').stdout)['result']['version'], self.old_version)


if __name__ == '__main__':
    unittest.main()
