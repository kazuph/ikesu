#!/usr/bin/env python3
"""Compare cell styles at a shared visible label; never normalize mismatches."""
import argparse
import json
from pathlib import Path

p = argparse.ArgumentParser()
p.add_argument('directory')
p.add_argument('screen')
p.add_argument('label')
a = p.parse_args()
results = {}
for app in ('herdr', 'ikesu'):
    path = Path(a.directory) / app / (a.screen + '.json')
    if not path.exists():
        results[app] = {'status': 'NOT_RUN'}
        continue
    data = json.loads(path.read_text())
    hits = []
    for y, row in enumerate(data['Cells']):
        text = ''.join(c.get('Content', '') for c in row)
        at = text.find(a.label)
        if at < 0:
            continue
        # Character offsets differ from columns for wide cells; count contents.
        offset = 0
        for x, cell in enumerate(row):
            if offset == at and cell.get('Content'):
                hits.append({'x': x, 'y': y, 'style': cell['Style']})
                break
            offset += len(cell.get('Content', ''))
    results[app] = {'status': 'OBSERVED' if len(hits) == 1 else 'NOT_UNIQUE', 'hits': hits}
results['equal_style'] = (all(results[app]['status'] == 'OBSERVED' for app in ('herdr', 'ikesu'))
                          and results['herdr']['hits'][0]['style'] == results['ikesu']['hits'][0]['style'])
print(json.dumps(results, ensure_ascii=False, indent=2))
raise SystemExit(0 if results['equal_style'] else 1)
