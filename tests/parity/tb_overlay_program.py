#!/usr/bin/env python3
"""Keep actual image placements alive while the observer opens opaque UI."""
import base64
import argparse
import os
from pathlib import Path
import subprocess
import sys
import tty

parser = argparse.ArgumentParser()
parser.add_argument('--toast', action='store_true')
parser.add_argument('--small', action='store_true', help='one image completely covered by the context menu')
parser.add_argument('--source-crop', action='store_true', help='preserve producer crop and leading pixel offsets')
parser.add_argument('--notification', action='store_true', help='use real background-pane status transitions for an endpoint toast')
args = parser.parse_args()
endpoint = os.environ.get('HERDR_SOCKET_PATH') or os.environ.get('GTX_SOCKET_PATH')
owned_home = str(Path.home().resolve()).startswith('/private/tmp/parity-') and Path.home().name == 'home'
if not owned_home or endpoint and not str(Path(endpoint).resolve()).startswith('/private/tmp/parity-'):
    raise SystemExit('REFUSED: not an observer-owned pane')

def background_action(*arguments):
    with (Path.home()/'overlay-notification-actions.log').open('a') as log:
        subprocess.run([sys.executable, str(Path(__file__).with_name('socket_action.py')), *arguments],
                       stdout=log, stderr=subprocess.STDOUT, check=True)

if args.notification:
    if not endpoint:
        raise SystemExit('REFUSED: notification comparison requires an isolated endpoint server')
    background_action('create-other')
    background_action('report-other-state', 'working')
tty.setraw(0)
placements = [(51, 5, 7, 40, 20), (52, 31, 101, 8, 4)]
if args.toast:
    placements = [(51, 1, 101, 30, 10), (52, 20, 7, 8, 4)]
if args.small:
    placements = [(51, 5, 7, 2, 1)]
for ident, row, col, cols, rows in placements:
    pixels = b''.join(bytes((x*41 % 256, y*61 % 256, (x+y)*31 % 256)) for y in range(4) for x in range(8))
    crop = ',x=2,y=1,w=4,h=2,X=3,Y=5' if args.source_crop else ''
    os.write(1, f'\x1b[{row};{col}H\x1b_Ga=T,t=d,f=24,s=8,v=4,i={ident},c={cols},r={rows},C=1,q=2{crop};'.encode()
             + base64.b64encode(pixels)+b'\x1b\\')
os.write(1, b'\x1b[1;1HTB_OVERLAY_READY')
while True:
    key = os.read(0, 1)
    if key == b'q':
        break
    if key == b'c':
        os.write(1, b'\x1b[1;1HTB_OVERLAY_CLOSED')
    if key == b'n' and args.notification:
        background_action('report-other-state', 'idle')
