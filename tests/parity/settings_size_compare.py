#!/usr/bin/env python3
"""Compare the measured Settings rectangle, including boxes without a title."""
import argparse
import json
from pathlib import Path

p = argparse.ArgumentParser()
p.add_argument('directory')
a = p.parse_args()
root = Path(a.directory)
results = []
for hp in sorted((root/'herdr').glob('*.json')):
    h = json.loads(hp.read_text())
    if 'Cells' not in h:
        continue
    gp = root/'gtx'/hp.name
    if not gp.exists():
        results.append({'screen': hp.name, 'status': 'NOT_RUN'})
        continue
    g = json.loads(gp.read_text())
    if (h['Cols'], h['Rows']) != (g['Cols'], g['Rows']):
        results.append({'screen': hp.name, 'status': 'DIFFERENT_VIEWPORT'})
        continue
    # settings-size.steps and settings-height-sections.steps measured these margins.
    w, height = min(76, h['Cols']-4), min(22, h['Rows']-2)
    x, y = (h['Cols']-w)//2, (h['Rows']-height)//2
    corners = [(x, y, '┌'), (x+w-1, y, '┐'),
               (x, y+height-1, '└'), (x+w-1, y+height-1, '┘')]
    if not all(d['Cells'][yy][xx]['Content'] == char
               for d in (h, g) for xx, yy, char in corners):
        results.append({'screen': hp.name, 'rect': [x, y, w, height],
                        'status': 'MISSING_EXPECTED_BOX'})
        continue
    differences = []
    for yy in range(y, y+height):
        for xx in range(x, x+w):
            for field in ('Content', 'Width', 'Style'):
                hc, gc = h['Cells'][yy][xx][field], g['Cells'][yy][xx][field]
                if hc != gc:
                    differences.append({'x': xx, 'y': yy, 'field': field,
                                        'herdr': hc, 'gtx': gc})
    results.append({'screen': hp.name, 'rect': [x, y, w, height],
                    'status': 'PASS' if not differences else 'DIFFERENT_CELLS',
                    'difference_count': len(differences), 'differences': differences})
(root/'settings-size-comparison.json').write_text(json.dumps(results, ensure_ascii=False, indent=2)+'\n')
for result in results:
    print(result['screen'], result['status'], result.get('difference_count', ''))
