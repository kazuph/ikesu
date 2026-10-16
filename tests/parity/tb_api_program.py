#!/usr/bin/env python3
"""Exercise the published graphics API against an observer-owned real server."""
import base64
import json
import os
from pathlib import Path
import socket
import struct
import time
import tty
import zlib

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


def request(method, params):
    started = time.monotonic_ns()
    with socket.socket(socket.AF_UNIX) as connection:
        connection.settimeout(5)
        connection.connect(endpoint)
        connection.sendall(json.dumps({'id': 'tb-api', 'method': method, 'params': {'pane_id': pane, **params}}).encode()+b'\n')
        response = line(connection)
    records.append({'method': method, 'params': params, 'response': response, 'elapsed_ns': time.monotonic_ns()-started})
    return response


def chunk(name, data):
    return struct.pack('>I', len(data))+name+data+struct.pack('>I', zlib.crc32(name+data))


png = (b'\x89PNG\r\n\x1a\n'+chunk(b'IHDR', struct.pack('>IIBBBBB', 2, 1, 8, 6, 0, 0, 0))
       +chunk(b'IDAT', zlib.compress(b'\0\xff\0\0\xff\0\xff\0\xff'))+chunk(b'IEND', b''))
request('pane.graphics.info', {})
for format_name, data in [('rgb', b'\xff\0\0\0\xff\0'), ('rgba', b'\xff\0\0\xff\0\xff\0\xff'),
                          ('bgra', b'\0\0\xff\xff\0\xff\0\xff'), ('png', png)]:
    request('pane.graphics.set', {'format': format_name, 'image_width': 2, 'image_height': 1,
                                 'data_base64': base64.b64encode(data).decode(), 'layer_id': format_name,
                                 'placement': {'viewport_col': 6, 'viewport_row': 4, 'grid_cols': 4, 'grid_rows': 2},
                                 'z_index': 1})
    request('pane.graphics.clear', {'layer_id': format_name})
request('pane.graphics.clear', {})
for i in range(17):
    request('pane.graphics.set', {'format': 'rgb', 'image_width': 2, 'image_height': 1,
                                 'data_base64': '/wAAAP8A', 'layer_id': f'layer-{i}',
                                 'placement': {'grid_cols': 4, 'grid_rows': 2}})
for i in range(17):
    request('pane.graphics.clear', {'layer_id': f'layer-{i}'})
request('pane.graphics.clear', {})
with socket.socket(socket.AF_UNIX) as connection:
    connection.settimeout(2)
    connection.connect(endpoint)
    connection.sendall(json.dumps({'id': 'tb-stream', 'method': 'pane.graphics.stream',
                                   'params': {'pane_id': pane, 'layer_id': 'stream'}}).encode()+b'\n')
    response = line(connection)
    records.append({'method': 'pane.graphics.stream', 'response': response})
    if 'error' not in response:
        header = {'format': 'rgb', 'image_width': 2, 'image_height': 1,
                  'data_length': 6, 'layer_id': 'stream',
                  'placement': {'grid_cols': 4, 'grid_rows': 2}}
        connection.sendall(json.dumps(header).encode()+b'\n'+b'\xff\0\0\0\xff\0')
        try:
            response = line(connection)
        except socket.timeout:
            response = {'observation_timeout': True}
        records.append({'stream_header': header, 'response': response})
(Path.home()/'tb-api-probe.json').write_text(json.dumps(records, indent=2)+'\n')
tty.setraw(0)
os.write(1, b'\x1b[1;1HTB_API_RECORDED')
os.read(0, 1)
