#!/usr/bin/env python3
"""Record the bytes delivered to a real raw-mode pane, without clipboard IO."""
import json
import os
import sys
import termios
import time
import tty

destination, modes = sys.argv[1:3]
saved = termios.tcgetattr(0)
try:
    tty.setraw(0)
    with open(destination, 'w', buffering=1) as output:
        os.write(1, bytes.fromhex(modes))
        if len(sys.argv) > 3:
            for line in range(int(sys.argv[3])):
                os.write(1, f'PROBE_LINE_{line:04d}\r\n'.encode())
        os.write(1, b'PROBE_READY')
        while True:
            data = os.read(0, 4096)
            if not data:
                break
            output.write(json.dumps({'ns': time.monotonic_ns(), 'unix_ns': time.time_ns(), 'hex': data.hex()}) + '\n')
finally:
    termios.tcsetattr(0, termios.TCSANOW, saved)
