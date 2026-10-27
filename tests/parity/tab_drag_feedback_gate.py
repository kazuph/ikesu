#!/usr/bin/env python3
"""Compare drag deltas, retaining existing tab label/status glyph differences."""
import json
import re
import sys
from pathlib import Path

reference, candidate = map(Path, sys.argv[1:3])
trailing = '--trailing-marker' in sys.argv[3:]


def cells(root, name):
    return json.loads((root / (name + '.json')).read_text())['Cells'][0]


def markers(row):
    return [i for i, cell in enumerate(row) if i >= 26 and cell['Content'] == '│']


names = [f'move-{x}' for x in range(29, 50)] + ['outside', 'returned', 'reverse', 'cancelled']
base_ref, base_ikesu = cells(reference, 'before'), cells(candidate, 'before')
report = []
for name in names:
    ref, ikesu = cells(reference, name), cells(candidate, name)
    want = markers(ref)
    # The parent's explicit improvement over herdr's missing trailing marker.
    if trailing and '--server' not in sys.argv[3:] and name == 'move-49':
        want = [51]
    assert markers(ikesu) == want, (name, markers(ikesu), want)
    for x in want:
        if markers(ref) == want:
            assert ikesu[x] == ref[x], (name, x, 'marker cell mismatch')
    # Only the indicator changes the strip before release; no live reorder or
    # invented grabbed-tab styling. Existing status dots remain untouched.
    if name not in ('reverse', 'cancelled'):
        for x in range(26, len(ikesu)):
            if x not in want:
                assert ikesu[x] == base_ikesu[x], (name, x, 'unexpected tab change')
    report.append({'frame': name, 'marker_columns_zero_based': want, 'status': 'PASS'})
for root in (reference, candidate):
    text = (root / 'dropped.txt').read_text().splitlines()[0][26:]
    assert re.findall('[ABC]', text) == ['B', 'A', 'C'], (root, text)
    before, after = cells(root, 'before'), cells(root, 'dropped')
    # C was active before and stays active; its chip remains accent-filled.
    assert before[46]['Style'] == after[46]['Style'], (root, 'focus changed')
    if '--server' in sys.argv[3:]:
        released = (root / 'cancelled.txt').read_text().splitlines()[0][26:]
        assert re.findall('[ABC]', released) == ['A', 'B', 'C'], (root, 'horizontal target not retained', released)
        assert cells(root, 'cancelled')[46]['Style'] == before[46]['Style'], (root, 'outside release changed focus')
    else:
        assert cells(root, 'cancelled') == after, (root, 'standalone outside motion changed tabs')
print(json.dumps(report, ensure_ascii=False, indent=2))
