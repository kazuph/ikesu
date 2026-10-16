#!/usr/bin/env python3
"""Sidebar fixtures: real public API calls, restricted to observe.py's socket."""
import json
import os
from pathlib import Path
import socket
import sqlite3
import subprocess
import sys

endpoint = os.environ.get('HERDR_SOCKET_PATH') or os.environ.get('GTX_SOCKET_PATH')
if not endpoint or not str(Path(endpoint).resolve()).startswith('/private/tmp/parity-'):
    raise SystemExit('REFUSED: not an observer-owned socket')
pane = os.environ.get('HERDR_PANE_ID') or os.environ.get('GTX_PANE_ID')
session = os.environ.get('PARITY_CONTROLLER_SESSION_ID')
if not pane or not session:
    raise SystemExit('NOT_RUN: actual pane/controller session missing')


def request(method, params):
    with socket.socket(socket.AF_UNIX) as connection:
        connection.settimeout(5)
        connection.connect(endpoint)
        connection.sendall(json.dumps({'id': 'stage4', 'method': method, 'params': params}).encode() + b'\n')
        response = bytearray()
        while b'\n' not in response:
            chunk = connection.recv(65536)
            if not chunk:
                raise RuntimeError('isolated API closed without a reply')
            response.extend(chunk)
    result = json.loads(response.split(b'\n')[0])
    path = Path.home() / 'stage4-actions.json'
    proof = json.loads(path.read_text()) if path.exists() else []
    proof.append({'method': method, 'params': params, 'response': result})
    path.write_text(json.dumps(proof, indent=2) + '\n')
    if 'error' in result:
        raise RuntimeError(result)
    return result['result']


def report(target, state='idle'):
    request('pane.report_agent', {'pane_id': target, 'source': 'codex', 'agent': 'codex',
                                'state': state, 'title': 'PARITY', 'session_id': session})


operation = sys.argv[1]
owner = request('pane.get', {'pane_id': pane})['pane']
pane = owner['pane_id']
if operation == 'review-overflow':
    records = [owner]
    for index in range(1, 30):
        created = request('workspace.create', {'cwd': str(Path.cwd()), 'label': f'REVIEW_{index+1:02}', 'focus': False})
        records.append(created['root_pane'])
    for index, record in enumerate(records):
        request('pane.report_agent', {'pane_id': record['pane_id'], 'source': 'codex', 'agent': 'codex',
                                     'state': 'blocked' if index == 0 else 'idle', 'session_id': session})
    (Path.home() / 'stage4-review-overflow.json').write_text(json.dumps(records) + '\n')
elif operation == 'review-stripped':
    created = request('workspace.create', {'cwd': str(Path.cwd()), 'label': 'SECOND_WORKSPACE', 'focus': False})
    for target, state in [(pane, 'blocked'), (created['root_pane']['pane_id'], 'idle')]:
        request('pane.report_agent', {'pane_id': target, 'source': 'codex', 'agent': 'codex',
                                     'state': state, 'session_id': session})
    (Path.home() / 'stage4-review-stripped.json').write_text(json.dumps(created) + '\n')
elif operation == 'review-title-second':
    created = json.loads((Path.home() / 'stage4-review-stripped.json').read_text())
    other = created['root_pane']['pane_id']
    request('pane.send_text', {'pane_id': other, 'text': "printf '\\033]0;SECOND_ROW\\007'"})
    request('pane.send_keys', {'pane_id': other, 'keys': ['Enter']})
elif operation == 'custom-height':
    created = request('workspace.create', {'cwd': str(Path.cwd()), 'label': 'SECOND_WORKSPACE', 'focus': False})
    other = created['root_pane']['pane_id']
    request('pane.rename', {'pane_id': other, 'label': 'SECOND_LABEL'})
    report(other)
    (Path.home() / 'stage4-custom-height.json').write_text(json.dumps(created) + '\n')
elif operation == 'create-finished-job':
    started = request('run.start', {'caller_pane': pane, 'label': 'UNKNOWN_EXIT', 'cwd': str(Path.cwd()),
                                   'argv': ['/usr/bin/true'], 'completion': 'none'})
    (Path.home() / 'stage4-finished-job.json').write_text(json.dumps(started) + '\n')
elif operation == 'remove-exit-code':
    started = json.loads((Path.home() / 'stage4-finished-job.json').read_text())
    root = next(path for path in Path(endpoint).resolve().parents if path.name.startswith('parity-'))
    changed = []
    for path in root.rglob('*.db'):
        with sqlite3.connect(path) as db:
            table = db.execute("select name from sqlite_master where type='table' and name='dispatches'").fetchone()
            if table is None:
                continue
            before = db.execute('select status,finished_at,exit_code from dispatches where external_id=?', (started['job'],)).fetchall()
            if not before:
                continue
            if len(before) != 1 or before[0][1] is None:
                raise RuntimeError('real job has not finished')
            db.execute('update dispatches set exit_code=NULL where external_id=?', (started['job'],))
            changed.append({'path': str(path), 'before': before, 'exit_code': None})
    if len(changed) != 1:
        raise RuntimeError('expected exactly one saved job to lose its exit code')
    (Path.home() / 'stage4-exit-code-removed.json').write_text(json.dumps(changed) + '\n')
elif operation == 'job-schema':
    request('run.start', {'caller_pane': pane, 'label': 'AGE_JOB', 'cwd': str(Path.cwd()),
                         'argv': ['/bin/sleep', '60'], 'completion': 'none'})
    root = next(path for path in Path(endpoint).resolve().parents if path.name.startswith('parity-'))
    proof = {}
    for path in root.rglob('*'):
        if not path.is_file():
            continue
        with path.open('rb') as stream:
            if stream.read(16) != b'SQLite format 3\x00':
                continue
        with sqlite3.connect(f'file:{path}?mode=ro', uri=True) as db:
            tables = db.execute("select name, sql from sqlite_master where type='table'").fetchall()
            proof[str(path)] = tables
    (Path.home() / 'stage4-job-schema.json').write_text(json.dumps(proof, indent=2) + '\n')
elif operation == 'remove-job-log':
    root = next(path for path in Path(endpoint).resolve().parents if path.name.startswith('parity-'))
    logs = list(root.rglob('job-logs/*.log'))
    if len(logs) != 1:
        raise RuntimeError('expected exactly one observer-owned job log')
    logs[0].unlink()
    (Path.home() / 'stage4-log-removed.json').write_text(json.dumps({'path': str(logs[0])}) + '\n')
elif operation in ('git-fixture', 'worktree-fixture'):
    cwd = Path.cwd().resolve()
    root = next(path for path in Path(endpoint).resolve().parents if path.name.startswith('parity-'))
    if not cwd.is_relative_to(root):
        raise SystemExit('REFUSED: git fixture cwd is not observer-owned')
    def git(*args):
        subprocess.run(['git', '-c', 'commit.gpgsign=false', *args], check=True)
    git('init', '-b', 'main')
    git('config', 'user.name', 'Parity fixture')
    git('config', 'user.email', 'parity@example.invalid')
    Path('tracked.txt').write_text('initial\n')
    git('add', 'tracked.txt')
    git('commit', '-m', 'fixture baseline')
    origin = root / 'home' / 'git-origin.git'
    git('init', '--bare', str(origin))
    git('remote', 'add', 'origin', str(origin))
    git('push', '-u', 'origin', 'main')
    Path('committed.txt').write_text('ahead\n')
    git('add', 'committed.txt')
    git('commit', '-m', 'fixture ahead')
    Path('tracked.txt').write_text('replacement\nadded\n')
    if operation == 'worktree-fixture':
        children = []
        for name in ('CHILD_ONE', 'CHILD_TWO'):
            children.append(request('worktree.create', {'workspace_id': owner['workspace_id'],
                            'branch': name.lower(), 'path': str(root / 'home' / name.lower()),
                            'label': name, 'focus': False}))
        (Path.home() / 'stage4-worktree-ready.json').write_text(json.dumps(children) + '\n')
    else:
        request('workspace.create', {'cwd': str(cwd), 'label': 'GIT_FIXTURE', 'focus': False})
    (Path.home() / 'stage4-git-ready.json').write_text(json.dumps({'cwd': str(cwd)}) + '\n')
elif operation in ('name-tab', 'long-labels'):
    request('tab.rename', {'tab_id': owner['tab_id'], 'label':
                          'TAB_LONG_ABCDEFGHIJKLMNOPQRSTUVWXYZ' if operation == 'long-labels' else 'NAMED_TAB'})
    if operation == 'long-labels':
        request('workspace.rename', {'workspace_id': owner['workspace_id'],
                                    'label': 'WORKSPACE_LONG_ABCDEFGHIJKLMNOPQRSTUVWXYZ'})
    report(pane)
elif operation == 'split-agents':
    request('pane.split', {'target_pane_id': pane, 'direction': 'right', 'focus': False})
    panes = request('pane.list', {'workspace_id': owner['workspace_id']})['panes']
    for item in panes:
        report(item['pane_id'])
    created = request('tab.create', {'workspace_id': owner['workspace_id'], 'label': 'OTHER_TAB', 'focus': False})
    report(created['root_pane']['pane_id'])
    (Path.home() / 'stage4-ready.json').write_text(json.dumps(panes) + '\n')
elif operation == 'focus-second':
    panes = json.loads((Path.home() / 'stage4-ready.json').read_text())
    targets = [item['pane_id'] for item in panes if item['pane_id'] != pane]
    if len(targets) != 1:
        raise RuntimeError('expected exactly two panes in the initial tab')
    request('pane.focus', {'pane_id': targets[0]})
else:
    raise SystemExit('unsupported fixture operation: ' + operation)
