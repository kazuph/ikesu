#!/usr/bin/env python3
"""Prove real working animation cycles and compare every other captured cell."""
import json
from pathlib import Path
import statistics
import sys

root = Path(sys.argv[1])
cycle = list('⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏')
proof = {}
frames = {}
animated = {}
for who in ('herdr', 'gtx'):
    case = root / who
    runtime = json.loads((case / 'runtime-json.json').read_text())
    actions = json.loads(runtime['home/parity-api-actions.json'])
    reports = [a for a in actions if a['method'] == 'pane.report_agent']
    assert [a['params']['state'] for a in reports] == ['working', 'blocked', 'idle']
    assert all('error' not in a['response'] for a in reports), 'real agent report failed'
    cleanup = json.loads((case / 'cleanup.json').read_text())
    assert cleanup['exit'] == 0
    frames[who] = {p.stem: json.loads(p.read_text()) for p in sorted(case.glob('working-*.json'))}
    assert frames[who]
    first = next(iter(frames[who].values()))
    positions = {(x, y) for y, row in enumerate(first['Cells'])
                 for x, c in enumerate(row) if c['Content'] in cycle}
    assert positions, 'working glyph not observed'
    animated[who] = positions
    observed = []
    for x, y in sorted(positions):
        transitions = []
        for name, frame in frames[who].items():
            glyph = frame['Cells'][y][x]['Content']
            assert glyph in cycle, (who, name, x, y, glyph)
            if not transitions or transitions[-1]['glyph'] != glyph:
                transitions.append({'glyph': glyph, 'time': frame['CellChangedUnixMs'][y][x]})
        assert set(t['glyph'] for t in transitions) == set(cycle)
        for before, after in zip(transitions, transitions[1:]):
            assert cycle[(cycle.index(before['glyph']) + 1) % len(cycle)] == after['glyph'], 'animation cycle changed or capture missed a frame'
        intervals = [b['time'] - a['time'] for a, b in zip(transitions, transitions[1:])]
        assert all(i > 0 for i in intervals)
        observed.append({'x': x, 'y': y, 'transitions': transitions, 'intervals_ms': intervals,
                         'median_ms': statistics.median(intervals)})
    proof[who] = observed
assert animated['herdr'] == animated['gtx'], 'working glyph positions differ'
assert frames['herdr'].keys() == frames['gtx'].keys()
comparisons = []
for name, h in frames['herdr'].items():
    g = frames['gtx'][name]
    assert (h['Cols'], h['Rows']) == (g['Cols'], g['Rows'])
    differences = []
    for y in range(h['Rows']):
        for x in range(h['Cols']):
            for field in ('Content', 'Width', 'Style'):
                if field == 'Content' and (x, y) in animated['herdr']:
                    continue
                if h['Cells'][y][x][field] != g['Cells'][y][x][field]:
                    differences.append({'x': x, 'y': y, 'field': field,
                                        'herdr': h['Cells'][y][x][field], 'gtx': g['Cells'][y][x][field]})
    comparisons.append({'screen': name, 'differences': differences})
for h, g in zip(proof['herdr'], proof['gtx']):
    assert min(h['intervals_ms']) <= g['median_ms'] <= max(h['intervals_ms']), 'GTX median outside observed herdr intervals'
    assert min(g['intervals_ms']) <= h['median_ms'] <= max(g['intervals_ms']), 'herdr median outside observed GTX intervals'
(root / 'agent-clock-gate.json').write_text(json.dumps({'proof': proof, 'comparisons': comparisons}, indent=2) + '\n')
assert all(not c['differences'] for c in comparisons), 'frame differences outside proven animation phase'
print('working cycles/timing/positions/styles/all other cells: PASS')
