#!/usr/bin/env python3
"""Record the real popup PTY dimensions without shell prompt redraw."""
import json
import os
from pathlib import Path
import sys
import time
size=os.get_terminal_size(0)
Path(sys.argv[1]).write_text(json.dumps({'cols':size.columns,'rows':size.lines})+'\n')
os.write(1,b'ABCDEFGHIJ')
while True:
    os.read(0,4096)
