#!/usr/bin/env python3
"""Compare an observed overlay box and every cell inside it."""
import argparse
import json
from pathlib import Path


def panel(data, title):
    grid = data['Cells']
    candidates = []
    for y, row in enumerate(grid[:-1]):
        for x, cell in enumerate(row):
            if cell['Content'] != '┌':
                continue
            right = next((i for i in range(x+1, len(row)) if row[i]['Content'] == '┐'), None)
            if right is None:
                continue
            header = ''.join(c['Content'] for c in grid[y+1][x+1:right])
            if not header.startswith(title):
                continue
            bottom = next((i for i in range(y+2, len(grid))
                           if grid[i][x]['Content'] == '└' and grid[i][right]['Content'] == '┘'), None)
            if bottom is not None:
                candidates.append((x, y, right-x+1, bottom-y+1))
    if len(candidates) != 1:
        return None
    return candidates[0]


def main():
    p = argparse.ArgumentParser()
    p.add_argument('directory')
    p.add_argument('--title', default=' / ', help='exact observed start of the panel header')
    p.add_argument('--result', default='navigator-comparison.json')
    a = p.parse_args()
    root = Path(a.directory)
    results = []
    for hp in sorted((root/'herdr').glob('*.json')):
        gp = root/'gtx'/hp.name
        h = json.loads(hp.read_text())
        if 'Cells' not in h:
            continue
        if not gp.exists():
            results.append({'screen':hp.name, 'status':'NOT_RUN'})
            continue
        g = json.loads(gp.read_text())
        hr, gr = panel(h, a.title), panel(g, a.title)
        result = {'screen':hp.name, 'herdr_rect':hr, 'gtx_rect':gr}
        if hr is None or gr is None:
            result['status'] = 'NOT_OBSERVED'
        elif hr != gr:
            result['status'] = 'DIFFERENT_GEOMETRY'
        else:
            x, y, w, height = hr
            differences = []
            for yy in range(y, y+height):
                for xx in range(x, x+w):
                    hc, gc = h['Cells'][yy][xx], g['Cells'][yy][xx]
                    for field in ('Content','Width','Style'):
                        if hc[field] != gc[field]:
                            differences.append({'x':xx, 'y':yy, 'field':field, 'herdr':hc[field], 'gtx':gc[field]})
            result.update(status='PASS' if not differences else 'DIFFERENT_CELLS',
                          difference_count=len(differences), differences=differences[:100])
        results.append(result)
    (root/a.result).write_text(json.dumps(results, ensure_ascii=False, indent=2)+'\n')
    for r in results:
        print(r['screen'], r['status'], r.get('difference_count',''))


if __name__ == "__main__":
    main()
