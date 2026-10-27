#!/usr/bin/env python3
"""Prove real jobs' labels, one-row wheel movement, and resize persistence."""
import json
from pathlib import Path
import re
import sys

root = Path(sys.argv[1])
proof = {}
for who in ('herdr', 'ikesu'):
    case = root / who
    runtime = json.loads((case / 'runtime-json.json').read_text())
    actions = json.loads(runtime['home/parity-api-actions.json'])
    starts = [a for a in actions if a['method'] == 'run.start']
    assert starts and all('error' not in a['response'] for a in starts)
    labels = [a['params']['label'] for a in reversed(starts)]
    assert len(set(a['response']['result']['job'] for a in starts)) == len(starts)
    screens = {}
    for name, offset in [('jobs-overflow', 0), ('jobs-down', 1), ('jobs-up', 0),
                         ('jobs-small', 0), ('jobs-small-down', 1), ('jobs-returned', 1)]:
        frame = json.loads((case / (name + '.json')).read_text())
        rows = []
        for y, row in enumerate(frame['Cells']):
            line = ''.join(c['Content'] for c in row[:25])
            match = re.search(r'ROW_\d+', line)
            if match:
                rows.append({'y': y, 'label': match[0]})
        assert rows and len(rows) < len(labels), 'fixture did not overflow'
        assert [r['label'] for r in rows] == labels[offset:offset+len(rows)], (who, name, rows)
        assert rows[-1]['y'] == frame['Rows']-2, 'job rows did not fill the viewport'
        assert all(b['y'] == a['y']+1 for a, b in zip(rows, rows[1:]))
        screens[name] = rows
    assert json.loads((case / 'cleanup.json').read_text())['exit'] == 0
    proof[who] = screens
assert proof['herdr'] == proof['ikesu'], 'job order/positions differ'
(root / 'jobs-overflow-gate.json').write_text(json.dumps(proof, indent=2)+'\n')
print('real job labels/wheel one row/resize persistence: PASS')
