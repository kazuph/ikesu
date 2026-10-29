#!/usr/bin/env python3
"""Integration tests for the runner: real Go E2E, real independent server."""
import importlib.util
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import sys
import tempfile
import time
import unittest

ROOT = Path(__file__).resolve().parent.parent
spec = importlib.util.spec_from_file_location('test_go', ROOT / 'scripts/test-go.py')
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


def rpc(state, method='ping'):
    with socket.socket(socket.AF_UNIX) as connection:
        connection.settimeout(1)
        connection.connect(str(Path(state) / 'e2e' / 'ikesu.sock'))
        connection.sendall((json.dumps(dict(id='runner-proof', method=method, params={})) + '\n').encode())
        return json.loads(connection.makefile('rb').readline())


class RunnerCleanup(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.fixture = tempfile.TemporaryDirectory(prefix='ikesu-runner-proof-', dir='/tmp')
        cls.root = Path(cls.fixture.name)
        cls.binary = cls.root / 'ikesu'
        subprocess.run(['go', 'build', '-o', str(cls.binary), './cmd/ikesu'], cwd=ROOT, check=True)
        home = cls.root / 'home'
        home.mkdir()
        config = cls.root / 'config.toml'
        config.write_text('onboarding=false\n[session]\nresume_agents_on_restore=false\n')
        cls.state = cls.root / 'foreign'
        env = dict(os.environ, HOME=str(home), IKESU_STATE_DIR=str(cls.state),
                   IKESU_CONFIG=str(config), SHELL='/bin/sh')
        cls.log = (cls.root / 'foreign.log').open('w')
        cls.foreign = subprocess.Popen([str(cls.binary), '--session', 'e2e', 'server'], env=env,
                                       stdout=cls.log, stderr=cls.log)
        cls.addClassCleanup(cls.stop_foreign)
        deadline = time.monotonic() + 10
        while True:
            try:
                response = rpc(cls.state)
                if response.get('result', {}).get('type') == 'pong':
                    break
            except (OSError, ValueError):
                pass
            if time.monotonic() >= deadline:
                raise RuntimeError('independent server did not start')
            time.sleep(0.05)

    @classmethod
    def stop_foreign(cls):
        try:
            rpc(cls.state, 'server.stop')
            cls.foreign.wait(timeout=10)
        finally:
            if cls.foreign.poll() is None:
                cls.foreign.kill()
                cls.foreign.wait()
            cls.log.close()
            cls.fixture.cleanup()

    def exercise(self, name, extra, wait_for_server=False, interrupt=False, log_directory=False, success=False):
        output = self.root / name
        output.mkdir()
        if log_directory:
            (output / '01-race.jsonl').mkdir()
        test_name = '^TestLastPaneExitFinalWorkspaceShowsHomeWithViewers$' if success else '^TestLiveHandoffUnresponsiveImporterRetainsPane$'
        command = [sys.executable, 'scripts/test-go.py', '--output', str(output),
                   '--run', test_name,
                   '--mode', 'both' if log_directory else 'normal',
                   '--parallel', '1', '-p', '1', './tests/e2e'] + extra
        with (output / 'runner.log').open('w') as log:
            process = subprocess.Popen(command, cwd=ROOT, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
            self.addCleanup(lambda: process.wait() if process.poll() is not None else (process.terminate(), process.wait(timeout=15)))
            first = process.stdout.readline()
            log.write(first)
            while first and not first.startswith('OWNERSHIP_ROOT='):
                first = process.stdout.readline()
                log.write(first)
            self.assertTrue(first.startswith('OWNERSHIP_ROOT='), first)
            ownership_root = Path(first.strip().split('=', 1)[1])
            watched = ownership_root / 'unrelated-reader.log'
            watched.write_text('unrelated reader must survive\n')
            tail = subprocess.Popen(['tail', '-f', str(watched)], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            self.addCleanup(lambda: tail.wait() if tail.poll() is not None else (tail.terminate(), tail.wait()))
            self.assertIsNone(process.poll(), 'runner finished before unrelated reader was started')
            if wait_for_server:
                deadline = time.monotonic() + 30
                observed = False
                while process.poll() is None and time.monotonic() < deadline:
                    try:
                        seen = json.loads((output / 'processes.json').read_text())['owned_processes']
                    except (OSError, ValueError):
                        seen = []
                    for item in seen:
                        if item['state_dir'] and item['state_dir'] != str(self.state):
                            try:
                                if rpc(item['state_dir']).get('result', {}).get('type') == 'pong':
                                    observed = True
                                    break
                            except (OSError, ValueError):
                                pass
                    if observed:
                        break
                    time.sleep(0.05)
                if not observed:
                    process.terminate()
                    process.wait(timeout=15)
                self.assertTrue(observed, (output / 'runner.log').read_text())
            if interrupt:
                process.send_signal(signal.SIGTERM)
            code = process.wait(timeout=60)
            log.write(process.stdout.read())
            process.stdout.close()
        summary = json.loads((output / 'summary.json').read_text())
        self.assertEqual(code, 0) if success else self.assertNotEqual(code, 0)
        self.assertFalse(summary['errors']) if success else self.assertTrue(summary['errors'])
        self.assertEqual(summary['unverified_processes'], [])
        self.assertIsNone(tail.poll(), 'unrelated reader of the same temporary path was killed')
        self.assertEqual(summary['remaining_processes'], [])
        self.assertFalse(Path(summary['ownership_root']).exists())
        self.assertTrue(summary['owned_processes'])
        self.assertIsNone(self.foreign.poll(), 'unrelated session stopped')
        self.assertEqual(rpc(self.state)['result']['type'], 'pong')
        # Revalidate retained PID identities after the wrapper itself exited.
        owner = runner.OwnedProcesses()
        try:
            owner.candidates = {item['pid']: tuple(item['started']) for item in summary['owned_processes']}
            self.assertEqual(owner.scan(), {})
            self.assertEqual(owner.unverified, {})
        finally:
            owner.cleanup([])
        report = dict(case=name, exit=code, observed_real_e2e=wait_for_server,
                      owned_count=len(summary['owned_processes']), remaining=0,
                      unrelated_server_alive=True, unrelated_tail_alive=tail.poll() is None,
                      watched_path=str(watched), tail_pid=tail.pid, errors=summary['errors'])
        print('CASE_RESULT=' + json.dumps(report), flush=True)
        if log_directory:
            self.assertEqual(summary['errors'][0]['type'], 'IsADirectoryError')
        return summary

    def test_different_token_excludes_observed_descendant(self):
        owner = runner.OwnedProcesses()
        process = subprocess.Popen([sys.executable, '-c', 'import time; time.sleep(60)'],
                                   env=dict(os.environ, GO_TEST_RUN_OWNER='different-run'))
        try:
            self.assertNotIn(process.pid, owner.scan([process.pid]))
            with self.assertRaisesRegex(RuntimeError, 'unverified descendants'):
                owner.cleanup([])
            self.assertIsNone(process.poll(), 'a different environment token was ignored')
            print('DIFFERENT_TOKEN_RESULT=not_stopped', flush=True)
        finally:
            process.terminate()
            process.wait()
            owner.candidates.clear()
            owner.cleanup([])

    def test_normal_completion(self):
        self.exercise('normal', [], success=True)

    def test_package_timeout(self):
        self.exercise('package-timeout', ['--package-timeout', '5s'], wait_for_server=True)

    def test_total_timeout(self):
        self.exercise('total-timeout', ['--total-timeout', '15'], wait_for_server=True)

    def test_interrupt(self):
        self.exercise('interrupt', [], wait_for_server=True, interrupt=True)

    def test_second_log_open_exception(self):
        self.exercise('log-directory', [], log_directory=True)


if __name__ == '__main__':
    unittest.main(verbosity=2)
