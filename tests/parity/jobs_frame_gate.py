#!/usr/bin/env python3
"""Compare full frames except job ages already proved from each real log's timestamps."""
import json
from pathlib import Path
import sys

root = Path(sys.argv[1])
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
    for y in range(frames[0]['Rows']):
        for x in range(frames[0]['Cols']):
            h,g = frames[0]['Cells'][y][x],frames[1]['Cells'][y][x]
            for key in h:
                if key == 'Content' and (x,y) in age_cells:
                    continue
                if h[key]!=g[key]: differences.append({'x':x,'y':y,'field':key,'herdr':h[key],'gtx':g[key]})
    results.append({'screen':name,'differences':differences,'timestamp_proven_age_cells':sorted(age_cells)})
    print(name, 'PASS' if not differences else 'DIFFERENT',len(differences))
(root/'jobs-frame-gate.json').write_text(json.dumps(results,ensure_ascii=False,indent=2)+'\n')
assert all(not p['differences'] for p in results), 'job frames differ beyond independently proven elapsed ages'
