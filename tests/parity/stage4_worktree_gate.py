#!/usr/bin/env python3
"""A16: collapsing a real worktree group preserves its active child only."""
import json
from pathlib import Path
import sys

root = Path(sys.argv[1])
proof = {}
for product in ('herdr', 'gtx'):
    rows = {}
    for name in ('child-active', 'collapsed'):
        lines = (root / product / (name + '.txt')).read_text().splitlines()
        sidebar = [line[:25] for line in lines]
        active = [i for i, line in enumerate(sidebar) if 'CHILD_ONE' in line]
        siblings = [i for i, line in enumerate(sidebar) if 'CHILD_TWO' in line]
        assert len(active) == 1 and sidebar[active[0]].startswith('▌'), (product, name, sidebar)
        assert len(siblings) == (1 if name == 'child-active' else 0), (product, name, siblings)
        rows[name] = {'active_child_y': active[0], 'sibling_count': len(siblings)}
    assert rows['child-active']['active_child_y'] == rows['collapsed']['active_child_y']
    proof[product] = rows
proof['A16_active_child'] = 'PASS'
print(json.dumps(proof, indent=2))
