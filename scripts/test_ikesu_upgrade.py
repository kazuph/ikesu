"""Real isolated servers; only the Homebrew command is the approved local emulator."""
import json
import os
from pathlib import Path
import shlex
import shutil
import subprocess
import tempfile
import socket
import signal
import time
import unittest

ROOT = Path(__file__).resolve().parents[1]
OLD = Path(os.environ['IKESU_UPGRADE_TEST_OLD']).resolve()
NEW = Path(os.environ['IKESU_UPGRADE_TEST_NEW']).resolve()
INCOMPATIBLE = Path(os.environ['IKESU_UPGRADE_TEST_INCOMPATIBLE']).resolve()
OLD_INCOMPATIBLE = INCOMPATIBLE.with_name('old-incompatible-ikesu')


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
        self.root = Path(self.temp.name).resolve()
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
if sys.argv[1:]==['list','--versions','ikesu']:
 if os.environ.get('TEST_BREW_INSTALLED','1')=='0':raise SystemExit(1)
 print('ikesu '+os.environ['TEST_BREW_VERSION'])
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

    def start(self, session='default', binary=None):
        log = open(self.root/(session+'.log'), 'w')
        child = subprocess.Popen([binary or self.old, '--session', session, 'server'], env=self.env, stdout=log, stderr=log)
        self.servers.append((session, child, log))
        until(lambda: self.cli('server', 'ping', session=session).returncode == 0)
        return json.loads(self.cli('pane', 'list', session=session).stdout)['result']['panes']

    def rpc(self, method, params):
        with socket.socket(socket.AF_UNIX) as client:
            client.connect(str(self.root/'state/default/ikesu.sock'))
            client.sendall((json.dumps({'id':'upgrade-test','method':method,'params':params})+'\n').encode())
            reply=json.loads(client.makefile().readline())
        self.assertNotIn('error',reply,reply)
        return reply['result']

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
        pid = until(lambda: pidfile.read_bytes() if pidfile.exists() else None)
        plugin = self.root/'plugin'
        plugin.mkdir()
        (plugin/'ikesu-plugin.toml').write_text('id="brew.proof"\nname="Upgrade proof"\nversion="0.1.0"\nmin_ikesu_version="0.1.0"\nplatforms=["macos","linux"]\n[[panes]]\nid="board"\ntitle="Board"\ncommand=["/bin/cat"]\n')
        self.rpc('plugin.link',{'path':str(plugin)})
        plugins = self.rpc('plugin.list',{})
        fifo = self.root/'job-input'
        os.mkfifo(fifo)
        running,finished = self.root/'job-running',self.root/'job-finished'
        script = 'printf ready > '+shlex.quote(str(running))+'; cat '+shlex.quote(str(fifo))+' > '+shlex.quote(str(finished))
        registered = self.cli('pane','report-agent',pane,'--source','upgrade-proof','--agent','test-shell','--state','idle')
        self.assertEqual(registered.returncode,0,registered.stdout+registered.stderr)
        job = self.cli('run','--caller',pane,'--label','brew-proof','--','/bin/sh','-c',script)
        self.assertEqual(job.returncode,0,job.stdout+job.stderr)
        until(running.exists)
        result = self.upgrade()
        self.assertEqual(result.returncode, 0, result.stdout+result.stderr)
        self.assertEqual(self.entry.resolve(), self.root/'.local/lib/ikesu/releases'/self.new_version/'ikesu')
        after = json.loads(self.cli('pane', 'list').stdout)['result']['panes']
        self.assertEqual([p['pane_id'] for p in before], [p['pane_id'] for p in after])
        pidfile.unlink()
        self.cli('pane', 'run', pane, 'printf "%s" $$ > '+shlex.quote(str(pidfile)))
        self.assertEqual(pid, until(lambda: pidfile.read_bytes() if pidfile.exists() else None))
        self.assertEqual(plugins,self.rpc('plugin.list',{}))
        with fifo.open('w') as stream: stream.write('job survived handoff')
        until(lambda: finished.exists() and finished.read_text()=='job survived handoff')
        shutil.rmtree(self.root/'Cellar')
        child = self.root/'new-child'
        agent = self.cli('agent','start','upgrade-proof','--cwd',str(self.root),'--no-focus','--','/bin/sh','-c','printf child > '+shlex.quote(str(child)))
        self.assertEqual(agent.returncode,0,agent.stdout+agent.stderr)
        until(child.exists)
        self.assertEqual(command(self.entry,'--version',env=self.env).stdout.strip(), 'ikesu '+self.new_version)
        calls=[json.loads(l) for l in (self.root/'brew-calls.jsonl').read_text().splitlines()]
        self.assertTrue(all(c['cleanup']=='1' for c in calls))

    def test_failed_real_protocol_handoff_retains_old_entry_and_server(self):
        pane=self.start()[0]['pane_id']
        pidfile=self.root/'failure-pid'
        self.cli('pane','run',pane,'printf "%s" $$ > '+shlex.quote(str(pidfile)))
        before=until(lambda:pidfile.read_bytes() if pidfile.exists() else None)
        shutil.copy2(INCOMPATIBLE, self.prefix/'bin/ikesu')
        result = self.upgrade()
        self.assertNotEqual(result.returncode, 0, result.stdout+result.stderr)
        self.assertEqual(self.entry.resolve(), self.old)
        self.assertEqual(json.loads(self.cli('server','ping').stdout)['result']['version'], self.old_version)
        pidfile.unlink()
        self.cli('pane','run',pane,'printf "%s" $$ > '+shlex.quote(str(pidfile)))
        self.assertEqual(before,until(lambda:pidfile.read_bytes() if pidfile.exists() else None))

    def test_brew_binary_version_mismatch_never_handoffs(self):
        self.start()
        self.env['TEST_BREW_VERSION'] = '999.999.999'
        result = self.upgrade()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.entry.resolve(), self.old)
        self.assertEqual(json.loads(self.cli('server','ping').stdout)['result']['version'], self.old_version)

    def test_ordered_sessions_stop_before_remaining_session(self):
        self.start('first')
        self.start('second', OLD_INCOMPATIBLE)
        self.start('third')
        result = self.upgrade('first','second','third')
        self.assertNotEqual(result.returncode,0)
        self.assertEqual(self.entry.resolve(),self.old)
        self.assertEqual(json.loads(self.cli('server','ping',session='first').stdout)['result']['version'], self.new_version)
        self.assertEqual(json.loads(self.cli('server','ping',session='third').stdout)['result']['version'], self.old_version)
        for session,version in [('first',self.new_version),('second',self.old_version),('third',self.old_version)]:
            self.assertIn(session+': '+version,result.stdout)

    def test_first_install_does_not_start_an_unrequested_server(self):
        self.entry.unlink()
        self.env['TEST_BREW_INSTALLED']='0'
        result=self.upgrade()
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)
        self.assertEqual(self.entry.resolve(),self.root/'.local/lib/ikesu/releases'/self.new_version/'ikesu')
        self.assertNotEqual(self.cli('server','ping').returncode,0)
        calls=[json.loads(l) for l in (self.root/'brew-calls.jsonl').read_text().splitlines()]
        self.assertIn(['install','kazuph/tap/ikesu'],[c['args'] for c in calls])

    def test_existing_immutable_release_is_not_replaced(self):
        self.start()
        target=self.root/'.local/lib/ikesu/releases'/self.new_version/'ikesu'
        target.parent.mkdir(parents=True)
        shutil.copy2(INCOMPATIBLE,target)
        original=target.read_bytes()
        result=self.upgrade()
        self.assertNotEqual(result.returncode,0)
        self.assertIn('refusing to overwrite immutable release',result.stderr)
        self.assertEqual(original,target.read_bytes())
        self.assertEqual(self.entry.resolve(),self.old)

    def test_paused_server_timeout_stops_before_later_session(self):
        self.start('paused')
        self.start('later')
        pid=self.servers[0][1].pid
        identity=subprocess.check_output(['ps','-p',str(pid),'-o','uid=,command='],text=True)
        self.assertEqual(int(identity.split()[0]),os.getuid())
        self.assertIn(str(self.old),identity)
        os.kill(pid,signal.SIGSTOP)
        try:
            result=self.upgrade('paused','later')
        finally:
            os.kill(pid,signal.SIGCONT)
        self.assertNotEqual(result.returncode,0,result.stdout+result.stderr)
        self.assertEqual(self.entry.resolve(),self.old)
        self.assertEqual(json.loads(self.cli('server','ping',session='paused').stdout)['result']['version'],self.old_version)
        self.assertEqual(json.loads(self.cli('server','ping',session='later').stdout)['result']['version'],self.old_version)
        self.assertNotIn('paused: not running',result.stdout)

    def test_stale_refused_socket_is_confirmed_not_running(self):
        socket_path=self.root/'state/default/ikesu.sock'
        socket_path.parent.mkdir(parents=True)
        with socket.socket(socket.AF_UNIX) as stale:
            stale.bind(str(socket_path))
        result=self.upgrade()
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)
        self.assertEqual(self.entry.resolve(),self.root/'.local/lib/ikesu/releases'/self.new_version/'ikesu')
        self.assertIn('default: not running',result.stdout)


if __name__ == '__main__':
    unittest.main()
