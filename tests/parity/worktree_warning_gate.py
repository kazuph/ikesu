#!/usr/bin/env python3
"""Check the exact rejection and banner, keeping SPEC product-name differences."""
import json
from pathlib import Path
import sys

root = Path(sys.argv[1])
results = []
messages = {who: brand + ' worktree actions require a workspace inside a Git work tree.'
            for who, brand in (('herdr', 'Herdr'), ('ikesu', 'ikesu'))}
for name in sys.argv[2:] or ('rejected', 'input', 'escape', 'resize', 'settings', 'closed'):
    pair = {who: json.loads((root / who / (name + '.json')).read_text()) for who in messages}
    h, g = pair['herdr'], pair['ikesu']
    assert (h['Cols'], h['Rows']) == (g['Cols'], g['Rows']), name
    starts = {who: data['Cols'] - len(messages[who]) - 2 for who, data in pair.items()}
    reference = h['Cells'][0][-1]['Style']
    assert reference['Attrs'] == (3 if name == 'settings' else 1), (name, reference)
    for who, data in pair.items():
        row = data['Cells'][0][starts[who]:]
        assert ''.join(cell['Content'] for cell in row) == ' ' + messages[who] + ' ', (name, who)
        assert all(cell['Width'] == 1 and cell['Style'] == reference for cell in row), (name, who, 'banner style')
    differences = []
    for y in range(h['Rows']):
        for x in range(h['Cols']):
            if y == 0 and x >= min(starts.values()):
                continue  # Exact product-specific text and every banner cell checked above.
            for field in ('Content', 'Width', 'Style'):
                if h['Cells'][y][x][field] != g['Cells'][y][x][field]:
                    differences.append({'x': x, 'y': y, 'field': field})
    assert not differences, (name, differences)
    results.append({'screen': name, 'status': 'PASS_SPEC_PRODUCT_NAME', 'banner_starts': starts})
(root / 'worktree-warning-gate.json').write_text(json.dumps(results, indent=2) + '\n')
print('PASS: exact rejection, banner styles and all surrounding cells', len(results))
