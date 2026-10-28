#!/usr/bin/env python3
"""Check local divider edge captures against the observed header positions."""
import argparse
import json
from pathlib import Path

p = argparse.ArgumentParser()
p.add_argument('directory', type=Path)
p.add_argument('--edge', choices=('top', 'bottom'), required=True)
a = p.parse_args()
expected = {
    'before': (45, 24),
    'edge': (45, 8 if a.edge == 'top' else 42),
    'height-25': (25, 8 if a.edge == 'top' else 23),
    'height-35': (35, 8 if a.edge == 'top' else 33),
    'height-90': (90, 10 if a.edge == 'top' else 82),
    'height-120': (120, 13 if a.edge == 'top' else 109),
}
results = []
for screen, (height, header_y) in expected.items():
    row = {'screen': screen, 'expected_header': header_y}
    for runtime in ('herdr', 'ikesu'):
        frame = json.loads((a.directory/runtime/(screen+'.json')).read_text())
        assert frame['Rows'] == height and frame['Cols'] == 160, (runtime, screen)
        matches = [y for y, cells in enumerate(frame['Cells'])
                   if '[agents]' in ''.join(c['Content'] for c in cells[:25])]
        row[runtime] = matches
    row['status'] = 'PASS' if row['herdr'] == row['ikesu'] == [header_y] else 'FAIL'
    results.append(row)
(a.directory/'panel-position-proof.json').write_text(json.dumps(results, indent=2)+'\n')
assert all(row['status'] == 'PASS' for row in results), results
print(f'{a.edge}: {len(results)} header positions PASS')
