#!/usr/bin/env python3
"""Compare agent states in full, permitting only independently proven animation phase."""
import json
from pathlib import Path
import sys

root, clock = map(Path, sys.argv[1:])
proof = json.loads((clock / 'agent-clock-gate.json').read_text())
assert all(not row['differences'] for row in proof['comparisons'])
cycles = [{t['glyph'] for row in proof['proof'][who] for t in row['transitions']}
          for who in ('herdr', 'gtx')]
assert cycles[0] == cycles[1] and cycles[0]
for h, g in zip(proof['proof']['herdr'], proof['proof']['gtx']):
    assert (h['x'], h['y']) == (g['x'], g['y'])
    assert min(h['intervals_ms']) <= g['median_ms'] <= max(h['intervals_ms'])
    assert min(g['intervals_ms']) <= h['median_ms'] <= max(g['intervals_ms'])
results = []
for who in ('herdr', 'gtx'):
    runtime = json.loads((root / who / 'runtime-json.json').read_text())
    reports = [row for row in json.loads(runtime['home/parity-api-actions.json'])
               if row['method'] == 'pane.report_agent']
    assert [row['params']['state'] for row in reports] == ['idle', 'blocked', 'unknown', 'working']
    assert all('error' not in row['response'] for row in reports)
    assert json.loads((root / who / 'cleanup.json').read_text())['exit'] == 0
for mode in ('desktop', 'mobile'):
    for state in ('idle', 'blocked', 'unknown', 'working'):
        name = mode + '-' + state
        h, g = [json.loads((root / who / (name + '.json')).read_text())
                for who in ('herdr', 'gtx')]
        assert (h['Rows'], h['Cols']) == (g['Rows'], g['Cols'])
        if state == 'working':
            positions = [{(x, y) for y, row in enumerate(frame['Cells'])
                          for x, cell in enumerate(row) if cell['Content'] in cycles[0]}
                         for frame in (h, g)]
            assert positions[0] and positions[0] == positions[1], 'working positions differ'
        differences, phases = [], []
        for y in range(h['Rows']):
            for x in range(h['Cols']):
                for field in ('Content', 'Width', 'Style'):
                    hv, gv = h['Cells'][y][x][field], g['Cells'][y][x][field]
                    if hv == gv:
                        continue
                    if state == 'working' and field == 'Content' and hv in cycles[0] and gv in cycles[1]:
                        phases.append({'x': x, 'y': y, 'herdr': hv, 'gtx': gv})
                    else:
                        differences.append({'x': x, 'y': y, 'field': field, 'herdr': hv, 'gtx': gv})
        results.append({'screen': name, 'differences': differences, 'proven_phase': phases})
        print(name, 'PASS' if not differences else 'FAIL', len(differences))
(root / 'agent-states-gate.json').write_text(json.dumps({'clock': str(clock), 'screens': results}, indent=2) + '\n')
assert all(not row['differences'] for row in results)
