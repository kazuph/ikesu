#!/usr/bin/env python3
"""Change a real pane's keyboard requests without relying on its input encoding."""
import os
import time

for label, sequence in [
    ('all', '\x1b[>8u'), ('pop', '\x1b[<u'),
    ('events', '\x1b[>3u'), ('events-mok', '\x1b[>4;1m'),
    ('mok-off', '\x1b[>4;0m'), ('restore', '\x1b[<u'),
]:
    os.write(1, (sequence + '\r\nMODE_' + label + '\r\n').encode())
    time.sleep(.3)
os.write(1, b'LIFECYCLE_DONE\r\n')
