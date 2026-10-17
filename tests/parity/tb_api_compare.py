#!/usr/bin/env python3
"""Compare every captured graphics API response; missing capability is a failure."""
import argparse
import json
from pathlib import Path

p = argparse.ArgumentParser()
p.add_argument('out', type=Path)
a = p.parse_args()
records = {}
for product in ('herdr', 'gtx'):
    runtime = json.loads((a.out/product/'runtime-json.json').read_text())
    records[product] = json.loads(runtime['home/tb-api-probe.json'])
differences = []
for index in range(max(len(rows) for rows in records.values())):
    pair = {}
    for product, rows in records.items():
        pair[product] = ({k: v for k, v in rows[index].items() if k != 'elapsed_ns'}
                         if index < len(rows) else None)
    if pair['herdr'] != pair['gtx']:
        differences.append({'index': index, **pair})
result = {'status': 'FAIL' if differences else 'PASS',
          'scope': 'captured API responses only; image display and unrecorded cases are not proved',
          'record_counts': {k: len(v) for k, v in records.items()}, 'differences': differences}
(a.out/'api-comparison.json').write_text(json.dumps(result, indent=2)+'\n')
print(json.dumps(result))
raise SystemExit(1 if differences else 0)
