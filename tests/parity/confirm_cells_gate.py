#!/usr/bin/env python3
"""Check confirmation geometry/styles; retain the SPEC product-name substitution."""
import argparse
import json
from pathlib import Path
import sys

from navigator_compare import panel

p = argparse.ArgumentParser()
p.add_argument('directory', type=Path)
p.add_argument('--navigation', action='store_true')
args = p.parse_args()
root = args.directory
# The title and meaning must remain exact; only the specified product name differs.
title = ' Stop server?'
details = {'herdr': ' Stops the Herdr server and all running panes.',
           'gtx': ' Stops the gtx server and all running panes.'}
results = []
names = ('open', 'letter', 'wheel', 'resize', 'escape', 'outside')
closed = {'escape', 'outside'}
if args.navigation:
    names = ('open', 'arrows', 'tab', 'inside', 'right-inside', 'cancelled', 'reopened', 'resize', 'escape')
    closed = {'inside', 'cancelled', 'escape'}
for name in names:
    pair = {runtime: json.loads((root/runtime/(name+'.json')).read_text())
            for runtime in ('herdr', 'gtx')}
    rects = {runtime: panel(data, title) for runtime, data in pair.items()}
    if name in closed:
        assert rects == {'herdr': None, 'gtx': None}, (name, rects)
        # The observed cancellation returns to workspace navigation.
        rows = {}
        for runtime, data in pair.items():
            row = data['Cells'][data['Rows']-2][26:]
            text = ''.join(cell['Content'] for cell in row)
            assert text.startswith(' NAVIGATE  esc back '), (name, runtime, text)
            rows[runtime] = row
        assert rows['herdr'] == rows['gtx'], (name, 'navigation row differs')
        results.append({'screen': name, 'status': 'PASS_CLOSED_NAVIGATE'})
        continue
    assert rects['herdr'] is not None and rects['herdr'] == rects['gtx'], (name, rects)
    x, y, w, h = rects['herdr']
    for runtime, data in pair.items():
        row = data['Cells'][y+2]
        expected = details[runtime].ljust(w-2)
        assert ''.join(c['Content'] for c in row[x+1:x+w-1]) == expected, (name, runtime)
        # Text is dim; unused detail cells keep only the popup background.
        for i, cell in enumerate(row[x+1:x+w-1]):
            reference = pair['herdr']['Cells'][y+2][x+1 if i < len(details[runtime]) else x+w-2]
            assert cell['Style'] == reference['Style'] and cell['Width'] == 1, (name, runtime, i)
    differences = []
    for yy in range(y, y+h):
        for xx in range(x, x+w):
            if yy == y+2 and x < xx < x+w-1:
                continue  # Checked against exact text/style above, never approximated.
            if pair['herdr']['Cells'][yy][xx] != pair['gtx']['Cells'][yy][xx]:
                differences.append((xx, yy))
    assert not differences, (name, differences)
    results.append({'screen': name, 'status': 'PASS_SPEC_NAME_SUBSTITUTION', 'rect': rects['herdr']})
(root/'confirm-gate.json').write_text(json.dumps(results, ensure_ascii=False, indent=2)+'\n')
for result in results:
    print(result['screen'], result['status'])
