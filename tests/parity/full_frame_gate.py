#!/usr/bin/env python3
"""Require every cells capture in the identical procedure to match in full."""
import argparse
import json
from pathlib import Path
import re

p = argparse.ArgumentParser()
p.add_argument('directory', type=Path)
p.add_argument('steps', type=Path)
a = p.parse_args()
names = re.findall(r'^cells @OUT@/([^\s]+)\.json$', a.steps.read_text(), re.M)
assert names, 'procedure has no cell captures'
results = []
for name in names:
    paths = [a.directory / who / (name + '.json') for who in ('herdr', 'ikesu')]
    row = {'screen': name, 'differences': []}
    if not all(path.is_file() for path in paths):
        row['status'] = 'NOT_RUN'
    else:
        h, g = [json.loads(path.read_text()) for path in paths]
        if (h['Cols'], h['Rows']) != (g['Cols'], g['Rows']):
            row['status'] = 'DIFFERENT_SIZE'
        else:
            for y in range(h['Rows']):
                for x in range(h['Cols']):
                    for field in ('Content', 'Width', 'Style'):
                        hv, gv = h['Cells'][y][x][field], g['Cells'][y][x][field]
                        if hv != gv:
                            row['differences'].append({'x': x, 'y': y, 'field': field,
                                                       'herdr': hv, 'ikesu': gv})
            row['status'] = 'DIFFERENT_CELLS' if row['differences'] else 'PASS'
    row['difference_count'] = len(row['differences'])
    results.append(row)
    print(name, row['status'], row['difference_count'])
(a.directory / 'full-frame-gate.json').write_text(json.dumps(results, ensure_ascii=False, indent=2) + '\n')
raise SystemExit(0 if all(row['status'] == 'PASS' for row in results) else 1)
