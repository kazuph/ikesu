#!/usr/bin/env python3
"""Producer for the isolated tb graphics comparison; never run in a user pane."""
import base64
import json
import os
from pathlib import Path
import select
import socket
import time
import tty

endpoint = os.environ.get('HERDR_SOCKET_PATH') or os.environ.get('GTX_SOCKET_PATH')
pane = os.environ.get('HERDR_PANE_ID') or os.environ.get('GTX_PANE_ID')
if not endpoint or not pane or not str(Path(endpoint).resolve()).startswith('/private/tmp/parity-'):
    raise SystemExit('REFUSED: not an observer-owned pane')

records = {'pane': pane, 'api': [], 'queries': []}
proof = Path.home() / 'tb-probe.json'
for method in ('pane.graphics.info', 'pane.graphics.set', 'pane.graphics.clear', 'pane.graphics.stream'):
    with socket.socket(socket.AF_UNIX) as connection:
        connection.settimeout(5)
        connection.connect(endpoint)
        connection.sendall(json.dumps({'id': 'tb-probe', 'method': method,
                                       'params': {'pane_id': pane}}).encode() + b'\n')
        response = bytearray()
        while b'\n' not in response:
            chunk = connection.recv(65536)
            if not chunk:
                raise RuntimeError('API closed without a reply')
            response.extend(chunk)
        records['api'].append({'method': method, 'response': json.loads(response.split(b'\n')[0])})

tty.setraw(0)
for query in (14, 16, 18):
    started = time.monotonic_ns()
    os.write(1, f'\x1b[{query}t'.encode())
    response = bytearray()
    deadline = time.monotonic() + 2
    while not response.endswith(b't'):
        remaining = deadline - time.monotonic()
        if remaining <= 0 or not select.select([0], [], [], remaining)[0]:
            break
        response.extend(os.read(0, 4096))
    records['queries'].append({'query': query, 'response_hex': response.hex(),
                              'elapsed_ns': time.monotonic_ns()-started})
proof.write_text(json.dumps(records, indent=2) + '\n')


def graphic(action, pixels):
    payload = base64.b64encode(pixels)
    os.write(1, f'\x1b_Ga={action},t=d,f=24,s=2,v=1,i=42,q=2'.encode()
             + (b',c=4,r=2,C=1' if action == 'T' else b'') + b';' + payload + b'\x1b\\')


os.write(1, b'\x1b[5;7H')
graphic('T', bytes([255, 0, 0, 0, 255, 0]))
os.write(1, b'\x1b[1;1HTB_PLACED')
while True:
    command = os.read(0, 1)
    if command == b'u':
        graphic('t', bytes([0, 0, 255, 0, 255, 0]))
        os.write(1, b'\x1b[1;1HTB_UPDATED')
    elif command == b'p':
        os.write(1, b'\x1b_Ga=p,i=42,c=4,r=2,C=1,q=2;\x1b\\\x1b[1;1HTB_REPLACED')
    elif command == b'q':
        break
