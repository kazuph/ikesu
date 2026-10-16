#!/usr/bin/env python3
"""Measure copy evidence without treating missing captures or crashes as parity."""
import argparse
import base64
import json
import math
import re
from pathlib import Path

p=argparse.ArgumentParser()
p.add_argument('directory',type=Path)
a=p.parse_args()
root=a.directory
results=[]
for case in sorted(root.iterdir()):
    if not case.is_dir() or not (case/'herdr').is_dir():
        continue
    captures={}
    for who in ['herdr','gtx']:
        rows={}
        for path in sorted((case/who).glob('*.json')):
            doc=json.loads(path.read_text())
            if not isinstance(doc,dict) or 'Cells' not in doc:
                continue
            text='\n'.join(''.join(c['Content'] for c in row) for row in doc['Cells'])
            labels=re.findall(r'ROW-(\d+)',text)
            hint=next((line.strip() for line in text.splitlines() if 'h/j/k/l' in line or 'enter search' in line),'')
            rows[path.stem]={'first_row':int(labels[0]) if labels else None,'hint':hint,
                             'captured_ms':doc['CapturedUnixMs'],'dimensions':[doc['Cols'],doc['Rows']]}
        raw=(case/who/'terminal.raw').read_bytes()
        copies=[base64.b64decode(x).decode() for x in re.findall(rb'\x1b\]52;[^;]*;([^\x07\x1b]*)',raw)]
        captures[who]={'screens':rows,'osc52':copies}
    comparisons=[]
    steps=(case/'herdr'/'steps').read_text()
    expected=re.findall(r'^cells .*/([^/]+)\.json$',steps,re.M)
    for name in expected:
        hp,gp=[case/who/(name+'.json') for who in ['herdr','gtx']]
        if not hp.exists() or not gp.exists():
            comparisons.append({'screen':name,'status':'NOT_RUN'})
            continue
        h,g=[json.loads(path.read_text()) for path in [hp,gp]]
        if any(not isinstance(doc,dict) or 'Cells' not in doc for doc in [h,g]):
            comparisons.append({'screen':name,'status':'INVALID_CAPTURE'})
            continue
        htext='\n'.join(''.join(c['Content'] for c in row) for row in h['Cells'])
        if 'panicked' in htext:
            comparisons.append({'screen':name,'status':'REFERENCE_CRASH'})
            continue
        if [h['Cols'],h['Rows']] != [g['Cols'],g['Rows']]:
            comparisons.append({'screen':hp.stem,'status':'DIMENSION_MISMATCH'})
            continue
        # Pane body and copy hint; exclude the tab/status row and pane title.
        # A collapsed-sidebar procedure uses the entire pane width.
        x0=0 if case.name=='k29-folded' else 26
        differences=[]
        for y in range(2,h['Rows']-1):
            for x in range(x0,h['Cols']):
                for field in ['Content','Width','Style']:
                    if h['Cells'][y][x][field] != g['Cells'][y][x][field]:
                        differences.append([x,y,field])
        comparisons.append({'screen':hp.stem,'status':'PASS' if not differences else 'DIFFERENT_CELLS',
                            'difference_count':len(differences),'differences':differences})
    timing=None
    if case.name=='k28-timing':
        source=Path(__file__).resolve().parents[2]/'internal'/'tui'
        period=int(re.search(r'selectionScrollInterval = (\d+) \* time.Millisecond',(source/'selection_scroll.go').read_text()).group(1))
        tick=int(re.search(r'tickInterval\s*= (\d+) \* time.Millisecond',(source/'tui.go').read_text()).group(1))
        samples={}
        for who in ['herdr','gtx']:
            screens=captures[who]['screens']
            first,last=screens.get('tick-0'),screens.get('tick-5')
            if first and last and first['first_row'] is not None and last['first_row'] is not None:
                samples[who]={'rows':first['first_row']-last['first_row'],
                              'elapsed_ms':last['captured_ms']-first['captured_ms']}
        if len(samples)==2:
            h,g=samples['herdr'],samples['gtx']
            residual=(g['rows']-g['elapsed_ms']/period)-(h['rows']-h['elapsed_ms']/period)
            # One reference row phase plus the candidate's 50 ms display tick.
            phase_bound=1+math.ceil(tick/period)
            timing={'samples':samples,'period_ms':period,'viewer_tick_ms':tick,
                    'phase_bound_rows':phase_bound,'residual_rows':residual,
                    'status':'PASS' if abs(residual)<=phase_bound else 'FAIL'}
        else:
            timing={'status':'NOT_RUN'}
    results.append({'timing':timing,'case':case.name,'observations':captures,'comparisons':comparisons,
                    'osc52_equal':captures['herdr']['osc52']==captures['gtx']['osc52']})
(root/'measurements.json').write_text(json.dumps(results,ensure_ascii=False,indent=2)+'\n')
for row in results:
    print(row['case'],[(c['screen'],c['status'],c.get('difference_count')) for c in row['comparisons']], 'osc52_equal='+str(row['osc52_equal']))
