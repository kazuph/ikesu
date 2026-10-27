#!/usr/bin/env python3
"""Compare the complete agent location row at normal and wide sidebar widths."""
import json
from pathlib import Path
import sys

root = Path(sys.argv[1])
proof = {}
for name in ('unnamed', 'named', 'long', 'wide'):
    width = 71 if name == 'wide' else 25
    rows = [(root / product / (name + '.txt')).read_text().splitlines()[26][:width]
            for product in ('herdr', 'ikesu')]
    assert rows[0] == rows[1], (name, rows)
    if name == 'unnamed':
        assert ' · ' not in rows[0]
    elif name == 'named':
        assert 'NAMED_TAB' in rows[0]
    else:
        assert 'WORKSPACE' in rows[0] and 'TAB_LONG' in rows[0]
    proof[name] = rows[0]
proof['A22'] = proof['A23'] = 'PASS'
print(json.dumps(proof, ensure_ascii=False, indent=2))
