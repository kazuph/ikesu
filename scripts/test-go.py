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
import sys
import time


def descendants(roots):
    rows = subprocess.check_output(['ps', '-axo', 'pid=,ppid='], text=True)
    pairs = [tuple(map(int, row.split())) for row in rows.splitlines()]
    found = set(roots)
    while True:
        children = {pid for pid, parent in pairs if parent in found}
        if children <= found:
            return found
        found |= children


def stop(processes):
    # Capture ancestry before stopping parents, including detached test servers.
    pids = descendants([p.pid for p in processes if p.poll() is None])
    for sig in (signal.SIGTERM, signal.SIGKILL):
        for pid in pids:
            try:
                os.kill(pid, sig)
            except ProcessLookupError:
                pass
    for process in processes:
        process.wait()


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
    processes = []
    def interrupted(signum, frame):
        raise InterruptedError(f'signal {signum}')
    for sig in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
        signal.signal(sig, interrupted)
    started = time.monotonic()
    results, timings = [], []
    try:
        for iteration in range(1, args.repeat + 1):
            processes = []
            logs = []
            for mode in modes:
                path = args.output / f'{iteration:02}-{mode}.jsonl'
                command = ['go', 'test', '-json', '-count=1', '-p', str(args.packages_at_once),
                           '-parallel', str(args.parallel), '-timeout', args.package_timeout]
                if mode == 'race':
                    command.append('-race')
                if args.run:
                    command += ['-run', args.run]
                command += packages
                env = dict(os.environ, GOMAXPROCS=str(args.gomaxprocs))
                with path.open('w') as log:
                    process = subprocess.Popen(command, stdout=log, stderr=subprocess.STDOUT, env=env)
                processes.append(process)
                logs.append((mode, path))
            for process, (mode, path) in zip(processes, logs):
                remaining = args.total_timeout - (time.monotonic() - started)
                if remaining <= 0:
                    raise subprocess.TimeoutExpired(process.args, args.total_timeout)
                code = process.wait(timeout=remaining)
                results.append(dict(mode=mode, iteration=iteration, exit=code, log=str(path)))
                timings += summarize(path, mode, iteration)
            if any(p.returncode != 0 for p in processes):
                break
    except (InterruptedError, subprocess.TimeoutExpired) as error:
        print(f'STOPPED: {error}', file=sys.stderr)
        stop(processes)
        results.append(dict(error=str(error), exit=1))
    elapsed = time.monotonic() - started
    summary = dict(elapsed_seconds=elapsed, process_baseline=baseline, process_limit=cap,
                   results=results, packages=timings)
    (args.output / 'summary.json').write_text(json.dumps(summary, indent=2) + '\n')
    print('mode\titeration\tpackage\tstatus\tseconds')
    for row in sorted(timings, key=lambda r: -r['elapsed']):
        print('{mode}\t{iteration}\t{package}\t{status}\t{elapsed:.3f}'.format(**row))
    print(f'TOTAL_SECONDS={elapsed:.3f}', flush=True)
    return int(any(r['exit'] != 0 for r in results))


if __name__ == '__main__':
    sys.exit(main())
