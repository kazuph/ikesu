#!/usr/bin/env python3
"""Interleave a focused implementation milestone with the approved main build."""
import argparse,json,os,statistics,subprocess
from pathlib import Path

p=argparse.ArgumentParser()
for name in ['base','harness','before','after','out']:p.add_argument('--'+name,required=True)
a=p.parse_args();base=Path(a.base).resolve();harness=Path(a.harness).resolve();out=Path(a.out).resolve();out.mkdir(parents=True,exist_ok=True)
cases=json.loads((base/'baseline-v2-core/conditions.json').read_text())
cases=[c for c in cases if c['mode']=='heavy' and ((c['kind']=='vertical' and c['rate']==4) or (c['cols']==106 and c['kind']=='vertical' and c['rate']==60) or (c['cols']==106 and c['kind']=='left' and c['rate']==4))]
env=dict(os.environ,DRAG_HARNESS=str(harness),DRAG_OBSERVER=str(base/'ptyrun-v2-frozen'))
rows=[]
for c in cases:
 scenario=out/(c['name']+'.json');scenario.write_text(json.dumps(c))
 values={key:[] for key in ['before','after']}
 for repeat in range(3):
  for key in ['before','after']:
   dest=out/f'{c["name"]}-r{repeat+1}-{key}'
   if dest.exists():raise RuntimeError('refuse to reuse milestone observations')
   subprocess.run(['python3',str(harness/'tests/dragresize/run_v2.py'),'--base',str(base),'--binary',getattr(a,key),'--product','ikesu','--scenario',str(scenario),'--out',str(dest)],env=env,check=True)
   subprocess.run(['python3',str(harness/'tests/dragresize/analyze_v2.py'),str(dest)],env=env,check=True,stdout=subprocess.DEVNULL)
   values[key].append(json.loads((dest/'analysis.json').read_text()))
 summary={}
 for key,items in values.items():
  summary[key]={}
  for metric in ['slow','application','update_interval_ms','tracking_lag_ms']:
   summary[key][metric]={q:statistics.median(v[metric][q] for v in items) if all(v[metric][q] is not None for v in items) else None for q in ['median','p95']}
  summary[key]['final_dimensions_verified']=all(v['final_dimensions_verified'] for v in items)
 rows.append(dict(condition=c,summary=summary))
 (out/'milestone.json').write_text(json.dumps(rows,indent=2)+'\n')
 print(c['name'],json.dumps(summary),flush=True)
