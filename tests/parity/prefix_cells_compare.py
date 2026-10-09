#!/usr/bin/env python3
"""Keep all independently generated prefix frames and their exact cell differences."""
import argparse
import json
from pathlib import Path

p = argparse.ArgumentParser()
p.add_argument('directory')
p.add_argument('steps_directory')
a = p.parse_args()
root = Path(a.directory)
results = []
for steps in sorted(Path(a.steps_directory).glob('*.steps')):
    for name in ('pending', 'result'):
        paths = [root / steps.stem / who / (name + '.json') for who in ('herdr', 'gtx')]
        row = {'case': steps.stem, 'screen': name}
        if not all(path.is_file() for path in paths):
            row['status'] = 'NOT_RUN'
        else:
            h, g = [json.loads(path.read_text()) for path in paths]
            differences = []
            if (h['Cols'], h['Rows']) != (g['Cols'], g['Rows']):
                row['status'] = 'DIFFERENT_SIZE'
            else:
                for y in range(h['Rows']):
                    for x in range(h['Cols']):
                        for field in ('Content', 'Width', 'Style'):
                            hv, gv = h['Cells'][y][x][field], g['Cells'][y][x][field]
                            if hv != gv:
                                differences.append({'x': x, 'y': y, 'field': field, 'herdr': hv, 'gtx': gv})
                row.update(status='PASS' if not differences else 'DIFFERENT_CELLS',
                           difference_count=len(differences), differences=differences)
        out = root / steps.stem / (name + '-full-comparison.json')
        out.parent.mkdir(parents=True, exist_ok=True)
        out.write_text(json.dumps(row, ensure_ascii=False, indent=2) + '\n')
        results.append({k: v for k, v in row.items() if k != 'differences'})
(root / 'prefix-full-comparison.json').write_text(json.dumps(results, ensure_ascii=False, indent=2) + '\n')
for row in results:
    print(row['case'], row['screen'], row['status'], row.get('difference_count', ''))
