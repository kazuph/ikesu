#!/usr/bin/env python3
"""Populate only the observer's disposable server with real workspaces."""
import json
import os
import socket
import sys
from pathlib import Path

endpoint = os.environ.get('HERDR_SOCKET_PATH') or os.environ.get('GTX_SOCKET_PATH')
if not endpoint or not str(Path(endpoint).resolve()).startswith('/private/tmp/parity-'):
    raise SystemExit('REFUSED: not an owned parity socket')
proof = []
def call(method, params):
    request = {'id': 'stage6b', 'method': method, 'params': params}
    with socket.socket(socket.AF_UNIX) as s:
        s.settimeout(5)
        s.connect(endpoint)
        s.sendall(json.dumps(request).encode()+b'\n')
        reply = b''
        while b'\n' not in reply:
            chunk = s.recv(65536)
            if not chunk:
                raise RuntimeError('socket closed')
            reply += chunk
    result = json.loads(reply.split(b'\n')[0])
    if 'error' in result:
        raise RuntimeError(result)
    proof.append({'request': request, 'response': result})
    return result['result']

if len(sys.argv)>1 and sys.argv[1]=='tabs':
    workspaces=call('workspace.list', {})['workspaces']
    if len(workspaces)!=1:
        raise SystemExit('REFUSED: expected one observer workspace')
    for i in range(9):
        call('tab.create', {'workspace_id': workspaces[0]['workspace_id'], 'focus': False})
else:
    for i in range(25):
        call('workspace.create', {'label': f'nav-{i:02}', 'cwd': os.getcwd(), 'focus': False})
(Path.home()/'stage6b-fixture.json').write_text(json.dumps(proof, indent=2))
print('STAGE6B_FIXTURE_READY')
