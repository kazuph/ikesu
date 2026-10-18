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

pane = os.environ.get('HERDR_PANE_ID') or os.environ.get('GTX_PANE_ID')
session = os.environ.get('PARITY_CONTROLLER_SESSION_ID')
if not pane or not session:
    raise SystemExit('REFUSED: real pane and controller session required')
if len(sys.argv)>1 and sys.argv[1]=='focus':
    focused = call('pane.get', {'pane_id':pane})['pane']
    (Path.home()/'stage6b-fix-focus.json').write_text(json.dumps(focused,indent=2))
    print('FOCUSED_PANE', focused['pane_id'], focused.get('label'))
    raise SystemExit(0)
owner = call('pane.get', {'pane_id':pane})['pane']
for i in range(2):
    call('pane.split', {'target_pane_id':pane,'direction':'right','focus':False})
panes = call('pane.list', {'workspace_id':owner['workspace_id']})['panes']
if len(panes)!=3:
    raise RuntimeError('expected 3 panes')
for i,item in enumerate(panes):
    call('pane.rename', {'pane_id':item['pane_id'],'label':['NEEDLE_A','NEEDLE_B','OTHER_C'][i]})
    call('pane.report_agent', {'pane_id':item['pane_id'],'source':'codex','agent':'codex',
                              'state':'blocked' if i==0 else 'idle','session_id':session})
(Path.home()/'stage6b-fix-fixture.json').write_text(json.dumps({'panes':panes,'proof':proof},indent=2))
print('STAGE6B_FIXTURE_READY')
