#!/usr/bin/env python3
"""A25's saved unknown-exit case: real finished job, exit code removed from SQLite."""
import json
from pathlib import Path
import sys

root = Path(sys.argv[1])
proof = {}
for product in ('herdr', 'gtx'):
    cells = json.loads((root / product / 'unknown.json').read_text())['Cells']
    text = ''.join(cell['Content'] for cell in cells[26][:25])
    assert text.startswith(' done '), (product, text)
    status = cells[26][1]['Style']
    dots = [cell for cell in cells[5][:25] if cell['Content'] == '●']
    assert len(dots) == 1 and dots[0]['Style']['Fg'] == status['Fg'], (product, dots)
    proof[product] = {'status_style': status, 'workspace_dot_style': dots[0]['Style']}
assert proof['herdr'] == proof['gtx'], proof
proof['A25_unknown_exit'] = 'PASS'
print(json.dumps(proof, indent=2))
