#!/usr/bin/env python3
"""Record actual application-side mouse reports for tb input parity."""
import json
import os
from pathlib import Path
import tty

endpoint = os.environ.get('HERDR_SOCKET_PATH') or os.environ.get('GTX_SOCKET_PATH')
if not endpoint or not str(Path(endpoint).resolve()).startswith('/private/tmp/parity-'):
    raise SystemExit('REFUSED: not an observer-owned pane')
tty.setraw(0)
os.write(1, b'\x1b[?1003h\x1b[?1006h')
records, pending = [], b''
for index, name in enumerate(['cell-hover', 'cell-left', 'cell-right', 'pixel-hover',
                              'pixel-drag-release-hover', 'pixel-left', 'pixel-right']):
    if index == 3:
        os.write(1, b'\x1b[?1016h')
    os.write(1, f'\x1b[1;1HTB_MOUSE_READY_{index}'.encode())
    while b'~' not in pending:
        chunk = os.read(0, 4096)
        if not chunk:
            raise SystemExit('application input closed')
        pending += chunk
    data, _, pending = pending.partition(b'~')
    records.append({'stage': name, 'received_hex': data.hex()})
(Path.home()/'tb-mouse-probe.json').write_text(json.dumps(records, indent=2)+'\n')
os.write(1, b'\x1b[?1016l\x1b[?1003l\x1b[?1006l\x1b[1;1HTB_MOUSE_RECORDED')
os.read(0, 1)
