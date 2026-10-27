#!/usr/bin/env python3
"""Observe real pane.read responses after real PTY window-size changes."""
import json
import os
from pathlib import Path
import signal
import socket
import sys
import time

endpoint = os.environ.get('HERDR_SOCKET_PATH') or os.environ.get('IKESU_SOCKET_PATH')
pane = os.environ.get('HERDR_PANE_ID') or os.environ.get('IKESU_PANE_ID')
if not endpoint or not pane or not str(Path(endpoint).resolve()).startswith('/private/tmp/parity-'):
    raise SystemExit('REFUSED: not a runner-owned pane/socket')

records = []
pending = False


def read(source, strip):
    with socket.socket(socket.AF_UNIX) as connection:
        connection.settimeout(5)
        connection.connect(endpoint)
        connection.sendall(json.dumps({'id': 'resize-observation', 'method': 'pane.read',
                                      'params': {'pane_id': pane, 'source': source,
                                                 'format': 'text',
                                                 'strip_ansi': strip}}).encode()+b'\n')
        response = bytearray()
        while b'\n' not in response:
            data = connection.recv(65536)
            if not data:
                raise RuntimeError('owned API closed without response')
            response.extend(data)
        result = json.loads(response.split(b'\n')[0])
        return result


def capture():
    size = os.get_terminal_size()
    records.append({'cols': size.columns, 'rows': size.lines,
                    'reads': [{'source': source, 'strip': strip, 'response': read(source, strip)}
                              for source in ('visible', 'recent', 'recent_unwrapped')
                              for strip in (True, False)]})
    (Path.home()/'read-resize-results.json').write_text(json.dumps(records, indent=2)+'\n')


def resized(signum, frame):
    global pending
    pending = True


signal.signal(signal.SIGWINCH, resized)
if sys.argv[1] == 'alternate':
    sys.stdout.write('\x1b[?1049h')
payload = ('日本語x'*50) if sys.argv[1] == 'main-wide' else 'A'+('0'*298)+'Z'
if sys.argv[1] == 'main-margin':
    payload = 'x'*(os.get_terminal_size().columns-1)+'界Z'
sys.stdout.write('\x1b[3J\x1b[H\x1b[2J\x1b[31m'+payload+'\x1b[0m\nREADY')
sys.stdout.flush()
# Use the same settling interval as the saved PTY comparison procedures.
time.sleep(.25)
capture()
while True:
    signal.pause()
    if pending:
        pending = False
        time.sleep(.25)
        capture()
