#!/usr/bin/env python3
"""Require measured small Settings frames, retaining the gtx product name."""
import json
from pathlib import Path
import sys

work = Path(sys.argv[1])
sizes = {'w100-h25.json', 'w160-h45.json', 'w20-h25.json', 'w40-h25.json',
         'w60-h15.json', 'w60-h25.json', 'w60-h9.json', 'w64-h25.json',
         'w76-h25.json', 'w79-h25.json', 'w80-h25.json'}
sections = {f's{s}-h{h}.json' for s in range(3) for h in range(6, 24)}
for scenario, expected in [('settings-size', sizes), ('settings-height-sections', sections)]:
    results = json.loads((work/f'parity-{scenario}-size-fixed/settings-size-comparison.json').read_text())
    assert {r['screen'] for r in results} == expected, (scenario, 'missing frames')
    for r in results:
        if r['status'] == 'PASS' and r['difference_count'] == 0:
            continue
        # Only these saved toasts frames show the project-specific product name.
        assert r['screen'] in {f's2-h{h}.json' for h in range(17, 24)}, r
        assert r['status'] == 'DIFFERENT_CELLS' and r['difference_count'] == 5, r
        assert [(d['x'], d['y'], d['field'], d['herdr'], d['gtx'])
                for d in r['differences']] == [
            (32+i, 11, 'Content', h, g) for i, (h, g) in enumerate(zip('herdr', 'gtx  '))], r
print('PASS: all 65 Settings frames; only the exact documented product-name cells differ')
