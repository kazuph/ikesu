#!/usr/bin/env python3
"""Check all required saved measurements, including restoration and cleanup."""
import json,re,sys
from pathlib import Path
b=Path(sys.argv[1]); rows=json.loads((b/'summary.json').read_text())
expected={f'{p}-{m}-{c}' for p in ['ikesu','herdr'] for m in ['light','heavy'] for c in ['vertical','horizontal','left','right'] if not(p=='herdr' and c=='right')}
expected|={p+'-heavy-alt-vertical' for p in ['ikesu','herdr']}
assert {r['case'] for r in rows}==expected
results=[]
def corners(path):
 return [(x,y,c) for y,line in enumerate(path.read_text().splitlines()) for x,c in enumerate(line) if c in '┌┐└┘']
for r in rows:
 p=b/r['case'];assert r['n']==r['observed']==40,r['case']
 assert r['clear_total']==0
 assert all(s['latency_ms'] is not None and s['latency_ms']>=-1 and s['coverage']<7200 for s in r['samples'])
 wanted=80 if r['case'].endswith(('vertical','horizontal')) else 40
 assert sum(pane['total'] for pane in r['resize'])==wanted
 assert all(sum(pane['per_motion'][i] for pane in r['resize'])==wanted//40 for i in range(40))
 assert len(r['cpu_client_server'])==2
 assert json.loads((p/'cleanup.json').read_text())['exit']==0
 assert corners(p/'before.txt')==corners(p/'after.txt'),r['case']
 assert ('0.4.4' if r['case'].startswith('ikesu') else '0.2.66') in (p/'version.txt').read_text()
 results.append({'case':r['case'],'observations':40,'resize_signals':wanted,'restored_corners':True,'server_exit':0})
profiles=list(b.glob('*-profile/*.cpu'));assert len(profiles)==10
for p in profiles:
 assert p.stat().st_size>0 and 'Total samples' in p.with_suffix('.cum.txt').read_text()
 assert json.loads((p.parent/'cleanup.json').read_text())['exit']==0
out={'cases':results,'observations_total':len(results)*40,'profiles':len(profiles),'pass':True}
(b/'audit.json').write_text(json.dumps(out,indent=2)+'\n');print(json.dumps(out))
