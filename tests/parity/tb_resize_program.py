#!/usr/bin/env python3
"""Keep a real Kitty placement alive while the owned pane is resized."""
import base64
import os
from pathlib import Path
import signal
import tty

endpoint = os.environ.get('HERDR_SOCKET_PATH') or os.environ.get('IKESU_SOCKET_PATH')
if not endpoint or not str(Path(endpoint).resolve()).startswith('/private/tmp/parity-'):
    raise SystemExit('REFUSED: not an observer-owned pane')
tty.setraw(0)


def resized(signum, frame):
    os.write(1, b'\x1b[1;1HTB_RESIZED')


signal.signal(signal.SIGWINCH, resized)
pixels = bytes([255, 0, 0, 0, 255, 0])
os.write(1, b'\x1b[5;7H\x1b_Ga=T,t=d,f=24,s=2,v=1,i=42,c=4,r=2,C=1,q=2;'
         + base64.b64encode(pixels)+b'\x1b\\\x1b[1;1HTB_RESIZE_READY')
os.read(0, 1)
