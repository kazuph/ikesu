#!/usr/bin/env python3
"""Compare explicit measured cell regions without normalizing text or styles."""
import argparse
import json
from pathlib import Path
p = argparse.ArgumentParser()
p.add_argument('directory')
p.add_argument('--rect', required=True, help='x,y,width,height in zero-based terminal cells')
p.add_argument('--screens', nargs='+', required=True)
p.add_argument('--result', default='region-comparison.json')
a = p.parse_args()
x, y, width, height = map(int, a.rect.split(','))
root = Path(a.directory)
results = []
for name in a.screens:
    pair = [root/who/(name+'.json') for who in ('herdr', 'gtx')]
    if not all(path.is_file() for path in pair):
        results.append({'screen':name, 'status':'NOT_RUN'})
        continue
    h, g = [json.loads(path.read_text()) for path in pair]
    if any(x+width>d['Cols'] or y+height>d['Rows'] for d in (h,g)):
        results.append({'screen':name, 'status':'INVALID_REGION'})
        continue
    differences = []
    for yy in range(y,y+height):
        for xx in range(x,x+width):
            for field in ('Content','Width','Style'):
                hc,gc=h['Cells'][yy][xx][field],g['Cells'][yy][xx][field]
                if hc != gc:
                    differences.append({'x':xx,'y':yy,'field':field,'herdr':hc,'gtx':gc})
    results.append({'screen':name,'rect':[x,y,width,height],
                    'status':'PASS' if not differences else 'DIFFERENT_CELLS',
                    'difference_count':len(differences),'differences':differences})
(root/a.result).write_text(json.dumps(results,ensure_ascii=False,indent=2)+'\n')
for result in results:
    print(result['screen'],result['status'],result.get('difference_count',''))
