#!/usr/bin/env python3
"""A real raw-mode pane: requests modes and records input across live handoff."""
import os
from pathlib import Path
import select
import sys
import tty

control, capture, pids = map(Path, sys.argv[1:])
tty.setraw(0)
requests = {'start': '\x1b[>3u\x1b[>31u\x1b[>4;2m',
            'pop1': '\x1b[<u', 'pop2': '\x1b[<u', 'off': '\x1b[>4;0m'}
last = None
while True:
    state = control.read_text().strip() if control.exists() else 'start'
    if state != last:
        with pids.open('a') as stream:
            stream.write(str(os.getpid())+'\n')
        os.write(1, (requests[state]+'\r\nHANDOFF_KEY_'+state+'\r\n').encode())
        last = state
    if select.select([0], [], [], .01)[0]:
        data = os.read(0, 65536)
        if not data:
            break
        with capture.open('ab') as stream:
            stream.write(data)
