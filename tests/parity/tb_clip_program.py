#!/usr/bin/env python3
"""Place a real Kitty image across pane boundaries in an owned observation."""
import base64
import json
import os
from pathlib import Path
import tty

endpoint = os.environ.get('HERDR_SOCKET_PATH') or os.environ.get('GTX_SOCKET_PATH')
if not endpoint or not str(Path(endpoint).resolve()).startswith('/private/tmp/parity-'):
    raise SystemExit('REFUSED: not an observer-owned pane')
size = os.get_terminal_size()
tty.setraw(0)
# Nonuniform pixels make source cropping distinguishable from scaling.
pixels = b''.join(bytes((x*41 % 256, y*61 % 256, (x+y)*31 % 256)) for y in range(4) for x in range(8))
(Path.home()/'tb-clip-input.json').write_text(json.dumps({'cols': size.columns, 'rows': size.lines,
                                                       'width': 8, 'height': 4, 'pixels_hex': pixels.hex()}))

def place(row, col, name):
    os.write(1, b'\x1b_Ga=d,d=A,q=2;\x1b\\')
    os.write(1, f'\x1b[{row};{col}H\x1b_Ga=T,t=d,f=24,s=8,v=4,i=91,c=8,r=4,C=1,q=2;'.encode()
             + base64.b64encode(pixels)+b'\x1b\\')
    os.write(1, b'\x1b[1;1H'+name.encode())

place(5, size.columns-3, 'TB_CLIP_RIGHT')
for action, row, col, name in [('b', size.lines-1, 7, 'TB_CLIP_BOTTOM'),
                              ('i', 5, 7, 'TB_CLIP_INSIDE')]:
    if os.read(0, 1) != action.encode():
        raise SystemExit('unexpected observation input')
    place(row, col, name)
os.read(0, 1)
