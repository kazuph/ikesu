#!/usr/bin/env python3
"""Prove exact endpoint rejection, retaining only the SPEC product-name exception."""
import json
from pathlib import Path
import sys

root = Path(sys.argv[1])
messages = {who: brand + ' worktree actions require a workspace inside a Git work tree'
            for who, brand in (('herdr', 'Herdr'), ('ikesu', 'ikesu'))}
results = []
for name in ('rejected', 'input', 'escape', 'resize', 'settings', 'closed'):
    frames = {who: json.loads((root/who/(name+'.json')).read_text()) for who in messages}
    h, g = frames['herdr'], frames['ikesu']
    assert (h['Cols'], h['Rows']) == (g['Cols'], g['Rows']), name
    positions, labels = {}, {}
    for who, frame in frames.items():
        line = ''.join(c['Content'] for c in frame['Cells'][-1])
        assert line.count('COPY') == line.count('CYCLE LAYOUT') == 1, (who, name)
        start = line.index('COPY') + len('COPY') + 3
        cycle = line.index('CYCLE LAYOUT') - 1
        # Captures leave two cells before the CYCLE button's leading blank.
        width = cycle-start-2
        assert start == 34 and width > 0, (who, name, start, width)
        label = messages[who]
        if len(label) > width:
            label = label[:width-1] + '…'
        assert line[start:cycle] == label.ljust(cycle-start), (who, name, line)
        positions[who] = (start, cycle)
        labels[who] = label
    assert positions['herdr'] == positions['ikesu'], name
    start, cycle = positions['herdr']
    reference = h['Cells'][-1][start]['Style']
    assert reference['Bg'] is None and reference['Underline'] == 0, reference
    assert reference['Attrs'] == (2 if name == 'settings' else 0), reference
    assert reference['Fg'] is not None, reference
    gap = h['Cells'][-1][cycle-1]['Style']
    for who, frame in frames.items():
        for x in range(start, cycle):
            cell = frame['Cells'][-1][x]
            expected = reference if x < start+len(labels[who]) else gap
            assert cell['Width'] == 1 and cell['Style'] == expected, (who, name, x, cell, expected)
    differences = []
    for y in range(h['Rows']):
        for x in range(h['Cols']):
            if y == h['Rows']-1 and start <= x < cycle:
                continue  # Every product-specific text and gap cell was proved above.
            for field in ('Content', 'Width', 'Style'):
                if h['Cells'][y][x][field] != g['Cells'][y][x][field]:
                    differences.append({'x': x, 'y': y, 'field': field})
    assert not differences, (name, differences)
    results.append({'screen': name, 'status': 'PASS_SPEC_PRODUCT_NAME',
                    'slot': [start, h['Rows']-1, cycle-start], 'labels': labels})
(root/'endpoint-worktree-warning-gate.json').write_text(json.dumps(results, indent=2)+'\n')
print('PASS: exact endpoint rejection, styles, buttons and every surrounding cell', len(results))
