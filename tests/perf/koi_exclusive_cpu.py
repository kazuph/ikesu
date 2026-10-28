#!/usr/bin/env python3
"""Collect three full off/on groups per binary, excluding observed foreign tests."""
import argparse
import datetime
import json
import os
import pathlib
import signal
import statistics
import subprocess
import threading

parser = argparse.ArgumentParser()
parser.add_argument('--before', required=True, type=pathlib.Path)
parser.add_argument('--after', required=True, type=pathlib.Path)
parser.add_argument('--driver', required=True, type=pathlib.Path)
parser.add_argument('--output', required=True, type=pathlib.Path)
args = parser.parse_args()
args.output.mkdir(parents=True, exist_ok=True)
versions = [('before', 'b3b625b', args.before), ('after', '1c5609d', args.after)]
records = []
events = (args.output / 'events.jsonl').open('w', buffering=1)
# Reuse the parent's explicitly requested one-minute observation cadence.
observation_seconds = 60
foreign_pattern = r'(^|/)go[[:space:]]+test([[:space:]]|$)|(^|/)[^ /]+[.]test([[:space:]]|$)'

def event(kind, **fields):
    events.write(json.dumps({'utc': datetime.datetime.now(datetime.timezone.utc).isoformat(),
                             'type': kind, **fields}) + '\n')

def foreign_tests():
    result = subprocess.run(['pgrep', '-u', str(os.getuid()), '-f', foreign_pattern],
                            text=True, capture_output=True)
    if result.returncode not in (0, 1):
        raise RuntimeError('foreign-test pgrep failed: ' + result.stderr)
    return [int(pid) for pid in result.stdout.split()]

def load(label):
    data = {'utc': datetime.datetime.now(datetime.timezone.utc).isoformat(),
            'load': os.getloadavg(),
            'processes': subprocess.check_output(
                ['ps', '-u', str(os.getuid()), '-o', 'pid=,ppid=,pcpu=,stat=,comm='], text=True)}
    (args.output / (label + '-load.json')).write_text(json.dumps(data, indent=2))

def run(command, directory, label, env=None):
    with (args.output / (label + '.log')).open('w') as output:
        subprocess.run(command, cwd=directory, env=env, stdout=output,
                       stderr=subprocess.STDOUT, check=True)

attempt = 0
for repeat in range(3):
    for name, commit, directory in versions:
        accepted = False
        while not accepted:
            waiting = foreign_tests()
            if waiting:
                event('waiting', pids=waiting)
            while waiting:
                threading.Event().wait(observation_seconds)
                waiting = foreign_tests()
            attempt += 1
            label = f'{attempt}-{name}'
            evidence = f'exclusive-attempt-{attempt}'
            load(label + '-before')
            command = ['python3', str(args.driver), '--seconds', '600', '--condition',
                       'pond', '--repeats', '1', '--evidence', evidence]
            detected = []
            event('started', attempt=attempt, name=name, commit=commit)
            with (args.output / (label + '-bench.log')).open('w') as output:
                process = subprocess.Popen(command, cwd=directory, stdout=output,
                                           stderr=subprocess.STDOUT)
                try:
                    while True:
                        detected = foreign_tests()
                        if detected:
                            event('tainted', attempt=attempt, name=name, pids=detected)
                            # The direct child and its exact argv are owned by this driver.
                            # SIGINT runs the benchmark's finally blocks to reap its PTYs/server.
                            if process.poll() is None:
                                assert process.args == command
                                process.send_signal(signal.SIGINT)
                            process.wait()
                            break
                        try:
                            process.wait(timeout=observation_seconds)
                            break
                        except subprocess.TimeoutExpired:
                            continue
                finally:
                    if process.poll() is None:
                        process.send_signal(signal.SIGINT)
                        process.wait()
            if not detected:
                detected = foreign_tests()
                if detected:
                    event('tainted-at-end', attempt=attempt, name=name, pids=detected)
            load(label + '-after')
            if detected:
                continue
            if process.returncode:
                raise RuntimeError(f'measurement {label} failed: exit={process.returncode}')
            env = {**os.environ, 'KOI_TRAFFIC_DIR': str(directory / evidence)}
            run(['go', 'test', './tests/perf', '-run', '^TestAuditKoiTraffic$', '-count=1', '-v'],
                directory, label + '-traffic', env)
            run(['python3', 'tests/perf/koi_protocol.py', evidence], directory, label + '-protocol')
            protocol = json.loads((directory / evidence / 'protocol.json').read_text())
            records.append({'repeat': repeat + 1, 'name': name, 'commit': commit,
                            'attempt': attempt, 'directory': str(directory / evidence),
                            'summary': protocol['summary'][0], 'wire': protocol['wire']})
            (args.output / 'records.json').write_text(json.dumps(records, indent=2))
            event('accepted', attempt=attempt, name=name)
            accepted = True
summary = []
for name, commit, _ in versions:
    rows = [r['summary'] for r in records if r['name'] == name]
    off = statistics.median(r['off_cpu_seconds'] for r in rows)
    on = statistics.median(r['on_cpu_seconds'] for r in rows)
    summary.append({'name': name, 'commit': commit, 'repeats': len(rows),
                    'median_off_cpu_seconds': off, 'median_on_cpu_seconds': on,
                    'additional_one_core_percent': (on - off) / 600 * 100})
(args.output / 'summary.json').write_text(json.dumps(summary, indent=2))
print(json.dumps(summary, indent=2))
assert all(r['summary']['server_transport']['additional_koi_updates'] == 0 for r in records)
assert summary[1]['additional_one_core_percent'] <= 1.1, summary
