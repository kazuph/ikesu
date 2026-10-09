#!/usr/bin/env python3
"""Keep full menu differences and compare outside the explicitly excluded item boxes."""
import json
from pathlib import Path
import re
import sys

root, steps_root = map(Path, sys.argv[1:])

def boxes(frame):
    cells = frame['Cells']
    found = []
    for y, row in enumerate(cells):
        for x, cell in enumerate(row):
            if cell['Content'] != '┌':
                continue
            rights = [xx for xx in range(x+1, frame['Cols']) if row[xx]['Content'] == '┐']
            for right in rights:
                if not all(c['Content'] == '─' for c in row[x+1:right]):
                    continue
                bottoms = [yy for yy in range(y+1, frame['Rows'])
                           if cells[yy][x]['Content'] == '└' and cells[yy][right]['Content'] == '┘']
                for bottom in bottoms:
                    if all(c['Content'] == '─' for c in cells[bottom][x+1:right]) and all(
                            cells[yy][x]['Content'] == cells[yy][right]['Content'] == '│'
                            for yy in range(y+1, bottom)):
                        found.append({'rect': [x, y, right, bottom],
                                      'items': [''.join(c['Content'] for c in cells[yy][x+1:right]).strip()
                                                for yy in range(y+1, bottom)]})
                        break
                break
    return found

results = []
for steps in sorted(steps_root.glob('*.steps')):
    operation = steps.stem.rsplit('-', 1)[-1]
    names = re.findall(r'^cells @OUT@/([^\s]+)\.json$', steps.read_text(), re.M)
    for name in names:
        paths = [root / steps.stem / who / (name+'.json') for who in ('herdr', 'gtx')]
        result = {'case': steps.stem, 'screen': name}
        if not all(p.is_file() for p in paths):
            result['status'] = 'NOT_RUN'
            results.append(result)
            continue
        h, g = [json.loads(p.read_text()) for p in paths]
        assert (h['Rows'], h['Cols']) == (g['Rows'], g['Cols'])
        # Selection opens a dialog/help; closed frames have no item exception.
        menu_expected = name != 'after' or steps.stem.endswith(('outside-right', 'keyboard', 'wheel', 'resize'))
        hs, gs = (boxes(h), boxes(g)) if menu_expected else ([], [])
        assert len(hs) <= 1 and len(gs) <= 1, 'ambiguous item region'
        excluded = [b['rect'] for b in hs+gs]
        diffs, outside = [], []
        for y in range(h['Rows']):
            for x in range(h['Cols']):
                for field in ('Content', 'Width', 'Style'):
                    hv, gv = h['Cells'][y][x][field], g['Cells'][y][x][field]
                    if hv != gv:
                        d = {'x':x, 'y':y, 'field':field, 'herdr':hv, 'gtx':gv}
                        diffs.append(d)
                        if not any(left <= x <= right and top <= y <= bottom
                                   for left, top, right, bottom in excluded):
                            outside.append(d)
        result.update(status='OUTSIDE_DIFF' if outside else 'ITEM_REGION_DIFF_RECORDED' if diffs else 'PASS',
                      herdr_menu=hs, gtx_menu=gs, differences=diffs, outside_differences=outside)
        if steps.stem == 'main-outside-right' and name == 'after':
            # Retention is checked against each product's own pre-click menu,
            # so differing menu items cannot conceal a change of menu kind.
            retained = {}
            for who, data in (('herdr', h), ('gtx', g)):
                before = json.loads((root / steps.stem / who / 'open.json').read_text())
                retained[who] = before['Cells'] == data['Cells']
            result['main_menu_retained'] = retained
            if not all(retained.values()):
                result['status'] = 'MENU_RETENTION_DIFF'
        target = root / steps.stem / (name+'-matrix-comparison.json')
        target.write_text(json.dumps(result, ensure_ascii=False, indent=2)+'\n')
        results.append({k:v for k,v in result.items() if k not in ('differences','outside_differences')}
                        | {'difference_count':len(diffs),'outside_difference_count':len(outside)})
        print(steps.stem, name, result['status'], len(diffs), len(outside))
(root / 'menu-matrix-comparison.json').write_text(json.dumps(results,ensure_ascii=False,indent=2)+'\n')
# This is an evidence collector; ITEM_REGION_DIFF_RECORDED is not full-frame equality.
