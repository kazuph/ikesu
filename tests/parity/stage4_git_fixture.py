#!/usr/bin/env python3
"""Real Git input transitions in observe.py's disposable initial pane."""
import json
from pathlib import Path
import subprocess
import sys

cwd = Path.cwd().resolve()
home = Path.home().resolve()
if not home.parent.name.startswith('parity-') or home.parent.parent != Path('/private/tmp') or not cwd.is_relative_to(home.parent):
    raise SystemExit('REFUSED: not an observer-owned repository')

def git(*args):
    return subprocess.check_output(['git', '-c', 'commit.gpgsign=false', *args], text=True).strip()

operation = sys.argv[1]
if operation == 'initial':
    git('init', '-b', 'main')
    git('config', 'user.name', 'Parity fixture')
    git('config', 'user.email', 'parity@example.invalid')
    Path('tracked.txt').write_text('initial\n')
    git('add', 'tracked.txt')
    git('commit', '-m', 'baseline')
    origin = home / 'git-origin.git'
    git('init', '--bare', str(origin))
    git('remote', 'add', 'origin', str(origin))
    git('push', '-u', 'origin', 'main')
    git('commit', '--allow-empty', '-m', 'ahead')
    Path('tracked.txt').write_text('replacement\nadded\n')
elif operation == 'updated':
    git('checkout', '-b', 'future')
    git('commit', '--allow-empty', '-m', 'future')
    git('checkout', 'main')
    git('branch', '--set-upstream-to=future')
    Path('tracked.txt').write_text('replacement\nadded\nthird\n')
elif operation == 'clean':
    git('add', 'tracked.txt')
    git('commit', '-m', 'clean')
    git('branch', 'synced')
    git('branch', '--set-upstream-to=synced')
else:
    raise SystemExit('unsupported Git transition')
proof = {'cwd': str(cwd), 'status': git('rev-list', '--left-right', '--count', 'HEAD...@{upstream}'),
         'diff': git('diff', '--numstat', 'HEAD', '--'), 'branch': git('branch', '--show-current')}
(home / ('stage4-git-' + operation + '.json')).write_text(json.dumps(proof, indent=2) + '\n')
