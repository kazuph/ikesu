#!/usr/bin/env python3
"""Interleave whole trials; never turn absent measurements into a pass."""
import argparse,itertools,json,os,subprocess
from pathlib import Path
p=argparse.ArgumentParser();p.add_argument('--base',required=True);p.add_argument('--out',required=True);p.add_argument('--group',choices=['core','extended','smoke'],required=True);p.add_argument('--repeats',type=int,default=3);p.add_argument('--candidate',required=True);a=p.parse_args();base=Path(a.base).resolve();harness=Path(os.environ.get('DRAG_HARNESS',str(base/'source'))).resolve();root=Path(a.out).resolve();root.mkdir(parents=True,exist_ok=True)
sizes=[(106,38),(160,45),(250,70)];kinds=['vertical','horizontal','left','right'];modes=['light','heavy','alt'];rates=[4,60,120]
cases=[]
if a.group=='core':
 for (cols,rows),kind,mode,rate in itertools.product(sizes,kinds,modes,rates):cases.append(dict(cols=cols,rows=rows,kind=kind,mode=mode,rate=rate,panes=2))
elif a.group=='smoke':
 for kind,mode in [('vertical','heavy'),('horizontal','heavy'),('left','light'),('right','alt')]:cases.append(dict(cols=106,rows=38,kind=kind,mode=mode,rate=60,panes=2))
else:
 # Each dimension is varied alone, followed by a combined stress case. The
 # core group already covers every operation, size, mode and input rate.
 variants=[dict(panes=3),dict(panes=4),dict(invisible=True),dict(history_rows=10000),dict(mode='alt',history_rows=10000),dict(no_session=True),dict(mode='image'),dict(mode='image-update'),dict(path='long'),dict(path='limit'),dict(release_different=True),dict(panes=4,invisible=True,history_rows=10000,mode='image-update',path='long')]
 for variant,kind,rate in itertools.product(variants,kinds,rates):
  c=dict(cols=160,rows=45,kind=kind,mode='heavy',rate=rate,panes=2);c.update(variant);cases.append(c)
for i,c in enumerate(cases):
 c.update(count=200,warmup=10)
 if c.get('path') in ['long','limit']:
  # Pane split ratios are clamped to 0.1..0.9 by the product contract. Leave
  # one cell before that limit for long travel; the limit case reaches it.
  if c['kind']=='horizontal':extent=int((c['rows']-2)*.4)
  elif c['kind']=='vertical':extent=int((c['cols']-26)*.4)
  else:extent=min(72-26,c['cols']-26-40)
  c['extent']=extent-(1 if c['path']=='long' else 0)
 else:c['extent']=1 if c['rate']==4 else 3
 c['name']=f'{i:03d}-{c["cols"]}x{c["rows"]}-{c["kind"]}-{c["mode"]}-{c["rate"]}hz'
(root/'conditions.json').write_text(json.dumps(cases,indent=2)+'\n')
for c in cases:
 scenario=root/(c['name']+'.json');scenario.write_text(json.dumps(c))
 for repeat in range(a.repeats):
  for product in ['ikesu','herdr']:
   out=root/f'{c["name"]}-r{repeat+1}-{product}'
   if (out/'analysis.json').exists():
    # Resumption only reuses complete immutable observations. Their binary
    # hash and scenario must match; no running/partial directory is reused.
    old=json.loads((out/'analysis.json').read_text())
    import hashlib
    binary=Path(a.candidate) if product=='ikesu' else Path.home()/'.local/share/herdr-builds/0.2.66/herdr'
    if old['metadata']['binary_sha256']!=hashlib.sha256(binary.read_bytes()).hexdigest() or old['metadata']['scenario']!=c:raise RuntimeError('resume input mismatch')
    continue
   binary=Path(a.candidate) if product=='ikesu' else Path.home()/'.local/share/herdr-builds/0.2.66/herdr'
   subprocess.run(['python3',str(harness/'tests/dragresize/run_v2.py'),'--base',str(base),'--binary',str(binary),'--product',product,'--scenario',str(scenario),'--out',str(out)],check=True)
   subprocess.run(['python3',str(harness/'tests/dragresize/analyze_v2.py'),str(out)],check=True,stdout=subprocess.DEVNULL)
   with (root/'completed.jsonl').open('a') as f:f.write(json.dumps(dict(condition=c['name'],repeat=repeat+1,product=product,analysis=str(out/'analysis.json')))+'\n')
