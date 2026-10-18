#!/usr/bin/env python3
"""Record graphics visibility through real split, focus and zoom changes."""
import argparse
import json
import os
from pathlib import Path
import socket
import tty

endpoint = os.environ.get('HERDR_SOCKET_PATH') or os.environ.get('GTX_SOCKET_PATH')
pane = os.environ.get('HERDR_PANE_ID') or os.environ.get('GTX_PANE_ID')
if not endpoint or not pane or not str(Path(endpoint).resolve()).startswith('/private/tmp/parity-'):
    raise SystemExit('REFUSED: not an observer-owned pane')
records = []
p = argparse.ArgumentParser()
p.add_argument('--images', action='store_true')
a = p.parse_args()


def request(method, params):
    with socket.socket(socket.AF_UNIX) as connection:
        connection.settimeout(5)
        connection.connect(endpoint)
        connection.sendall(json.dumps({'id': 'tb-visibility', 'method': method, 'params': params}).encode()+b'\n')
        response = bytearray()
        while b'\n' not in response:
            chunk = connection.recv(65536)
            if not chunk:
                raise RuntimeError('API closed without a reply')
            response.extend(chunk)
        reply = json.loads(response.split(b'\n')[0])
        records.append({'method': method, 'params': params, 'response': reply})
        if 'error' in reply:
            raise RuntimeError(reply)
        return reply['result']


def sample(phase, panes):
    for target in panes:
        if a.images:
            request('pane.graphics.set', {'pane_id': target, 'layer_id': 'visibility',
                    'format': 'rgba', 'image_width': 1, 'image_height': 1,
                    'data_base64': '/wAA/w==', 'placement': {'grid_cols': 2, 'grid_rows': 2}})
        request('pane.graphics.info', {'pane_id': target})
        records[-1]['phase'] = phase
        records[-1]['role'] = 'original' if target == pane else 'split'


tty.setraw(0)
os.write(1, b'\x1b[1;1HTB_VISIBILITY_READY')
assert os.read(0, 1) == b'v'
try:
    sample('single', [pane])
    created = request('pane.split', {'target_pane_id': pane, 'direction': 'right', 'focus': False})
    second = created['pane']['pane_id']
    sample('split-original-focused', [pane, second])
    request('pane.focus', {'pane_id': second})
    sample('split-new-focused', [pane, second])
    request('pane.zoom', {'pane_id': second, 'mode': 'on'})
    sample('zoom-new', [pane, second])
    request('pane.zoom', {'pane_id': second, 'mode': 'off'})
    request('pane.focus', {'pane_id': pane})
    sample('restored', [pane, second])
finally:
    (Path.home()/'tb-visibility.json').write_text(json.dumps(records, indent=2)+'\n')
os.write(1, b'\x1b[1;1HTB_VISIBILITY_RECORDED')
assert os.read(0, 1) == b'q'
