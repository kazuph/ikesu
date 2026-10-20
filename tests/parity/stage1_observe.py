#!/usr/bin/env python3
"""Observe stage-one procedures in both independent launch paths."""
import argparse
import os
from pathlib import Path
import subprocess

p = argparse.ArgumentParser()
p.add_argument('--herdr', required=True)
p.add_argument('--ikesu','--gtx',dest='gtx', required=True)
p.add_argument('--ptyrun', required=True)
p.add_argument('--config-file', required=True)
p.add_argument('--out', required=True)
p.add_argument('--only', nargs='*')
a = p.parse_args()
here = Path(__file__).resolve().parent
procedures = here/'stage1_cases'
subprocess.run(['python3', str(here/'stage1_cases.py'), str(procedures)], check=True)
selected = sorted(procedures.glob('*.steps'))
if a.only:
    selected = [path for path in selected if path.stem in a.only]
    if {path.stem for path in selected} != set(a.only):
        p.error('unknown procedure in --only')
for mode in ['local', 'server']:
    for procedure in selected:
        env = dict(os.environ)
        env.pop('TMUX', None)
        env.pop('WEZTERM_PANE', None)
        env['TERM_PROGRAM'] = 'ghostty'
        command = ['python3', str(here/'observe.py'), '--herdr', a.herdr,
                   '--ikesu', a.gtx, '--ptyrun', a.ptyrun, '--config-file', a.config_file,
                   '--steps', str(procedure), '--out', str(Path(a.out)/mode/procedure.stem),
                   '--clipboard-sandbox']
        if mode == 'server':
            command.append('--server')
        print(mode, procedure.stem, flush=True)
        subprocess.run(command, env=env, check=True)
        for name in ['herdr', 'gtx']:
            log = Path(a.out)/mode/procedure.stem/name/'run.log'
            if 'TIMEOUT' in log.read_text():
                raise RuntimeError('procedure did not reach its required state: ' + str(log))
