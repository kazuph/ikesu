"""Run a real terminal application with optional drag mouse reporting."""
import json
import os
from pathlib import Path
import sys
import termios
import tty

mode = sys.argv[1]
assert mode in ('on', 'off')
path = Path.home()/('mouse-report-'+mode+'.json')
fd = sys.stdin.fileno()
saved = termios.tcgetattr(fd)
try:
    tty.setraw(fd)
    if mode == 'on':
        os.write(1, b'\x1b[?1002h\x1b[?1006h')
    print('MOUSE_CAPTURE_'+mode.upper(), flush=True)
    path.write_text('[]')
    chunks = []
    while True:
        data = os.read(fd, 4096)
        chunks.append(data.hex())
        path.write_text(json.dumps(chunks))
        print(repr(data), flush=True)
        if b'q' in data:
            break
finally:
    os.write(1, b'\x1b[?1002l\x1b[?1006l')
    termios.tcsetattr(fd, termios.TCSANOW, saved)
