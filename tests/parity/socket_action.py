#!/usr/bin/env python3
"""Real socket operations for disposable servers used by the PTY scenarios."""
import json
import os
from pathlib import Path
import socket
import sys

endpoint = os.environ.get('HERDR_SOCKET_PATH') or os.environ.get('GTX_SOCKET_PATH')
if not endpoint or not str(Path(endpoint).resolve()).startswith('/private/tmp/parity-'):
    raise SystemExit('REFUSED: socket is not in the runner-owned temporary state')
operation = sys.argv[1]
if operation == 'open':
    pane = os.environ.get('HERDR_PANE_ID') or os.environ.get('GTX_PANE_ID')
    if not pane:
        raise SystemExit('NOT_RUN: exact pane identity absent')
    method = 'pane.command.execute'
    params = {'pane_id': pane, 'action': 'popup',
              'command': "env PS1='POPUP> ' /bin/zsh -f", 'width': 60, 'height': 20}
elif operation == 'report-state':
    pane = os.environ.get('HERDR_PANE_ID') or os.environ.get('GTX_PANE_ID')
    session = os.environ.get('PARITY_CONTROLLER_SESSION_ID')
    if not pane or not session:
        raise SystemExit('NOT_RUN: pane identity or actual controller session absent')
    method = 'pane.report_agent'
    params = {'pane_id':pane, 'source':'codex', 'agent':'codex',
              'state':sys.argv[2], 'title':'PARITY', 'session_id':session}
elif operation == 'close':
    method, params = 'popup.close', {}
else:
    raise SystemExit('unsupported fixture operation')
with socket.socket(socket.AF_UNIX) as connection:
    connection.settimeout(5)
    connection.connect(endpoint)
    connection.sendall(json.dumps({'id': 'parity-popup', 'method': method, 'params': params}).encode()+b'\n')
    response = bytearray()
    while b'\n' not in response:
        chunk = connection.recv(65536)
        if not chunk:
            raise SystemExit('NOT_RUN: socket closed without reply')
        response.extend(chunk)
    result = json.loads(response.split(b'\n')[0])
    print(json.dumps(result, ensure_ascii=False))
    if 'error' in result:
        raise SystemExit(1)
