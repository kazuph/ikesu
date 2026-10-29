#!/usr/bin/env python3
"""Run real Go tests with bounded parallelism and per-package JSON timings.

On mini: python3 scripts/test-go.py --output /tmp/ikesu-test-results
During edits: python3 scripts/test-go.py --changed origin/main
Repeat changed tests: add --mode race --run REGEXP --repeat 10.
"""
import argparse
import json
import os
from pathlib import Path
import resource
import signal
import subprocess
import shutil
import tempfile
import traceback
import uuid
import sys
import time


class OwnedProcesses:
    """Identify the run before parents exit; retain identities across reparenting."""
    def __init__(self):
        self.root = Path(tempfile.mkdtemp(prefix='ikesu-test-run-', dir='/tmp'))
        self.token = uuid.uuid4().hex
        self.identities = {}
        self.seen = {}
        for name in ('tmp', 'build'):
            (self.root / name).mkdir()

    def environment(self):
        return dict(os.environ, GO_TEST_RUN_OWNER=self.token,
                    TMPDIR=str(self.root / 'tmp'), GOTMPDIR=str(self.root / 'build'))

    def scan(self, roots=()):
        output = subprocess.check_output(
            ['ps', 'eww', '-U', str(os.getuid()), '-o', 'pid=,ppid=,stat=,lstart=,command='], text=True)
        rows = {}
        for line in output.splitlines():
            parts = line.split(None, 8)
            if len(parts) != 9:
                continue
            pid, parent = map(int, parts[:2])
            rows[pid] = (parent, parts[2], tuple(parts[3:8]), parts[8])
        owned = {pid for pid, row in rows.items()
                 if ('GO_TEST_RUN_OWNER=' + self.token) in row[3].split()
                 or str(self.root) + '/' in row[3]}
        owned.update(pid for pid in roots if pid in rows)
        owned.update(pid for pid, identity in self.identities.items()
                     if pid in rows and rows[pid][2] == identity)
        while True:
            children = {pid for pid, row in rows.items() if row[0] in owned}
            if children <= owned:
                break
            owned |= children
        for pid in owned:
            row = rows[pid]
            self.identities[pid] = row[2]
            if pid not in self.seen:
                executable = subprocess.run(['ps', '-p', str(pid), '-o', 'comm='],
                                            capture_output=True, text=True).stdout.strip()
                state = next((word.split('=', 1)[1] for word in row[3].split()
                              if word.startswith('IKESU_STATE_DIR=')), None)
                self.seen[pid] = dict(pid=pid, started=list(row[2]), executable=executable,
                                      state_dir=state)
        return {pid: rows[pid] for pid in owned}

    def cleanup(self, processes):
        # Re-scan after each kill: detached servers keep the inherited token or
        # run-specific executable path even after their original parent exits.
        deadline = time.monotonic() + 10  # Existing server fixture shutdown limit.
        while True:
            live = self.scan([p.pid for p in processes if p.poll() is None])
            for pid, row in live.items():
                if row[1].startswith('Z'):
                    continue
                try:
                    os.kill(pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
            for process in processes:
                if process.poll() is not None:
                    process.wait()
            remaining = self.scan()
            if not remaining:
                shutil.rmtree(self.root)
                return []
            if time.monotonic() >= deadline:
                raise RuntimeError('owned processes remain: ' + str(sorted(remaining)))
            time.sleep(0.1)


def changed_packages(base):
    paths = subprocess.check_output(['git', 'diff', '--name-only', base, '--'], text=True).splitlines()
    paths += subprocess.check_output(['git', 'ls-files', '--others', '--exclude-standard'], text=True).splitlines()
    if any(p in ('go.mod', 'go.sum') for p in paths):
        return ['./...']
    return sorted({'./' + str(Path(p).parent) for p in paths if p.endswith('.go')})


def summarize(path, mode, iteration):
    rows = []
    with path.open() as stream:
        for line in stream:
            try:
                row = json.loads(line)
            except ValueError:
                continue
            if row.get('Action') in ('pass', 'fail', 'skip') and not row.get('Test'):
                rows.append(dict(mode=mode, iteration=iteration, package=row['Package'],
                                 status=row['Action'], elapsed=row.get('Elapsed', 0)))
    return rows


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('packages', nargs='*')
    parser.add_argument('--changed', metavar='BASE')
    parser.add_argument('--mode', choices=['both', 'normal', 'race'], default='both')
    parser.add_argument('-p', '--packages-at-once', type=int, default=4)
    parser.add_argument('--parallel', type=int, default=4)
    parser.add_argument('--gomaxprocs', type=int, default=2)
    parser.add_argument('--repeat', type=int, default=1)
    parser.add_argument('--run')
    parser.add_argument('--output', type=Path, default=Path('.artifacts/go-tests'))
    # Repository policy: 2 min/package and 10 min/full are targets; stop at 2x.
    parser.add_argument('--package-timeout', default='4m')
    parser.add_argument('--total-timeout', type=float, default=1200)
    args = parser.parse_args()
    if min(args.packages_at_once, args.parallel, args.gomaxprocs, args.repeat) < 1:
        parser.error('parallelism and repeat must be positive')
    os.chdir(Path(__file__).resolve().parent.parent)
    packages = args.packages or (changed_packages(args.changed) if args.changed else ['./...'])
    if not packages:
        print('No changed Go packages.')
        return 0
    args.output.mkdir(parents=True, exist_ok=True)
    modes = ['normal', 'race'] if args.mode == 'both' else [args.mode]
    baseline = len(subprocess.check_output(['ps', '-U', str(os.getuid()), '-o', 'pid='], text=True).splitlines())
    # Explicit resource budget: 32 child processes per concurrently running test.
    requested = baseline + 32 * len(modes) * args.packages_at_once * args.parallel
    soft, hard = resource.getrlimit(resource.RLIMIT_NPROC)
    cap = requested if hard == resource.RLIM_INFINITY else min(requested, hard)
    resource.setrlimit(resource.RLIMIT_NPROC, (cap, hard))
    print(f'packages={packages} process_baseline={baseline} process_limit={cap}', flush=True)
    owner = OwnedProcesses()
    processes, results, timings, errors = [], [], [], []
    started = time.monotonic()
    def interrupted(signum, frame):
        raise InterruptedError(f'signal {signum}')
    for sig in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
        signal.signal(sig, interrupted)
    try:
        for iteration in range(1, args.repeat + 1):
            active, logs = [], []
            for mode in modes:
                path = args.output / f'{iteration:02}-{mode}.jsonl'
                command = ['go', 'test', '-json', '-count=1', '-p', str(args.packages_at_once),
                           '-parallel', str(args.parallel), '-timeout', args.package_timeout]
                if mode == 'race':
                    command.append('-race')
                if args.run:
                    command += ['-run', args.run]
                command += packages
                env = dict(owner.environment(), GOMAXPROCS=str(args.gomaxprocs))
                with path.open('w') as log:
                    process = subprocess.Popen(command, stdout=log, stderr=subprocess.STDOUT, env=env)
                processes.append(process)
                active.append(process)
                logs.append((mode, path))
                owner.scan([process.pid])
            while any(process.poll() is None for process in active):
                owner.scan([process.pid for process in active if process.poll() is None])
                (args.output / 'processes.json').write_text(json.dumps(list(owner.seen.values()), indent=2) + '\n')
                if any(process.poll() not in (None, 0) for process in active):
                    raise RuntimeError('go test failed: ' + str([p.returncode for p in active]))
                if time.monotonic() - started >= args.total_timeout:
                    raise TimeoutError(f'total timeout {args.total_timeout}s')
                time.sleep(0.1)
            for process, (mode, path) in zip(active, logs):
                code = process.wait()
                results.append(dict(mode=mode, iteration=iteration, exit=code, log=str(path)))
                timings += summarize(path, mode, iteration)
            if any(process.returncode != 0 for process in active):
                raise RuntimeError('go test failed: ' + str([p.returncode for p in active]))
    except BaseException as error:
        errors.append(dict(type=type(error).__name__, message=str(error), traceback=traceback.format_exc()))
    finally:
        remaining = None
        for sig in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
            signal.signal(sig, signal.SIG_IGN)
        try:
            remaining = owner.cleanup(processes)
        except BaseException as error:
            errors.append(dict(type=type(error).__name__, message=str(error), traceback=traceback.format_exc()))
    elapsed = time.monotonic() - started
    summary = dict(elapsed_seconds=elapsed, process_baseline=baseline, process_limit=cap,
                   results=results, packages=timings, errors=errors,
                   owned_processes=list(owner.seen.values()), remaining_processes=remaining,
                   ownership_root=str(owner.root))
    try:
        (args.output / 'summary.json').write_text(json.dumps(summary, indent=2) + '\n')
        print('mode\titeration\tpackage\tstatus\tseconds')
        for row in sorted(timings, key=lambda r: -r['elapsed']):
            print('{mode}\t{iteration}\t{package}\t{status}\t{elapsed:.3f}'.format(**row))
    except BaseException as error:
        errors.append(dict(type=type(error).__name__, message=str(error), traceback=traceback.format_exc()))
    for error in errors:
        print(error['traceback'], file=sys.stderr)
    print('RUN_SUMMARY=' + json.dumps(summary), flush=True)
    print(f'TOTAL_SECONDS={elapsed:.3f}', flush=True)
    return int(bool(errors) or any(r['exit'] != 0 for r in results))


if __name__ == '__main__':
    sys.exit(main())
