#!/usr/bin/env python3
"""Record the user's process inventory each minute while a measurement runs."""
import argparse
import collections
import datetime
import json
import os
import pathlib
import subprocess
import threading

parser = argparse.ArgumentParser()
parser.add_argument('output', type=pathlib.Path)
parser.add_argument('command', nargs=argparse.REMAINDER)
args = parser.parse_args()
if not args.command:
    parser.error('a measurement command is required')
args.output.parent.mkdir(parents=True, exist_ok=True)
stop = threading.Event()
errors = []

with args.output.open('w', buffering=1) as output:
    def sample():
        data = subprocess.check_output(
            ['ps', '-u', str(os.getuid()), '-o', 'pid=,ppid=,stat=,comm='], text=True)
        processes = []
        for line in data.splitlines():
            pid, parent, state, command = line.split(None, 3)
            processes.append({'pid': int(pid), 'ppid': int(parent),
                              'state': state, 'command': command})
        counts = collections.Counter(p['command'] for p in processes)
        output.write(json.dumps({
            'utc': datetime.datetime.now(datetime.timezone.utc).isoformat(),
            'load': os.getloadavg(), 'total': len(processes),
            'commands': dict(counts), 'processes': processes}) + '\n')

    def monitor():
        try:
            sample()
            # This interval is the parent's explicitly requested minute sampler.
            while not stop.wait(60):
                sample()
        except Exception as error:
            errors.append(str(error))
            output.write(json.dumps({'sampling_error': str(error)}) + '\n')

    thread = threading.Thread(target=monitor)
    thread.start()
    try:
        result = subprocess.run(args.command)
    finally:
        stop.set()
        thread.join()
        sample()

if errors:
    raise RuntimeError('process inventory failed: ' + '; '.join(errors))
raise SystemExit(result.returncode)
