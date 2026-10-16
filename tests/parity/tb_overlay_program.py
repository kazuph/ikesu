#!/usr/bin/env python3
"""Keep actual image placements alive while the observer opens opaque UI."""
import base64
import argparse
import os
from pathlib import Path
import tty

parser = argparse.ArgumentParser()
parser.add_argument('--toast', action='store_true')
args = parser.parse_args()
endpoint = os.environ.get('HERDR_SOCKET_PATH') or os.environ.get('GTX_SOCKET_PATH')
owned_home = str(Path.home().resolve()).startswith('/private/tmp/parity-') and Path.home().name == 'home'
if not owned_home or endpoint and not str(Path(endpoint).resolve()).startswith('/private/tmp/parity-'):
    raise SystemExit('REFUSED: not an observer-owned pane')
tty.setraw(0)
placements = [(51, 5, 7, 40, 20), (52, 31, 101, 8, 4)]
if args.toast:
    placements = [(51, 1, 101, 30, 10), (52, 20, 7, 8, 4)]
for ident, row, col, cols, rows in placements:
    pixels = b''.join(bytes((x*41 % 256, y*61 % 256, (x+y)*31 % 256)) for y in range(4) for x in range(8))
    os.write(1, f'\x1b[{row};{col}H\x1b_Ga=T,t=d,f=24,s=8,v=4,i={ident},c={cols},r={rows},C=1,q=2;'.encode()
             + base64.b64encode(pixels)+b'\x1b\\')
os.write(1, b'\x1b[1;1HTB_OVERLAY_READY')
while True:
    key = os.read(0, 1)
    if key == b'q':
        break
    if key == b'c':
        os.write(1, b'\x1b[1;1HTB_OVERLAY_CLOSED')
