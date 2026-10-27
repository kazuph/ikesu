#!/usr/bin/env python3
"""Compare actual size-query bytes, separately checking known pane geometry."""
import argparse
import json
from pathlib import Path

p = argparse.ArgumentParser()
p.add_argument('out', type=Path)
p.add_argument('--cell-width', type=int, default=0)
p.add_argument('--cell-height', type=int, default=0)
a = p.parse_args()
results = {}
for product in ('herdr', 'ikesu'):
    saved = json.loads((a.out/product/'runtime-json.json').read_text())
    phases = {}
    for phase in ('initial', 'after-resize'):
        record = json.loads(saved[f'home/tb-size-{phase}.json'])
        replies = {item['query']: item['response_hex'] for item in record['queries']}
        expected = {18: f'\x1b[8;{record["rows"]};{record["cols"]}t'.encode().hex()}
        if a.cell_width > 0 and a.cell_height > 0:
            expected[14] = f'\x1b[4;{record["rows"]*a.cell_height};{record["cols"]*a.cell_width}t'.encode().hex()
            expected[16] = f'\x1b[6;{a.cell_height};{a.cell_width}t'.encode().hex()
        phases[phase] = {**record, 'known_geometry_matches': all(replies[key] == value for key, value in expected.items())}
    if (phases['initial']['cols'], phases['initial']['rows']) == (phases['after-resize']['cols'], phases['after-resize']['rows']):
        raise SystemExit(f'{product}: the actual pane was not resized')
    results[product] = phases
same = all({r['query']: r['response_hex'] for r in results['herdr'][phase]['queries']}
           == {r['query']: r['response_hex'] for r in results['ikesu'][phase]['queries']}
           for phase in ('initial', 'after-resize'))
report = {'same_replies': same, 'products': results}
(a.out/'size-gate.json').write_text(json.dumps(report, indent=2)+'\n')
if not same or not all(record['known_geometry_matches'] for phases in results.values() for record in phases.values()):
    raise SystemExit('FAIL: size-query replies differ or do not match actual pane geometry')
print('PASS: exact reference replies before and after the real pane resize')
