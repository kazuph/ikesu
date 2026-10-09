#!/usr/bin/env python3
"""Reuse Settings theme selection, then exercise workspace card states."""
from pathlib import Path
import subprocess
import sys

out = Path(sys.argv[1])
source = Path(__file__).resolve().parent
subprocess.run([sys.executable, str(source/'theme_cases.py'), str(out)], check=True)
body = (source/'sidebar-card-menu.steps').read_text().removesuffix('kill\n')
body += '''mouse down 5 8
mouse motion 5 5
screen @OUT@/drag-preview.txt
cells @OUT@/drag-preview.json
mouse up 5 5
sleep 250ms
screen @OUT@/reordered.txt
cells @OUT@/reordered.json
kill
'''
for file in out.glob('*.steps'):
    selection = file.read_text().split('send "\\x02g"', 1)[0]
    file.write_text(selection+body)
