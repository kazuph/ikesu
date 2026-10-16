#!/usr/bin/env python3
"""Observe file frame parsing on the isolated graphics stream, without reusing files."""
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


def line(connection):
    data = bytearray()
    while b'\n' not in data:
        chunk = connection.recv(65536)
        if not chunk:
            return {'closed': True, 'received_hex': data.hex()}
        data.extend(chunk)
    return json.loads(data.split(b'\n')[0])


for index, (format_name, layer, representation) in enumerate([
        ('rgba', None, 'path'), ('rgba', None, 'object'),
        ('bgra', None, 'path'), ('rgba', 'secondary', 'path')]):
    pixels = Path.home()/f'file-frame-{index}.rgba'
    pixels.write_bytes(b'\xff\0\0\xff\0\xff\0\xff')
    with socket.socket(socket.AF_UNIX) as connection:
        connection.settimeout(2)
        connection.connect(endpoint)
        connection.sendall(json.dumps({'id': 'file-probe', 'method': 'pane.graphics.stream',
                                       'params': {'pane_id': pane}}).encode()+b'\n')
        opened = line(connection)
        header = {'format': format_name, 'image_width': 2, 'image_height': 1,
                  'file': str(pixels) if representation == 'path' else {'path': str(pixels)},
                  'placement': {'viewport_col': 6, 'viewport_row': 4, 'grid_cols': 4, 'grid_rows': 2}}
        if layer is not None:
            header['layer_id'] = layer
        response = None
        if 'error' not in opened:
            connection.sendall(json.dumps(header).encode()+b'\n')
            try:
                response = line(connection)
            except socket.timeout:
                response = {'observation_timeout': True}
        records.append({'header': header, 'opened': opened, 'response': response,
                        'file_exists_after_observation': pixels.exists()})
(Path.home()/'tb-file-probe.json').write_text(json.dumps(records, indent=2)+'\n')
tty.setraw(0)
os.write(1, b'\x1b[1;1HTB_FILES_RECORDED')
os.read(0, 1)
