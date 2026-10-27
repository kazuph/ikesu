#!/usr/bin/env python3
"""Compare all notification cells; retain only the parent-approved seen exception."""
import json
from pathlib import Path
import sys

root = Path(sys.argv[1])
finished = sys.argv[2] == 'finished'
frames = {who: {name: json.loads((root/who/(name+'.json')).read_text())
                for name in ('notification','pending','target','input')}
          for who in ('herdr','ikesu')}
results = []
for name in frames['herdr']:
    h, g = frames['herdr'][name], frames['ikesu'][name]
    assert (h['Cols'], h['Rows']) == (g['Cols'], g['Rows'])
    exceptions = {}
    if finished and name in ('target', 'input'):
        before = frames['herdr']['notification']
        for y, row in enumerate(h['Cells']):
            text = ''.join(c['Content'] for c in row)
            if text.split('│', 1)[0].strip() in ('▌● 2 other', '● other'):
                x = next(i for i,c in enumerate(row) if c['Content'] == '●')
                hc, gc = row[x], g['Cells'][y][x]
                assert hc['Content'] == '●' and gc['Content'] == '○'
                assert before['Cells'][y][x]['Content'] == '●'
                assert hc['Style']['Fg'] == before['Cells'][y][x]['Style']['Fg']
                owner_idle = before['Cells'][4][1]
                assert owner_idle['Content'] == '○'
                assert gc['Style']['Fg'] == owner_idle['Style']['Fg']
                hs, gs = dict(hc['Style']), dict(gc['Style'])
                del hs['Fg'], gs['Fg']
                assert hs == gs and hc['Width'] == gc['Width']
                exceptions[x,y] = 'parent decision: viewed ikesu pane clears unseen completion'
        assert len(exceptions) in (0, 2), 'only the workspace and agent seen exception is permitted'
    differences = []
    retained = []
    for y in range(h['Rows']):
        for x in range(h['Cols']):
            for field in ('Content', 'Width', 'Style'):
                hv, gv = h['Cells'][y][x][field], g['Cells'][y][x][field]
                if hv == gv:
                    continue
                difference = {'x':x,'y':y,'field':field,'herdr':hv,'ikesu':gv}
                if (x,y) in exceptions and field in ('Content','Style'):
                    difference['reason'] = exceptions[x,y]
                    retained.append(difference)
                else:
                    differences.append(difference)
    results.append({'screen':name,'differences':differences,'retained_spec_differences':retained})
    print(name, 'PASS' if not differences else 'DIFFERENT_CELLS', len(differences), 'SPEC retained', len(retained))
(root/'notification-frame-gate.json').write_text(json.dumps(results,ensure_ascii=False,indent=2)+'\n')
assert all(not row['differences'] for row in results)
