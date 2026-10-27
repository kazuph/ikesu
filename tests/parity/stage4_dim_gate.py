#!/usr/bin/env python3
"""Check A24's faint attribute only; retain colors as evidence, not a color PASS."""
import json
import sys
from pathlib import Path

root = Path(sys.argv[1])
proof = {}
for product in ('herdr', 'ikesu'):
    cells = json.loads((root / product / 'custom.json').read_text())['Cells']
    styles = []
    for y in (26, 27):
        hits = [cell['Style'] for cell in cells[y][:25] if cell['Content'] == 'c']
        assert len(hits) == 1, (product, y, hits)
        assert bool(hits[0]['Attrs'] & 2) == (y == 26), (product, y, hits)
        styles.append(hits[0])
    proof[product] = styles
proof['A24_dim_true_then_false'] = 'PASS'
print(json.dumps(proof, indent=2))
