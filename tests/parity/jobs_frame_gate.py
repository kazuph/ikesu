#!/usr/bin/env python3
"""Compare full frames except job ages already proved from each real log's timestamps."""
import json
from pathlib import Path
import sys

root = Path(sys.argv[1])
animation = None
if len(sys.argv) > 2:
    clock = Path(sys.argv[2])
    reference = json.loads((root/'reference.json').read_text())
    clock_reference = json.loads((clock/'reference.json').read_text())
    for field in ('herdr_sha256', 'gtx_sha256', 'config'):
        assert reference[field] == clock_reference[field], ('different animation inputs', field)
    animation = json.loads((clock/'agent-clock-gate.json').read_text())
    assert animation['comparisons'] and all(not row['differences'] for row in animation['comparisons'])
    for h, g in zip(animation['proof']['herdr'], animation['proof']['gtx']):
        assert (h['x'], h['y']) == (g['x'], g['y'])
        assert min(h['intervals_ms']) <= g['median_ms'] <= max(h['intervals_ms'])
        assert min(g['intervals_ms']) <= h['median_ms'] <= max(g['intervals_ms'])
    for who in ('herdr', 'gtx'):
        runtime = json.loads((root/who/'runtime-json.json').read_text())
        reports = [r for r in json.loads(runtime['home/parity-api-actions.json']) if r['method'] == 'pane.report_agent']
        assert len(reports) == 1 and reports[0]['params']['state'] == 'working' and 'error' not in reports[0]['response']
proof = json.loads((root/'age-proof.json').read_text())
assert proof.keys() == {'herdr', 'gtx'}
assert [p['screen'] for p in proof['herdr']] == [p['screen'] for p in proof['gtx']]
results = []
for index, row in enumerate(proof['herdr']):
    name = row['screen']
    frames = [json.loads((root/who/(name+'.json')).read_text()) for who in ('herdr','gtx')]
    assert (frames[0]['Cols'], frames[0]['Rows']) == (frames[1]['Cols'], frames[1]['Rows'])
    age_cells = set()
    for who, frame in zip(('herdr','gtx'), frames):
        matches = [y for y, cells in enumerate(frame['Cells'])
                   if 'PARITY_' in ''.join(c['Content'] for c in cells[:25])]
        assert len(matches) == 1, 'missing or ambiguous job row'
        y = matches[0]
        elapsed = ''.join(c['Content'] for c in frame['Cells'][y][13:17]).strip()
        assert elapsed == proof[who][index]['elapsed'], 'age proof does not describe this frame'
        age_cells.update((x,y) for x in range(13,17))
    differences = []
    phases = []
    if animation is not None:
        for h, g in zip(animation['proof']['herdr'], animation['proof']['gtx']):
            x, y = h['x'], h['y']
            if y >= frames[0]['Rows'] or x >= frames[0]['Cols']:
                continue  # Resizing can remove the agents-panel row entirely.
            present = [frame['Cells'][y][x]['Content'] in {t['glyph'] for t in row['transitions']}
                       for frame, row in zip(frames, (h, g))]
            if not any(present):
                continue  # The jobs panel replaces the agents-panel spinner row.
            assert all(present), ('working glyph exists in only one frame', name, x, y)
            for frame, row in zip(frames, (h, g)):
                assert frame['Cells'][y][x]['Content'] in {t['glyph'] for t in row['transitions']}
            phases.append((x, y))
        assert phases, ('no independently proven working glyph in jobs frame', name)
    for y in range(frames[0]['Rows']):
        for x in range(frames[0]['Cols']):
            h,g = frames[0]['Cells'][y][x],frames[1]['Cells'][y][x]
            for key in h:
                if key == 'Content' and (x,y) in age_cells:
                    continue
                if key == 'Content' and (x,y) in phases:
                    continue
                if h[key]!=g[key]: differences.append({'x':x,'y':y,'field':key,'herdr':h[key],'gtx':g[key]})
    results.append({'screen':name,'differences':differences,'timestamp_proven_age_cells':sorted(age_cells), 'cycle_proven_phase_cells': phases})
    print(name, 'PASS' if not differences else 'DIFFERENT',len(differences))
(root/'jobs-frame-gate.json').write_text(json.dumps(results,ensure_ascii=False,indent=2)+'\n')
assert all(not p['differences'] for p in results), 'job frames differ beyond independently proven elapsed ages'
