#!/usr/bin/env python3
"""Generate real image updates while recording any input leaked from the host."""
import base64
import json
import os
from pathlib import Path
import select
import time
import tty

endpoint = os.environ.get('HERDR_SOCKET_PATH') or os.environ.get('IKESU_SOCKET_PATH')
owned_home = str(Path.home().resolve()).startswith('/private/tmp/parity-') and Path.home().name == 'home'
if not owned_home or endpoint and not str(Path(endpoint).resolve()).startswith('/private/tmp/parity-'):
    raise SystemExit('REFUSED: not an observer-owned pane')
tty.setraw(0)


def draw(index):
    pixels = bytes([index, 0, 255, 0, 255, 0])
    os.write(1, b'\x1b[5;7H\x1b_Ga=T,t=d,f=24,s=2,v=1,i=42,c=4,r=2,C=1,q=2;'
             + base64.b64encode(pixels)+b'\x1b\\')


draw(0)
os.write(1, b'\x1b[1;1HTB_STATUS_READY')
assert os.read(0, 1) == b'u'
started = time.monotonic()
received = bytearray()
for index in range(1, 11):
    deadline = started+index*.02
    while time.monotonic() < deadline:
        if select.select([0], [], [], max(0, deadline-time.monotonic()))[0]:
            received.extend(os.read(0, 4096))
    draw(index)
os.write(1, b'\x1b[1;1HTB_STATUS_FINISHED')
while b'q' not in received:
    received.extend(os.read(0, 4096))
(Path.home()/'tb-status.json').write_text(json.dumps({'received_hex': received.hex(), 'final_pixel_hex': bytes([10, 0, 255, 0, 255, 0]).hex()})+'\n')
os.write(1, b'\x1b[2;1HTB_STATUS_RECORDED')
os.read(0, 1)
