#!/usr/bin/env python3
"""Controlled large-image changes on the observer's actual saved-machine PTY."""
import base64
import hashlib
import json
import os
from pathlib import Path
import random
import sys
import tty

home = Path.home()
if not str(home).startswith('/Users/kazuph/bench-terrace.noindex/work/tb-optimize/r-') or home.name != 'home':
    raise SystemExit('REFUSED: not an observer-owned saved machine')
tty.setraw(0)
width, height = 1024, 256
pixels = bytearray(b''.join(bytes((x % 256, y % 256, (x+y) % 256, 255))
                           for y in range(height) for x in range(width)))
records = []


def emit(data):
    remaining = memoryview(data)
    while remaining:
        remaining = remaining[os.write(1, remaining):]


def display(label, first=False):
    encoded = base64.b64encode(pixels)
    emit(b'\x1b[5;7H')
    for start in range(0, len(encoded), 4096):
        more = int(start+4096 < len(encoded))
        header = (f'Ga={"T" if first else "t"},t=d,f=32,s={width},v={height},i=42,q=2,'
                  + ('c=80,r=20,C=1,' if first else '')) if start == 0 else 'G'
        emit(b'\x1b_'+header.encode()+f'm={more};'.encode()+encoded[start:start+4096]+b'\x1b\\')
    records.append({'stage': label, 'width': width, 'height': height, 'bytes': len(pixels),
                    'sha256': hashlib.sha256(pixels).hexdigest()})
    (home/'image-probe.json').write_text(json.dumps(records, indent=2)+'\n')
    emit(b'\x1b[1;1HTB_TRANSFER_'+label.encode())


display('INITIAL', first=True)
assert os.read(0, 1) == b's'
for y in range(10, 14):
    for x in range(10, 14):
        pixels[(y*width+x)*4+1] ^= 255
display('SMALL')
assert os.read(0, 1) == b'l'
for position in range(len(pixels)):
    if position % 4 != 3:
        pixels[position] ^= 255
display('LARGE')
assert os.read(0, 1) == b'n'
pixels = bytearray(random.Random(7).randbytes(len(pixels)))
display('NOISE')
os.read(0, 1)
