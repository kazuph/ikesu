#!/usr/bin/env python3
"""Record actual shell-PTY input while modal paste is exercised."""
import json
import os
import termios
import tty
from pathlib import Path

fd = 0
previous = termios.tcgetattr(fd)
record = Path.home()/'stage6b-paste.json'
received = []
record.write_text(json.dumps(received))
try:
    tty.setraw(fd)
    print('STAGE6B_RECORDER_READY alpha beta', flush=True)
    while True:
        data = os.read(fd, 65536)
        if not data:
            break
        received.append(data.hex())
        record.write_text(json.dumps(received))
finally:
    termios.tcsetattr(fd, termios.TCSANOW, previous)
