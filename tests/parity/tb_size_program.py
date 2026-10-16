#!/usr/bin/env python3
"""Record real pane size replies before and after an actual PTY resize."""
import json
import os
from pathlib import Path
import select
import signal
import time
import tty

home = Path.home()
if not str(home.resolve()).startswith('/private/tmp/parity-') or home.name != 'home':
    raise SystemExit('REFUSED: not an observer-owned pane')
tty.setraw(0)
initial_dimensions = None


def query(phase):
    global initial_dimensions
    size = os.get_terminal_size()
    records = {'cols': size.columns, 'rows': size.lines, 'queries': []}
    for command in (14, 16, 18):
        response = bytearray()
        started = time.monotonic_ns()
        os.write(1, f'\x1b[{command}t'.encode())
        deadline = time.monotonic()+2
        while not response.endswith(b't'):
            remaining = deadline-time.monotonic()
            if remaining <= 0 or not select.select([0], [], [], remaining)[0]:
                break
            response.extend(os.read(0, 4096))
        records['queries'].append({'query': command, 'response_hex': response.hex(),
                                   'elapsed_ns': time.monotonic_ns()-started})
    if phase == 'initial':
        initial_dimensions = (records['cols'], records['rows'])
    (home/f'tb-size-{phase}.json').write_text(json.dumps(records, indent=2)+'\n')


def resized(*_):
    size = os.get_terminal_size()
    if initial_dimensions is None or (size.columns, size.lines) == initial_dimensions:
        return
    (home/'tb-size-winched.json').write_text(json.dumps({'cols': size.columns, 'rows': size.lines})+'\n')


signal.signal(signal.SIGWINCH, resized)
query('initial')
os.write(1, b'\x1b[1;1HTB_SIZE_INITIAL')
assert os.read(0, 1) == b's'
query('after-resize')
os.write(1, b'\x1b[1;1HTB_SIZE_AFTER_RESIZE')
assert os.read(0, 1) == b'q'
