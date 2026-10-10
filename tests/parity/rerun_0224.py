#!/usr/bin/env python3
"""Rerun the recorded procedures with default and user appearance, without perf."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys

p = argparse.ArgumentParser()
p.add_argument('--herdr', required=True)
p.add_argument('--gtx', required=True)
p.add_argument('--ptyrun', required=True)
p.add_argument('--config-file', required=True)
p.add_argument('--out', type=Path, required=True)
p.add_argument('--only', nargs='*', help='explicit procedure names for a repair rerun')
a = p.parse_args()
if not os.environ.get('PARITY_CONTROLLER_SESSION_ID'):
    p.error('validated controller session ID required')
fixtures = Path(__file__).resolve().parent
a.out.mkdir(parents=True, exist_ok=True)
procedures = [(path.stem, path) for path in sorted(fixtures.glob('*.steps'))]
for generator in sorted(fixtures.glob('*_cases.py')):
    target = a.out/'generated'/generator.stem
    subprocess.run([sys.executable, str(generator), str(target)], check=True)
    procedures.extend((generator.stem+'/'+path.stem, path) for path in sorted(target.glob('*.steps')))
if a.only:
    procedures = [(name, path) for name, path in procedures if name in a.only]
    if {name for name, _ in procedures} != set(a.only):
        p.error('unknown requested procedure')
results = []
# Socket-driven procedures require the persistent endpoint. The remaining
# procedures also exercise the standalone distribution, as in the old gates.
for name, steps in procedures:
    body = steps.read_text()
    modes = ['server'] if (name.startswith('server-') or name.startswith('server_tab_cases/') or
                          'socket_action.py' in body or 'job_fixture.py' in body or
                          'read_resize_program.py' in body or 'popup_program.py' in body) else ['local', 'server']
    for config in ('default', 'user'):
        for mode in modes:
            out = a.out/config/mode/name
            out.mkdir(parents=True, exist_ok=True)
            command = [sys.executable, str(fixtures/'observe.py'), '--herdr', a.herdr,
                       '--gtx', a.gtx, '--ptyrun', a.ptyrun, '--steps', str(steps),
                       '--out', str(out), '--clipboard-sandbox']
            if mode == 'server':
                command += ['--server', '--server-reuse-initial']
            if config == 'user':
                command += ['--config-file', a.config_file]
            if 'notification' in name or name.startswith('jobs-') or name == 'agent-working-clock' or 'job_fixture.py' in body:
                command += ['--cell-clock']
            if 'notification' in name:
                command += ['--in-app-toast']
            with (out/'comparison.log').open('w') as log:
                observed = subprocess.run(command, stdout=log, stderr=subprocess.STDOUT).returncode
                compared = None
                text_compared = None
                if observed == 0:
                    text_compared = subprocess.run([sys.executable, str(fixtures/'compare.py'),
                                                   str(out)], stdout=log,
                                                  stderr=subprocess.STDOUT).returncode
                if observed == 0 and 'cells @OUT@/' in body:
                    compared = subprocess.run([sys.executable, str(fixtures/'full_frame_gate.py'),
                                               str(out), str(steps)], stdout=log,
                                              stderr=subprocess.STDOUT).returncode
            row = {'procedure': name, 'mode': mode, 'config': config,
                   'observe_exit': observed, 'cell_comparison_exit': compared,
                   'text_collection_exit': text_compared,
                   'status': 'PASS' if observed == 0 and compared == 0 else 'NOT_MET',
                   'evidence': str(out)}
            results.append(row)
            (a.out/'index.json').write_text(json.dumps(results, ensure_ascii=False, indent=2)+'\n')
            print(json.dumps(row, ensure_ascii=False), flush=True)
            # Never retry a rejected clipboard guard or continue after copied
            # text was found in the user's pasteboard.
            if observed in (75, 76) or 'STOP:' in (out/'comparison.log').read_text():
                raise SystemExit(observed)
print('Recorded all procedures; mismatches remain explicit in index.json', flush=True)
