#!/usr/bin/env python3
"""A20: compare focused-card colors before/after an actual click in split panes."""
import json
from pathlib import Path
import sys

root = Path(sys.argv[1])
proof = {}
for case in ('focus-agents', 'custom-focus'):
    x = 3 if case == 'focus-agents' else 1
    snapshots = {}
    for name in ('first', 'second'):
        styles = []
        for product in ('herdr', 'gtx'):
            rows = json.loads((root / case / product / (name + '.json')).read_text())['Cells']
            styles.append([rows[y][x]['Style'] for y in (26, 28, 30)])
        assert styles[0] == styles[1], (case, name, styles)
        snapshots[name] = styles[0]
    assert snapshots['first'][0] == snapshots['second'][0]
    assert snapshots['first'][1] == snapshots['second'][2]
    assert snapshots['first'][2] == snapshots['second'][1]
    assert snapshots['first'][1] != snapshots['first'][2]
    proof[case] = snapshots
proof['A20'] = 'PASS'
print(json.dumps(proof, indent=2))
