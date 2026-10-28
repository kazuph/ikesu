#!/usr/bin/env python3
# 比べる版は cmd/ikesu を持つ版（改名後）。
"""Choose the approved P1 host; retain identical builds and the shared lock."""
from pathlib import Path
import json
import os
import subprocess
import sys

source = Path(__file__).resolve().parents[2]
archive = Path(sys.argv[1]).resolve(strict=True)
out = Path(sys.argv[2]).resolve()
out.mkdir(parents=True, exist_ok=True)
load = os.getloadavg()[0]
host = 'mini' if load < 4 else 'ubuntu-3090'
worker = source / 'tests/parity/right_sidebar_perf.sh'
if host == 'mini':
    command = ['python3', str(Path.home() / 'bench-terrace.noindex/work/perf-lock.py'),
               'sh', str(worker), str(source), str(archive), str(out)]
    result = subprocess.run(command)
else:
    root = 'bench-terrace/work/right-sidebar-perf'
    subprocess.run(['ssh', host, f'mkdir -p "$HOME/{root}/candidate"'], check=True)
    subprocess.run(['rsync', '-az', '--delete', '--exclude=/.git', '--exclude=/.artifacts',
                    '--exclude=/.worktree', '--exclude=/ikesu', str(source)+'/', f'{host}:{root}/candidate/'], check=True)
    subprocess.run(['rsync', '-az', str(archive), f'{host}:{root}/baseline.tar'], check=True)
    command = ['ssh', host, '. "$HOME/bench-terrace/env.sh"; '
               f'python3 "$HOME/bench-terrace/work/perf-lock.py" sh '
               f'"$HOME/{root}/candidate/tests/parity/right_sidebar_perf.sh" '
               f'"$HOME/{root}/candidate" "$HOME/{root}/baseline.tar" "$HOME/{root}/result"']
    result = subprocess.run(command)
    subprocess.run(['rsync', '-az', '--include=*.log', '--include=*.json', '--include=*.md',
                    '--exclude=*', f'{host}:{root}/result/', str(out)+'/'], check=True)
(out / 'launch.json').write_text(json.dumps({'mini_load': load, 'host': host,
    'baseline': '70579fe', 'command': command, 'exit': result.returncode}, indent=2)+'\n')
raise SystemExit(result.returncode)
