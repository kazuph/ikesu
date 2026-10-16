#!/usr/bin/env python3
"""Receive real PTY input during the saved-machine switch measurement."""
import argparse
import json
import os
from pathlib import Path
import tty

p = argparse.ArgumentParser()
p.add_argument('--out', type=Path, required=True)
a = p.parse_args()
if not str(a.out.resolve()).startswith('/private/tmp/tb-saved-'):
    raise SystemExit('REFUSED: receiver output is outside the owned measurement')
tty.setraw(0)
os.write(1, b'\x1b[2J\x1b[1;1HLOCAL_SWITCH_READY')
received = bytearray()
a.out.write_text(json.dumps({'received_hex': ''})+'\n')
while b'\r' not in received and b'\n' not in received:
    chunk = os.read(0, 4096)
    if not chunk:
        raise SystemExit('input closed before the measurement terminator')
    received.extend(chunk)
    a.out.write_text(json.dumps({'received_hex': received.hex()}, indent=2)+'\n')
a.out.write_text(json.dumps({'received_hex': received.hex()}, indent=2)+'\n')
os.write(1, b'\x1b[2;1HLOCAL_KEYS_RECORDED')
os.read(0, 1)
