#!/usr/bin/env python3
"""Produce identical real scrollback and report the shell's terminal dimensions."""
import json
import os
from pathlib import Path
size = os.get_terminal_size()
pane = os.environ.get('HERDR_PANE_ID') or os.environ.get('IKESU_PANE_ID')
(Path.home()/('stage5-size-'+pane+'.json')).write_text(json.dumps({'columns':size.columns,'rows':size.lines})+'\n')
print('\x1b[3J\x1b[2J\x1b[H',end='')
print('\n'.join('LINE%03d'%i for i in range(100)))
