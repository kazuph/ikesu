#!/usr/bin/env python3
"""Sidebar fixtures: real public API calls, restricted to observe.py's socket."""
import json
import os
from pathlib import Path
import socket
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
if operation in ('name-tab', 'long-labels'):
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
