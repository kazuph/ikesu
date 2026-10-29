#!/usr/bin/env python3
"""Queue verification with real flock holders, processes, and Go tests."""
import importlib.util
import json
import os
from pathlib import Path
import select
import signal
import subprocess
import sys
import tempfile
import time
import unittest

ROOT = Path(__file__).resolve().parent.parent
spec = importlib.util.spec_from_file_location('test_go', ROOT / 'scripts/test-go.py')
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


def read_line(process, prefix):
    deadline = time.monotonic() + 5
    while True:
        ready, _, _ = select.select([process.stdout], [], [], max(0, deadline-time.monotonic()))
        if not ready:
            raise AssertionError('process did not report ' + prefix)
        line = process.stdout.readline().decode()
        if not line:
            raise AssertionError('process exited before ' + prefix)
        if line.startswith(prefix):
            return line.rstrip()


def stop_owned(process):
    if process.stdin and not process.stdin.closed:
        process.stdin.close()
    try:
        process.wait(timeout=5)
    except subprocess.TimeoutExpired:
        matches = subprocess.check_output(['pgrep', '-u', str(os.getuid()), '-f', str(ROOT)], text=True).split()
        if str(process.pid) not in matches:
            raise AssertionError('refusing to stop an unverified process')
        os.kill(process.pid, signal.SIGTERM)
        process.wait(timeout=5)
    process.stdout.close()


def helper(path, label, events, child):
    with runner.heavy_test_slot(path, Path.cwd(), label):
        if child == 'child':
            command = "import sys; sys.stdin.buffer.read(); print('CHILD_DONE',flush=True)"
            p = subprocess.Popen([sys.executable, '-c', command], close_fds=False)
            print('CHILD_PID=' + str(p.pid), flush=True)
        with open(events, 'a') as log:
            log.write(label + '-enter\n')
        print('ENTERED', flush=True)
        sys.stdin.buffer.readline()
        with open(events, 'a') as log:
            log.write(label + '-exit\n')


class QueueCase(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='ikesu-queue-proof-')
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name).resolve()
        self.lock = self.root / '.locks/test-go.lock'
        self.events = self.root / 'events'

    def start(self, label, child='none'):
        cwd = self.root / label
        cwd.mkdir()
        p = subprocess.Popen([sys.executable, str(Path(__file__).resolve()), 'helper', str(self.lock), label,
                              str(self.events), child], cwd=cwd, stdin=subprocess.PIPE,
                             stdout=subprocess.PIPE, stderr=subprocess.STDOUT, bufsize=0)
        self.addCleanup(stop_owned, p)
        return p


class QueueLockTests(QueueCase):
    def test_classification(self):
        for packages, changed, selected, heavy in [
            (['./...'], None, None, True),
            (['all'], 'origin/main', '^TestOne$', True),
            (['./internal/...'], 'origin/main', 'TestOne', True),
            (['./internal/server'], None, None, True),
            (['./internal/model', './internal/render'], None, None, True),
            (['./internal/model', './internal/render'], 'origin/main', None, False),
            (['./internal/server'], None, '^TestOne$', False),
        ]:
            with self.subTest(packages=packages, changed=changed, selected=selected):
                self.assertEqual(runner.is_heavy_run(packages, changed, selected), heavy)

    def test_distinct_worktrees_wait_for_same_owner(self):
        holder = self.start('holder')
        read_line(holder, 'ENTERED')
        waiter = self.start('waiter')
        owner = json.loads(read_line(waiter, 'TEST_QUEUE waiting ').removeprefix('TEST_QUEUE waiting '))
        self.assertEqual(owner['worktree'], str(self.root / 'holder'))
        self.assertEqual(owner['label'], 'holder')
        self.assertEqual(owner['pid'], holder.pid)
        self.assertTrue(owner['started_at'].endswith('+00:00'))
        holder.stdin.write(b'\n')
        read_line(waiter, 'ENTERED')
        self.assertEqual(self.events.read_text().splitlines(), ['holder-enter', 'holder-exit', 'waiter-enter'])
        print('QUEUE_WAIT_OWNER=' + json.dumps(owner), flush=True)
        waiter.stdin.write(b'\n')
        self.assertEqual(holder.wait(timeout=5), 0)
        self.assertEqual(waiter.wait(timeout=5), 0)

    def test_sigkill_releases_lock_even_with_live_child(self):
        holder = self.start('holder', 'child')
        child_pid = int(read_line(holder, 'CHILD_PID=').split('=', 1)[1])
        read_line(holder, 'ENTERED')
        waiter = self.start('waiter')
        read_line(waiter, 'TEST_QUEUE waiting ')
        matches = subprocess.check_output(['pgrep', '-u', str(os.getuid()), '-f', str(Path(__file__).resolve())], text=True).split()
        self.assertIn(str(holder.pid), matches)
        os.kill(holder.pid, signal.SIGKILL)
        self.assertEqual(holder.wait(timeout=5), -signal.SIGKILL)
        os.kill(child_pid, 0)
        read_line(waiter, 'ENTERED')
        waiter.stdin.write(b'\n')
        self.assertEqual(waiter.wait(timeout=5), 0)
        holder.stdin.close()
        read_line(holder, 'CHILD_DONE')
        print('QUEUE_SIGKILL=' + json.dumps(dict(holder=holder.pid, child=child_pid, child_alive_at_unlock=True, waiter=waiter.pid, released=True)), flush=True)


class QueueRunnerTests(QueueCase):
    def test_unfiltered_real_go_waits_for_shared_lock(self):
        self.lock = self.root / 'bench-terrace.noindex/.locks/test-go.lock'
        holder = self.start('holder')
        read_line(holder, 'ENTERED')
        go_env = json.loads(subprocess.check_output(['go', 'env', '-json', 'GOCACHE', 'GOPATH'], text=True))
        env = dict(os.environ, HOME=str(self.root), GOCACHE=go_env['GOCACHE'], GOPATH=go_env['GOPATH'])
        output = self.root / 'heavy-result'
        process = subprocess.Popen([sys.executable, 'scripts/test-go.py', './internal/model', '--mode', 'normal',
                                    '--label', 'heavy-proof', '--output', str(output)], cwd=ROOT, env=env,
                                   stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, bufsize=0)
        self.addCleanup(stop_owned, process)
        owner = json.loads(read_line(process, 'TEST_QUEUE waiting ').removeprefix('TEST_QUEUE waiting '))
        self.assertEqual(owner['pid'], holder.pid)
        self.assertFalse((output / 'summary.json').exists())
        holder.stdin.write(b'\n')
        acquired = json.loads(read_line(process, 'TEST_QUEUE acquired ').removeprefix('TEST_QUEUE acquired '))
        self.assertEqual(acquired['label'], 'heavy-proof')
        self.assertEqual(acquired['worktree'], str(ROOT))
        stdout, _ = process.communicate(timeout=30)
        self.assertEqual(process.returncode, 0, stdout.decode())
        summary = json.loads((output / 'summary.json').read_text())
        self.assertGreater(summary['queue_wait_seconds'], 0)
        self.assertTrue(summary['results'])
        print('QUEUE_HEAVY=' + json.dumps(dict(owner=owner, acquired=acquired, wait=summary['queue_wait_seconds'], exit=process.returncode)), flush=True)
        self.assertEqual(holder.wait(timeout=5), 0)

    def test_filtered_real_go_bypasses_held_shared_lock(self):
        self.lock = self.root / 'bench-terrace.noindex/.locks/test-go.lock'
        holder = self.start('holder')
        read_line(holder, 'ENTERED')
        go_env = json.loads(subprocess.check_output(['go', 'env', '-json', 'GOCACHE', 'GOPATH'], text=True))
        env = dict(os.environ, HOME=str(self.root), GOCACHE=go_env['GOCACHE'], GOPATH=go_env['GOPATH'])
        output = self.root / 'light-result'
        result = subprocess.run([sys.executable, 'scripts/test-go.py', './internal/model', '--mode', 'normal',
                                 '--run', '^Test', '--output', str(output)], cwd=ROOT, env=env,
                                capture_output=True, text=True, timeout=30)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertNotIn('TEST_QUEUE waiting', result.stdout)
        summary = json.loads((output / 'summary.json').read_text())
        self.assertEqual(summary['queue_wait_seconds'], 0)
        self.assertTrue(summary['results'])
        self.assertIsNone(holder.poll())
        print('QUEUE_LIGHT=' + json.dumps(dict(wait=summary['queue_wait_seconds'], exit=result.returncode, holder_alive=True)), flush=True)
        holder.stdin.write(b'\n')
        self.assertEqual(holder.wait(timeout=5), 0)


if __name__ == '__main__':
    if len(sys.argv) > 1 and sys.argv[1] == 'helper':
        helper(*sys.argv[2:])
    else:
        unittest.main()
