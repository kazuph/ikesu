#!/usr/bin/env python3
"""Queue verification with real flock holders, processes, and Go tests."""
import fcntl
from datetime import datetime, timezone
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
    if child == 'late-owner':
        path = Path(path)
        path.parent.mkdir(parents=True, exist_ok=True)
        with path.open('a+') as lock:
            fcntl.flock(lock.fileno(), fcntl.LOCK_EX)
            print('LOCKED_BEFORE_RECORD', flush=True)
            sys.stdin.buffer.readline()
            owner = dict(pid=os.getpid(), worktree=str(Path.cwd()), label=label,
                         started_at=datetime.now(timezone.utc).isoformat(timespec='seconds'))
            path.with_name(path.name + '.owner.json').write_text(json.dumps(owner))
            print('RECORDED', flush=True)
            sys.stdin.buffer.readline()
        return
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
            (['./.../server'], None, '^Test', True),
            (['./.../server'], 'origin/main', None, True),
            *[([word], changed, selected, True)
              for word in ('all', 'work', 'std', 'cmd')
              for changed, selected in [('origin/main', None), (None, '^Test')]],
            (['./internal/server'], None, None, True),
            (['./internal/model', './internal/render'], None, None, True),
            (['./internal/model', './internal/render'], 'origin/main', None, False),
            (['./internal/server'], None, '^TestOne$', False),
        ]:
            with self.subTest(packages=packages, changed=changed, selected=selected):
                self.assertEqual(runner.is_heavy_run(packages, changed, selected), heavy)

    def test_waiting_owner_refreshes_after_publication(self):
        # Also reproduce an obsolete record left by a previously killed owner.
        for stale in (False, True):
            with self.subTest(stale=stale):
                owner_path = self.lock.with_name(self.lock.name + '.owner.json')
                if stale:
                    owner_path.write_text(json.dumps(dict(pid=-1, label='old-owner')))
                holder = self.start('late-holder-' + str(stale), 'late-owner')
                read_line(holder, 'LOCKED_BEFORE_RECORD')
                waiter = self.start('late-waiter-' + str(stale))
                initial = json.loads(read_line(waiter, 'TEST_QUEUE waiting ').removeprefix('TEST_QUEUE waiting '))
                self.assertEqual(initial['label'], 'old-owner' if stale else 'initializing')
                holder.stdin.write(b'\n')
                read_line(holder, 'RECORDED')
                owner = json.loads(read_line(waiter, 'TEST_QUEUE waiting ').removeprefix('TEST_QUEUE waiting '))
                self.assertEqual(owner['pid'], holder.pid)
                self.assertEqual(owner['label'], 'late-holder-' + str(stale))
                self.assertEqual(owner['worktree'], str(self.root / owner['label']))
                self.assertTrue(owner['started_at'].endswith('+00:00'))
                self.assertIsNone(holder.poll())
                holder.stdin.write(b'\n')
                read_line(waiter, 'ENTERED')
                waiter.stdin.write(b'\n')
                self.assertEqual(holder.wait(timeout=5), 0)
                self.assertEqual(waiter.wait(timeout=5), 0)
                print('QUEUE_OWNER_REFRESH=' + json.dumps(owner), flush=True)

    def test_owner_record_removed_on_release(self):
        owner_path = self.lock.with_name(self.lock.name + '.owner.json')
        with runner.heavy_test_slot(self.lock, self.root, 'owner-cleanup'):
            self.assertEqual(json.loads(owner_path.read_text())['pid'], os.getpid())
        self.assertFalse(owner_path.exists())
        with runner.heavy_test_slot(self.lock, self.root, 'next-owner'):
            self.assertEqual(json.loads(owner_path.read_text())['label'], 'next-owner')
        self.assertFalse(owner_path.exists())

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
    @classmethod
    def setUpClass(cls):
        # Compile the real package before measuring queue behavior; a cold Go
        # cache must not turn a lock-routing assertion into a compiler deadline.
        cls.build = tempfile.TemporaryDirectory(prefix='ikesu-queue-model-build-')
        cls.addClassCleanup(cls.build.cleanup)
        subprocess.run(['go', 'test', '-c', '-o', str(Path(cls.build.name) / 'model.test'),
                        './internal/model'], cwd=ROOT, check=True)

    def test_host_without_lock_root_does_not_create_it(self):
        go_env = json.loads(subprocess.check_output(['go', 'env', '-json', 'GOCACHE', 'GOPATH'], text=True))
        env = dict(os.environ, HOME=str(self.root), GOCACHE=go_env['GOCACHE'], GOPATH=go_env['GOPATH'])
        output = self.root / 'unlocked-result'
        result = subprocess.Popen([sys.executable, str(ROOT / 'scripts/test-go.py'), './internal/model', '--mode', 'normal',
                                   '--output', str(output)], cwd=ROOT, env=env, stdin=subprocess.PIPE,
                                  stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        self.addCleanup(stop_owned, result)
        stdout, _ = result.communicate(timeout=30)
        self.assertEqual(result.returncode, 0, stdout.decode())
        self.assertIn('TEST_QUEUE no-lock-dir ' + str(self.root / 'bench-terrace.noindex'), stdout.decode())
        self.assertNotIn('TEST_QUEUE acquired', stdout.decode())
        self.assertFalse((self.root / 'bench-terrace.noindex').exists())
        self.assertEqual(json.loads((output / 'summary.json').read_text())['queue_wait_seconds'], 0)

    def test_recursive_filtered_real_go_waits_for_shared_lock(self):
        self.assert_real_go_waits('./.../model', ['--run', '^Test'])

    def test_unfiltered_real_go_waits_for_shared_lock(self):
        self.assert_real_go_waits('./internal/model', [])

    def assert_real_go_waits(self, package, selection):
        self.lock = self.root / 'bench-terrace.noindex/.locks/test-go.lock'
        holder = self.start('holder')
        read_line(holder, 'ENTERED')
        go_env = json.loads(subprocess.check_output(['go', 'env', '-json', 'GOCACHE', 'GOPATH'], text=True))
        env = dict(os.environ, HOME=str(self.root), GOCACHE=go_env['GOCACHE'], GOPATH=go_env['GOPATH'])
        output = self.root / 'heavy-result'
        process = subprocess.Popen([sys.executable, str(ROOT / 'scripts/test-go.py'), package, '--mode', 'normal',
                                    '--label', 'heavy-proof', '--output', str(output), *selection], cwd=ROOT, env=env,
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
        result = subprocess.Popen([sys.executable, str(ROOT / 'scripts/test-go.py'), './internal/model', '--mode', 'normal',
                                   '--run', '^Test', '--output', str(output)], cwd=ROOT, env=env, stdin=subprocess.PIPE,
                                  stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        self.addCleanup(stop_owned, result)
        stdout, _ = result.communicate(timeout=30)
        self.assertEqual(result.returncode, 0, stdout.decode())
        self.assertNotIn('TEST_QUEUE waiting', stdout.decode())
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
